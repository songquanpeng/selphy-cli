const $ = (id) => document.getElementById(id);
const api = window.go?.main?.App;
const state = {
  busy: false,
  selected: false,
  preview: null,
  view: "trimmed",
  area: "safe",
  config: {},
  logs: [],
  printing: false,
};
const paperNames = {
  postcard: "明信片 · RP-108 / 100 × 148 mm",
  l: "L 尺寸",
  card: "卡片尺寸",
  "qx-square": "方形 · 带边框",
  "qx-square-borderless": "方形 · 无边框",
  "qx-card": "卡片",
};
const request = () => ({
  host: $("host").value.trim(),
  model: $("model").value,
  paper: $("paper").value,
  area: state.area,
  fit: $("fit").value,
  rotate: Number($("rotate").value),
  margin: Number($("margin").value),
  quality: Number($("quality").value),
});
function friendlyError(error) {
  const text = String(error?.message || error);
  if (/host is down|no route to host|network is unreachable/i.test(text))
    return "打印机暂时不可达。请确认已开机，且 Mac 与打印机在同一 Wi-Fi；详细信息见操作记录。";
  if (/timeout|deadline exceeded|网络连接超时/i.test(text))
    return "连接打印机超时。请检查电源、IP 地址和同一 Wi-Fi，再刷新状态。";
  if (/broken pipe|operation not permitted|permission denied/i.test(text))
    return "连接被系统或网络中断。请检查“系统设置 → 隐私与安全性 → 局域网”中的 SELPHY Studio 权限后重试。";
  if (/context canceled/i.test(text)) return "操作已取消。";
  return text;
}
function notice(text, type = "") {
  $("notice").textContent = text;
  $("notice").className = `notice ${type}`;
}
function log(text) {
  state.logs.push(
    `${new Date().toLocaleTimeString("zh-CN", { hour12: false })}  ${text}`,
  );
  state.logs = state.logs.slice(-150);
  $("activity-log").textContent = state.logs.join("\n");
  $("activity-count").textContent = state.logs.length;
  $("activity-log").scrollTop = $("activity-log").scrollHeight;
}
function controls() {
  document.querySelectorAll("button,input,select").forEach((el) => {
    if (!el.closest("dialog")) el.disabled = state.busy;
  });
  $("print").disabled = state.busy || !state.preview;
  $("export").disabled = state.busy || !state.preview;
  $("cancel").disabled = false;
  $("cancel").hidden = !state.printing;
  $("regenerate").hidden =
    !state.selected || Boolean(state.preview) || state.busy;
  $("rendering").hidden = !(state.busy && !state.printing && state.selected);
  document.querySelectorAll("[data-view]").forEach((el) => {
    el.disabled = false;
  });
}
async function perform(fn, { printing = false } = {}) {
  if (state.busy) return;
  state.busy = true;
  state.printing = printing;
  controls();
  try {
    return await fn();
  } catch (e) {
    const message = String(e?.message || e);
    notice(friendlyError(e), "error");
    log(`失败：${message}`);
  } finally {
    state.busy = false;
    state.printing = false;
    controls();
  }
}
function invalidate() {
  state.preview = null;
  $("preview-image").hidden = true;
  $("empty").hidden = state.selected;
  $("image-info").hidden = true;
  $("preview-caption").textContent = "设置已更改，请等待新预览";
  controls();
}
function papers() {
  const previous = $("paper").value;
  const values =
    $("model").value === "CP1500"
      ? ["postcard", "l", "card"]
      : $("model").value === "QX10"
        ? ["qx-square"]
        : ["qx-square", "qx-square-borderless", "qx-card"];
  $("paper").replaceChildren(
    ...values.map((value) => {
      const el = document.createElement("option");
      el.value = value;
      el.textContent = paperNames[value];
      return el;
    }),
  );
  if (values.includes(previous)) $("paper").value = previous;
  $("device-name").textContent = `Canon ${$("model").value}`;
  calibration();
}
function calibration() {
  const r = request(),
    key = `${r.host}|${r.model}|${r.paper}`;
  const c = state.config.calibrations?.[key];
  $("offset-x").value = c?.offset_x_mm || 0;
  $("offset-y").value = c?.offset_y_mm || 0;
  $("calibration-note").textContent = c
    ? `已保存校准：X ${c.offset_x_mm} mm，Y ${c.offset_y_mm} mm。`
    : "未保存此设备 / 相纸校准，使用机型估计。";
}
function showPreview() {
  const p = state.preview;
  if (!p) return;
  $("preview-image").src = p[state.view];
  $("preview-image").hidden = false;
  $("empty").hidden = true;
  $("photo-name").textContent = p.name;
  const captions = {
    trimmed: "预计撕边后的成品 · 机械进纸可能有偏差",
    guides: "红实线：预计成品边界　绿虚线：安全区（标线不进入打印）",
    full: "实际发送给打印机的完整图像 · 不含标线",
  };
  $("preview-caption").textContent = captions[state.view];
  document
    .querySelectorAll("[data-view]")
    .forEach((el) =>
      el.classList.toggle("active", el.dataset.view === state.view),
    );
}
async function renderPreview() {
  if (!state.selected) return;
  invalidate();
  const p = await api.Preview(request());
  state.preview = p;
  showPreview();
  const im = p.image;
  $("image-info").hidden = false;
  $("image-info").textContent =
    `${im.width} × ${im.height} px · ${(im.bytes / 1024 / 1024).toFixed(2)} MB\n${im.trim_basis === "saved-calibration" ? "已应用保存校准" : "裁切边界：机型估计"} · X ${im.offset_x_mm} / Y ${im.offset_y_mm} mm`;
  $("image-info").style.whiteSpace = "pre-line";
  $("print-hint").textContent = "每次打印消耗 1 张相纸。\n点击后会再次确认。";
  notice(
    state.area === "bleed"
      ? "预览已更新。满版模式会裁去边缘，请查看裁切线。"
      : "预览已更新。重要文字和边框请放在安全区内。",
  );
}
async function choose() {
  await perform(async () => {
    const s = await api.ChoosePhoto();
    if (!s?.name) return;
    state.selected = true;
    $("photo-name").textContent = s.name;
    log(`选择照片：${s.name}`);
    await renderPreview();
  });
}
function changed({ model = false, target = false } = {}) {
  if (model) papers();
  if (target) {
    calibration();
    $("connection").textContent = "设备已修改，尚未查询";
    $("connection").className = "status neutral";
    $("supplies").hidden = true;
  }
  invalidate();
  if (state.selected) void perform(renderPreview);
}
$("choose").onclick = choose;
$("choose-empty").onclick = choose;
$("model").onchange = () => changed({ model: true, target: true });
$("host").oninput = () => {
  invalidate();
  calibration();
  $("connection").textContent = "地址已修改，尚未查询";
  $("connection").className = "status neutral";
  $("supplies").hidden = true;
};
$("host").onchange = () => {
  calibration();
  invalidate();
  notice("地址已修改。保存设备或重新生成预览后，即可按新地址打印。");
};
for (const id of ["paper", "fit", "rotate", "margin", "quality"])
  $(id).onchange = () => {
    if (id === "paper") calibration();
    changed();
  };
