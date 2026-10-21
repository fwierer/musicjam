const startButton = document.getElementById("start-playback");
const playbackStatus = document.getElementById("playback-status");
const memberList = createMemberList(document.getElementById("member-list"), async (member) => {
  if (!confirm(`Remove ${member.name} from the jam?`)) return;
  try {
    showGuests(await removeRoomMember(member.id));
  } catch (error) {
    playbackStatus.textContent = `Could not remove ${member.name}. Please try again.`;
  }
});

let code;
let player;
let playerReady = false;
let apiReady = false;
let started = false;
let currentSongId = "";
let playingSongId = "";
let finishedSongId = "";
let latestQueue = [];
let refreshing = false;
let advancing = false;
let deciding = false;
let queueVersion = 0;

const showSearchStatus = initSongSearch(async () => {
  queueVersion++;
  await refresh();
});

init();

async function init() {
  const response = await fetch("/api/session");
  const session = response.ok ? await response.json() : null;
  if (!session?.isHost) {
    location.href = "/";
    return;
  }

  code = session.roomCode;
  showGenre(session.genre);
  // Guests land on the start page with the code filled in, since they need to enter a name
  const joinUrl = location.origin + "/?join=" + code;
  document.getElementById("room-code").textContent = code;
  document.getElementById("join-url").textContent = joinUrl;
  new QRCode(document.getElementById("qr-code"), { text: joinUrl, width: 260, height: 260 });

  window.onYouTubeIframeAPIReady = () => {
    apiReady = true;
    syncPlayer();
  };
  const script = document.createElement("script");
  script.src = "https://www.youtube.com/iframe_api";
  document.body.append(script);

  await refresh();
  setInterval(refresh, 3000);
}

async function refresh() {
  if (refreshing || advancing || deciding) return;
  refreshing = true;
  const version = queueVersion;
  try {
    const response = await fetch("/api/room/requests");
    if (!response.ok) throw new Error("Could not load queue.");
    const state = await response.json();
    // Ignore a poll that started before the current song was removed.
    if (version !== queueVersion) return;
    updateQueue(state.queue ?? []);
    showGenre(state.genre);
    renderSongRequests(state.requests, decideRequest);
    showSearchStatus(state.requests);

    showGuests(await getRoomMembers(code));
  } catch (error) {
    playbackStatus.textContent = "Could not refresh queue. Retrying...";
  } finally {
    refreshing = false;
  }
}

async function decideRequest(requestId, decision) {
  deciding = true;
  queueVersion++;
  const version = queueVersion;
  try {
    const response = await fetch("/api/room/requests/decision", {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ requestId, decision }),
    });
    if (!response.ok) throw new Error("Decision failed");
    const state = await response.json();
    if (version === queueVersion) {
      updateQueue(state.queue ?? []);
      renderSongRequests(state.requests, decideRequest);
      showSearchStatus(state.requests);
    }
  } finally {
    deciding = false;
  }
  await refresh();
}

function updateQueue(queue) {
  latestQueue = queue;
  renderQueue(queue);
  syncPlayer();
}

function syncPlayer() {
  if (advancing) return;
  const song = latestQueue[0];
  if (!song) {
    if (player) {
      player.destroy();
      player = null;
      playerReady = false;
      document.getElementById("player-frame").innerHTML =
        '<div id="player" class="d-flex align-items-center justify-content-center text-body-secondary">Waiting for a guest to pick a song</div>';
    }
    currentSongId = "";
    playingSongId = "";
    finishedSongId = "";
    startButton.hidden = true;
    document.getElementById("song-title").textContent = "Nothing yet";
    document.getElementById("song-meta").textContent = "";
    return;
  }
  if (finishedSongId === song.id) {
    advanceQueue();
    return;
  }
  if (!apiReady || song.id === currentSongId || (player && !playerReady)) return;
  currentSongId = song.id;
  playingSongId = "";
  finishedSongId = "";
  playbackStatus.textContent = "";
  playSong(song);
}

function showGuests(members) {
  const guests = members.filter((member) => !member.isHost);
  document.getElementById("guest-count").textContent = guests.length;
  memberList.show(guests);
}

function playSong(song) {
  document.getElementById("song-title").textContent = song.title;
  document.getElementById("song-meta").textContent = song.channel;

  if (!player) {
    startButton.disabled = true;
    player = new YT.Player("player", {
      videoId: song.videoId,
      playerVars: { playsinline: 1, origin: location.origin },
      events: {
        onReady: (event) => {
          if (event.target !== player) return;
          playerReady = true;
          startButton.disabled = false;
          startButton.hidden = started;
          syncPlayer();
          if (started && player) player.playVideo();
        },
        onStateChange: (event) => {
          if (event.target !== player || !currentSongId) return;
          if (event.data === YT.PlayerState.PLAYING) {
            started = true;
            playingSongId = currentSongId;
            startButton.hidden = true;
          }
          if (event.data === YT.PlayerState.ENDED && playingSongId === currentSongId) {
            playingSongId = "";
            finishedSongId = currentSongId;
            advanceQueue();
          }
        },
        onAutoplayBlocked: (event) => {
          if (event.target !== player) return;
          started = false;
          startButton.hidden = false;
        },
        onError: () => {
          playbackStatus.textContent = "This video could not be played.";
        },
      },
    });
  } else if (started) {
    player.loadVideoById(song.videoId);
  } else {
    player.cueVideoById(song.videoId);
  }
}

async function advanceQueue() {
  if (advancing || !finishedSongId) return;
  advancing = true;
  queueVersion++;
  const version = queueVersion;
  let queue;
  try {
    const response = await fetch("/api/room/queue", {
      method: "DELETE",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ songId: finishedSongId }),
    });
    if (!response.ok) throw new Error("Could not advance queue.");
    queue = (await response.json()) ?? [];
    finishedSongId = "";
    playbackStatus.textContent = "";
  } catch (error) {
    playbackStatus.textContent = "Could not advance queue. Retrying...";
  } finally {
    advancing = false;
  }
  if (queue && version === queueVersion) updateQueue(queue);
  else if (queue) await refresh();
}

startButton.addEventListener("click", () => {
  if (!playerReady || !currentSongId) return;
  started = true;
  player.playVideo();
});

// Closing the room on the server is not implemented yet.
document.getElementById("end-session").addEventListener("click", () => {
  if (confirm("End the session?")) location.href = "/";
});
