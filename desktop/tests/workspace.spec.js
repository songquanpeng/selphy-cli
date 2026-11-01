import { test, expect } from "@playwright/test";
async function setup(page, options = {}) {
  await page.addInitScript((options) => {
    const image =
      "data:image/svg+xml;base64," +
      btoa(
        '<svg xmlns="http://www.w3.org/2000/svg" width="400" height="600"><rect width="400" height="600" fill="#aec1b4"/><path d="M0 420L210 180l190 240v180H0" fill="#718879"/><path d="M0 470l160-170 240 250v50H0" fill="#bdac87"/><circle cx="300" cy="110" r="40" fill="#eee0bf"/></svg>',
      );
    const config = {
      host: "192.168.1.88",
      model: "CP1500",
      calibrations: {
        "192.168.1.88|CP1500|postcard": {
          offset_x_mm: -0.8,
          offset_y_mm: -0.5,
        },
      },
    };
    window.mock = {
      calls: [],
      printState: options.printState || "idle-after-submission",
      failPreview: false,
      failPrint: false,
      failChoose: false,
      delay: options.delay || 0,
      events: {},
      config,
    };
    const m = window.mock;
    window.runtime = { EventsOn: (n, fn) => (m.events[n] = fn) };
    window.go = {
      main: {
        App: {
          Boot: async () => structuredClone(config),
          ChoosePhoto: async () => {
            m.calls.push(["ChoosePhoto"]);
            if (m.failChoose) throw "照片损坏";
            return options.cancelChoose ? {} : { name: "海边的回忆.jpg" };
          },
          Preview: async (r) => {
            m.calls.push(["Preview", r]);
            await new Promise((resolve) => setTimeout(resolve, m.delay));
            if (m.failPreview) throw "无法排版";
            return {
              id: "preview-" + m.calls.length,
              name: "海边的回忆.jpg",
              full: image,
              guides: image,
              trimmed: image,
              image: {
                width: 1248,
                height: 1872,
                bytes: 800000,
                trim_basis: "saved-calibration",
                offset_x_mm: -0.8,
                offset_y_mm: -0.5,
              },
              paper: { name: r.paper },
            };
          },
          SaveTarget: async (h, model) => {
            m.calls.push(["SaveTarget", h, model]);
            config.host = h;
            config.model = model;
          },
          Calibrate: async (r, x, y) => {
            m.calls.push(["Calibrate", r, x, y]);
            config.calibrations[`${r.host}|${r.model}|${r.paper}`] = {
              offset_x_mm: x,
              offset_y_mm: y,
            };
          },
          Query: async (kind, h, model) => {
            m.calls.push(["Query", kind, h, model]);
            if (options.offline) throw "网络连接超时";
            return {
              data:
                kind === "discover"
                  ? [{ host: "192.168.1.99" }]
                  : kind === "doctor"
                    ? { reachable: true, host: h }
                    : {
                        state: "idle",
                        tray: "cp-postcard",
                        ink: "cp-postcard",
                        error_code: 0,
                        firmware: "1.0.0",
                      },
            };
          },
          Export: async (id) => {
            m.calls.push(["Export", id]);
            return "/tmp/photo.jpg";
          },
          Print: async (id) => {
            m.calls.push(["Print", id]);
            if (m.failPrint) throw "connection lost after upload";
            if (options.pendingPrint)
              await new Promise(
                (resolve, reject) =>
                  (m.cancelPrint = () => reject("context canceled")),
              );
            return { data: { state: m.printState }, log: "Job submitted" };
          },
          Cancel: async () => {
            m.calls.push(["Cancel"]);
            m.cancelPrint?.();
          },
        },
      },
    };
  }, options);
  await page.goto("/");
  await expect(page.locator("#host")).toHaveValue("192.168.1.88");
}
const choose = async (page) => {
  await page.locator("#choose").click();
  await expect(page.locator("#print")).toBeEnabled();
};
test("startup restores printer and calibration without network access", async ({
  page,
}) => {
  await setup(page);
  await expect(page.locator("#print")).toBeDisabled();
  await expect(page.locator("#offset-x")).toHaveValue("-0.8");
  expect(await page.evaluate(() => window.mock.calls)).toEqual([]);
  await expect(page.locator("#choose-empty")).toBeVisible();
});

