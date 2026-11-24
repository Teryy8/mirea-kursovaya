const total = document.querySelector("#total");
const sessions = document.querySelector("#sessions");
const statusMessage = document.querySelector("#status");
const period = document.querySelector("#period");
const chart = document.querySelector("#chart");
const chartStatus = document.querySelector("#chart-status");
const chartDetails = document.querySelector("#chart-details");
const number = new Intl.NumberFormat("ru-RU");
const clock = new Intl.DateTimeFormat("ru-RU", {hour: "2-digit", minute: "2-digit"});
let refreshTimer;
let activeRefresh;
let lastPoints = "";

function timeLabel(iso) {
  return clock.format(new Date(iso));
}

function row(values) {
  const item = document.createElement("tr");
  for (const value of values) item.insertCell().textContent = value;
  return item;
}

function renderTable(id, emptyId, values) {
  const table = document.querySelector("#" + id);
  table.querySelector("tbody").replaceChildren(...values.map(row));
  table.hidden = values.length === 0;
  document.querySelector("#" + emptyId).hidden = !table.hidden;
}

function renderChart(points) {
  const snapshot = JSON.stringify(points);
  if (snapshot === lastPoints && !chart.hidden) return;
  lastPoints = snapshot;
  const bars = document.querySelector("#chart-bars");
  const step = Math.max(1, Math.ceil(Math.max(...points.map(point => point.views)) / 3));
  bars.style.setProperty("--points", points.length);
  bars.replaceChildren(...points.map(point => {
    const bar = document.createElement("span");
    bar.className = "chart-bar";
    bar.style.height = (point.views / (step * 3) * 100) + "%";
    bar.title = timeLabel(point.minute) + " · " + number.format(point.views);
    return bar;
  }));

  const setLabels = (selector, labels) => {
    document.querySelector(selector).replaceChildren(...labels.map(label => {
      const el = document.createElement("span");
      el.textContent = label;
      return el;
    }));
  };
  setLabels("#chart-y", [step * 3, step * 2, step, 0].map(n => number.format(n)));
  setLabels("#chart-x", [points[0], points[Math.floor(points.length / 2)], points.at(-1)]
    .map(point => timeLabel(point.minute)));
  document.querySelector("#minute-rows").replaceChildren(...points.map(point =>
    row([timeLabel(point.minute), number.format(point.views)])));

  chart.hidden = false;
  chartDetails.hidden = false;
  const count = points.reduce((sum, point) => sum + point.views, 0);
  chartStatus.textContent = "Просмотров за период: " + number.format(count) + " · местное время";
}

async function getJSON(path, signal) {
  const response = await fetch(path, {signal});
  if (!response.ok) throw new Error("Статистика недоступна");
  return response.json();
}

async function refreshStats() {
  window.clearTimeout(refreshTimer);
  activeRefresh?.abort();
  const request = new AbortController();
  activeRefresh = request;
  const signal = AbortSignal.any([request.signal, AbortSignal.timeout(5000)]);

  try {
    const [stats, live] = await Promise.all([
      getJSON("/api/stats", signal),
      getJSON("/api/timeseries?minutes=" + period.value, signal),
    ]);
    if (request.signal.aborted) return;
    total.textContent = number.format(stats.total_views);
    sessions.textContent = number.format(live.active_sessions);
    renderTable("pages", "empty", stats.pages.map(page => [page.path, number.format(page.views)]));
    renderTable("sources", "sources-empty", live.sources.map(source =>
      [source.domain || "Прямой или внутренний", number.format(source.views)]));
    renderTable("recent", "recent-empty", live.recent.map(event =>
      [event.path, event.type === "click" ? "Клик" : "Просмотр", timeLabel(event.received_at)]));
    renderChart(live.points);
    const message = "Данные обновляются каждые 2 секунды";
    if (statusMessage.textContent !== message) statusMessage.textContent = message;
  } catch {
    if (!request.signal.aborted) {
      statusMessage.textContent = "Связь с сервером потеряна. Пробуем снова…";
      if (chart.hidden) chartStatus.textContent = "График временно недоступен";
    }
  } finally {
    if (activeRefresh === request) refreshTimer = window.setTimeout(refreshStats, 2000);
  }
}

period.addEventListener("change", () => {
  chart.hidden = true;
  chartDetails.hidden = true;
  chartStatus.textContent = "Загрузка выбранного периода…";
  refreshStats();
});

refreshStats();
