function showGenre(genre) {
  document.getElementById("room-genre").textContent = genre === "Any" ? "All genres" : genre;
}

function requestStatus(request) {
  if (request.queued) return "Added to queue";
  return {
    pending: "Checking genre", review: "Host review required",
    block: "Rejected", allow: "Waiting for earlier requests",
  }[request.status] ?? "Unknown status";
}

function renderSongRequests(requests, onDecision) {
  const list = document.getElementById("song-requests");
  // Keep controls stable between polls so a click is not lost during a refresh.
  const visible = requests.filter((request) => !request.queued);
  const open = visible.filter((request) => request.status !== "block");
  const rejected = visible.filter((request) => request.status === "block");
  const shown = [...open, ...rejected];
  const signature = JSON.stringify(shown);
  if (list.dataset.requests === signature) return;
  list.dataset.requests = signature;
  list.replaceChildren();
  if (!shown.length) {
    const item = document.createElement("li");
    item.className = "text-body-secondary small";
    item.textContent = "No requests awaiting approval";
    list.append(item);
    return;
  }
  for (const request of shown) {
    const row = document.createElement("li");
    row.className = "song-request";
    const title = document.createElement("div");
    title.className = "fw-medium queue-title";
    title.textContent = request.song.title;
    const status = document.createElement("p");
    status.className = "small mb-1 request-status request-status-" + request.status;
    status.textContent = requestStatus(request);
    const reason = document.createElement("p");
    reason.className = "small text-body-secondary mb-0 queue-channel";
    reason.textContent = request.reason;
    const author = document.createElement("p");
    author.className = "small text-body-secondary mb-1";
    author.textContent = request.requestedBy;
    row.append(title, author, status, reason);
    if (onDecision && request.status !== "pending") {
      const actions = document.createElement("div");
      actions.className = "d-flex flex-wrap gap-2 mt-2";
      for (const [decision, label] of [["allow", "Approve"], ["block", "Reject"]]) {
        if (request.status === decision) continue;
        const button = document.createElement("button");
        button.type = "button";
        button.className = "btn btn-sm " + (decision === "allow" ? "btn-outline-primary" : "btn-outline-danger");
        button.textContent = label;
        button.addEventListener("click", async () => {
          const buttons = actions.querySelectorAll("button");
          buttons.forEach((control) => { control.disabled = true; });
          try {
            await onDecision(request.id, decision);
          } catch (error) {
            status.textContent = "Could not save decision. Please try again.";
          } finally {
            buttons.forEach((control) => { control.disabled = false; });
          }
        });
        actions.append(button);
      }
      row.append(actions);
    }
    list.append(row);
  }
}
