const pagePath = window.location.pathname;
const currentLink = Array.from(document.querySelectorAll("nav a"))
  .find(link => link.pathname === pagePath);

if (currentLink) {
  currentLink.setAttribute("aria-current", "page");
  document.querySelector("#page-title").textContent = currentLink.textContent;
}

const eventStatus = document.querySelector("#event-status");

function newEventId() {
  const bytes = crypto.getRandomValues(new Uint8Array(16));
  bytes[6] = (bytes[6] & 0x0f) | 0x40;
  bytes[8] = (bytes[8] & 0x3f) | 0x80;
  const hex = Array.from(bytes, byte => byte.toString(16).padStart(2, "0")).join("");
  return [hex.slice(0, 8), hex.slice(8, 12), hex.slice(12, 16), hex.slice(16, 20), hex.slice(20)].join("-");
}

async function sendView() {
  const body = JSON.stringify({event_id: newEventId(), type: "page_view", path: pagePath});
  for (let attempt = 0; attempt < 2; attempt++) {
    try {
      const response = await fetch("/api/events", {
        method: "POST",
        headers: {"Content-Type": "application/json"},
        body,
        signal: AbortSignal.timeout(5000),
      });
      if (response.ok) {
        eventStatus.textContent = "Просмотр принят";
        return;
      }
      if (response.status < 500) break;
    } catch {
      // A lost response can arrive after SQLite already committed the event.
      // The retry sends the original ID so the server can recognize it.
    }
    if (attempt === 0) await new Promise(resolve => setTimeout(resolve, 350));
  }
  eventStatus.textContent = "Не удалось отправить просмотр";
}

sendView();
