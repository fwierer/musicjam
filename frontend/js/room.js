// Start page: host a jam or join one. Both need a name, the server keeps it in the session.
const nameInput = document.getElementById("user-name");
const roomNameInput = document.getElementById("room-name");
const joinCodeInput = document.getElementById("join-code");
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

// The QR code on the host screen opens /?join=CODE_OR_UUID, so only the name is left to fill in
const joinParam = new URLSearchParams(location.search).get("join");
if (joinParam) {
  joinCodeInput.value = joinParam.length === 36 ? joinParam.toLowerCase() : joinParam.toUpperCase();
  nameInput.focus();
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

document.getElementById("join-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  const name = readName();
  if (!name) {
    return;
  }

  const rawCode = joinCodeInput.value.trim();
  const code = rawCode.length === 36 ? rawCode.toLowerCase() : rawCode.toUpperCase();
  try {
    const response = await fetchWithTimeout(
      `/api/room/join?code=${encodeURIComponent(code)}&name=${encodeURIComponent(name)}`,
      { method: "POST" },
      ACTION_TIMEOUT_MS
    );
    if (!response.ok) {
      showError(
        "join-error",
        response.status === 404
          ? "No jam found with this code."
          : "Could not join. Please try again."
      );
      return;
    }
    const data = await response.json().catch(() => ({}));
    rememberName(name);
    const targetCode = data.code || data.id || code;
    location.href = "/guest.html?code=" + encodeURIComponent(targetCode);
  } catch (error) {
    if (error instanceof OfflineError) {
      redirectToOffline(error.isTimeout ? "timeout" : "offline");
      return;
    }
    showError("join-error", "Could not join. Please try again.");
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
