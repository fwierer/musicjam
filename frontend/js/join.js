const nameInput = document.getElementById("user-name");
const params = new URLSearchParams(location.search);
const code = (params.get("code") || params.get("join") || "").trim().toLowerCase();

try {
  const savedName = localStorage.getItem("musicjam.name");
  if (savedName && !nameInput.value) {
    nameInput.value = savedName;
  }
} catch {
  // Ignore localStorage errors
}

nameInput.focus();

document.getElementById("join-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  nameInput.value = nameInput.value.trim();
  if (!nameInput.reportValidity()) return;
  const name = nameInput.value;

  if (!code) {
    showError("Missing room link. Please scan the QR code again.");
    return;
  }

  try {
    const response = await fetchWithTimeout(
      `/api/room/join?code=${encodeURIComponent(code)}&name=${encodeURIComponent(name)}`,
      { method: "POST" },
      ACTION_TIMEOUT_MS
    );
    if (!response.ok) {
      showError(
        response.status === 404
          ? "No jam found. The session may have ended."
          : "Could not join. Please try again."
      );
      return;
    }
    try {
      localStorage.setItem("musicjam.name", name);
    } catch {}
    location.href = "/guest.html?code=" + encodeURIComponent(code);
  } catch (error) {
    if (error instanceof OfflineError) {
      redirectToOffline(error.isTimeout ? "timeout" : "offline");
      return;
    }
    showError("Could not join. Please try again.");
  }
});

function showError(message) {
  const error = document.getElementById("join-error");
  error.textContent = message;
  error.hidden = false;
}
