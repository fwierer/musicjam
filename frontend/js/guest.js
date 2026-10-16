// The page is opened as /guest.html?code=K7MX2P
const code = (new URLSearchParams(location.search).get("code") ?? "").toUpperCase();
document.getElementById("room-code").textContent = code;

let poll;

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
  const response = await fetch("/api/room/queue");
  if (!response.ok) {
    showEnded(); // the room is gone
    return;
  }
  const queue = await response.json();
  // No queue in Sprint 1: the song picked last is the one that plays
  const song = queue[queue.length - 1];
  if (song) {
    document.getElementById("now-thumb").src = `https://i.ytimg.com/vi/${song.videoId}/default.jpg`;
    document.getElementById("now-title").textContent = song.title;
    document.getElementById("now-meta").textContent = song.channel + " · picked by a guest";
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
      <button type="button" class="btn btn-outline-primary">Play</button>
    `;
    // textContent, not innerHTML, so song titles can't inject HTML
    row.querySelector("img").src = song.thumbnail;
    row.querySelector(".fw-medium").textContent = song.title;
    row.querySelector(".small").textContent = song.channel;
    row.querySelector("button").addEventListener("click", async () => {
      await fetch("/api/room/queue", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ videoId: song.videoId, title: song.title, channel: song.channel }),
      });
      refresh();
    });
    results.appendChild(row);
  }
});