for (const id of ["margin", "quality"]) $(id).oninput = invalidate;
document.querySelectorAll("[data-area]").forEach(
  (el) =>
    (el.onclick = () => {
      state.area = el.dataset.area;
      document
        .querySelectorAll("[data-area]")
        .forEach((b) => b.classList.toggle("active", b === el));
      if (state.area === "bleed") $("fit").value = "cover";
      changed();
    }),
);
document.querySelectorAll("[data-view]").forEach(
  (el) =>
    (el.onclick = () => {
      state.view = el.dataset.view;
      showPreview();
    }),
);
$("regenerate").onclick = () => perform(renderPreview);
$("save-target").onclick = () =>
  perform(async () => {
    const r = request();
    await api.SaveTarget(r.host, r.model);
    state.config = await api.Boot();
    notice("已保存默认打印机。下次启动会自动恢复。", "success");
    log("已保存默认打印机");
    if (state.selected) await renderPreview();
  });
$("save-calibration").onclick = () =>
  perform(async () => {
    await api.Calibrate(
      request(),
      Number($("offset-x").value),
      Number($("offset-y").value),
    );
    state.config = await api.Boot();
    calibration();
    notice("已保存此设备和相纸的校准。", "success");
    log("已保存相纸校准");
    if (state.selected) await renderPreview();
  });
$("discover").onclick = () =>
  perform(async () => {
    notice("正在搜索同一网络中的打印机…");
    const result = await api.Query("discover", "", "");
    const ds = result.data || [];
    $("devices").replaceChildren();
    $("devices").hidden = !ds.length;
    for (const d of ds) {
      const b = document.createElement("button");
      b.textContent = `${d.host}　选择 →`;
      b.onclick = () => {
        $("host").value = d.host;
        $("devices").hidden = true;
        changed({ target: true });
        notice(`已选择 ${d.host}，请确认型号后保存设备。`);
      };
      $("devices").append(b);
    }
    notice(
      ds.length
        ? `找到 ${ds.length} 台设备，点击地址选择。`
        : "没有找到打印机。请检查同一 Wi-Fi，或手动输入 IP 地址。",
    );
    log(`搜索设备：找到 ${ds.length} 台`);
  });
