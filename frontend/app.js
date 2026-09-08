"use strict";

const byId = (id) => document.getElementById(id);
const appState = {
  data: { tasks: [], now: Date.now(), lastNotification: 0, notificationCount: 0, configWarning: "" },
  filter: "all",
  search: "",
  view: "tasks",
  historyDate: localDateString(new Date()),
  dayView: { date: "", items: [], total: 0, completed: 0, pending: 0 },
  loadingState: false,
  historyRequest: 0,
  testingTasks: new Set(),
  toastTimer: 0,
};

function localDateString(value) {
  const year = value.getFullYear();
  const month = String(value.getMonth() + 1).padStart(2, "0");
  const day = String(value.getDate()).padStart(2, "0");
  return `${year}-${month}-${day}`;
}

function localDateTimeValue(value) {
  return `${localDateString(value)}T${String(value.getHours()).padStart(2, "0")}:${String(value.getMinutes()).padStart(2, "0")}:${String(value.getSeconds()).padStart(2, "0")}`;
}

function errorText(error) {
  const value = error && error.message ? error.message : String(error || "操作失败");
  return value.replace(/^Error:\s*/i, "");
}

function appMethod(name) {
  const api = window.go && window.go.main && window.go.main.App;
  if (!api || typeof api[name] !== "function") {
    throw new Error("应用接口正在连接，请稍后重试");
  }
  return api[name];
}

async function invoke(name, ...args) {
  return appMethod(name)(...args);
}

function element(tag, className, text) {
  const node = document.createElement(tag);
  if (className) node.className = className;
  if (text !== undefined) node.textContent = text;
  return node;
}

function showToast(message, isError) {
  const toast = byId("toast");
  window.clearTimeout(appState.toastTimer);
  toast.textContent = message;
  toast.classList.toggle("error", Boolean(isError));
  toast.classList.remove("hidden");
  appState.toastTimer = window.setTimeout(() => toast.classList.add("hidden"), isError ? 5200 : 3000);
}

async function refreshState(silent) {
  if (appState.loadingState) return;
  appState.loadingState = true;
  try {
    const state = await invoke("GetState");
    applyState(state);
  } catch (error) {
    if (!silent) showToast(errorText(error), true);
  } finally {
    appState.loadingState = false;
  }
}

function applyState(state) {
  appState.data = state || appState.data;
  if (!Array.isArray(appState.data.tasks)) appState.data.tasks = [];
  renderSummary();
  renderTasks();
  renderWarning();
  if (appState.view === "history") loadDayView();
}

function renderWarning() {
  const banner = byId("warning-banner");
  const message = appState.data.configWarning || "";
  banner.textContent = message;
  banner.classList.toggle("hidden", !message);
}

function renderSummary() {
  const tasks = appState.data.tasks;
  byId("active-count").textContent = String(tasks.filter((task) => task.active && !task.awaitingCompletion).length);
  byId("waiting-count").textContent = String(tasks.filter((task) => task.awaitingCompletion).length);
  byId("total-count").textContent = String(tasks.length);
}

function filteredTasks() {
  const query = appState.search.trim().toLocaleLowerCase("zh-CN");
  return appState.data.tasks.filter((task) => {
    let matches = true;
    if (appState.filter === "interval") matches = task.kind === "interval";
    if (appState.filter === "scheduled") matches = task.kind === "scheduled";
    if (appState.filter === "awaiting") matches = task.awaitingCompletion;
    if (appState.filter === "completed") matches = task.completed;
    if (!matches || !query) return matches;
    return `${task.name} ${task.content}`.toLocaleLowerCase("zh-CN").includes(query);
  });
}

function taskStatus(task) {
  if (task.awaitingCompletion) {
    return { key: "awaiting", label: task.active ? "待完成" : "待完成 · 已暂停" };
  }
  if (task.completed) return { key: "completed", label: "已完成" };
  if (task.active) return { key: "running", label: "运行中" };
  return { key: "paused", label: "已暂停" };
}

function taskTypeLabel(task) {
  if (task.kind === "interval") return "循环任务";
  return task.scheduleMode === "daily" ? "每日定时" : "指定日期";
}

function scheduleText(task) {
  if (task.kind === "interval") return `每 ${task.intervalMinutes} 分钟提醒`;
  if (task.scheduleMode === "daily") return `每天 ${task.dailyTime}`;
  return `${formatFullDate(task.onceAt)} 提醒`;
}

