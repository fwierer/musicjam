// The page is opened as /guest.html?code=K7MX2P
const code = (new URLSearchParams(location.search).get("code") ?? "").toUpperCase();
document.getElementById("room-code").textContent = code;

let poll;
let refreshing = false;
let addingSong = false;
let queueVersion = 0;

join();

async function join() {
  // Joining sets the userId cookie that /api/room/queue needs
  const response = await fetch("/api/room/join?code=" + encodeURIComponent(code), { method: "POST" });
  if (!response.ok) {
    showEnded();
    return;
  }
  refresh();
  poll = setInterval(refresh, 3000);
}

async function refresh() {
  if (refreshing || addingSong) return;
  refreshing = true;
  const version = queueVersion;
  try {
    const response = await fetch("/api/room/queue");
    if (response.status === 401 || response.status === 404) {
      showEnded();
      return;
    }
    if (!response.ok) throw new Error("Could not load queue.");
    const queue = await response.json();
    if (version === queueVersion) updateQueue(queue ?? []);
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

// Search only on submit, not on every keystroke (YouTube API quota)
document.getElementById("search-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  const query = document.getElementById("search-input").value.trim();
  const status = document.getElementById("search-status");
  const results = document.getElementById("results");
  if (!query) {
    return;
  }

  status.textContent = "Searching…";
  results.innerHTML = "";

  const response = await fetch("/api/youtube/search?q=" + encodeURIComponent(query));
  if (!response.ok) {
    status.textContent = "Search failed. Please try again.";
    return;
  }
  const songs = await response.json();
  status.textContent = songs.length ? "" : "No songs found.";

  for (const song of songs) {
    const row = document.createElement("div");
    row.className = "result";
    row.innerHTML = `
      <img alt="">
      <div class="result-text">
        <div class="fw-medium text-truncate"></div>
        <div class="small text-body-secondary text-truncate"></div>
      </div>
      <button type="button" class="btn btn-outline-primary">Add to queue</button>
    `;
    // textContent, not innerHTML, so song titles can't inject HTML
    row.querySelector("img").src = song.thumbnail;
    row.querySelector(".fw-medium").textContent = song.title;
    row.querySelector(".small").textContent = song.channel;
    const button = row.querySelector("button");
    button.addEventListener("click", async () => {
      if (addingSong) return;
      addingSong = true;
      queueVersion++;
      button.disabled = true;
      try {
        const response = await fetch("/api/room/queue", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ videoId: song.videoId, title: song.title, channel: song.channel }),
        });
        if (!response.ok) throw new Error("Could not add song.");
        updateQueue((await response.json()) ?? []);
        status.textContent = "Added to queue.";
      } catch (error) {
        status.textContent = "Could not add song to queue. Please try again.";
      } finally {
        addingSong = false;
        button.disabled = false;
      }
    });
    results.appendChild(row);
  }
});
