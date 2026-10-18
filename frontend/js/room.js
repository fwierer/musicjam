// Start page: host a jam or join one. Both need a name, the server keeps it in the session.
const nameInput = document.getElementById("user-name");
const joinCodeInput = document.getElementById("join-code");

// The QR code on the host screen opens /?join=CODE, so only the name is left to fill in
const joinParam = new URLSearchParams(location.search).get("join");
if (joinParam) {
  joinCodeInput.value = joinParam.toUpperCase();
  nameInput.focus();
}

document.getElementById("start-jam").addEventListener("click", async () => {
  const name = readName();
  if (!name) {
    return;
  }

  // The server remembers the host through the userId cookie it sets here
  const response = await fetch("/api/room/create?name=" + encodeURIComponent(name), { method: "POST" });
  if (!response.ok) {
    showError("start-error", "Could not start a jam. Please try again.");
    return;
  }
  location.href = "/host.html";
});

document.getElementById("join-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  const name = readName();
  if (!name) {
    return;
  }

  const code = joinCodeInput.value.trim().toUpperCase();
  const response = await fetch(
    `/api/room/join?code=${encodeURIComponent(code)}&name=${encodeURIComponent(name)}`,
    { method: "POST" }
  );
  if (!response.ok) {
    showError("join-error", response.status === 404
      ? "No jam found with this code."
      : "Could not join. Please try again.");
    return;
  }
  location.href = "/guest.html?code=" + encodeURIComponent(code);
});

// Returns the name without surrounding spaces, or "" after the browser has asked for one
function readName() {
  nameInput.value = nameInput.value.trim();
  return nameInput.reportValidity() ? nameInput.value : "";
}

function showError(id, message) {
  const error = document.getElementById(id);
  error.textContent = message;
  error.hidden = false;
}
