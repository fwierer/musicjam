function renderQueue(queue) {
  const list = document.getElementById("queue");
  list.replaceChildren();

  if (!queue.length) {
    const empty = document.createElement("li");
    empty.className = "text-body-secondary";
    empty.textContent = "Nothing queued yet";
    list.append(empty);
    return;
  }

  queue.forEach((song, index) => {
    const item = document.createElement("li");
    item.className = "queue-entry";
    const number = document.createElement("span");
    number.className = "text-body-secondary";
    number.textContent = `${index + 1}.`;
    const details = document.createElement("div");
    const title = document.createElement("div");
    title.className = "queue-title fw-medium";
    title.textContent = song.title;
    const channel = document.createElement("div");
    channel.className = "queue-channel small text-body-secondary";
    channel.textContent = song.channel;
    details.append(title, channel);
    item.append(number, details);
    list.append(item);
  });
}
