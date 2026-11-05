const rawCode = new URLSearchParams(location.search).get("code") ?? "";
const code = rawCode.length === 36 ? rawCode.toLowerCase() : rawCode.toUpperCase();
document.getElementById("room-code").textContent = code;

let poll;
let refreshing = false;
let queueVersion = 0;
let offlineFailures = 0;

const showSearchStatus = initSongSearch(async () => {
  queueVersion++;
  await refresh();
});
init();

async function init() {
  const session = await loadSessionWithRetry(2);
  if (!session) {
    if (!location.pathname.endsWith("/offline.html")) {
      location.href = "/?join=" + encodeURIComponent(code);
    }
    return;
  }
  const matchesRoom =
    session.roomCode === code ||
    session.roomId === code ||
    session.id === code;
  if (!matchesRoom) {
    location.href = "/?join=" + encodeURIComponent(code);
    return;
  }
  if (session.roomCode) {
    document.getElementById("room-code").textContent = session.roomCode;
  }
  showGenre(session.genre);
  showRoomName(session.roomName);
  await refresh();
  poll = setInterval(refresh, 3000);
}

async function refresh() {
  if (refreshing) return;
  refreshing = true;
  const version = queueVersion;
  const searchStatus = document.getElementById("search-status");
  try {
    const response = await fetchWithTimeout("/api/room/requests", {}, DEFAULT_TIMEOUT_MS);
    offlineFailures = 0;
    setConnectionState(true);
    if (searchStatus.textContent === "Could not refresh queue. Retrying...") {
      searchStatus.textContent = "";
    }
    if (response.status === 401 || response.status === 404) {
      showEnded();
      return;
    }
    if (!response.ok) throw new Error("Could not load queue.");
    const state = await response.json();
    if (version !== queueVersion) return;
    updateQueue(state.queue ?? []);
    showGenre(state.genre);
    if (state.roomName) showRoomName(state.roomName);
    renderSongRequests(state.requests);
    showSearchStatus(state.requests);
  } catch (error) {
    if (error instanceof OfflineError) {
      offlineFailures++;
      setConnectionState(false, error.isTimeout ? "Timed out…" : "Reconnecting…");
      if (offlineFailures >= 4) {
        clearInterval(poll);
        redirectToOffline(error.isTimeout ? "timeout" : "offline");
        return;
      }
    } else {
      searchStatus.textContent = "Could not refresh queue. Retrying...";
    }
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
