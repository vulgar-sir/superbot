export {};

declare global {
  interface Window {
    superbotDesktop?: {
      platform: string;
      versions: Record<string, string>;
      showNotification: (title: string, body: string, url?: string) => Promise<boolean>;
      setSessionToken: (token: string) => Promise<boolean>;
      getSessionToken: () => Promise<string>;
      openExternal: (url: string) => Promise<unknown>;
      minimizeWindow?: () => Promise<void>;
      toggleMaximizeWindow?: () => Promise<boolean>;
      closeWindow?: () => Promise<void>;
      isWindowMaximized?: () => Promise<boolean>;
      getDesktopSettings?: () => Promise<{ hardwareAcceleration: boolean; updateChannel: string }>;
      getAppInfo?: () => Promise<{ version: string; packaged: boolean; channel: string; platform: string }>;
      checkForUpdates?: () => Promise<{
        status: "dev" | "latest" | "available" | "error";
        version: string;
        latest?: string | null;
        message?: string;
      }>;
      setDesktopSettings?: (patch: { hardwareAcceleration?: boolean; updateChannel?: string }) => Promise<{
        hardwareAcceleration: boolean;
        updateChannel: string;
      }>;
    };
  }
}
