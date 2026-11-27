const startButton = document.getElementById("start-playback");
const skipButton = document.getElementById("skip-song");
const playbackStatus = document.getElementById("playback-status");
const memberList = createMemberList(document.getElementById("member-list"), async (member) => {
  if (!confirm(`Remove ${member.name} from the jam?`)) return;
  try {
    showGuests(await removeRoomMember(member.id));
  } catch (error) {
    playbackStatus.textContent = error.isTimeout
      ? `Removing ${member.name} timed out. Please try again.`
      : `Could not remove ${member.name}. Please try again.`;
  }
});

let code;
let roomId;
let latestQueue = [];
let refreshing = false;
let advancing = false;
let deciding = false;
let queueVersion = 0;
let offlineFailures = 0;

const hostPlayer = createHostPlayer({
  startButton,
  skipButton,
  statusEl: playbackStatus,
  titleEl: document.getElementById("song-title"),
  metaEl: document.getElementById("song-meta"),
  frameEl: document.getElementById("player-frame"),
  onFinishSong: advanceQueue,
  onSkipSong: skipCurrentSong,
});

const showSearchStatus = initSongSearch(async () => {
  queueVersion++;
  await refresh();
});

init();

function resolveJoinOrigin(serverOrigin) {
  const isLoopbackUrl = (url) =>
    !url || /^https?:\/\/(localhost|127\.0\.0\.1|\[::1\])(:\d+)?$/i.test(url);
  const browserIsLan = !/^(localhost|127\.0\.0\.1|\[::1\])$/i.test(location.hostname);
  if (isLoopbackUrl(serverOrigin) && browserIsLan) {
    return location.origin;
  }
  return serverOrigin || location.origin;
}

async function init() {
  const session = await loadSessionWithRetry(2);
  if (!session) {
    if (!location.pathname.endsWith("/offline.html")) {
      location.href = "/";
    }
    return;
  }
  if (!session.isHost) {
    location.href = "/";
    return;
  }

  code = session.roomCode || session.code;
  roomId = session.roomId || session.id || code;
  showGenre(session.genre);
  showRoomName(session.roomName);

  const joinOrigin = resolveJoinOrigin(session.joinOrigin);
  const joinTarget = session.roomId || session.id || code;
  const joinUrl = joinOrigin + "/join.html?code=" + encodeURIComponent(joinTarget);

  document.getElementById("join-url").textContent = joinUrl;

  const localhostWarning = document.getElementById("localhost-warning");
  if (localhostWarning) {
    const isLoopbackJoin = /^https?:\/\/(localhost|127\.0\.0\.1|\[::1\])(:\d+)?$/i.test(joinOrigin);
    if (isLoopbackJoin) {
      localhostWarning.hidden = false;
      localhostWarning.textContent =
        "Open this page using your computer's network IP (e.g. http://192.168.x.x:8000) or set HOST_IP in .env so phones can scan the QR code.";
    } else {
      localhostWarning.hidden = true;
    }
  }

  new QRCode(document.getElementById("qr-code"), { text: joinUrl, width: 260, height: 260 });

  hostPlayer.init();

  await refresh();
  setInterval(refresh, 3000);
}

async function refresh() {
  if (refreshing || advancing || deciding || hostPlayer.isAdvancing()) return;
  refreshing = true;
  const version = queueVersion;
  try {
    const response = await fetchWithTimeout("/api/room/requests", {}, DEFAULT_TIMEOUT_MS);
    offlineFailures = 0;
    setConnectionState(true);
    if (playbackStatus.textContent === "Could not refresh queue. Retrying...") {
      playbackStatus.textContent = "";
    }
    if (response.status === 401 || response.status === 404) {
      playbackStatus.textContent = "This jam is no longer available.";
      location.href = "/";
      return;
    }
    if (!response.ok) throw new Error("Could not load queue.");
    const state = await response.json();
    // Ignore a poll that started before the current song was removed.
    if (version !== queueVersion) return;
    updateQueue(state.queue ?? []);
    showGenre(state.genre);
    if (state.roomName) showRoomName(state.roomName);
    renderSongRequests(state.requests, decideRequest);
    showSearchStatus(state.requests);

    showGuests(await getRoomMembers(roomId || code));
  } catch (error) {
    if (error instanceof OfflineError) {
      offlineFailures++;
      setConnectionState(false, error.isTimeout ? "Request timed out — retrying…" : "Reconnecting…");
      if (offlineFailures >= 4) {
        redirectToOffline(error.isTimeout ? "timeout" : "offline");
        return;
      }
    } else {
      playbackStatus.textContent = "Could not refresh queue. Retrying...";
    }
  } finally {
    refreshing = false;
  }
}

async function decideRequest(requestId, decision) {
  deciding = true;
  queueVersion++;
  const version = queueVersion;
  try {
    const response = await fetchWithTimeout(
      "/api/room/requests/decision",
      {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ requestId, decision }),
      },
      ACTION_TIMEOUT_MS
    );
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
  hostPlayer.sync(latestQueue);
}

function showGuests(members) {
  const guests = members.filter((member) => !member.isHost);
  document.getElementById("guest-count").textContent = guests.length;
  memberList.show(guests);
}

async function advanceQueue(songId) {
  if (advancing || !songId) return false;
  advancing = true;
  queueVersion++;
  const version = queueVersion;
  let queue;
  try {
    const response = await fetchWithTimeout(
      "/api/room/queue",
      {
        method: "DELETE",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ songId }),
      },
      ACTION_TIMEOUT_MS
    );
    if (response.status === 409) {
      advancing = false;
      await refresh();
      return true;
    }
    if (!response.ok) throw new Error("Could not advance queue.");
    queue = (await response.json()) ?? [];
    playbackStatus.textContent = "";
  } catch (error) {
    playbackStatus.textContent = error.isTimeout
      ? "Advancing queue timed out. Retrying..."
      : "Could not advance queue. Retrying...";
    return false;
  } finally {
    advancing = false;
  }
  if (queue && version === queueVersion) updateQueue(queue);
  else if (queue) await refresh();
  return true;
}

async function skipCurrentSong(songId, reason = "host") {
  if (advancing || !songId) return false;
  advancing = true;
  queueVersion++;
  const version = queueVersion;
  let queue;
  try {
    const response = await fetchWithTimeout(
      "/api/room/skip",
      {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ songId, reason }),
      },
      ACTION_TIMEOUT_MS
    );
    if (response.status === 409) {
      advancing = false;
      await refresh();
      return true;
    }
    if (!response.ok) throw new Error("Could not skip song.");
    queue = (await response.json()) ?? [];
    if (reason === "host") {
      playbackStatus.textContent = "";
    }
  } catch (error) {
    playbackStatus.textContent = error.isTimeout
      ? "Skipping song timed out. Please try again."
      : "Could not skip song. Please try again.";
    return false;
  } finally {
    advancing = false;
  }
  if (queue && version === queueVersion) updateQueue(queue);
  else if (queue) await refresh();
  return true;
}

// Closing the room on the server is not implemented yet.
document.getElementById("end-session").addEventListener("click", () => {
  if (confirm("End the session?")) location.href = "/";
});
