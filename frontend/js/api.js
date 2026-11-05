// Shared HTTP timeout & connection helper for Music Jam.
// Wraps fetch() with an AbortController timeout and distinguishes server/network
// unavailability (OfflineError) from normal HTTP responses (like 400/401/404).

const DEFAULT_TIMEOUT_MS = 5000;
const ACTION_TIMEOUT_MS = 6000;
const SEARCH_TIMEOUT_MS = 10000;

class OfflineError extends Error {
  constructor(message, isTimeout = false) {
    super(message);
    this.name = "OfflineError";
    this.isTimeout = isTimeout;
  }
}

async function fetchWithTimeout(url, options = {}, timeoutMs = DEFAULT_TIMEOUT_MS) {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMs);
  try {
    const response = await fetch(url, { ...options, signal: controller.signal });
    if (response.status === 503 || response.status === 504) {
      throw new OfflineError(`Server unavailable (${response.status}).`, false);
    }
    return response;
  } catch (error) {
    if (error instanceof OfflineError) {
      throw error;
    }
    const isTimeout = error?.name === "AbortError" || error?.name === "TimeoutError";
    throw new OfflineError(
      isTimeout
        ? "The request timed out because the server took too long to respond."
        : "Could not connect to the Music Jam server.",
      isTimeout
    );
  } finally {
    clearTimeout(timer);
  }
}

function redirectToOffline(reason = "offline", returnTo = location.pathname + location.search) {
  const params = new URLSearchParams({
    reason,
    returnTo: returnTo || "/",
  });
  location.href = "/offline.html?" + params.toString();
}

function setConnectionState(online, customText = "") {
  const pill = document.getElementById("connection-pill");
  const status = document.getElementById("connection-status");
  const dot = document.querySelector(".dot");

  if (dot) {
    dot.classList.toggle("offline", !online);
  }
  if (pill) {
    pill.hidden = online;
  }
  if (status) {
    status.hidden = online;
    status.textContent = online ? "" : (customText || "Reconnecting…");
  }
}

function showRoomName(name) {
  const el = document.getElementById("room-name");
  if (!el) return;
  if (name) {
    el.textContent = name;
    el.hidden = false;
  } else {
    el.hidden = true;
  }
}

async function loadSessionWithRetry(maxAttempts = 2) {
  let lastError = null;
  for (let attempt = 1; attempt <= maxAttempts; attempt++) {
    try {
      const response = await fetchWithTimeout("/api/session", {}, DEFAULT_TIMEOUT_MS);
      setConnectionState(true);
      if (!response.ok) {
        return null;
      }
      return await response.json();
    } catch (error) {
      lastError = error;
      if (!(error instanceof OfflineError)) {
        throw error;
      }
      setConnectionState(false, error.isTimeout ? "Request timed out — retrying…" : "Reconnecting…");
      if (attempt < maxAttempts) {
        await new Promise((resolve) => setTimeout(resolve, 1500));
      }
    }
  }
  redirectToOffline(lastError?.isTimeout ? "timeout" : "offline");
  return null;
}
