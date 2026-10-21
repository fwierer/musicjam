function initSongSearch(onAdded) {
  const form = document.getElementById("search-form");
  const input = document.getElementById("search-input");
  const status = document.getElementById("search-status");
  const results = document.getElementById("results");
  let searching = false;
  let adding = false;
  let lastRequestId = "";
  let lastRequests = [];
  function showRequests(requests) {
    lastRequests = requests;
    if (searching || adding || !lastRequestId) return;
    const request = requests.find((item) => item.id === lastRequestId);
    if (request) status.textContent = requestStatus(request) + ".";
  }
  form.addEventListener("submit", async (event) => {
    event.preventDefault();
    const query = input.value.trim();
    if (!query || searching) return;
    searching = true;
    lastRequestId = "";
    status.textContent = "Searching...";
    results.replaceChildren();
    try {
      const response = await fetch("/api/youtube/search?q=" + encodeURIComponent(query));
      if (!response.ok) throw new Error("Search failed");
      const songs = await response.json();
      status.textContent = songs.length ? "" : "No songs found.";
      for (const song of songs) {
        const row = document.createElement("div");
        row.className = "result";
        row.innerHTML = '<img alt=""><div class="result-text"><div class="fw-medium text-truncate"></div><div class="small text-body-secondary text-truncate"></div></div><button type="button" class="btn btn-outline-primary">Request song</button>';
        row.querySelector("img").src = song.thumbnail;
        row.querySelector(".fw-medium").textContent = song.title;
        row.querySelector(".small").textContent = song.channel;
        const button = row.querySelector("button");
        button.addEventListener("click", async () => {
          if (adding) return;
          adding = true;
          button.disabled = true;
          try {
            const added = await fetch("/api/room/requests", {
              method: "POST", headers: { "Content-Type": "application/json" },
              body: JSON.stringify(song),
            });
            if (!added.ok) throw new Error("Request failed");
            const request = await added.json();
            lastRequestId = request.id;
            status.textContent = requestStatus(request) + ".";
            await onAdded();
          } catch (error) {
            status.textContent = "Could not request song. Please try again.";
          } finally {
            adding = false;
            button.disabled = false;
            showRequests(lastRequests);
          }
        });
        results.append(row);
      }
    } catch (error) {
      status.textContent = "Could not search. Please try again.";
    } finally {
      searching = false;
    }
  });
  return showRequests;
}
