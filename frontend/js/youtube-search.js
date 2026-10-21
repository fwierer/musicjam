const searchForm = document.getElementById("song-search-form");
const searchInput = document.getElementById("song-search-input");
const resultsList = document.getElementById("search-results");
const playerFrame = document.getElementById("player-frame");
const queueList = document.getElementById("queue");
let currentSession = null;
let currentSongId = "";
let latestQueue = [];
let player = null;
let playerReady = false;
let youtubeApiReady = false;
let advancingQueue = false;
let endedSongId = "";
let playingSongId = "";
let queueRequests = Promise.resolve();

window.onYouTubeIframeAPIReady = () => {
  youtubeApiReady = true;
  loadFirstSongForHost(latestQueue);
};

searchForm.addEventListener("submit", async (event) => {
  event.preventDefault();

  const query = searchInput.value.trim();
  if (!query) {
    return;
  }

  resultsList.innerHTML = '<div class="list-group-item text-body-secondary">Searching...</div>';

  try {
    const response = await fetch(`/api/youtube/search?q=${encodeURIComponent(query)}`);
    if (!response.ok) {
      throw new Error("Search failed");
    }

    const results = await response.json();
    renderResults(results);
  } catch (error) {
    resultsList.innerHTML = '<div class="list-group-item text-danger">Could not load YouTube results.</div>';
  }
});

function renderResults(results) {
  if (!results.length) {
    resultsList.innerHTML = '<div class="list-group-item text-body-secondary">No songs found.</div>';
    return;
  }

  resultsList.innerHTML = "";

  for (const result of results) {
    const button = document.createElement("button");
    button.type = "button";
    button.className = "list-group-item list-group-item-action d-flex gap-3 align-items-center";
    button.innerHTML = `
      <img src="${result.thumbnail}" alt="" width="88" height="66" class="rounded object-fit-cover">
      <span class="text-start">
        <span class="d-block fw-semibold">${escapeHtml(result.title)}</span>
        <span class="d-block small text-body-secondary">${escapeHtml(result.channel)}</span>
      </span>
    `;
    button.addEventListener("click", () => addSongToQueue(result));
    resultsList.appendChild(button);
  }
}

async function addSongToQueue(song) {
  try {
    const response = await fetch("/api/room/requests", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(song),
    });
    if (!response.ok) throw new Error(`(${response.status})`);
    const request = await response.json();
    await loadQueue();
    const message = request.queued ? "Added to queue." : "Song request submitted for genre review.";
    resultsList.textContent = message;
  } catch (error) {
    resultsList.innerHTML = `<div class="list-group-item text-danger">Could not add song to queue. ${escapeHtml(error.message)}</div>`;
  }
}

async function loadQueue() {
  if (advancingQueue) {
    return;
  }
  try {
    await requestQueue();
  } catch (error) {
    console.error("Could not load queue:", error);
  }
}

function requestQueue(options = {}) {
  // Keep polling and queue changes in order so older responses cannot restore a finished song.
  const request = queueRequests.then(async () => {
    const response = await fetch("/api/room/queue", {
      credentials: "same-origin",
      ...options,
    });
    if (!response.ok) {
      throw new Error(`(${response.status})`);
    }
    latestQueue = (await response.json()) ?? [];
    renderQueue(latestQueue);
    loadFirstSongForHost(latestQueue);
  });
  queueRequests = request.catch(() => {});
  return request;
}

function renderQueue(queue) {
  if (!queue.length) {
    queueList.innerHTML = '<li class="list-group-item text-body-secondary">Nothing queued yet</li>';
    return;
  }

  queueList.innerHTML = "";

  queue.forEach((song, index) => {
    const item = document.createElement("li");
    item.className = "list-group-item";
    item.innerHTML = `
      <span class="fw-semibold text-body-secondary">${index + 1}.</span>
      <span class="fw-semibold">${escapeHtml(song.title)}</span>
      <span class="small text-body-secondary"> - ${escapeHtml(song.channel)}</span>
    `;
    queueList.appendChild(item);
  });
}

function loadFirstSongForHost(queue) {
  if (!currentSession?.isHost || !youtubeApiReady || advancingQueue) {
    return;
  }

  if (!queue.length) {
    if (player) {
      player.destroy();
      player = null;
      playerReady = false;
      playerFrame.innerHTML = '<div class="d-flex align-items-center justify-content-center text-white">Choose a song to start playback</div>';
    }
    currentSongId = "";
    endedSongId = "";
    playingSongId = "";
    return;
  }

  if (endedSongId === queue[0].id) {
    advanceQueue();
    return;
  }

  if (currentSongId === queue[0].id || (player && !playerReady)) {
    return;
  }

  loadVideo(queue[0]);
}

function loadVideo(song) {
  currentSongId = song.id;
  endedSongId = "";
  playingSongId = "";

  if (player) {
    player.loadVideoById(song.videoId);
    return;
  }

  playerFrame.innerHTML = `
    <iframe id="youtube-player"
      src="https://www.youtube.com/embed/${encodeURIComponent(song.videoId)}?enablejsapi=1&origin=${encodeURIComponent(location.origin)}&playsinline=1"
      title="YouTube video player"
      allow="accelerometer; autoplay; clipboard-write; encrypted-media; gyroscope; picture-in-picture"
      allowfullscreen>
    </iframe>
  `;

  player = new YT.Player("youtube-player", {
    events: {
      onReady: () => {
        playerReady = true;
        loadFirstSongForHost(latestQueue);
      },
      onStateChange: (event) => {
        if (event.data === YT.PlayerState.PLAYING) {
          playingSongId = currentSongId;
        }
        if (event.data === YT.PlayerState.ENDED && playingSongId === currentSongId && currentSongId) {
          playingSongId = "";
          endedSongId = currentSongId;
          advanceQueue();
        }
      },
    },
  });
}

async function advanceQueue() {
  if (!currentSession?.isHost || advancingQueue || !endedSongId) {
    return;
  }

  advancingQueue = true;
  let advanced = false;
  try {
    await requestQueue({
      method: "DELETE",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ songId: endedSongId }),
    });
    endedSongId = "";
    advanced = true;
  } catch (error) {
    resultsList.innerHTML = `<div class="list-group-item text-danger">Could not advance queue. ${escapeHtml(error.message)}</div>`;
  } finally {
    advancingQueue = false;
  }

  if (advanced) {
    loadFirstSongForHost(latestQueue);
  }
}

function escapeHtml(value) {
  const element = document.createElement("span");
  element.textContent = value;
  return element.innerHTML;
}

async function initQueue() {
  const response = await fetch("/api/session");
  if (response.ok) {
    currentSession = await response.json();
  }

  await loadQueue();
  setInterval(loadQueue, 3000);
}

initQueue();
