const nameInput = document.getElementById("user-name");
const joinForm = document.getElementById("join-form");
const joinCodeInput = document.getElementById("join-code");
const errorBox = document.getElementById("error");

// Prefill the room code when arriving via the QR code (/?join=CODE)
const joinParam = new URLSearchParams(location.search).get("join");
if (joinParam) {
  joinCodeInput.value = joinParam;
  nameInput.focus();
}

document.getElementById("create-session").addEventListener("click", async () => {
  const name = getName();
  if (!name) {
    return;
  }

  const response = await fetch(`/api/room/create?name=${encodeURIComponent(name)}`, { method: "POST" });
  if (!response.ok) {
    showError("Could not create a room.");
    return;
  }

  const data = await response.json();
  window.location.href = "session.html?room=" + data.code;
});

joinForm.addEventListener("submit", async (event) => {
  event.preventDefault();

  const name = getName();
  const code = joinCodeInput.value.trim().toUpperCase();
  if (!name) {
    return;
  }
  if (!code) {
    showError("Please enter a room code.");
    return;
  }

  const response = await fetch(
    `/api/room/join?code=${encodeURIComponent(code)}&name=${encodeURIComponent(name)}`,
    { method: "POST" }
  );
  if (!response.ok) {
    showError("Room not found.");
    return;
  }

  window.location.href = "session.html?room=" + code;
});

function getName() {
  const name = nameInput.value.trim();
  if (!name) {
    showError("Please enter your name.");
    nameInput.focus();
  }
  return name;
}

function showError(message) {
  errorBox.textContent = message;
  errorBox.classList.remove("d-none");
}