function repeatText(task) {
  if (task.kind === "interval") return "提醒后持续循环，完成即停止";
  return `未完成时每 ${task.repeatMinutes} 分钟再次提醒`;
}

function renderTasks() {
  const list = byId("task-list");
  const empty = byId("task-empty");
  const tasks = filteredTasks();
  list.replaceChildren();
  empty.classList.toggle("hidden", tasks.length !== 0);
  list.classList.toggle("hidden", tasks.length === 0);
  if (!tasks.length) {
    const title = empty.querySelector("h2");
    const message = empty.querySelector("p");
    const button = empty.querySelector("button");
    const noTasksAtAll = appState.data.tasks.length === 0;
    title.textContent = noTasksAtAll ? "这里还没有任务" : "没有符合条件的任务";
    message.textContent = noTasksAtAll ? "新建一个循环或定时任务，提醒会在后台安静运行。" : "换一个筛选条件或搜索词试试。";
    button.classList.toggle("hidden", !noTasksAtAll);
    return;
  }
  const fragment = document.createDocumentFragment();
  tasks.forEach((task) => fragment.appendChild(createTaskCard(task)));
  list.appendChild(fragment);
  updateCountdowns();
}

function createTaskCard(task) {
  const status = taskStatus(task);
  const card = element("article", `task-card ${status.key}`);
  card.dataset.taskId = task.id;

  const main = element("div", "task-main");
  const heading = element("div", "task-heading");
  heading.appendChild(element("span", `task-type ${task.kind}`, taskTypeLabel(task)));
  heading.appendChild(element("h2", "", task.name));
  main.appendChild(heading);
  main.appendChild(element("p", "task-content", task.content));

  const meta = element("div", "task-meta");
  meta.appendChild(element("span", "", `◷ ${scheduleText(task)}`));
  meta.appendChild(element("span", "", `↻ ${repeatText(task)}`));
  if (task.notificationCount > 0) meta.appendChild(element("span", "", `已通知 ${task.notificationCount} 次`));
  main.appendChild(meta);
  if (task.lastError) main.appendChild(element("p", "task-error", task.lastError));

  const side = element("div", "task-side");
  const timing = element("div", "task-timing");
  const countdown = element("span", "countdown");
  if (task.nextAt > 0 && task.active) {
    countdown.dataset.nextAt = String(task.nextAt);
    countdown.dataset.prefix = task.awaitingCompletion ? "再次提醒" : "下次提醒";
  } else if (task.completed) {
    countdown.textContent = task.kind === "scheduled" && task.scheduleMode === "daily" ? "等待下一次" : "任务已结束";
  } else {
    countdown.textContent = task.awaitingCompletion ? "再次提醒已暂停" : "计时已暂停";
  }
  timing.appendChild(countdown);
  timing.appendChild(element("span", `status-pill ${status.key}`, status.label));
  side.appendChild(timing);

  const actions = element("div", "task-actions");
  if (task.awaitingCompletion) actions.appendChild(actionButton("complete", "完成", task.id, "complete"));
  if (task.active) {
    actions.appendChild(actionButton("pause", "暂停", task.id));
  } else if (!(task.completed && task.kind === "scheduled" && task.scheduleMode === "once")) {
    actions.appendChild(actionButton("start", task.completed ? "重新开始" : "启用", task.id));
  }
  const testing = appState.testingTasks.has(task.id);
  const testButton = actionButton("test", testing ? "发送中…" : "测试", task.id);
  testButton.disabled = testing;
  actions.appendChild(testButton);
  actions.appendChild(actionButton("edit", "编辑", task.id));
  actions.appendChild(actionButton("delete", "删除", task.id, "delete"));
  side.appendChild(actions);

  card.appendChild(main);
  card.appendChild(side);
  return card;
}

function actionButton(action, label, id, extraClass) {
  const button = element("button", `action-button${extraClass ? ` ${extraClass}` : ""}`, label);
  button.type = "button";
  button.dataset.action = action;
  button.dataset.id = id;
  return button;
}

function updateCountdowns() {
  document.querySelectorAll(".countdown[data-next-at]").forEach((node) => {
    const remaining = Number(node.dataset.nextAt) - Date.now();
    node.textContent = `${node.dataset.prefix} ${formatDuration(remaining)}`;
  });
}

