import {
  app,
  BrowserWindow,
  Menu,
  Notification,
  Tray,
  desktopCapturer,
  globalShortcut,
  ipcMain,
  nativeImage,
  safeStorage,
  session,
  shell,
} from "electron";
import type { MenuItemConstructorOptions } from "electron";
import { autoUpdater } from "electron-updater";
import fs from "node:fs";
import path from "node:path";
import {
  reloadDeviceAgent,
  showDeviceAgentStatus,
  startDeviceAgent,
  stopDeviceAgent,
} from "./deviceAgent";

const WEB_URL = process.env.SUPERBOT_WEB_URL || "http://127.0.0.1:3000/";
const ALLOWED_ORIGIN = new URL(WEB_URL).origin;

type DesktopSettings = {
  hardwareAcceleration: boolean;
  updateChannel: string;
};

function desktopSettingsPath() {
  return path.join(app.getPath("userData"), "desktop-settings.json");
}

function readDesktopSettings(): DesktopSettings {
  try {
    const parsed = JSON.parse(fs.readFileSync(desktopSettingsPath(), "utf8")) as Partial<DesktopSettings>;
    return {
      hardwareAcceleration: parsed.hardwareAcceleration !== false,
      updateChannel: parsed.updateChannel === "beta" ? "beta" : "stable",
    };
  } catch {
    return { hardwareAcceleration: true, updateChannel: "stable" };
  }
}

const initialDesktopSettings = readDesktopSettings();
if (!initialDesktopSettings.hardwareAcceleration) {
  app.disableHardwareAcceleration();
}

// Screen capture (getDisplayMedia) needs a secure context. The self-hosted
// deployment may still be plain HTTP, so treat the configured origin as
// trustworthy inside the desktop client only.
if (WEB_URL.startsWith("http://")) {
  app.commandLine.appendSwitch(
    "unsafely-treat-insecure-origin-as-secure",
    ALLOWED_ORIGIN,
  );
}
const statePath = () => path.join(app.getPath("userData"), "window-state.json");

type WindowState = {
  width: number;
  height: number;
  x?: number;
  y?: number;
  maximized?: boolean;
};

function readState(): WindowState {
  try {
    return JSON.parse(fs.readFileSync(statePath(), "utf8")) as WindowState;
  } catch {
    return { width: 1280, height: 860 };
  }
}

function saveState(win: BrowserWindow) {
  const bounds = win.getBounds();
  fs.writeFileSync(statePath(), JSON.stringify({ ...bounds, maximized: win.isMaximized() }));
}

let mainWindow: BrowserWindow | null = null;
let tray: Tray | null = null;

function createWindow() {
  const state = readState();
  mainWindow = new BrowserWindow({
    width: state.width,
    height: state.height,
    x: state.x,
    y: state.y,
    minWidth: 900,
    minHeight: 600,
    title: "superbot",
    frame: false,
    backgroundColor: "#fcfcfc",
    show: false,
    webPreferences: {
      preload: path.join(__dirname, "../preload/index.js"),
      contextIsolation: true,
      sandbox: true,
      nodeIntegration: false,
      webSecurity: true,
      spellcheck: true,
    },
  });
  if (state.maximized) mainWindow.maximize();
  mainWindow.once("ready-to-show", () => mainWindow?.show());
  mainWindow.on("close", () => {
    if (mainWindow) saveState(mainWindow);
  });
  mainWindow.on("closed", () => {
    mainWindow = null;
  });
  mainWindow.webContents.setWindowOpenHandler(({ url }) => {
    if (url.startsWith("http://") || url.startsWith("https://")) void shell.openExternal(url);
    return { action: "deny" };
  });
  mainWindow.webContents.on("will-navigate", (event, url) => {
    if (!url.startsWith(ALLOWED_ORIGIN) && !url.startsWith("http://127.0.0.1")) {
      event.preventDefault();
      void shell.openExternal(url);
    }
  });
  void mainWindow.loadURL(WEB_URL);
}

function buildMenu() {
  const template: MenuItemConstructorOptions[] = [
    {
      label: "superbot",
      submenu: [
        { role: "about" },
        { type: "separator" },
        { role: "hide" },
        { role: "hideOthers" },
        { role: "unhide" },
        { type: "separator" },
        { role: "quit" },
      ],
    },
    { role: "editMenu" },
    { role: "viewMenu" },
    { role: "windowMenu" },
    {
      label: "帮助",
      submenu: [
        { label: "打开 Web 版", click: () => void shell.openExternal(WEB_URL) },
        { label: "本地代理状态", click: () => void showDeviceAgentStatus() },
        { label: "重新连接本地代理", click: () => reloadDeviceAgent() },
        { type: "separator" },
        { label: "检查更新", click: () => void autoUpdater.checkForUpdatesAndNotify() },
      ],
    },
  ];
  Menu.setApplicationMenu(Menu.buildFromTemplate(template));
}

function createTray() {
  tray = new Tray(nativeImage.createEmpty());
  tray.setToolTip("superbot");
  tray.setContextMenu(
    Menu.buildFromTemplate([
      { label: "显示 superbot", click: () => mainWindow?.show() },
      { label: "隐藏", click: () => mainWindow?.hide() },
      { type: "separator" },
      { label: "退出", click: () => app.quit() },
    ]),
  );
  tray.on("click", () => mainWindow?.show());
}

