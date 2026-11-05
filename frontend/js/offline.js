const params = new URLSearchParams(location.search);
const reason = params.get("reason") || "offline";
const rawReturnTo = params.get("returnTo") || "/";
const returnTo = rawReturnTo.startsWith("/") && !rawReturnTo.startsWith("//") ? rawReturnTo : "/";

const titleEl = document.getElementById("offline-title");
const messageEl = document.getElementById("offline-message");
const badgeEl = document.getElementById("offline-badge");
const retryBtn = document.getElementById("retry-btn");
const retryStatus = document.getElementById("retry-status");

if (reason === "timeout") {
  badgeEl.textContent = "Timed out";
  titleEl.textContent = "Request timed out";
  messageEl.textContent =
    "The Music Jam server took too long to respond. Your Wi-Fi connection may be slow or the host server is unreachable.";
} else {
  badgeEl.textContent = "Offline";
  titleEl.textContent = "Can't reach Music Jam";
  messageEl.textContent =
    "Your browser could not connect to the Music Jam server. It may be restarting or on a different network.";
}

let checking = false;

async function tryReconnect(manual = false) {
  if (checking) return;
  checking = true;
  retryBtn.disabled = true;
  if (manual) {
    retryBtn.textContent = "Checking…";
    retryStatus.textContent = "Pinging server…";
  }

  try {
    const response = await fetchWithTimeout("/api/genres", { cache: "no-store" }, 4000);
    if (response.ok) {
      retryStatus.textContent = "Connected! Returning to your jam…";
      location.replace(returnTo);
      return;
    }
    retryStatus.textContent = "Server responded with an error. Retrying in 5s…";
  } catch (error) {
    retryStatus.textContent = error.isTimeout
      ? "Still timing out. Retrying automatically in 5s…"
      : "Server still unreachable. Retrying automatically in 5s…";
  } finally {
    checking = false;
    retryBtn.disabled = false;
    retryBtn.textContent = "Try reconnecting";
  }
}

retryBtn.addEventListener("click", () => tryReconnect(true));
setInterval(() => tryReconnect(false), 5000);
