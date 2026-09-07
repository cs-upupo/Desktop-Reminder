"use strict";

(() => {
  const $ = id => document.getElementById(id);
  const content = $("content"), interval = $("interval");
  const start = $("start"), pause = $("pause"), test = $("test");
  const settings = $("settings");
  const presets = Array.from(document.querySelectorAll("[data-minutes]"));
  let state = null, stateAt = 0, ready = false, busy = false;
  let saveTimer = 0, flashTimer = 0, revision = 0, queue = Promise.resolve();
  const api = () => window.go.main.App;
  const errorText = err => typeof err === "string" ? err : (err && err.message) || "操作未完成，请重试";

  function showMessage(text, error = false) {
    clearTimeout(flashTimer);
    $("message").textContent = text;
    $("message").classList.toggle("error", error);
    $("message").hidden = !text;
    if (text && !error) flashTimer = setTimeout(() => { $("message").hidden = true; }, 6500);
  }

  function setSaved(text, unsaved = false) {
    $("save-state").textContent = text;
    $("save-state").classList.toggle("unsaved", unsaved);
  }

  function readConfig() {
    const value = content.value.trim();
    const minutes = Number(interval.value);
    if (!value) throw new Error("请输入提醒内容。");
    if (Array.from(value).length > 500) throw new Error("提醒内容最多 500 个字符。");
    if (!Number.isInteger(minutes) || minutes < 1 || minutes > 10080) {
      throw new Error("提醒间隔必须是 1～10080 之间的整数分钟。");
    }
    return { content: value, intervalMinutes: minutes };
  }

  function updateInputs() {
    $("character-count").textContent = `${Array.from(content.value).length} / 500`;
    presets.forEach(button => button.classList.toggle("selected", Number(button.dataset.minutes) === Number(interval.value)));
  }

  function renderControls() {
    const running = Boolean(state && state.running);
    start.disabled = !ready || busy || running;
    pause.disabled = !ready || busy || !running;
    test.disabled = !ready || busy;
    settings.disabled = !ready;
    content.disabled = interval.disabled = !ready || busy || running;
    presets.forEach(button => { button.disabled = !ready || busy || running; });
    start.querySelector("span").textContent = state && state.started && !running ? "继续提醒" : "开始提醒";
  }

  function renderClock() {
    if (!state) return;
    const elapsed = state.running ? performance.now() - stateAt : 0;
    const remaining = Math.max(0, state.remainingMs - elapsed);
    const seconds = Math.ceil(remaining / 1000);
    const hours = Math.floor(seconds / 3600);
    const minutes = Math.floor(seconds / 60);
    const pad = n => String(n).padStart(2, "0");
    $("countdown").textContent = hours ? `${pad(hours)}:${pad(minutes % 60)}:${pad(seconds % 60)}` : `${pad(minutes)}:${pad(seconds % 60)}`;
    $("countdown").classList.toggle("long", hours > 0);
    $("clock-unit").textContent = hours ? "小时 / 分钟 / 秒" : "分钟 / 秒";
    const fraction = Math.min(1, remaining / (state.config.intervalMinutes * 60000));
    $("ring-progress").style.strokeDashoffset = String(100 * (1 - fraction));
  }

  function applyState(next, hydrate = false) {
    state = next;
    stateAt = performance.now();
    if (hydrate) {
      content.value = state.config.content;
      interval.value = state.config.intervalMinutes;
      updateInputs();
    }
    const label = state.running ? "提醒中" : state.started ? "已暂停" : "准备就绪";
    $("status").replaceChildren();
    $("status").append(document.createElement("i"), document.createTextNode(label));
    $("status").className = `status ${state.running ? "running" : state.started ? "paused" : ""}`;
    $("clock-caption").textContent = state.running ? "下一次小小的休息" : state.started ? "准备好后，随时继续" : "留一点时间给自己";
    $("next-time").textContent = state.running
      ? `预计 ${new Date(state.nextAt).toLocaleTimeString("zh-CN", { hour12: false })} 提醒`
      : state.started ? "已保留剩余时间，继续后接着计时" : "点击开始，开启第一轮提醒";
    $("last-notification").textContent = state.lastNotification
      ? `最近通知 ${new Date(state.lastNotification).toLocaleTimeString("zh-CN", { hour12: false })} · 本次 ${state.notificationCount} 条`
      : "还没有发送通知";
    const warning = [state.configWarning, state.notificationError].filter(Boolean).join("；");
    $("warning").textContent = warning;
    $("warning").hidden = !warning;
    renderClock();
    renderControls();
  }

  // 串行执行保存与开始/暂停，避免旧的自动保存覆盖新设置。
  function enqueue(action) {
    const operation = queue.then(async () => {
      busy = true;
      revision++;
      renderControls();
      try { await action(); }
      catch (err) { showMessage(errorText(err), true); }
      finally { busy = false; renderControls(); }
    });
    queue = operation.catch(() => {});
    return operation;
  }

  function scheduleSave() {
    updateInputs();
    setSaved("等待保存…", true);
    clearTimeout(saveTimer);
    saveTimer = setTimeout(() => enqueue(async () => {
      try {
        const next = await api().SaveConfig(readConfig());
        applyState(next);
        setSaved("已自动保存");
        showMessage("");
      } catch (err) { setSaved("尚未保存", true); throw err; }
    }), 550);
  }

  content.addEventListener("input", scheduleSave);
  interval.addEventListener("input", scheduleSave);
  presets.forEach(button => button.addEventListener("click", () => { interval.value = button.dataset.minutes; scheduleSave(); }));

  start.addEventListener("click", () => {
    clearTimeout(saveTimer);
    enqueue(async () => {
      const next = await api().Start(readConfig());
      applyState(next, true);
      setSaved("已自动保存");
      showMessage("");
    });
  });

  pause.addEventListener("click", () => enqueue(async () => { applyState(await api().Pause()); showMessage("已暂停，剩余时间已保留。"); }));

  test.addEventListener("click", () => {
    clearTimeout(saveTimer);
    enqueue(async () => {
      const config = readConfig();
      if (!state.running) { applyState(await api().SaveConfig(config)); setSaved("已自动保存"); }
      test.querySelector("span").textContent = "正在发送…";
      try {
        await api().TestNotification(config);
        applyState(await api().GetState());
        showMessage("已请求发送无声通知；若未显示，请检查 Windows 通知设置与勿扰模式。");
      } finally { test.querySelector("span").textContent = "测试无声通知"; }
    });
  });

  settings.addEventListener("click", async () => {
    try { await api().OpenNotificationSettings(); }
    catch (err) { showMessage(errorText(err), true); }
  });

  async function poll() {
    if (!ready || busy) return;
    const observedRevision = revision;
    try {
      const next = await api().GetState();
      if (!busy && observedRevision === revision) applyState(next);
    } catch (err) { showMessage(`连接提醒服务失败：${errorText(err)}`, true); }
  }

  async function init() {
    try {
      if (!window.go || !window.go.main || !window.go.main.App) throw new Error("请通过桌面程序或 wails dev 打开此界面。");
      const initial = await api().GetState();
      ready = true;
      applyState(initial, true);
      setSaved(initial.configWarning ? "请检查设置" : "设置已就绪", Boolean(initial.configWarning));
      setInterval(poll, 800);
      setInterval(renderClock, 100);
      document.addEventListener("visibilitychange", () => { if (!document.hidden) poll(); });
    } catch (err) { showMessage(errorText(err), true); setSaved("初始化失败", true); }
  }
  init();
})();