test("saving a typed address works on the first click", async ({ page }) => {
  await setup(page);
  await choose(page);
  await page.locator("#host").fill("192.168.1.90");
  await page.locator("#save-target").click();
  await expect(page.locator("#notice")).toContainText("预览已更新");
  await expect.poll(() => page.evaluate(() => window.mock.calls.filter(c => c[0] === "SaveTarget").length)).toBe(1);
  expect(await page.evaluate(() => window.mock.calls.find(c => c[0] === "SaveTarget")[1])).toBe("192.168.1.90");
});
test("photo preview, view tabs, layout changes and export", async ({
  page,
}) => {
  await setup(page);
  await choose(page);
  await expect(page.locator("#preview-image")).toBeVisible();
  await page.locator("#view-guides").click();
  await expect(page.locator("#preview-caption")).toContainText("红实线");
  await page.locator('[data-area="bleed"]').click();
  await expect(page.locator("#print")).toBeEnabled();
  await expect(page.locator("#fit")).toHaveValue("cover");
  await page.locator("#rotate").selectOption("90");
  await expect(page.locator("#print")).toBeEnabled();
  const calls = await page.evaluate(() =>
    window.mock.calls.filter((c) => c[0] === "Preview"),
  );
  expect(calls.at(-1)[1]).toMatchObject({
    area: "bleed",
    rotate: 90,
    fit: "cover",
  });
  await page.locator("#export").click();
  await expect(page.locator("#notice")).toContainText("已导出");
});
test("model changes rebuild compatible paper choices and isolate calibration", async ({
  page,
}) => {
  await setup(page);
  await page.locator("#model").selectOption("QX10");
  await expect(page.locator("#paper option")).toHaveCount(1);
  await expect(page.locator("#paper")).toHaveValue("qx-square");
  await expect(page.locator("#offset-x")).toHaveValue("0");
  await page.locator("#model").selectOption("QX20");
  await expect(page.locator("#paper option")).toHaveCount(3);
});
test("unsaved typing invalidates print immediately and does not send a job", async ({
  page,
}) => {
  await setup(page);
  await choose(page);
  await page.locator("#host").fill("192.168.1.90");
  await expect(page.locator("#print")).toBeDisabled();
  expect(
    await page.evaluate(() =>
      window.mock.calls.filter((c) => c[0] === "Print"),
    ),
  ).toHaveLength(0);
});
test("slow render prevents duplicate operations", async ({ page }) => {
  await setup(page, { delay: 350 });
  await page.locator("#choose").click();
  await expect(page.locator("#choose")).toBeDisabled();
  await expect(page.locator("#print")).toBeDisabled();
  await expect(page.locator("#print")).toBeEnabled();
  expect(
    await page.evaluate(() =>
      window.mock.calls.filter((c) => c[0] === "Preview"),
    ),
  ).toHaveLength(1);
});
test("failed render disables print and supports regeneration", async ({
  page,
}) => {
  await setup(page);
  await choose(page);
  await page.evaluate(() => (window.mock.failPreview = true));
  await page.locator("#rotate").selectOption("90");
  await expect(page.locator("#notice")).toContainText("无法排版");
  await expect(page.locator("#print")).toBeDisabled();
  await page.evaluate(() => (window.mock.failPreview = false));
  await page.locator("#regenerate").click();
  await expect(page.locator("#print")).toBeEnabled();
});
test("confirmation cancellation retains printable preview", async ({
  page,
}) => {
  await setup(page, { printState: "cancelled" });
  await choose(page);
  await page.locator("#print").click();
  await expect(page.locator("#notice")).toContainText("尚未提交");
  await expect(page.locator("#print")).toBeEnabled();
});
test("success consumes preview and requires regeneration", async ({ page }) => {
  await setup(page);
  await choose(page);
  await page.locator("#print").click();
  await expect(page.locator("#notice")).toContainText("回到空闲");
  await expect(page.locator("#print")).toBeDisabled();
  await page.locator("#regenerate").click();
  await expect(page.locator("#print")).toBeEnabled();
});
test("ambiguous failure never retries and tells user to inspect output", async ({
  page,
}) => {
  await setup(page);
  await choose(page);
  await page.evaluate(() => (window.mock.failPrint = true));
  await page.locator("#print").click();
  await expect(page.locator("#notice")).toContainText(
    "请先检查打印机和出纸情况",
  );
  await expect(page.locator("#print")).toBeDisabled();
  expect(
    await page.evaluate(() =>
      window.mock.calls.filter((c) => c[0] === "Print"),
    ),
  ).toHaveLength(1);
});
test("discovery selection, status, save and diagnostics", async ({ page }) => {
  await setup(page);
  await page.locator("#discover").click();
  await page.locator("#devices button").click();
  await expect(page.locator("#host")).toHaveValue("192.168.1.99");
  await page.locator("#save-target").click();
  await expect(page.locator("#notice")).toContainText("已保存默认");
  await page.locator("#status").click();
  await expect(page.locator("#connection")).toContainText("空闲");
  await page.locator("#doctor").click();
  await expect(page.locator("#diagnostic")).toBeVisible();
  await page.locator("#close-diagnostic").click();
  await expect(page.locator("#diagnostic")).not.toBeVisible();
});
test("offline status gives actionable error and preview remains usable", async ({
  page,
}) => {
  await setup(page, { offline: true });
  await choose(page);
  await page.locator("#status").click();
  await expect(page.locator("#connection")).toContainText("连接失败");
  await expect(page.locator("#notice")).toContainText("连接打印机超时");
  await expect(page.locator("#print")).toBeEnabled();
});
test("calibration saves per device and paper then refreshes preview", async ({
  page,
}) => {
  await setup(page);
  await choose(page);
  await page.locator("#advanced summary").click();
  await page.locator("#offset-x").fill("-1.2");
  await page.locator("#save-calibration").click();
  await expect(page.locator("#print")).toBeEnabled();
  expect(
    await page.evaluate(
      () => window.mock.calls.find((c) => c[0] === "Calibrate")[2],
    ),
  ).toBe(-1.2);
  await expect(page.locator("#calibration-note")).toContainText("-1.2");
});
test("cancelled file picker and corrupt selection retain existing preview", async ({
  page,
}) => {
  await setup(page, { cancelChoose: true });
  await page.locator("#choose").click();
  await expect(page.locator("#print")).toBeDisabled();
  await expect(page.locator("#empty")).toBeVisible();
});
test("keyboard shortcut and minimum window keep actions accessible", async ({
  page,
}) => {
  await setup(page);
  await page.setViewportSize({ width: 980, height: 680 });
  await page.keyboard.press("Meta+o");
  await expect(page.locator("#print")).toBeEnabled();
  await expect(page.locator("#print")).toBeInViewport();
  await expect(page.locator("#choose")).toBeInViewport();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
});

test("corrupt selection keeps a previous valid preview", async ({ page }) => {
  await setup(page);
  await choose(page);
  await page.evaluate(() => (window.mock.failChoose = true));
  await page.locator("#choose").click();
  await expect(page.locator("#notice")).toContainText("照片损坏");
  await expect(page.locator("#print")).toBeEnabled();
});

test("printing progress and cancellation keep duplicate submission disabled", async ({
  page,
}) => {
  await setup(page, { pendingPrint: true });
  await choose(page);
  await page.locator("#print").click();
  await expect(page.locator("#cancel")).toBeVisible();
  await expect(page.locator("#print")).toBeDisabled();
  await page.evaluate(() =>
    window.mock.events["print-progress"](
      "Printer: printing-yellow; request=0x1",
    ),
  );
  await expect(page.locator("#notice")).toContainText("打印黄色");
  await page.locator("#cancel").click();
  await expect(page.locator("#regenerate")).toBeVisible();
  await expect(page.locator("#print")).toBeDisabled();
  expect(
    await page.evaluate(() =>
      window.mock.calls.filter((c) => c[0] === "Print"),
    ),
  ).toHaveLength(1);
});
