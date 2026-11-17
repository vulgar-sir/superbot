import { app, clipboard, desktopCapturer, dialog, screen } from "electron";
import { exec } from "node:child_process";
import net from "node:net";
import { createHash } from "node:crypto";
import {
  createPrivateKey,
  createPublicKey,
  generateKeyPairSync,
  sign,
  verify,
  type KeyObject,
} from "node:crypto";
import fs from "node:fs";
import path from "node:path";
import { promisify } from "node:util";

const execAsync = promisify(exec);
const POLL_INTERVAL_MS = 2000;
const COMMAND_TIMEOUT_MS = 120_000;
const MAX_OUTPUT_BYTES = 256 * 1024;

type DeviceConfig = {
  serverUrl?: string;
  token?: string;
  privateKey?: string;
  publicKey?: string;
};

type DeviceJob = {
  id: string;
  kind: string;
  command: string;
  created_at: string;
  nonce: string;
  signature?: string;
};

type PollResponse = {
  job?: DeviceJob | null;
};

let running = false;
let tunnelSocket: WebSocket | null = null;
const tunnelSockets = new Map<number, net.Socket>();
let loopPromise: Promise<void> | null = null;
let config: DeviceConfig = {};
let serverPublicKey: KeyObject | null = null;
let devicePrivateKey: KeyObject | null = null;
let devicePublicKeyPEM = "";

function writeConfig() {
  fs.mkdirSync(path.dirname(configPath()), { recursive: true });
  fs.writeFileSync(configPath(), JSON.stringify(config), { mode: 0o600 });
}

function ensureDeviceKeys() {
  if (config.privateKey && config.publicKey) {
    devicePrivateKey = createPrivateKey(config.privateKey);
    devicePublicKeyPEM = config.publicKey;
    return;
  }
  const { privateKey, publicKey } = generateKeyPairSync("ed25519");
  devicePrivateKey = privateKey;
  devicePublicKeyPEM = publicKey.export({ type: "spki", format: "pem" }).toString();
  config.privateKey = privateKey.export({ type: "pkcs8", format: "pem" }).toString();
  config.publicKey = devicePublicKeyPEM;
  writeConfig();
}

async function registerDeviceKey() {
  if (!devicePublicKeyPEM) ensureDeviceKeys();
  const response = await fetch(`${serverBase()}/api/device/register-key`, {
    method: "POST",
    headers: {
      Authorization: `Bearer ${config.token}`,
      "Content-Type": "application/json",
    },
    body: JSON.stringify({ public_key: devicePublicKeyPEM }),
  });
  if (!response.ok) throw new Error(`register-key HTTP ${response.status}`);
}

function signPayload(payload: string) {
  if (!devicePrivateKey) throw new Error("device private key unavailable");
  return sign(null, Buffer.from(payload), devicePrivateKey).toString("base64");
}

function configPath() {
  return path.join(app.getPath("userData"), "device-agent.json");
}

function readConfig(): DeviceConfig {
  const envToken = process.env.SUPERBOT_DEVICE_TOKEN || "";
  const envServer = process.env.SUPERBOT_SERVER_URL || "";
  try {
    const file = JSON.parse(fs.readFileSync(configPath(), "utf8")) as DeviceConfig;
    return {
      serverUrl: envServer || file.serverUrl || "",
      token: envToken || file.token || "",
    };
  } catch {
    return { serverUrl: envServer, token: envToken };
  }
}

function serverBase() {
  return (config.serverUrl || process.env.SUPERBOT_WEB_URL || "http://127.0.0.1:3000").replace(/\/$/, "");
}

async function fetchServerPublicKey(): Promise<KeyObject> {
  const response = await fetch(`${serverBase()}/api/device/pubkey`);
  if (!response.ok) throw new Error(`pubkey HTTP ${response.status}`);
  const data = (await response.json()) as { public_key?: string };
  if (!data.public_key) throw new Error("server public key missing");
  return createPublicKey(data.public_key);
}

async function verifyJob(job: DeviceJob): Promise<boolean> {
  if (!job.signature) return false;
  const payload = Buffer.from(`${job.id}|${job.kind}|${job.command}|${job.nonce}`);
  const signature = Buffer.from(job.signature, "base64");
  if (!serverPublicKey) serverPublicKey = await fetchServerPublicKey();
  if (verify(null, payload, serverPublicKey, signature)) return true;
  // The API process may have restarted and rotated its ephemeral key.
  serverPublicKey = await fetchServerPublicKey();
  return verify(null, payload, serverPublicKey, signature);
}

