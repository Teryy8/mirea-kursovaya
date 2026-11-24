const pagePath = window.location.pathname;
const currentLink = Array.from(document.querySelectorAll("nav a"))
  .find(link => link.pathname === pagePath);

if (currentLink) {
  currentLink.setAttribute("aria-current", "page");
  document.querySelector("#page-title").textContent = currentLink.textContent;
}

const eventStatus = document.querySelector("#event-status");

fetch("/api/events", {
  method: "POST",
  headers: {"Content-Type": "application/json"},
  body: JSON.stringify({type: "page_view", path: pagePath}),
  signal: AbortSignal.timeout(5000),
}).then(response => {
  if (!response.ok) throw new Error("Просмотр не принят");
  eventStatus.textContent = "Просмотр учтён";
}).catch(() => {
  eventStatus.textContent = "Не удалось отправить просмотр";
});
