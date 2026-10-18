// Room members: the API calls, and a list that shows members with a Remove button each

// Returns the members of a room, host first
async function getRoomMembers(code) {
  const response = await fetch("/api/room/getRoomMembers?code=" + encodeURIComponent(code));
  if (!response.ok) {
    throw new Error(await response.text());
  }
  return response.json();
}

// Kicks a member out of the host's room and returns the members that are left
async function removeRoomMember(memberId) {
  const response = await fetch("/api/room/removeRoomMember", {
    method: "DELETE",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ memberId }),
  });
  if (!response.ok) {
    throw new Error(await response.text());
  }
  return response.json();
}

// Shows members in the given <ul>. onRemove(member) runs when their Remove button is clicked.
function createMemberList(list, onRemove) {
  let shown;

  function row(member) {
    const item = document.createElement("li");
    item.className = "member";
    item.innerHTML = `
      <span class="text-truncate"></span>
      <button type="button" class="btn btn-sm btn-outline-danger">Remove</button>
    `;
    // textContent, not innerHTML, so names can't inject HTML
    item.querySelector("span").textContent = member.name;
    const button = item.querySelector("button");
    button.setAttribute("aria-label", "Remove " + member.name);
    button.addEventListener("click", () => onRemove(member));
    return item;
  }

  function emptyRow() {
    const item = document.createElement("li");
    item.className = "text-body-secondary";
    item.textContent = "No guests yet";
    return item;
  }

  return {
    show(members) {
      // Only redraw on changes, so a Remove button isn't replaced while it is being clicked
      const json = JSON.stringify(members);
      if (json === shown) {
        return;
      }
      shown = json;
      list.replaceChildren(...(members.length ? members.map(row) : [emptyRow()]));
    },
  };
}
