const total = document.querySelector("#total");
const status = document.querySelector("#status");
const empty = document.querySelector("#empty");
const table = document.querySelector("#pages");
const rows = table.querySelector("tbody");
const formatNumber = new Intl.NumberFormat("ru-RU");
let connected = false;

async function refreshStats() {
  try {
    const response = await fetch("/api/stats", {signal: AbortSignal.timeout(5000)});
    if (!response.ok) throw new Error("Статистика недоступна");
    const stats = await response.json();
    total.textContent = formatNumber.format(stats.total_views);
    rows.replaceChildren(...stats.pages.map(({path, views}) => {
      const row = document.createElement("tr");
      row.insertCell().textContent = path;
      row.insertCell().textContent = formatNumber.format(views);
      return row;
    }));
    table.hidden = stats.pages.length === 0;
    empty.hidden = !table.hidden;
    if (!connected) status.textContent = "Данные обновляются каждые 2 секунды";
    connected = true;
  } catch {
    status.textContent = "Связь с сервером потеряна. Пробуем снова…";
    connected = false;
  } finally {
    window.setTimeout(refreshStats, 2000);
  }
}

refreshStats();
