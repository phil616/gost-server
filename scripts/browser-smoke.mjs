// Real Chromium smoke test against a running development control server.
// Node 24 provides fetch/WebSocket, so no browser automation dependency is needed.
import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { spawn } from "node:child_process";

const origin = process.env.SMOKE_ORIGIN || "http://127.0.0.1:18081";
const chrome =
  process.env.CHROME_BINARY ||
  "C:/Program Files/Google/Chrome/Application/chrome.exe";
const password = process.env.SMOKE_PASSWORD;
if (!password)
  throw Error(
    "SMOKE_PASSWORD is required (use an isolated test administrator)",
  );
const profile = await fs.mkdtemp(path.join(os.tmpdir(), "altasci-browser-"));
const browser = spawn(
  chrome,
  [
    "--headless=new",
    "--disable-gpu",
    "--no-first-run",
    "--remote-debugging-port=0",
    "--remote-debugging-address=127.0.0.1",
    `--user-data-dir=${profile}`,
    "about:blank",
  ],
  { windowsHide: true, stdio: "ignore" },
);
const pause = (ms) => new Promise((r) => setTimeout(r, ms));
let socket;
const pending = new Map();
let next = 0;
const errors = [];
try {
  let port;
  for (let i = 0; i < 100; i++) {
    try {
      port = (
        await fs.readFile(path.join(profile, "DevToolsActivePort"), "utf8")
      ).split("\n")[0];
      break;
    } catch {
      await pause(200);
    }
  }
  if (!port) throw Error("Chromium did not expose its debugging port");
  const pages = await (
    await fetch(`http://127.0.0.1:${port}/json/list`)
  ).json();
  socket = new WebSocket(
    pages.find((p) => p.type === "page").webSocketDebuggerUrl,
  );
  await new Promise((resolve, reject) => {
    socket.addEventListener("open", resolve, { once: true });
    socket.addEventListener("error", reject, { once: true });
  });
  socket.addEventListener("message", (e) => {
    const msg = JSON.parse(e.data);
    if (msg.id) {
      const p = pending.get(msg.id);
      pending.delete(msg.id);
      msg.error
        ? p?.reject(Error(JSON.stringify(msg.error)))
        : p?.resolve(msg.result);
    }
    if (msg.method === "Runtime.exceptionThrown")
      errors.push(
        msg.params.exceptionDetails.exception?.description ||
          msg.params.exceptionDetails.text,
      );
    if (
      msg.method === "Runtime.consoleAPICalled" &&
      msg.params.type === "error"
    )
      errors.push(
        msg.params.args.map((a) => a.value || a.description).join(" "),
      );
  });
  const send = (method, params = {}) =>
    new Promise((resolve, reject) => {
      const id = ++next;
      pending.set(id, { resolve, reject });
      socket.send(JSON.stringify({ id, method, params }));
    });
  const evaluate = async (expression) => {
    const r = await send("Runtime.evaluate", {
      expression,
      awaitPromise: true,
      returnByValue: true,
      userGesture: true,
    });
    if (r.exceptionDetails)
      throw Error(r.exceptionDetails.exception?.description || r.exceptionDetails.text);
    return r.result.value;
  };
  const wait = async (expression) => {
    for (let i = 0; i < 120; i++) {
      if (await evaluate(expression)) return;
      await pause(250);
    }
    const shot = await send("Page.captureScreenshot", { format: "png" });
    const failureScreenshot = process.env.SMOKE_FAILURE_SCREENSHOT ||
      ".local/browser-smoke-failure.png";
    await fs.mkdir(path.dirname(path.resolve(failureScreenshot)), { recursive: true });
    await fs.writeFile(
      failureScreenshot,
      Buffer.from(shot.data, "base64"),
    );
    const controls = await evaluate(
      '[...document.querySelectorAll("button,a")].map(b=>b.textContent.trim()).filter(Boolean)',
    );
    throw Error(
      `Timed out: ${expression}; controls=${JSON.stringify(controls)}; browser errors=${errors.join("; ")}`,
    );
  };
  const clickText = (text) =>
    evaluate(
      `(() => { const b = [...document.querySelectorAll('button,a')].find(b => b.textContent.trim() === ${JSON.stringify(text)}); if (!b) throw Error('Missing control'); b.click(); })()`,
    );
  const fill = (id, value) =>
    evaluate(
      `(() => { const input=document.getElementById(${JSON.stringify(id)}); if (!input) throw Error('Missing input'); Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value').set.call(input,${JSON.stringify(value)}); input.dispatchEvent(new Event('input',{bubbles:true})); input.dispatchEvent(new Event('change',{bubbles:true})); })()`,
    );
  const modalPrimary = () =>
    evaluate(
      `(() => { const modal=[...document.querySelectorAll('.ant-modal')].find(m=>m.getClientRects().length); if (!modal) throw Error('Missing modal'); modal.querySelector('.ant-btn-primary').click(); })()`,
    );
  const closeModal = () =>
    evaluate(
      `(() => { const modal=[...document.querySelectorAll('.ant-modal')].find(m=>m.getClientRects().length); modal.querySelector('.ant-modal-close').click(); })()`,
    );
  const confirmPopover = async () => {
    await wait(
      '!!document.querySelector(".ant-popconfirm-buttons .ant-btn-primary")',
    );
    await evaluate(
      'document.querySelector(".ant-popconfirm-buttons .ant-btn-primary").click()',
    );
  };
  await send("Runtime.enable");
  await send("Page.enable");
  await send("Emulation.setDeviceMetricsOverride", {
    width: 1440,
    height: 1000,
    deviceScaleFactor: 1,
    mobile: false,
  });
  await fs.mkdir(path.join(profile, "downloads"));
  await send("Browser.setDownloadBehavior", {
    behavior: "allow",
    downloadPath: path.join(profile, "downloads"),
  });
  await send("Page.navigate", { url: `${origin}/login` });
  await wait('!!document.getElementById("username")');
  await fill("username", process.env.SMOKE_USERNAME || "smoke-admin");
  await fill("password", password);
  await evaluate('document.querySelector("form").requestSubmit()');
  await wait('document.body.innerText.includes("注册设备")');
  await clickText("注册设备");
  await wait('!!document.getElementById("name")');
  await wait('document.body.innerText.includes("Windows · 已安装 MSI")');
  await wait('document.body.innerText.includes("Get-Service -Name AltasCITunnel")');
  await fill("name", `browser-smoke-${Date.now()}`);
  await clickText("生成注册命令");
  await wait(
    'document.body.innerText.includes("& $agent register --server")',
  );
  const registrationScript = await evaluate(
    'document.querySelector(".ant-modal .code-block").textContent',
  );
  if (
    !registrationScript.includes("Join-Path $env:ProgramFiles 'AltasCITunnel\\altascitunnel.exe'") ||
    !/& \$agent register --server 'wss:\/\/[^']+' --token '[^']+'/.test(registrationScript) ||
    !registrationScript.includes("& $agent status") ||
    !await evaluate('document.body.innerText.includes("client_id 非空") && document.body.innerText.includes("Enrollment accepted")')
  ) throw Error("Windows MSI registration guide is incomplete");
  await send("Browser.grantPermissions", {
    origin,
    permissions: ["clipboardReadWrite", "clipboardSanitizedWrite"],
  });
  await send("Emulation.setFocusEmulationEnabled", { enabled: true });
  await send("Page.bringToFront");
  await evaluate(`window.__registrationCopy = ${JSON.stringify(registrationScript)}`);
  await evaluate('document.querySelector(".ant-modal .ant-typography-copy").click()');
  // Windows clipboard text may use CRLF even though the rendered script uses LF.
  await wait('navigator.clipboard.readText().then(text => text.replace(/\\r\\n/g, "\\n") === window.__registrationCopy)');
  await evaluate('delete window.__registrationCopy');
  await evaluate('[...document.querySelectorAll(".ant-modal [role=tab]")].find(t=>t.textContent.includes("Linux")).click()');
  await wait('document.body.innerText.includes("sudo altascitunnel register --server")');
  await evaluate('[...document.querySelectorAll(".ant-modal [role=tab]")].find(t=>t.textContent.includes("Windows")).click()');
  await wait('document.body.innerText.includes("& $agent register --server")');
  console.log("PASS: Windows MSI guide, complete PowerShell clipboard contents, registration status criteria and Linux tab");
  await evaluate('document.querySelector(".ant-modal-close").click()');
  await clickText("隧道管理");
  await wait('document.body.innerText.includes("新增隧道")');
  if (process.env.SMOKE_MUTATIONS === "1") {
    let name = `browser-tunnel-${Date.now()}`;
    const row = () =>
      `[...document.querySelectorAll('.ant-table-row')].find(r=>r.innerText.includes(${JSON.stringify(name)}))`;
    const rowButton = (text) =>
      evaluate(
        `(() => { const row=${row()}; const button=[...row.querySelectorAll('button')].find(b=>b.textContent.trim()===${JSON.stringify(text)}); button.click(); })()`,
      );
    await clickText("新增隧道");
    await wait('!!document.getElementById("client_id")');
    await fill("name", name);
    await evaluate(
      'document.getElementById("client_id").dispatchEvent(new MouseEvent("mousedown",{bubbles:true}))',
    );
    await wait('!!document.querySelector(".ant-select-item-option-content")');
    await evaluate(
      `(() => { const option=[...document.querySelectorAll('.ant-select-item-option-content')].find(o=>o.textContent.includes(${JSON.stringify(process.env.SMOKE_CLIENT_NAME || "live-daemon")})); if(!option) throw Error('Fixture device not found'); option.click(); })()`,
    );
    await fill("target_port", process.env.SMOKE_TARGET_PORT || "22");
    await modalPrimary();
    await wait(`!!(${row()}) && (${row()}).innerText.includes('已连接')`);
    await rowButton("编辑");
    await wait('!!document.getElementById("target_ip")');
    const edited = `${name}-edited`;
    await fill("name", edited);
    await modalPrimary();
    name = edited;
    await wait(`!!(${row()})`);
    await evaluate(`(${row()}).querySelector('[role="switch"]').click()`);
    await wait(`(${row()})?.innerText.includes('已停用')`);
    await evaluate(`(${row()}).querySelector('[role="switch"]').click()`);
    await wait(`(${row()})?.innerText.includes('已连接')`);
    await rowButton("访问凭据");
    await clickText("发放凭据");
    await wait('document.body.innerText.includes("发放访问凭据")');
    await modalPrimary();
    await wait(
      '[...document.querySelectorAll(".ant-drawer button")].some(b=>b.textContent.trim()==="连接命令")',
    );
    await clickText("连接命令");
    await wait('document.body.innerText.includes("tunnel+wss://")');
    await clickText("下载 YAML");
    let yaml;
    for (let i = 0; i < 100; i++) {
      try {
        yaml = await fs.readFile(
          path.join(profile, "downloads", "gost-visitor.yaml"),
          "utf8",
        );
        break;
      } catch {
        await pause(100);
      }
    }
    if (!yaml?.includes("tunnel") || !yaml.includes("secure: true"))
      throw Error("Visitor YAML download was incomplete");
    await closeModal();
    await clickText("撤销");
    await confirmPopover();
    await wait(
      'document.querySelector(".ant-drawer")?.innerText.includes("已撤销")',
    );
    await evaluate('document.querySelector(".ant-drawer-close").click()');
    await rowButton("删除");
    await confirmPopover();
    await wait(`!(${row()})`);
    console.log(
      "PASS: create/edit/disable/enable tunnel, real Provider Bind, issue/reveal/download/revoke Visitor, delete tunnel",
    );
  }
  await clickText("操作审计");
  await wait('document.body.innerText.includes("enrollment.create")');
  await clickText("边缘状态");
  await wait('document.body.innerText.includes("引擎运行状态")');
  await pause(2200);
  const screenshot = await send("Page.captureScreenshot", { format: "png" });
  const output = process.env.SMOKE_SCREENSHOT || ".local/browser-smoke.png";
  await fs.mkdir(path.dirname(path.resolve(output)), { recursive: true });
  await fs.writeFile(output, Buffer.from(screenshot.data, "base64"));
  if (errors.length) throw Error(`Browser errors: ${errors.join("\n")}`);
  console.log(
    "PASS: login, create enrollment, tunnel navigation, audit, edge status; no browser errors",
  );
  console.log(`Screenshot: ${output}`);
  await clickText("退出");
  await wait('!!document.getElementById("username")');
  await send("Browser.close");
} finally {
  socket?.close();
  browser.kill();
  // Keep the temporary profile only while Chromium is shutting down. This is a
  // directory created by mkdtemp above, never a user-supplied deletion target.
  await pause(500);
  if (
    path.dirname(profile) !== path.resolve(os.tmpdir()) ||
    !path.basename(profile).startsWith("altasci-browser-")
  )
    throw Error("Unexpected temporary profile path");
  await fs
    .rm(profile, {
      recursive: true,
      force: true,
      maxRetries: 5,
      retryDelay: 200,
    })
    .catch(() => {});
}