function supplies(s) {
  $("connection").textContent = s.error_code
    ? `设备异常：${s.error}`
    : s.state === "idle"
      ? "● 已连接 · 空闲"
      : `● 已连接 · ${s.state === "busy" ? "忙碌" : s.state}`;
  $("connection").className = `status ${s.error_code ? "bad" : "good"}`;
  $("supplies").hidden = false;
  $("supplies").replaceChildren();
  for (const [k, v] of [
    ["纸盒", s.tray],
    ["墨盒", s.ink],
    ["固件", s.firmware],
  ]) {
    const dt = document.createElement("dt"),
      dd = document.createElement("dd");
    dt.textContent = k;
    dd.textContent = v;
    $("supplies").append(dt, dd);
  }
}
$("status").onclick = () =>
  perform(async () => {
    try {
      const r = request(),
        result = await api.Query("status", r.host, r.model);
      supplies(result.data);
      notice(
        result.data.error_code
          ? `打印机报告：${result.data.error}`
          : "打印机状态已刷新。",
        "",
      );
      log("已查询打印机状态");
    } catch (e) {
      $("connection").textContent = "连接失败";
      $("connection").className = "status bad";
      $("supplies").hidden = true;
      throw e;
    }
  });
$("doctor").onclick = () =>
  perform(async () => {
    const r = request();
    let result;
    try {
      result = await api.Query("doctor", r.host, r.model);
    } catch (e) {
      result = { data: { error: String(e) } };
    }
    $("diagnostic-content").textContent = JSON.stringify(result.data, null, 2);
    $("diagnostic").showModal();
    log("已运行连接诊断");
  });
$("close-diagnostic").onclick = () => $("diagnostic").close();
$("export").onclick = () =>
  perform(async () => {
    const path = await api.Export(state.preview.id);
    if (path) {
      notice(`已导出打印用 JPEG：${path}`, "success");
      log("已导出无标线 JPEG");
    }
  });
$("print").onclick = () =>
  perform(
    async () => {
      const id = state.preview.id;
      notice("等待打印确认。确认后请保持打印机连接。");
      try {
        const result = await api.Print(id),
          status = result.data.state;
        if (status === "cancelled") {
          notice("已取消，尚未提交打印。");
          return;
        }
        state.preview = null;
        const descriptions = {
          "idle-after-submission":
            "照片已提交，打印机已回到空闲状态。请检查实物出纸。",
          submitted: "照片已进入打印队列，请检查实物出纸。",
          completed: "打印机报告任务完成，请检查实物出纸。",
        };
        notice(descriptions[status] || `打印结果：${status}`, "success");
        log(`打印结果：${status}`);
        if (result.log) log(result.log);
        $("print-hint").textContent = "再次打印前，请重新生成预览。";
      } catch (e) {
        state.preview = null;
        notice(
          `打印未确认完成：${friendlyError(e)}\n请先检查打印机和出纸情况，再决定是否重新打印。`,
          "error",
        );
        log(`打印失败或已取消：${String(e)}`);
        $("print-hint").textContent = "先检查实物，再重新生成预览。";
      }
    },
    { printing: true },
  );
$("cancel").onclick = async () => {
  await api.Cancel();
  notice("已请求取消。已进入打印的照片仍可能继续出纸，请检查打印机。");
  log("请求取消当前操作");
};
document.addEventListener("keydown", (e) => {
  if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "o") {
    e.preventDefault();
    if (!state.busy) void choose();
  }
});
window.runtime?.EventsOn("choose-photo", () => {
  if (!state.busy) void choose();
});
window.runtime?.EventsOn("print-progress", (text) => {
  log(text.trim());
  const labels = {
    preparing: "准备打印",
    "heating-yellow": "加热黄色",
    "printing-yellow": "打印黄色",
    "heating-magenta": "加热品红",
    "printing-magenta": "打印品红",
    "heating-cyan": "加热青色",
    "printing-cyan": "打印青色",
    "heating-overcoat": "加热保护膜",
    "printing-overcoat": "打印保护膜",
    finishing: "完成中",
  };
  const match = String(text).match(/Printer: ([\w-]+)/);
  if (match) notice(`打印机：${labels[match[1]] || match[1]}。请保持连接。`);
});
if (api) {
  void perform(async () => {
    state.config = await api.Boot();
    $("host").value = state.config.host || "192.168.0.1";
    $("model").value = state.config.model || "CP1500";
    papers();
    notice("准备就绪。先选择一张照片，也可以离线预览。");
    log("工作台已启动");
  });
} else {
  notice("桌面连接尚未就绪，请退出后重新打开应用。", "error");
}
