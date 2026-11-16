import { contextBridge, ipcRenderer } from "electron";

contextBridge.exposeInMainWorld("superbotDesktop", {
  platform: process.platform,
  versions: process.versions,
  showNotification: (title: string, body: string, url?: string) =>
    ipcRenderer.invoke("superbot:notify", { title, body, url }),
  setSessionToken: (token: string) => ipcRenderer.invoke("superbot:set-token", token),
  getSessionToken: () => ipcRenderer.invoke("superbot:get-token"),
  openExternal: (url: string) => ipcRenderer.invoke("superbot:open-external", url),
  minimizeWindow: () => ipcRenderer.invoke("superbot:window-minimize"),
  toggleMaximizeWindow: () => ipcRenderer.invoke("superbot:window-toggle-maximize"),
  closeWindow: () => ipcRenderer.invoke("superbot:window-close"),
  isWindowMaximized: () => ipcRenderer.invoke("superbot:window-is-maximized"),
  getDesktopSettings: () => ipcRenderer.invoke("superbot:get-desktop-settings"),
  getAppInfo: () => ipcRenderer.invoke("superbot:get-app-info"),
  checkForUpdates: () => ipcRenderer.invoke("superbot:check-for-updates"),
  setDesktopSettings: (patch: { hardwareAcceleration?: boolean; updateChannel?: string }) =>
    ipcRenderer.invoke("superbot:set-desktop-settings", patch),
});