async function postJSON(url: string, body?: unknown) {
  const response = await fetch(url, {
    method: "POST",
    headers: {
      Authorization: `Bearer ${config.token}`,
      "Content-Type": "application/json",
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (!response.ok) {
    throw new Error(`HTTP ${response.status}`);
  }
  return (await response.json()) as PollResponse;
}

async function approve(command: string): Promise<boolean> {
  // GUI approval is mandatory in packaged builds. The env override exists
  // only for local development and automated smoke tests.
  if (!app.isPackaged && process.env.SUPERBOT_DEVICE_AUTO_APPROVE === "1") {
    return true;
  }
  const result = await dialog.showMessageBox({
    type: "question",
    buttons: ["拒绝", "允许"],
    defaultId: 0,
    cancelId: 0,
    noLink: true,
    title: "superbot 本地命令审批",
    message: "Bot 请求在这台设备上执行命令",
    detail: command.length > 1200 ? `${command.slice(0, 1200)}…` : command,
  });
  return result.response === 1;
}

async function captureScreenPng(): Promise<Buffer> {
  try {
    const display = screen.getPrimaryDisplay();
    const sources = await desktopCapturer.getSources({
      types: ["screen"],
      thumbnailSize: {
        width: Math.round(display.size.width * display.scaleFactor),
        height: Math.round(display.size.height * display.scaleFactor),
      },
    });
    const png = sources[0]?.thumbnail.toPNG() ?? Buffer.alloc(0);
    console.info("[device-agent] desktopCapturer bytes", png.length, sources[0]?.thumbnail.getSize());
    if (png.length > 0) return png;
  } catch (err) {
    console.warn("[device-agent] desktopCapturer failed", err);
  }
  // desktopCapturer can return an empty thumbnail on some Windows/RDP sessions.
  // Fall back to the interactive session's GDI screen capture.
  const target = path.join(app.getPath("temp"), `superbot-screenshot-${Date.now()}.png`);
  const escaped = target.replace(/'/g, "''");
  const script = [
    "Add-Type -AssemblyName System.Windows.Forms,System.Drawing",
    "$bounds=[System.Windows.Forms.Screen]::PrimaryScreen.Bounds",
    "$bmp=New-Object System.Drawing.Bitmap($bounds.Width,$bounds.Height)",
    "$g=[System.Drawing.Graphics]::FromImage($bmp)",
    "$g.CopyFromScreen($bounds.Location,[System.Drawing.Point]::Empty,$bounds.Size)",
    `$bmp.Save('${escaped}',[System.Drawing.Imaging.ImageFormat]::Png)`,
    "$g.Dispose();$bmp.Dispose()",
  ].join(";");
  await execAsync(`powershell -NoProfile -NonInteractive -Command "${script}"`, {
    timeout: 30_000,
    windowsHide: true,
  });
  const png = fs.readFileSync(target);
  fs.unlinkSync(target);
  console.info("[device-agent] GDI screenshot bytes", png.length);
  return png;
}

async function uploadScreenshot(job: DeviceJob) {
  const png = await captureScreenPng();
  if (png.length === 0) throw new Error("屏幕截图内容为空（可能缺少屏幕录制权限）");
  const form = new FormData();
  const hash = createHash("sha256").update(png).digest("hex");
  form.append("file", new Blob([new Uint8Array(png)], { type: "image/png" }), "screenshot.png");
  form.append("nonce", job.nonce);
  form.append("signature", signPayload(`${job.id}|${job.nonce}|${hash}`));
  const response = await fetch(`${serverBase()}/api/device/jobs/${job.id}/artifact`, {
    method: "POST",
    headers: { Authorization: `Bearer ${config.token}` },
    body: form,
  });
  if (!response.ok) throw new Error(`HTTP ${response.status}`);
}

async function runJob(job: DeviceJob) {
  let output = "";
  let error = "";
  try {
    if (!(await verifyJob(job))) {
      throw new Error("任务签名无效，已拒绝执行");
    }
    const labels: Record<string, string> = {
      screenshot: "[截图请求] 允许后会上传当前屏幕截图",
      clipboard_read: "[剪贴板读取] 允许后 Bot 可读取当前剪贴板文本",
      clipboard_write: `[剪贴板写入] 允许后写入：${job.command.slice(0, 200)}`,
      fs_read: `[文件读取] 允许后读取：${job.command}`,
      fs_write: `[文件写入] 允许后写入文件：${job.command.slice(0, 200)}`,
      app_list: "[应用列表] 允许后读取当前可见窗口列表",
      app_open: `[启动应用] 允许后启动：${job.command.slice(0, 200)}`,
      location: "[定位] 允许后通过公网 IP 查询大致位置",
      audio_list: "[音频设备] 允许后读取本机音频设备列表",
      http_request: `[本机请求] 允许后从这台电脑访问：${job.command.slice(0, 300)}`,
    };
    const approved = await approve(labels[job.kind] || job.command);
    if (!approved) {
      error = "用户拒绝了该本地任务";
    } else if (job.kind === "screenshot") {
      await uploadScreenshot(job);
      return;
    } else if (job.kind === "shell") {
      const result = await execAsync(job.command, {
        timeout: COMMAND_TIMEOUT_MS,
        maxBuffer: MAX_OUTPUT_BYTES,
        windowsHide: true,
      });
      output = `${result.stdout || ""}${result.stderr || ""}`;
    } else if (job.kind === "clipboard_read") {
      output = clipboard.readText();
    } else if (job.kind === "clipboard_write") {
      clipboard.writeText(job.command);
      output = "clipboard updated";
    } else if (job.kind === "fs_read") {
      output = fs.readFileSync(job.command, "utf8");
    } else if (job.kind === "fs_write") {
      const payload = JSON.parse(job.command) as { path?: string; content?: string };
      if (!payload.path) throw new Error("path is required");
      fs.writeFileSync(payload.path, payload.content || "", "utf8");
      output = `wrote ${Buffer.byteLength(payload.content || "", "utf8")} bytes`;
    } else if (job.kind === "app_list") {
      const result = await execAsync(
        'powershell -NoProfile -NonInteractive -Command "Get-Process | Where-Object { $_.MainWindowTitle } | Select-Object -First 50 Id,ProcessName,MainWindowTitle | ConvertTo-Json -Compress"',
        { timeout: 30_000, maxBuffer: MAX_OUTPUT_BYTES, windowsHide: true },
      );
      output = result.stdout.trim();
    } else if (job.kind === "app_open") {
      const escaped = job.command.replace(/'/g, "''");
      await execAsync(`powershell -NoProfile -NonInteractive -Command "Start-Process -FilePath '${escaped}'"`, {
        timeout: 30_000,
        windowsHide: true,
      });
      output = `started ${job.command}`;
    } else if (job.kind === "location") {
      let response = await fetch("https://ipapi.co/json/");
      if (!response.ok) response = await fetch("http://ip-api.com/json/");
      if (!response.ok) throw new Error(`location HTTP ${response.status}`);
      output = (await response.text()).slice(0, MAX_OUTPUT_BYTES);
    } else if (job.kind === "audio_list") {
      const result = await execAsync(
        'powershell -NoProfile -NonInteractive -Command "Get-CimInstance Win32_SoundDevice | Select-Object Name,Manufacturer,Status | ConvertTo-Json -Compress"',
        { timeout: 30_000, maxBuffer: MAX_OUTPUT_BYTES, windowsHide: true },
      );
      output = result.stdout.trim();
    } else if (job.kind === "http_request") {
      const payload = JSON.parse(job.command) as {
        url?: string;
        method?: string;
        headers?: Record<string, string>;
        body?: string;
      };
      const target = new URL(String(payload.url || ""));
      if (target.protocol !== "http:" && target.protocol !== "https:") {
        throw new Error("only http/https is allowed");
      }
      const method = (payload.method || "GET").toUpperCase();
      const controller = new AbortController();
      const timer = setTimeout(() => controller.abort(), 30_000);
      try {
        const response = await fetch(target.toString(), {
          method,
          headers: payload.headers || {},
          body: method === "GET" || method === "HEAD" ? undefined : payload.body || "",
          signal: controller.signal,
        });
        const text = await response.text();
        output = JSON.stringify({
          status: response.status,
          headers: Object.fromEntries(response.headers.entries()),
          body: text.slice(0, MAX_OUTPUT_BYTES),
        });
      } finally {
        clearTimeout(timer);
      }
    } else {
      throw new Error(`unsupported job kind: ${job.kind}`);
    }
  } catch (err) {
    error = err instanceof Error ? err.message : String(err);
  }
  const outHash = createHash("sha256").update(output).digest("hex");
  const errHash = createHash("sha256").update(error).digest("hex");
  await postJSON(`${serverBase()}/api/device/jobs/${job.id}/result`, {
    nonce: job.nonce,
    output,
    error,
    signature: signPayload(`${job.id}|${job.nonce}|${outHash}|${errHash}`),
  });
}

async function pollOnce() {
  const data = await postJSON(`${serverBase()}/api/device/poll`);
  if (data.job) {
    await runJob(data.job);
  }
}

async function loop() {
  while (running) {
    try {
      await pollOnce();
    } catch (err) {
      console.error("[device-agent] poll failed", err);
    }
    await new Promise((resolve) => setTimeout(resolve, POLL_INTERVAL_MS));
  }
}

export function startDeviceAgent() {
  config = readConfig();
  console.info("[device-agent] start", { configured: Boolean(config.token), server: serverBase() });
  if (!config.token) return;
  if (running) return;
  running = true;
  loopPromise = (async () => {
    try {
      ensureDeviceKeys();
      await registerDeviceKey();
    } catch (err) {
      console.error("[device-agent] register key failed", err);
    }
    await loop();
  })();
  startTunnel();
}

function sendTunnel(payload: unknown) {
  if (tunnelSocket && tunnelSocket.readyState === 1) {
    tunnelSocket.send(JSON.stringify(payload));
  }
}

async function handleTunnelMessage(raw: unknown) {
  let message: any;
  try {
    message = JSON.parse(String(raw));
  } catch {
    return;
  }
  const id = Number(message.id);
  if (!Number.isFinite(id)) return;
  if (message.type === "open") {
    const target = String(message.target || "");
    const separator = target.lastIndexOf(":");
    if (separator <= 0) {
      sendTunnel({ type: "open_error", id, message: "invalid target" });
      return;
    }
    const host = target.slice(0, separator).replace(/^\[|\]$/g, "");
    const port = Number(target.slice(separator + 1));
    if (!host || !Number.isFinite(port) || port <= 0) {
      sendTunnel({ type: "open_error", id, message: "invalid target" });
      return;
    }
    const approved = await approve(`[流量隧道] 允许通过这台电脑访问 ${host}:${port}`);
    if (!approved) {
      sendTunnel({ type: "open_error", id, message: "用户拒绝了该隧道连接" });
      return;
    }
    const socket = net.connect({ host, port });
    tunnelSockets.set(id, socket);
    socket.on("connect", () => sendTunnel({ type: "open_ok", id }));
    socket.on("data", (data) =>
      sendTunnel({ type: "data", id, data: data.toString("base64") }),
    );
    socket.on("error", (error) => {
      sendTunnel({ type: "open_error", id, message: String(error) });
      tunnelSockets.delete(id);
    });
    socket.on("close", () => {
      sendTunnel({ type: "close", id });
      tunnelSockets.delete(id);
    });
    return;
  }
  const socket = tunnelSockets.get(id);
  if (message.type === "data" && socket) {
    socket.write(Buffer.from(String(message.data || ""), "base64"));
    return;
  }
  if (message.type === "close" && socket) {
    socket.destroy();
    tunnelSockets.delete(id);
  }
}

function startTunnel() {
  if (!config.token || tunnelSocket) return;
  const WebSocketImpl = (globalThis as any).WebSocket;
  if (!WebSocketImpl) {
    console.warn("[device-agent] WebSocket is unavailable; tunnel disabled");
    return;
  }
  const url = `${serverBase().replace(/^http/, "ws")}/api/device/tunnel?token=${encodeURIComponent(config.token)}`;
  const socket = new WebSocketImpl(url);
  tunnelSocket = socket;
  socket.onmessage = (event: MessageEvent) => void handleTunnelMessage(event.data);
  socket.onclose = () => {
    if (tunnelSocket === socket) tunnelSocket = null;
    for (const [id, target] of tunnelSockets) {
      target.destroy();
      tunnelSockets.delete(id);
    }
    if (running) setTimeout(startTunnel, 5000);
  };
  socket.onerror = () => {
    try {
      socket.close();
    } catch {
      // ignore
    }
  };
}

export function stopDeviceAgent() {
  running = false;
  loopPromise = null;
  tunnelSocket?.close();
  tunnelSocket = null;
  for (const [id, socket] of tunnelSockets) {
    socket.destroy();
    tunnelSockets.delete(id);
  }
}

export function reloadDeviceAgent() {
  stopDeviceAgent();
  serverPublicKey = null;
  startDeviceAgent();
}

export function deviceAgentStatus() {
  return {
    running,
    configured: Boolean(config.token),
    serverUrl: serverBase(),
    configPath: configPath(),
  };
}

export async function showDeviceAgentStatus() {
  const status = deviceAgentStatus();
  await dialog.showMessageBox({
    type: "info",
    title: "本地代理",
    message: status.running ? "本地代理运行中" : "本地代理未运行",
    detail: status.configured
      ? `服务器：${status.serverUrl}\n配置文件：${status.configPath}\n\n首次使用请把设备 token 写入配置文件，或在启动前设置 SUPERBOT_DEVICE_TOKEN。`
      : `尚未配置设备 token。\n配置文件：${status.configPath}\n\n请在 Web 端创建设备后，把 token 写入 device-agent.json。`,
  });
}
