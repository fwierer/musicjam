// Start page: host a jam or join one. Both need a name, the server keeps it in the session.
const nameInput = document.getElementById("user-name");
const roomNameInput = document.getElementById("room-name");
const genreInput = document.getElementById("genre-input");
const startJam = document.getElementById("start-jam");

try {
  const savedName = localStorage.getItem("musicjam.name");
  if (savedName && !nameInput.value) {
    nameInput.value = savedName;
  }
} catch {
  // Ignore localStorage errors in restricted/private browsing modes
}

startJam.disabled = true;
fetchWithTimeout("/api/genres", {}, DEFAULT_TIMEOUT_MS)
  .then(async (response) => {
    if (!response.ok) throw new Error("Could not load genres");
    for (const genre of await response.json()) {
      const option = document.createElement("option");
      option.value = genre;
      option.textContent = genre === "Any" ? "All genres" : genre;
      genreInput.append(option);
    }
    startJam.disabled = false;
  })
  .catch((error) => {
    if (error instanceof OfflineError) {
      redirectToOffline(error.isTimeout ? "timeout" : "offline");
      return;
    }
    showError("start-error", "Could not load genres. Please reload.");
  });

// If opened with /?join=UUID, redirect to the dedicated join page
const joinParam = new URLSearchParams(location.search).get("join");
if (joinParam) {
  location.replace("/join.html?code=" + encodeURIComponent(joinParam));
}

document.getElementById("start-jam").addEventListener("click", async () => {
  const name = readName();
  if (!name) {
    return;
  }

  const roomName = roomNameInput ? roomNameInput.value.trim() : "";
  startJam.disabled = true;
  try {
    const query = new URLSearchParams({
      name,
      genre: genreInput.value,
    });
    if (roomName) {
      query.set("roomName", roomName);
    }
    // The server remembers the host through the userId cookie it sets here
    const response = await fetchWithTimeout(
      "/api/room/create?" + query.toString(),
      { method: "POST" },
      ACTION_TIMEOUT_MS
    );
    if (!response.ok) {
      const msg = (await response.text()).trim();
      showError("start-error", msg || "Could not start a jam. Please try again.");
      return;
    }
    rememberName(name);
    location.href = "/host.html";
  } catch (error) {
    if (error instanceof OfflineError) {
      redirectToOffline(error.isTimeout ? "timeout" : "offline");
      return;
    }
    showError("start-error", "Could not start a jam. Please try again.");
  } finally {
    startJam.disabled = false;
  }
});

// Returns the name without surrounding spaces, or "" after the browser has asked for one
function readName() {
  nameInput.value = nameInput.value.trim();
  return nameInput.reportValidity() ? nameInput.value : "";
}

function rememberName(name) {
  try {
    localStorage.setItem("musicjam.name", name);
  } catch {
    // Ignore storage errors
  }
}

function showError(id, message) {
  const error = document.getElementById(id);
  error.textContent = message;
  error.hidden = false;
}
