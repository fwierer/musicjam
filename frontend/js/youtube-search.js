const searchForm = document.getElementById("song-search-form");
const searchInput = document.getElementById("song-search-input");
const resultsList = document.getElementById("search-results");
const playerFrame = document.getElementById("player-frame");

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
    button.addEventListener("click", () => loadVideo(result.videoId));
    resultsList.appendChild(button);
  }
}

function loadVideo(videoId) {
  playerFrame.innerHTML = `
    <iframe
      src="https://www.youtube.com/embed/${encodeURIComponent(videoId)}"
      title="YouTube video player"
      allow="accelerometer; autoplay; clipboard-write; encrypted-media; gyroscope; picture-in-picture"
      allowfullscreen>
    </iframe>
  `;
}

function escapeHtml(value) {
  const element = document.createElement("span");
  element.textContent = value;
  return element.innerHTML;
}
