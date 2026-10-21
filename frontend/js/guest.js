const code = (new URLSearchParams(location.search).get("code") ?? "").toUpperCase();
document.getElementById("room-code").textContent = code;

let poll;
let refreshing = false;
let queueVersion = 0;

const showSearchStatus = initSongSearch(async () => {
  queueVersion++;
  await refresh();
});
init();

async function init() {
  const response = await fetch("/api/session");
  const session = response.ok ? await response.json() : null;
  if (session?.roomCode !== code) {
    location.href = "/?join=" + encodeURIComponent(code);
    return;
  }
  showGenre(session.genre);
  await refresh();
  poll = setInterval(refresh, 3000);
}

async function refresh() {
  if (refreshing) return;
  refreshing = true;
  const version = queueVersion;
  try {
    const response = await fetch("/api/room/requests");
    if (response.status === 401 || response.status === 404) {
      showEnded();
      return;
    }
    if (!response.ok) throw new Error("Could not load queue.");
    const state = await response.json();
    if (version !== queueVersion) return;
    updateQueue(state.queue ?? []);
    showGenre(state.genre);
    renderSongRequests(state.requests);
    showSearchStatus(state.requests);
  } catch (error) {
    document.getElementById("search-status").textContent = "Could not refresh queue. Retrying...";
  } finally {
    refreshing = false;
  }
}

function updateQueue(queue) {
  renderQueue(queue);
  const song = queue[0];
  if (song) {
    document.getElementById("now-thumb").src = `https://i.ytimg.com/vi/${song.videoId}/default.jpg`;
    document.getElementById("now-title").textContent = song.title;
    document.getElementById("now-meta").textContent = song.channel;
  } else {
    document.getElementById("now-thumb").removeAttribute("src");
    document.getElementById("now-title").textContent = "Nothing yet";
    document.getElementById("now-meta").textContent = "";
  }
}

function showEnded() {
  clearInterval(poll);
  document.getElementById("jam").hidden = true;
  document.getElementById("ended").hidden = false;
}