function handleDeepLink(url: string) {
  try {
    const parsed = new URL(url);
    if (parsed.protocol !== "superbot:") return;
    const target = new URL(WEB_URL);
    for (const [key, value] of parsed.searchParams) target.searchParams.set(key, value);
    if (mainWindow) {
      void mainWindow.loadURL(target.toString());
      mainWindow.show();
    }
  } catch {
    // ignore malformed links
  }
}

app.setAsDefaultProtocolClient("superbot");
const gotLock = app.requestSingleInstanceLock();
if (!gotLock) {
  app.quit();
} else {
  app.on("second-instance", (_event, argv) => {
    const link = argv.find((arg) => arg.startsWith("superbot://"));
    if (link) handleDeepLink(link);
    if (mainWindow) {
      if (mainWindow.isMinimized()) mainWindow.restore();
      mainWindow.show();
    }
  });
  app.whenReady().then(() => {
    session.defaultSession.setDisplayMediaRequestHandler(
      async (_request, callback) => {
        try {
          const sources = await desktopCapturer.getSources({ types: ["screen"] });
          callback(sources[0] ? { video: sources[0], audio: "loopback" } : {});
        } catch {
          callback({});
        }
      },
      { useSystemPicker: false },
    );
    buildMenu();
    createWindow();
    createTray();
    startDeviceAgent();
    globalShortcut.register("CommandOrControl+Shift+Space", () => mainWindow?.show());
    if (app.isPackaged) {
      autoUpdater.autoDownload = true;
      autoUpdater.channel = readDesktopSettings().updateChannel;
      autoUpdater.checkForUpdatesAndNotify().catch(() => undefined);
    }
    app.on("activate", () => {
      if (!mainWindow) createWindow();
    });
  });
}

app.on("window-all-closed", () => {
  if (process.platform !== "darwin") app.quit();
});
app.on("will-quit", () => {
  stopDeviceAgent();
  globalShortcut.unregisterAll();
});

ipcMain.handle(
  "superbot:notify",
  (_event, payload: { title: string; body: string; url?: string }) => {
    if (!Notification.isSupported()) return false;
    const notification = new Notification({ title: payload.title, body: payload.body });
    notification.on("click", () => {
      if (payload.url && mainWindow) {
        const target = new URL(payload.url, WEB_URL).toString();
        if (target.startsWith(ALLOWED_ORIGIN)) void mainWindow.loadURL(target);
        mainWindow.show();
      }
    });
    notification.show();
    return true;
  },
);

ipcMain.handle("superbot:get-desktop-settings", () => readDesktopSettings());
function appVersion(): string {
  try {
    const packagePath = path.join(__dirname, "../../package.json");
    const parsed = JSON.parse(fs.readFileSync(packagePath, "utf8"));
    if (typeof parsed.version === "string" && parsed.version) return parsed.version;
  } catch {
    // fall through to Electron's version
  }
  return app.getVersion();
}

ipcMain.handle("superbot:get-app-info", () => ({
  version: appVersion(),
  packaged: app.isPackaged,
  channel: readDesktopSettings().updateChannel,
  platform: process.platform,
}));
ipcMain.handle("superbot:check-for-updates", async () => {
  const version = appVersion();
  if (!app.isPackaged) {
    return { status: "dev", version };
  }
  try {
    autoUpdater.channel = readDesktopSettings().updateChannel;
    const result = await autoUpdater.checkForUpdates();
    const latest = result?.updateInfo?.version ?? null;
    return {
      status: latest && latest !== version ? "available" : "latest",
      version,
      latest,
    };
  } catch (error) {
    return { status: "error", version, message: String(error) };
  }
});
ipcMain.handle(
  "superbot:set-desktop-settings",
  (_event, patch: Partial<DesktopSettings>) => {
    const previous = readDesktopSettings();
    const next = { ...previous, ...patch };
    fs.mkdirSync(path.dirname(desktopSettingsPath()), { recursive: true });
    fs.writeFileSync(desktopSettingsPath(), JSON.stringify(next, null, 2));
    if (app.isPackaged && next.updateChannel !== previous.updateChannel) {
      autoUpdater.channel = next.updateChannel;
      void autoUpdater.checkForUpdatesAndNotify().catch(() => undefined);
    }
    return next;
  },
);

ipcMain.handle("superbot:set-token", (_event, token: string) => {
  if (!safeStorage.isEncryptionAvailable()) return false;
  fs.writeFileSync(
    path.join(app.getPath("userData"), "session.bin"),
    safeStorage.encryptString(token),
  );
  return true;
});

ipcMain.handle("superbot:get-token", () => {
  try {
    const raw = fs.readFileSync(path.join(app.getPath("userData"), "session.bin"));
    return safeStorage.decryptString(raw);
  } catch {
    return "";
  }
});

ipcMain.handle("superbot:open-external", (_event, url: string) => {
  if (url.startsWith("https://") || url.startsWith("http://")) return shell.openExternal(url);
  return false;
});

ipcMain.handle("superbot:window-minimize", () => mainWindow?.minimize());

ipcMain.handle("superbot:window-toggle-maximize", () => {
  if (!mainWindow) return false;
  if (mainWindow.isMaximized()) mainWindow.unmaximize();
  else mainWindow.maximize();
  return mainWindow.isMaximized();
});

ipcMain.handle("superbot:window-close", () => mainWindow?.close());

ipcMain.handle("superbot:window-is-maximized", () => mainWindow?.isMaximized() ?? false);