function formatDuration(milliseconds) {
  let seconds = Math.max(0, Math.ceil(milliseconds / 1000));
  const days = Math.floor(seconds / 86400);
  seconds %= 86400;
  const hours = Math.floor(seconds / 3600);
  seconds %= 3600;
  const minutes = Math.floor(seconds / 60);
  const rest = seconds % 60;
  const clock = [hours, minutes, rest].map((value) => String(value).padStart(2, "0")).join(":");
  return days ? `${days}天 ${clock}` : clock;
}

function formatFullDate(milliseconds) {
  if (!milliseconds) return "未设置时间";
  const value = new Date(milliseconds);
  return `${value.getFullYear()}-${String(value.getMonth() + 1).padStart(2, "0")}-${String(value.getDate()).padStart(2, "0")} ${formatClock(milliseconds)}`;
}

function formatClock(milliseconds) {
  if (!milliseconds) return "全天";
  const value = new Date(milliseconds);
  return [value.getHours(), value.getMinutes(), value.getSeconds()].map((part) => String(part).padStart(2, "0")).join(":");
}

async function handleTaskAction(event) {
  const button = event.target.closest("button[data-action]");
  if (!button) return;
  const task = appState.data.tasks.find((item) => item.id === button.dataset.id);
  if (!task) return;
  const action = button.dataset.action;
  if (action === "edit") {
    openTaskModal(task);
    return;
  }
  if (action === "delete") {
    if (!window.confirm(`确定删除“${task.name}”吗？已保存的完成记录仍会保留。`)) return;
    await runTaskMutation(button, "DeleteTask", task.id, "任务已删除");
    return;
  }
  if (action === "test") {
    await runTestNotification(button, task.id);
    return;
  }
  const methods = { start: "StartTask", pause: "PauseTask", complete: "CompleteTask" };
  const messages = { start: "任务已启用", pause: "任务已暂停", complete: "任务已完成" };
  if (methods[action]) await runTaskMutation(button, methods[action], task.id, messages[action]);
}

async function runTaskMutation(button, method, id, successMessage) {
  button.disabled = true;
  try {
    const state = await invoke(method, id);
    applyState(state);
    showToast(successMessage, false);
  } catch (error) {
    showToast(errorText(error), true);
  } finally {
    button.disabled = false;
  }
}

async function runTestNotification(button, id) {
  if (appState.testingTasks.has(id)) return;
  appState.testingTasks.add(id);
  renderTasks();
  try {
    await invoke("TestTaskNotification", id);
    showToast("测试通知已发送，请查看 Windows 通知中心", false);
    await refreshState(true);
  } catch (error) {
    showToast(errorText(error), true);
  } finally {
    appState.testingTasks.delete(id);
    renderTasks();
  }
}

function selectedRadio(name) {
  const selected = document.querySelector(`input[name="${name}"]:checked`);
  return selected ? selected.value : "";
}

function setRadio(name, value) {
  const input = document.querySelector(`input[name="${name}"][value="${value}"]`);
  if (input) input.checked = true;
}

function openTaskModal(task) {
  const editing = Boolean(task);
  byId("modal-eyebrow").textContent = editing ? "修改提醒" : "创建提醒";
  byId("modal-title").textContent = editing ? "编辑任务" : "新建任务";
  byId("save-task").textContent = editing ? "保存修改" : "保存任务";
  byId("task-id").value = editing ? task.id : "";
  byId("task-name").value = editing ? task.name : "";
  byId("task-content").value = editing ? task.content : "";
  setRadio("task-kind", editing ? task.kind : "interval");
  setRadio("schedule-mode", editing && task.scheduleMode ? task.scheduleMode : "daily");
  byId("interval-minutes").value = editing && task.intervalMinutes ? task.intervalMinutes : 30;
  byId("daily-time").value = editing && task.dailyTime ? task.dailyTime : "09:00:00";
  const defaultOnce = new Date(Date.now() + 60 * 60 * 1000);
  defaultOnce.setMilliseconds(0);
  byId("once-at").value = editing && task.onceAt ? localDateTimeValue(new Date(task.onceAt)) : localDateTimeValue(defaultOnce);
  byId("repeat-minutes").value = editing && task.repeatMinutes ? task.repeatMinutes : 10;
  byId("task-active").checked = editing ? Boolean(task.active) : true;
  byId("form-error").classList.add("hidden");
  byId("content-count").textContent = String(byId("task-content").value.length);
  updateTaskFormMode();
  byId("task-modal").classList.remove("hidden");
  window.setTimeout(() => byId("task-name").focus(), 40);
}

