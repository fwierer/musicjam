const startButton = document.getElementById("start-playback");

let code;
let player;
let started = false;
let currentSongId = "";

init();

async function init() {
  // The userId cookie from /api/room/create tells the server who the host is
  const response = await fetch("/api/session");
  const session = response.ok ? await response.json() : null;
  if (!session?.isHost) {
    location.href = "/";
    return;
  }

  code = session.roomCode;
  const joinUrl = location.origin + "/guest.html?code=" + code;
  document.getElementById("room-code").textContent = code;
  document.getElementById("join-url").textContent = joinUrl;
  new QRCode(document.getElementById("qr-code"), { text: joinUrl, width: 260, height: 260 });

  // Load the YouTube player API, then check for new songs every 3 seconds
  window.onYouTubeIframeAPIReady = () => {
    refresh();
    setInterval(refresh, 3000);
  };
  const script = document.createElement("script");
  script.src = "https://www.youtube.com/iframe_api";
  document.body.append(script);
}

async function refresh() {
  const queue = await (await fetch("/api/room/queue")).json();
  // No queue in Sprint 1: the song picked last is the one that plays
  const song = queue[queue.length - 1];
  if (song && song.id !== currentSongId) {
    currentSongId = song.id;
    playSong(song);
  }

  const members = await (await fetch("/api/room/getRoomMembers?code=" + code)).json();
  document.getElementById("guest-count").textContent = Object.keys(members).length - 1; // minus the host
}

function playSong(song) {
  document.getElementById("song-title").textContent = song.title;
  document.getElementById("song-meta").textContent = song.channel + " · picked by a guest";

  if (!player) {
    player = new YT.Player("player", { videoId: song.videoId, playerVars: { playsinline: 1 } });
    startButton.hidden = false;
  } else if (started) {
    player.loadVideoById(song.videoId);
  } else {
    player.cueVideoById(song.videoId);
  }
}

// Browsers block autoplay until the user has clicked once
startButton.addEventListener("click", () => {
  player.playVideo();
  started = true;
  startButton.hidden = true;
});

// The backend has no way to close a room yet, so this only takes the host back to the start page
document.getElementById("end-session").addEventListener("click", () => {
  if (confirm("End the session?")) {
    location.href = "/";
  }
});