function closeTaskModal() {
  byId("task-modal").classList.add("hidden");
  byId("task-form").reset();
  byId("form-error").classList.add("hidden");
}

function updateTaskFormMode() {
  const scheduled = selectedRadio("task-kind") === "scheduled";
  byId("interval-fields").classList.toggle("hidden", scheduled);
  byId("scheduled-fields").classList.toggle("hidden", !scheduled);
  const once = selectedRadio("schedule-mode") === "once";
  byId("daily-time-field").classList.toggle("hidden", once);
  byId("once-time-field").classList.toggle("hidden", !once);
}

function normalizeDailyTime(value) {
  if (/^\d{2}:\d{2}$/.test(value)) return `${value}:00`;
  return value;
}

function integerValue(id, label) {
  const value = Number(byId(id).value);
  if (!Number.isInteger(value) || value < 1 || value > 10080) {
    throw new Error(`${label}必须是 1～10080 之间的整数分钟`);
  }
  return value;
}

async function saveTask(event) {
  event.preventDefault();
  const errorBox = byId("form-error");
  const saveButton = byId("save-task");
  errorBox.classList.add("hidden");
  try {
    const kind = selectedRadio("task-kind");
    const mode = selectedRadio("schedule-mode");
    const name = byId("task-name").value.trim();
    const content = byId("task-content").value.trim();
    if (!name) throw new Error("请输入任务名称");
    if (!content) throw new Error("请输入提醒内容");
    let onceAt = 0;
    if (kind === "scheduled" && mode === "once") {
      onceAt = new Date(byId("once-at").value).getTime();
      if (!Number.isFinite(onceAt) || onceAt <= 0) throw new Error("请选择一次性任务的执行日期和时间");
    }
    const input = {
      id: byId("task-id").value,
      name,
      content,
      kind,
      scheduleMode: kind === "scheduled" ? mode : "",
      intervalMinutes: kind === "interval" ? integerValue("interval-minutes", "循环间隔") : 0,
      dailyTime: kind === "scheduled" && mode === "daily" ? normalizeDailyTime(byId("daily-time").value) : "",
      onceAt,
      repeatMinutes: kind === "scheduled" ? integerValue("repeat-minutes", "再次提醒间隔") : 0,
      active: byId("task-active").checked,
    };
    if (kind === "scheduled" && mode === "daily" && !input.dailyTime) throw new Error("请选择每天执行时间");
    saveButton.disabled = true;
    saveButton.textContent = "保存中…";
    const state = await invoke("SaveTask", input);
    applyState(state);
    closeTaskModal();
    showToast(input.id ? "任务修改已保存" : "任务已创建", false);
  } catch (error) {
    errorBox.textContent = errorText(error);
    errorBox.classList.remove("hidden");
  } finally {
    saveButton.disabled = false;
    saveButton.textContent = byId("task-id").value ? "保存修改" : "保存任务";
  }
}

function switchView(view) {
  appState.view = view;
  document.querySelectorAll(".nav-item").forEach((button) => button.classList.toggle("active", button.dataset.view === view));
  byId("tasks-view").classList.toggle("hidden", view !== "tasks");
  byId("history-view").classList.toggle("hidden", view !== "history");
  byId("page-title").textContent = view === "tasks" ? "任务列表" : "日期记录";
  byId("page-subtitle").textContent = view === "tasks"
    ? "安排循环任务和定时任务，到点后持续提醒"
    : "按日期查看当天任务以及完成情况";
  byId("new-task-button").classList.toggle("hidden", view !== "tasks");
  if (view === "history") loadDayView();
}

async function loadDayView() {
  const request = ++appState.historyRequest;
  try {
    const view = await invoke("GetDayView", appState.historyDate);
    if (request !== appState.historyRequest) return;
    appState.dayView = view;
    renderDayView();
  } catch (error) {
    if (request === appState.historyRequest) showToast(errorText(error), true);
  }
}

function renderDayView() {
  const view = appState.dayView || { items: [], total: 0, completed: 0, pending: 0 };
  const items = Array.isArray(view.items) ? view.items : [];
  byId("day-total").textContent = String(view.total || 0);
  byId("day-completed").textContent = String(view.completed || 0);
  byId("day-pending").textContent = String(view.pending || 0);
  const percent = view.total ? Math.round((view.completed / view.total) * 100) : 0;
  byId("day-percent").textContent = `${percent}%`;
  byId("day-progress").style.width = `${percent}%`;

  const list = byId("history-list");
  const empty = byId("history-empty");
  list.replaceChildren();
  empty.classList.toggle("hidden", items.length !== 0);
  list.classList.toggle("hidden", items.length === 0);
  const fragment = document.createDocumentFragment();
  items.forEach((item) => fragment.appendChild(createHistoryCard(item)));
  list.appendChild(fragment);
}

function createHistoryCard(item) {
  const statusLabels = {
    completed: "已完成",
    pending: "待完成",
    missed: "未完成",
    scheduled: "待执行",
    paused: "已暂停",
    running: "进行中",
  };
  const card = element("article", "history-card");
  card.appendChild(element("time", "history-time", item.scheduledAt ? formatClock(item.scheduledAt) : "全天"));
  const body = element("div", "history-body");
  body.appendChild(element("h2", "", item.name));
  let detail = `${item.kind === "interval" ? "循环任务" : item.scheduleMode === "daily" ? "每日定时" : "指定日期"} · ${item.content}`;
  if (item.status === "completed" && item.completedAt) detail += ` · ${formatClock(item.completedAt)} 完成`;
  body.appendChild(element("p", "", detail));
  card.appendChild(body);
  card.appendChild(element("span", `history-status ${item.status}`, statusLabels[item.status] || "未完成"));
  return card;
}

function shiftHistoryDate(days) {
  const parts = appState.historyDate.split("-").map(Number);
  const value = new Date(parts[0], parts[1] - 1, parts[2], 12, 0, 0);
  value.setDate(value.getDate() + days);
  appState.historyDate = localDateString(value);
  byId("history-date").value = appState.historyDate;
  loadDayView();
}

function bindEvents() {
  document.querySelectorAll(".nav-item").forEach((button) => button.addEventListener("click", () => switchView(button.dataset.view)));
  document.querySelectorAll(".filter-button").forEach((button) => button.addEventListener("click", () => {
    appState.filter = button.dataset.filter;
    document.querySelectorAll(".filter-button").forEach((item) => item.classList.toggle("active", item === button));
    renderTasks();
  }));
  byId("task-search").addEventListener("input", (event) => {
    appState.search = event.target.value;
    renderTasks();
  });
  byId("task-list").addEventListener("click", handleTaskAction);
  byId("new-task-button").addEventListener("click", () => openTaskModal(null));
  document.querySelector(".create-empty").addEventListener("click", () => openTaskModal(null));
  byId("close-modal").addEventListener("click", closeTaskModal);
  byId("cancel-modal").addEventListener("click", closeTaskModal);
  byId("task-modal").addEventListener("click", (event) => {
    if (event.target === byId("task-modal")) closeTaskModal();
  });
  document.querySelectorAll('input[name="task-kind"], input[name="schedule-mode"]').forEach((input) => input.addEventListener("change", updateTaskFormMode));
  byId("task-content").addEventListener("input", (event) => {
    byId("content-count").textContent = String(event.target.value.length);
  });
  byId("task-form").addEventListener("submit", saveTask);
  byId("settings-button").addEventListener("click", async () => {
    try {
      await invoke("OpenNotificationSettings");
    } catch (error) {
      showToast(errorText(error), true);
    }
  });
  byId("history-date").value = appState.historyDate;
  byId("history-date").addEventListener("change", (event) => {
    if (!event.target.value) return;
    appState.historyDate = event.target.value;
    loadDayView();
  });
  byId("previous-day").addEventListener("click", () => shiftHistoryDate(-1));
  byId("next-day").addEventListener("click", () => shiftHistoryDate(1));
  byId("today-button").addEventListener("click", () => {
    appState.historyDate = localDateString(new Date());
    byId("history-date").value = appState.historyDate;
    loadDayView();
  });
  document.addEventListener("keydown", (event) => {
    if (event.key === "Escape" && !byId("task-modal").classList.contains("hidden")) closeTaskModal();
  });
}

function initialize() {
  bindEvents();
  updateTaskFormMode();
  refreshState(false);
  window.setInterval(updateCountdowns, 250);
  window.setInterval(() => {
    if (!document.hidden) refreshState(true);
  }, 1500);
}

document.addEventListener("DOMContentLoaded", initialize);
