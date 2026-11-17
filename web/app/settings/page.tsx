"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import {
  ArrowLeft,
  BarChart3,
  Bell,
  ChevronDown,
  Cpu,
  KeyRound,
  Laptop,
  Loader2,
  LockKeyhole,
  Palette,
  Plus,
  RefreshCw,
  Search,
  Server,
  Settings2,
  ShieldCheck,
  Sparkles,
  Trash2,
  Users,
  Volume2,
  X,
} from "lucide-react";

import { api, ApiError } from "@/lib/api";
import { registerPasskey } from "@/lib/webauthn";
import type {
  Device,
  Model,
  NotificationPreferences,
  PermissionRule,
  Provider,
  UsageData,
  User,
  VaultItem,
  VaultReveal,
  Bot,
} from "@/lib/types";
import { playNotificationSound } from "@/lib/notificationSound";
import { notifyDataSharing } from "@/components/ClientErrorReporter";
import { StatusPill } from "@/components/StatusPill";
import { Modal as UiModal } from "@/components/ui/Modal";
import { Menu } from "@/components/ui/Menu";
import { useToast } from "@/components/ui/Toast";
import { useConfirm } from "@/components/ui/Confirm";
import { useTheme, type AccentName } from "@/lib/theme";
import { useLanguage } from "@/components/LanguageProvider";
import { cn, formatCNY, formatNumber } from "@/lib/format";
import packageInfo from "../../package.json";

const presets = [
  { key: "deepseek", label: "DeepSeek", kind: "openai_compat", base_url: "https://api.deepseek.com" },
  {
    key: "dashscope",
    label: "通义千问",
    kind: "openai_compat",
    base_url: "https://dashscope.aliyuncs.com/compatible-mode/v1",
  },
  { key: "moonshot", label: "Moonshot", kind: "openai_compat", base_url: "https://api.moonshot.cn/v1" },
  { key: "zhipu", label: "智谱", kind: "openai_compat", base_url: "https://open.bigmodel.cn/api/paas/v4" },
  {
    key: "ark",
    label: "火山方舟",
    kind: "openai_compat",
    base_url: "https://ark.cn-beijing.volces.com/api/v3",
  },
  // Anthropic Messages API; the server defaults the base URL when it is empty.
  { key: "anthropic", label: "Anthropic", kind: "anthropic", base_url: "" },
];

const ANTHROPIC_DEFAULT_BASE_URL = "https://api.anthropic.com";

type ProviderForm = {
  type: string;
  preset: string;
  kind: string;
  name: string;
  base_url: string;
  api_key: string;
};

const emptyProviderForm: ProviderForm = {
  type: "llm",
  preset: "",
  kind: "openai_compat",
  name: "",
  base_url: "",
  api_key: "",
};

type ModelForm = {
  name: string;
  context_window: string;
  max_output: string;
  supports_tools: boolean;
  supports_vision: boolean;
  supports_reasoning: boolean;
  price_in: string;
  price_cache: string;
  price_out: string;
};

const emptyModelForm: ModelForm = {
  name: "",
  context_window: "128000",
  max_output: "4096",
  supports_tools: true,
  supports_vision: false,
  supports_reasoning: false,
  price_in: "0",
  price_cache: "0",
  price_out: "0",
};

const SECTION_LABELS: Record<string, string> = {
  appearance: "通用",
  account: "账户安全",
  passkey: "安全密钥",
  vault: "保险库",
  devices: "电脑",
  permissions: "权限",
  usage: "用量与账单",
  updates: "更新",
  providers: "模型与 Provider",
  members: "成员",
};

export function SettingsScreen({
  embedded = false,
  initialSection,
  onClose,
  onPrimaryBotChange,
}: {
  embedded?: boolean;
  initialSection?: string;
  onClose?: () => void;
  onPrimaryBotChange?: (user: User) => void;
}) {
  const router = useRouter();
  const [user, setUser] = useState<User | null>(null);
  const [personalBots, setPersonalBots] = useState<Bot[]>([]);
  const [primarySaving, setPrimarySaving] = useState(false);
  const [providers, setProviders] = useState<Provider[]>([]);
  const [models, setModels] = useState<Record<string, Model[]>>({});
  const [booting, setBooting] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [providerOpen, setProviderOpen] = useState(false);
  const [editingProvider, setEditingProvider] = useState<Provider | null>(null);
  const [providerEditForm, setProviderEditForm] = useState({
    name: "",
    base_url: "",
    api_key: "",
    enabled: true,
  });
  const [providerTesting, setProviderTesting] = useState<string | null>(null);
  const [providerForm, setProviderForm] = useState<ProviderForm>(emptyProviderForm);
  const [modelProvider, setModelProvider] = useState<Provider | null>(null);
  const [modelForm, setModelForm] = useState<ModelForm>(emptyModelForm);
  const [saving, setSaving] = useState(false);
  const [passwordForm, setPasswordForm] = useState({ current: "", next: "", confirm: "" });
  const [passwordSaving, setPasswordSaving] = useState(false);
  const [passwordError, setPasswordError] = useState<string | null>(null);
  const [passwordNotice, setPasswordNotice] = useState<string | null>(null);
  const [vaultItems, setVaultItems] = useState<VaultItem[]>([]);
  const [vaultOpen, setVaultOpen] = useState(false);
  const [editingVaultID, setEditingVaultID] = useState<string | null>(null);
  const [vaultSaving, setVaultSaving] = useState(false);
  const [vaultError, setVaultError] = useState<string | null>(null);
  const [vaultForm, setVaultForm] = useState({
    kind: "login",
    name: "",
    username: "",
    password: "",
    url: "",
    notes: "",
  });
  const [revealed, setRevealed] = useState<VaultReveal | null>(null);
  const [vaultTab, setVaultTab] = useState<"all" | "login" | "payment" | "contact">("all");
  const [vaultQuery, setVaultQuery] = useState("");
  const [devices, setDevices] = useState<Device[]>([]);
  const [deviceOpen, setDeviceOpen] = useState(false);
  const [deviceSaving, setDeviceSaving] = useState(false);
  const [deviceError, setDeviceError] = useState<string | null>(null);
  const [deviceNotice, setDeviceNotice] = useState<string | null>(null);
  const [deviceToken, setDeviceToken] = useState<string | null>(null);
  const [deviceForm, setDeviceForm] = useState({ name: "", platform: "windows" });
  const [usage, setUsage] = useState<UsageData | null>(null);
  const { mode, setMode, accent, setAccent } = useTheme();
  const { t, language, setLanguage } = useLanguage();
  const { toast } = useToast();
  const weeklyBudget = usage?.weekly_budget_micros ?? 0;
  const weeklySpent = usage?.weekly_spent_micros ?? 0;
  const weeklyPercent =
    weeklyBudget > 0 ? Math.min(100, Math.round((weeklySpent / weeklyBudget) * 100)) : 0;
  const contentRef = useRef<HTMLDivElement>(null);
  const { confirm, confirmElement } = useConfirm();
  const [activeSection, setActiveSection] = useState("appearance");
  const [themeMenuAnchor, setThemeMenuAnchor] = useState<{ rect: DOMRect } | null>(null);
  const [accentMenuAnchor, setAccentMenuAnchor] = useState<{ rect: DOMRect } | null>(null);
  const [spellcheck, setSpellcheck] = useState(true);

  useEffect(() => {
    try {
      setSpellcheck(window.localStorage.getItem("superbot-spellcheck") !== "0");
    } catch {
      // ignore
    }
  }, []);
  useEffect(() => {
    document.documentElement.spellcheck = spellcheck;
    try {
      window.localStorage.setItem("superbot-spellcheck", spellcheck ? "1" : "0");
    } catch {
      // ignore
    }
  }, [spellcheck]);

  const [notificationPrefs, setNotificationPrefs] = useState<NotificationPreferences>({
    desktop: true,
    sound: "default",
    volume: 0.6,
    messages: true,
    approvals: true,
    agents: true,
  });
  const [notificationSaving, setNotificationSaving] = useState(false);
  const [pushSupported, setPushSupported] = useState(false);
  const [pushSubscribed, setPushSubscribed] = useState(false);
  const [pushBusy, setPushBusy] = useState(false);
  const [pushMessage, setPushMessage] = useState<string | null>(null);
  const [dataSharing, setDataSharing] = useState(false);
  const [micDevices, setMicDevices] = useState<Array<{ id: string; label: string }>>([]);
  const [selectedMic, setSelectedMic] = useState("");
  const [timezone, setTimezone] = useState("");
  const [timezoneSaving, setTimezoneSaving] = useState(false);
  const [desktopSettings, setDesktopSettings] = useState<{ hardwareAcceleration: boolean; updateChannel: string } | null>(null);
  const [appInfo, setAppInfo] = useState<{ version: string; packaged: boolean; channel: string } | null>(null);
  const [updateCheck, setUpdateCheck] = useState<string | null>(null);
  const [checkingUpdate, setCheckingUpdate] = useState(false);
  const [billing, setBilling] = useState<{ plan: string; credits_micros: number; weekly_budget_micros: number; weekly_spent_micros: number; ledger: Array<{ id: string; kind: string; amount_micros: number; balance_after_micros: number; note: string; created_at: string }> } | null>(null);
  const [creditAmount, setCreditAmount] = useState("");
  const [creditNote, setCreditNote] = useState("");
  const [creditSaving, setCreditSaving] = useState(false);
  const [dataSharingSaving, setDataSharingSaving] = useState(false);

  useEffect(() => {
    let cancelled = false;
    api
      .getNotificationPreferences()
      .then((result) => {
        if (!cancelled) setNotificationPrefs(result.preferences);
      })
      .catch(() => undefined);
    api
      .getDataSharing()
      .then((result) => {
        if (!cancelled) setDataSharing(result.enabled);
      })
      .catch(() => undefined);
    try {
      setSelectedMic(window.localStorage.getItem("superbot-mic-device") ?? "");
      setTimezone(
        window.localStorage.getItem("superbot-timezone") ||
          Intl.DateTimeFormat().resolvedOptions().timeZone ||
          "UTC",
      );
    } catch {
      // ignore
    }
    api
      .getTimezone()
      .then((result) => {
        if (!cancelled && result.timezone) setTimezone(result.timezone);
      })
      .catch(() => undefined);
    navigator.mediaDevices
      ?.enumerateDevices()
      .then((devices) => {
        if (!cancelled) {
          setMicDevices(
            devices
              .filter((device) => device.kind === "audioinput")
              .map((device, index) => ({
                id: device.deviceId,
                label: device.label || `麦克风 ${index + 1}`,
              })),
          );
        }
      })
      .catch(() => undefined);
    api
      .billing()
      .then((result) => {
        if (!cancelled) setBilling(result);
      })
      .catch(() => undefined);
    window.superbotDesktop
      ?.getDesktopSettings?.()
      .then((settings) => {
        if (!cancelled) setDesktopSettings(settings);
      })
      .catch(() => undefined);
    window.superbotDesktop
      ?.getAppInfo?.()
      .then((info) => {
        if (!cancelled) setAppInfo(info);
      })
      .catch(() => undefined);
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      if (typeof navigator === "undefined" || !("serviceWorker" in navigator) || !("PushManager" in window)) {
        if (!cancelled) setPushSupported(false);
        return;
      }
      try {
        const status = await api.pushStatus();
        if (!status.available || !status.public_key) {
          if (!cancelled) setPushSupported(false);
          return;
        }
        const registration = await navigator.serviceWorker.getRegistration();
        const subscription = await registration?.pushManager.getSubscription();
        if (!cancelled) {
          setPushSupported(true);
          setPushSubscribed(Boolean(subscription));
        }
      } catch {
        if (!cancelled) setPushSupported(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  const updateNotificationPrefs = async (patch: Partial<NotificationPreferences>) => {
    const next = { ...notificationPrefs, ...patch };
    setNotificationPrefs(next);
    setNotificationSaving(true);
    try {
      const result = await api.updateNotificationPreferences(next);
      setNotificationPrefs(result.preferences);
      window.dispatchEvent(
        new CustomEvent("superbot:notification-prefs", { detail: result.preferences }),
      );
    } catch (err) {
      setNotificationPrefs(notificationPrefs);
      toast(err instanceof ApiError ? err.message : "保存通知设置失败", "danger");
    } finally {
      setNotificationSaving(false);
    }
  };

  const enablePush = async () => {
    setPushBusy(true);
    setPushMessage(null);
    try {
      const status = await api.pushStatus();
      if (!status.available || !status.public_key) {
        setPushMessage("Web Push 服务未启动");
        return;
      }
      const registration = await navigator.serviceWorker.register("/sw.js");
      await navigator.serviceWorker.ready;
      const existing = await registration.pushManager.getSubscription();
      const subscription =
        existing ??
        (await registration.pushManager.subscribe({
          userVisibleOnly: true,
          applicationServerKey: urlBase64ToUint8Array(status.public_key),
        }));
      const raw = subscription.toJSON();
      await api.subscribePush({
        endpoint: subscription.endpoint,
        keys: { p256dh: raw.keys?.p256dh ?? "", auth: raw.keys?.auth ?? "" },
      });
      setPushSubscribed(true);
      setPushMessage("已开启 Web Push");
    } catch (err) {
      setPushMessage(err instanceof Error ? err.message : "开启 Web Push 失败");
    } finally {
      setPushBusy(false);
    }
  };

  const disablePush = async () => {
    setPushBusy(true);
    setPushMessage(null);
    try {
      const registration = await navigator.serviceWorker.getRegistration();
      const subscription = await registration?.pushManager.getSubscription();
      if (subscription) {
        await api.unsubscribePush(subscription.endpoint).catch(() => undefined);
        await subscription.unsubscribe();
      }
      setPushSubscribed(false);
      setPushMessage("已关闭 Web Push");
    } catch (err) {
      setPushMessage(err instanceof Error ? err.message : "关闭 Web Push 失败");
    } finally {
      setPushBusy(false);
    }
  };

  const testPush = async () => {
    setPushBusy(true);
    setPushMessage(null);
    try {
      const result = await api.testPush();
      setPushMessage(`已发送 ${result.sent} 条测试通知`);
    } catch (err) {
      setPushMessage(err instanceof Error ? err.message : "发送测试通知失败");
    } finally {
      setPushBusy(false);
    }
  };

  const [autoReview, setAutoReview] = useState(true);
  const [alwaysAskRules, setAlwaysAskRules] = useState<PermissionRule[]>([]);
  const [allowRules, setAllowRules] = useState<PermissionRule[]>([]);
  const [ruleDraft, setRuleDraft] = useState<{ kind: "always" | "allow"; text: string } | null>(null);
  const [updateNotice, setUpdateNotice] = useState<string | null>(null);
  const [users, setUsers] = useState<User[]>([]);
  const [userOpen, setUserOpen] = useState(false);
  const [userSaving, setUserSaving] = useState(false);
  const [userError, setUserError] = useState<string | null>(null);
  const [userForm, setUserForm] = useState({ email: "", password: "", display_name: "", role: "member" });
  const [passkeyCount, setPasskeyCount] = useState(0);
  const [passkeyCredentials, setPasskeyCredentials] = useState<Array<{ id: string; name: string; created_at: string }>>([]);
  const [renamingPasskeyID, setRenamingPasskeyID] = useState<string | null>(null);
  const [renamePasskeyValue, setRenamePasskeyValue] = useState("");
  const [passkeyBusy, setPasskeyBusy] = useState(false);
  const [passkeyNotice, setPasskeyNotice] = useState<string | null>(null);

  const loadModels = useCallback(async (nextProviders: Provider[]) => {
    const entries = await Promise.all(
      nextProviders
        .filter((provider) => provider.type === "llm")
        .map(async (provider) => [provider.id, (await api.listModels(provider.id)).models] as const),
    );
    setModels(Object.fromEntries(entries));
  }, []);

  const toggleDataSharing = async () => {
    const next = !dataSharing;
    setDataSharing(next);
    setDataSharingSaving(true);
    try {
      await api.updateDataSharing(next);
      notifyDataSharing(next);
    } catch (err) {
      setDataSharing(!next);
      toast(err instanceof ApiError ? err.message : "保存数据共享设置失败", "danger");
    } finally {
      setDataSharingSaving(false);
    }
  };

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const me = await api.me();
        const [response, vaultResponse, devicesResponse, usageResponse, usersResponse] =
          await Promise.all([
            api.listProviders(),
            api.listVault(),
            api.listDevices(),
            api.getUsage(),
            me.user.role === "admin" ? api.listUsers().catch(() => ({ users: [] })) : Promise.resolve({ users: [] }),
          ]);
        if (cancelled) return;
        setUser(me.user);
        api
          .listBots()
          .then((result) => {
            if (!cancelled) setPersonalBots(result.bots.filter((bot) => bot.kind !== "team"));
          })
          .catch(() => undefined);
        setProviders(response.providers);
        setVaultItems(vaultResponse.items);
        setDevices(devicesResponse.devices);
        setUsage(usageResponse);
        setUsers(usersResponse.users);
        try {
          const keys = await api.webauthnCredentials();
          setPasskeyCount(keys.count);
          setPasskeyCredentials(keys.credentials ?? []);
        } catch {
          setPasskeyCount(0);
          setPasskeyCredentials([]);
        }
        await loadModels(response.providers);
        setBooting(false);
      } catch (err) {
        if (err instanceof ApiError && err.status === 401) {
          router.replace("/login");
          return;
        }
        setError(err instanceof Error ? err.message : "加载失败");
        setBooting(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [loadModels, router]);


  const logout = useCallback(async () => {
    try {
      await api.logout();
    } finally {
      router.replace("/login");
    }
  }, [router]);

  const updatePassword = useCallback(async () => {
    setPasswordError(null);
    setPasswordNotice(null);
    if (passwordForm.next.length < 10) {
      setPasswordError("新密码至少需要 10 个字符");
      return;
    }
    if (passwordForm.next !== passwordForm.confirm) {
      setPasswordError("两次输入的新密码不一致");
      return;
    }
    setPasswordSaving(true);
    try {
      await api.changePassword(passwordForm.current, passwordForm.next);
      setPasswordForm({ current: "", next: "", confirm: "" });
      setPasswordNotice("密码已更新，其他设备上的登录会话已全部退出。");
    } catch (err) {
      setPasswordError(err instanceof ApiError ? err.message : "修改密码失败");
    } finally {
      setPasswordSaving(false);
    }
  }, [passwordForm]);

  const saveVault = useCallback(async () => {
    if (!vaultForm.name.trim()) {
      setVaultError("名称必填");
      return;
    }
    setVaultSaving(true);
    setVaultError(null);
    try {
      const payload = {
        kind: vaultForm.kind,
        name: vaultForm.name.trim(),
        username: vaultForm.username.trim(),
        password: vaultForm.password,
        url: vaultForm.url.trim(),
        notes: vaultForm.notes,
      };
      if (editingVaultID) {
        const result = await api.updateVault(editingVaultID, payload);
        setVaultItems((prev) =>
          prev.map((item) => (item.id === editingVaultID ? result.item : item)),
        );
      } else {
        const result = await api.createVault(payload);
        setVaultItems((prev) => [result.item, ...prev]);
      }
      setVaultOpen(false);
      setEditingVaultID(null);
      setVaultForm({ kind: "login", name: "", username: "", password: "", url: "", notes: "" });
    } catch (err) {
      setVaultError(err instanceof ApiError ? err.message : "保存失败");
    } finally {
      setVaultSaving(false);
    }
  }, [editingVaultID, vaultForm]);

  const removeVault = useCallback(async (item: VaultItem) => {
    const ok = await confirm({
      title: `删除保险库项目「${item.name}」？`,
      description: "删除后无法恢复。",
      confirmLabel: "删除",
      danger: true,
    });
    if (!ok) return;
    try {
      await api.deleteVault(item.id);
      setVaultItems((prev) => prev.filter((entry) => entry.id !== item.id));
      if (revealed?.item.id === item.id) setRevealed(null);
    } catch (err) {
      setVaultError(err instanceof ApiError ? err.message : "删除失败");
    }
  }, [revealed, confirm]);

  const revealVault = useCallback(async (item: VaultItem) => {
    try {
      setRevealed(await api.revealVault(item.id));
    } catch (err) {
      setVaultError(err instanceof ApiError ? err.message : "读取失败");
    }
  }, []);

  const createDevice = useCallback(async () => {
    if (!deviceForm.name.trim()) {
      setDeviceError("设备名称必填");
      return;
    }
    setDeviceSaving(true);
    setDeviceError(null);
    setDeviceToken(null);
    try {
      const result = await api.createDevice(deviceForm.name.trim(), deviceForm.platform);
      setDevices((prev) => [result.device, ...prev]);
      setDeviceToken(result.token);
      setDeviceForm({ name: "", platform: "windows" });
      setDeviceOpen(false);
    } catch (err) {
      setDeviceError(err instanceof ApiError ? err.message : "创建设备失败");
    } finally {
      setDeviceSaving(false);
    }
  }, [deviceForm]);

  const removeDevice = useCallback(async (device: Device) => {
    const ok = await confirm({
      title: `删除设备「${device.name}」？`,
      description: "设备上的守护进程会失效，需要重新配对。",
      confirmLabel: "删除",
      danger: true,
    });
    if (!ok) return;
    try {
      await api.deleteDevice(device.id);
      setDevices((prev) => prev.filter((item) => item.id !== device.id));
    } catch (err) {
      setDeviceError(err instanceof ApiError ? err.message : "删除设备失败");
    }
  }, [confirm]);

  const toggleDevice = useCallback(async (device: Device) => {
    try {
      const result = await api.setDeviceAllowBot(device.id, !device.allow_bot);
      setDevices((prev) => prev.map((item) => (item.id === device.id ? result.device : item)));
    } catch (err) {
      setDeviceError(err instanceof ApiError ? err.message : "更新设备失败");
    }
  }, []);

  const testDevice = useCallback(async (device: Device) => {
    setDeviceNotice(null);
    setDeviceError(null);
    try {
      const command = device.platform === "windows" ? "Write-Output DEVICE_OK" : "echo DEVICE_OK";
      const result = await api.execDevice(device.id, command);
      setDeviceNotice(
        result.error
          ? `测试返回错误：${result.error}`
          : `设备回复：${result.output.trim() || "（空输出）"}`,
      );
    } catch (err) {
      setDeviceError(err instanceof ApiError ? err.message : "设备测试失败");
    }
  }, []);

  const createUser = useCallback(async () => {
    if (!userForm.email.trim() || userForm.password.length < 10) {
      setUserError("邮箱必填，密码至少 10 位");
      return;
    }
    setUserSaving(true);
    setUserError(null);
    try {
      const result = await api.createUser({
        email: userForm.email.trim(),
        password: userForm.password,
        display_name: userForm.display_name.trim(),
        role: userForm.role,
      });
      setUsers((prev) => [...prev, result.user]);
      setUserOpen(false);
      setUserForm({ email: "", password: "", display_name: "", role: "member" });
    } catch (err) {
      setUserError(err instanceof ApiError ? err.message : "创建用户失败");
    } finally {
      setUserSaving(false);
    }
  }, [userForm]);

  const removeUser = useCallback(async (target: User) => {
    const ok = await confirm({
      title: `删除成员「${target.email}」？`,
      description: "该成员及其全部数据会被删除，此操作不可恢复。",
      confirmLabel: "删除",
      danger: true,
    });
    if (!ok) return;
    try {
      await api.deleteUser(target.id);
      setUsers((prev) => prev.filter((item) => item.id !== target.id));
    } catch (err) {
      setUserError(err instanceof ApiError ? err.message : "删除用户失败");
    }
  }, [confirm]);

  const applyPreset = (key: string) => {
    const preset = presets.find((item) => item.key === key);
    if (!preset) return;
    setProviderForm((prev) => ({
      ...prev,
      preset: key,
      type: "llm",
      kind: preset.kind,
      name: preset.label,
      base_url: preset.base_url,
    }));
  };

  const createProvider = useCallback(async () => {
    if (!providerForm.name.trim()) {
      setError("Provider 名称必填");
      return;
    }
    setSaving(true);
    setError(null);
    try {
      const result = await api.createProvider({
        type: providerForm.type,
        kind: providerForm.kind,
        name: providerForm.name.trim(),
        base_url: providerForm.base_url.trim(),
        api_key: providerForm.api_key.trim(),
        enabled: true,
      });
      const next = [...providers, result.provider];
      setProviders(next);
      setModels((prev) => ({ ...prev, [result.provider.id]: [] }));
      setProviderOpen(false);
      setProviderForm(emptyProviderForm);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "创建 Provider 失败");
    } finally {
      setSaving(false);
    }
  }, [providerForm, providers]);

  const createModel = useCallback(async () => {
    if (!modelProvider) return;
    if (!modelForm.name.trim()) {
      setError("模型名称必填");
      return;
    }
    setSaving(true);
    setError(null);
    try {
      const toMicros = (value: string) => Math.max(0, Math.round(Number(value || "0") * 1_000_000));
      const result = await api.createModel(modelProvider.id, {
        name: modelForm.name.trim(),
        context_window: Math.max(0, Number(modelForm.context_window || "0")),
        max_output: Math.max(0, Number(modelForm.max_output || "0")),
        supports_tools: modelForm.supports_tools,
        supports_vision: modelForm.supports_vision,
        supports_reasoning: modelForm.supports_reasoning,
        price_in_micros_per_mtok: toMicros(modelForm.price_in),
        price_cache_micros_per_mtok: toMicros(modelForm.price_cache),
        price_out_micros_per_mtok: toMicros(modelForm.price_out),
      });
      setModels((prev) => ({
        ...prev,
        [modelProvider.id]: [...(prev[modelProvider.id] ?? []), result.model],
      }));
      setModelProvider(null);
      setModelForm(emptyModelForm);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "创建模型失败");
    } finally {
      setSaving(false);
    }
  }, [modelForm, modelProvider]);

  const deleteProvider = useCallback(
    async (provider: Provider) => {
      const ok = await confirm({
        title: `删除 Provider「${provider.name}」？`,
        description: "该 Provider 下的模型会一并删除，使用它的 Bot 需要重新选择模型。",
        confirmLabel: "删除",
        danger: true,
      });
      if (!ok) return;
      try {
        await api.deleteProvider(provider.id);
        setProviders((prev) => prev.filter((item) => item.id !== provider.id));
      } catch (err) {
        setError(err instanceof ApiError ? err.message : "删除失败");
      }
    },
    [confirm],
  );

  const deleteModel = useCallback(async (provider: Provider, model: Model) => {
    const ok = await confirm({
      title: `删除模型「${model.name}」？`,
      description: "使用该模型的 Bot 需要重新选择模型。",
      confirmLabel: "删除",
      danger: true,
    });
    if (!ok) return;
    try {
      await api.deleteModel(provider.id, model.id);
      setModels((prev) => ({
        ...prev,
        [provider.id]: (prev[provider.id] ?? []).filter((item) => item.id !== model.id),
      }));
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "删除失败");
    }
  }, [confirm]);

  useEffect(() => {
    if (booting || !initialSection || !SECTION_LABELS[initialSection]) return;
    setActiveSection(initialSection);
  }, [booting, initialSection]);

  useEffect(() => {
    contentRef.current?.scrollTo({ top: 0 });
    window.dispatchEvent(
      new CustomEvent("superbot:settings-section", { detail: activeSection }),
    );
  }, [activeSection]);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const permissions = await api.getPermissions();
        if (cancelled) return;
        setAutoReview(permissions.auto_review);
        setAlwaysAskRules(permissions.rules.filter((rule) => rule.kind === "always_ask"));
        setAllowRules(permissions.rules.filter((rule) => rule.kind === "allow"));
      } catch {
        // keep defaults when the API is unavailable
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  const addRule = async (kind: "always" | "allow", text: string) => {
    const value = text.trim();
    if (!value) return;
    try {
      const result = await api.createPermissionRule(
        kind === "always" ? "always_ask" : "allow",
        value,
      );
      if (kind === "always") setAlwaysAskRules((prev) => [...prev, result.rule]);
      else setAllowRules((prev) => [...prev, result.rule]);
      setRuleDraft(null);
      setError(null);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "添加权限规则失败");
    }
  };

  const removeRule = async (kind: "always" | "allow", id: string) => {
    try {
      await api.deletePermissionRule(id);
      if (kind === "always") setAlwaysAskRules((prev) => prev.filter((rule) => rule.id !== id));
      else setAllowRules((prev) => prev.filter((rule) => rule.id !== id));
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "删除权限规则失败");
    }
  };

  const toggleAutoReview = async () => {
    const next = !autoReview;
    setAutoReview(next);
    try {
      await api.updatePermissions(next);
    } catch (err) {
      setAutoReview(!next);
      setError(err instanceof ApiError ? err.message : "更新自动审核失败");
    }
  };

  const checkUpdates = () => {
    setUpdateNotice("Web 版随服务器部署更新；桌面端由 Electron 客户端更新通道管理。");
  };

  useEffect(() => {
    const onSection = (event: Event) => {
      const detail = (event as CustomEvent<string>).detail;
      if (detail && SECTION_LABELS[detail]) setActiveSection(detail);
    };
    window.addEventListener("superbot:settings-section", onSection);
    return () => window.removeEventListener("superbot:settings-section", onSection);
  }, []);

  if (booting) {
    return (
      <div className="flex h-screen items-center justify-center">
        <Loader2 className="h-5 w-5 animate-spin text-[var(--sb-text-2)]" />
      </div>
    );
  }

  if (!user) {
    return (
      <div className="flex h-screen items-center justify-center">
        <Link href="/login" className="text-[var(--sb-text-2)]">
          去登录
        </Link>
      </div>
    );
  }

  return (
    <div className={embedded ? "flex h-full w-full overflow-hidden bg-[var(--sb-bg)]" : "flex h-screen w-full overflow-hidden bg-[var(--sb-bg)]"}>
      <aside className="hidden w-[208px] shrink-0 flex-col border-r border-[var(--sb-border)] bg-[var(--sb-sidebar)] md:flex">
        {!embedded ? (
          <div className="px-4 pb-3 pt-4">
            <Link href="/" className="sb-btn w-full justify-start" data-variant="ghost" data-size="sm">
              <ArrowLeft className="h-4 w-4" />
              {t("common.backToChat")}
            </Link>
            <div className="mt-4 px-2 text-[18px] font-semibold tracking-[-0.02em]">{t("nav.settings")}</div>
            <div className="mt-0.5 px-2 text-[12px] text-[var(--sb-text-3)]">{user.email}</div>
          </div>
        ) : (
          <div className="h-3" />
        )}
        <nav className="flex-1 space-y-0.5 overflow-y-auto px-3 py-1">
          <SettingsNavLink id="appearance" icon={<Palette className="h-4 w-4" />} label={t("settings.general")} />
          <SettingsNavLink id="devices" icon={<Laptop className="h-4 w-4" />} label={t("settings.computer")} />
          <SettingsNavLink id="permissions" icon={<ShieldCheck className="h-4 w-4" />} label={t("settings.permissions")} />
          <SettingsNavLink id="vault" icon={<LockKeyhole className="h-4 w-4" />} label={t("settings.vault")} />
          <SettingsNavLink id="usage" icon={<BarChart3 className="h-4 w-4" />} label={t("settings.usage")} />
          <SettingsNavLink id="updates" icon={<RefreshCw className="h-4 w-4" />} label={t("settings.updates")} />
          <div className="my-2 h-px bg-[var(--sb-border)]" />
          <SettingsNavLink id="providers" icon={<Server className="h-4 w-4" />} label={t("settings.models")} />
          {user.role === "admin" && (
            <SettingsNavLink id="members" icon={<Users className="h-4 w-4" />} label={t("settings.members")} />
          )}
        </nav>
      </aside>

      <main className="flex min-w-0 flex-1 flex-col">
        {!embedded && (
          <header className="sb-page-header flex h-[61px] shrink-0 items-center justify-between border-b border-[var(--sb-border)] bg-[var(--sb-bg)]/95 px-6 backdrop-blur">
            <div className="flex items-center gap-2">
              <Link href="/" className="sb-icon-btn md:hidden" aria-label={t("common.backToChat")}>
                <ArrowLeft className="h-4 w-4" />
              </Link>
              <h1 className="text-[17px] font-semibold">{t("nav.settings")}</h1>
              <span className="hidden text-[12px] text-[var(--sb-text-3)] sm:inline">
                {t("settings.subtitle")}
              </span>
            </div>
            <Link href="/" className="sb-icon-btn" aria-label="关闭设置">
              <X className="h-4 w-4" />
            </Link>
          </header>
        )}
        {embedded && (
          <button
            type="button"
            className="sb-icon-btn absolute right-4 top-4 z-20"
            aria-label="关闭设置"
            onClick={onClose}
          >
            <X className="h-4 w-4" />
          </button>
        )}

        <div ref={contentRef} className={cn("flex-1 overflow-y-auto", !embedded && "pt-0")}>
          <div
            className={cn(
              "mx-auto w-full max-w-[820px] px-6 py-8",
              embedded && "!px-9 !pt-10",
            )}
          >
            {embedded && (
              <h1 className="mb-4 text-[17px] font-semibold">
                {SECTION_LABELS[activeSection] ?? "设置"}
              </h1>
            )}
            <select
              className="mb-5 h-9 w-full rounded-lg bg-[var(--sb-hover)] px-3 text-[13px] text-[var(--sb-text)] outline-none md:hidden"
              value={activeSection}
              onChange={(event) => setActiveSection(event.target.value)}
              aria-label="设置分区"
            >
              {Object.entries(SECTION_LABELS)
                .filter(([key]) => key !== "account" && key !== "passkey")
                .filter(([key]) => key !== "members" || user?.role === "admin")
                .map(([key, label]) => (
                  <option key={key} value={key}>
                    {label}
                  </option>
                ))}
            </select>
          {error && (
            <div className="mb-5 rounded-xl border border-danger/25 bg-danger/10 px-4 py-3 text-[12px] text-danger">
              {error}
            </div>
          )}

          <div className={activeSection === "appearance" ? undefined : "hidden"}>
          <div className="mb-2 text-[13px] text-[var(--sb-text-2)]">{t("settings.account")}</div>
          <section
            id="appearance"
            className="mb-6 rounded-2xl border border-[var(--sb-border)] bg-[var(--sb-pane)] p-5"
          >
            {user ? (
              <div className="flex flex-wrap items-center gap-3">
                <span className="flex h-11 w-11 shrink-0 items-center justify-center rounded-full bg-[var(--sb-hover)] text-[15px] font-medium text-[var(--sb-text-2)]">
                  {(user.display_name || user.email).slice(0, 1).toUpperCase()}
                </span>
                <div className="min-w-0 flex-1">
                  <div className="truncate text-[15px] font-medium">
                    {user.display_name || user.email}
                  </div>
                  <div className="truncate text-[13px] text-[var(--sb-text-2)]">{user.email}</div>
                </div>
                <button
                  type="button"
                  onClick={() => void logout()}
                  className="sb-btn"
                  data-variant="secondary"
                  data-size="sm"
                >
                  {t("settings.logout")}
                </button>
              </div>
            ) : (
              <div className="h-11 animate-pulse rounded-lg bg-[var(--sb-hover)]" />
            )}
            {user && (
              <>
                <div className="my-4 h-px bg-[var(--sb-border)]" />
                <div className="flex flex-wrap items-center justify-between gap-3">
                  <div className="min-w-0 flex-1 basis-[260px]">
                    <div className="text-[14px]">{t("settings.primaryBot")}</div>
                    <div className="text-[11.5px] leading-[16px] text-[var(--sb-text-3)]">
                      {personalBots.length > 0 ? t("settings.primaryBotHint") : t("settings.primaryBotEmpty")}
                    </div>
                  </div>
                  <div className="flex items-center gap-2">
                    {primarySaving && <Loader2 className="sb-spin h-3.5 w-3.5 text-[var(--sb-text-3)]" />}
                    <select
                      value={user.primary_bot_id ?? ""}
                      disabled={primarySaving || personalBots.length === 0}
                      onChange={(event) => {
                        const botId = event.target.value;
                        if (!botId || botId === user.primary_bot_id) return;
                        setPrimarySaving(true);
                        api
                          .setPrimaryBot(botId)
                          .then((result) => {
                            setUser(result.user);
                            onPrimaryBotChange?.(result.user);
                            toast(t("sidebar.primarySet", { name: result.primary_bot.name }), "success");
                          })
                          .catch((err) => {
                            toast(err instanceof ApiError ? err.message : t("sidebar.primaryFailed"), "danger");
                          })
                          .finally(() => setPrimarySaving(false));
                      }}
                      className="h-9 max-w-[210px] rounded-lg bg-[var(--sb-hover)] px-3 text-[13px] text-[var(--sb-text)] outline-none"
                      aria-label={t("settings.primaryBot")}
                    >
                      {!personalBots.some((bot) => bot.id === user.primary_bot_id) && (
                        <option value="">{t("settings.primaryBotNone")}</option>
                      )}
                      {personalBots.map((bot) => (
                        <option key={bot.id} value={bot.id}>{bot.name}</option>
                      ))}
                    </select>
                  </div>
                </div>
              </>
            )}
          </section>

          <div className="mb-2 text-[13px] text-[var(--sb-text-2)]">{t("settings.appearance")}</div>
          <section className="mb-6 overflow-hidden rounded-2xl border border-[var(--sb-border)] bg-[var(--sb-pane)]">
            <div className="flex h-14 items-center justify-between gap-4 px-5">
              <span className="text-[14px]">{t("settings.theme")}</span>
              <button
                type="button"
                onClick={(event) =>
                  setThemeMenuAnchor({ rect: event.currentTarget.getBoundingClientRect() })
                }
                className="flex h-9 min-w-[132px] items-center justify-between gap-2 rounded-lg bg-[var(--sb-hover)] px-3 text-[13px] text-[var(--sb-text)]"
                aria-label={t("settings.theme")}
              >
                {mode === "system" ? t("settings.themeSystem") : mode === "light" ? t("settings.themeLight") : t("settings.themeDark")}
                <ChevronDown className="h-3.5 w-3.5 text-[var(--sb-text-3)]" />
              </button>
            </div>
            <div className="mx-5 h-px bg-[var(--sb-border)]" />
            <div className="flex h-14 items-center justify-between gap-4 px-5">
              <div>
                <div className="text-[14px]">{t("settings.language")}</div>
                <div className="text-[11.5px] text-[var(--sb-text-3)]">{t("settings.languageHint")}</div>
              </div>
              <select
                value={language}
                onChange={(event) => setLanguage(event.target.value as "zh" | "en")}
                className="h-9 min-w-[132px] rounded-lg bg-[var(--sb-hover)] px-3 text-[13px] text-[var(--sb-text)] outline-none"
                aria-label={t("settings.language")}
              >
                <option value="zh">{t("settings.languageChinese")}</option>
                <option value="en">{t("settings.languageEnglish")}</option>
              </select>
            </div>
            <div className="mx-5 h-px bg-[var(--sb-border)]" />
            <div className="flex h-14 items-center justify-between gap-4 px-5">
              <div>
                <div className="text-[14px]">{t("settings.timezone")}</div>
                <div className="text-[11.5px] text-[var(--sb-text-3)]">{t("settings.timezoneHint")}</div>
              </div>
              <select
                value={timezone}
                disabled={timezoneSaving}
                onChange={(event) => {
                  const value = event.target.value;
                  setTimezone(value);
                  try {
                    window.localStorage.setItem("superbot-timezone", value);
                  } catch {
                    // ignore
                  }
                  setTimezoneSaving(true);
                  api
                    .updateTimezone(value)
                    .catch(() => undefined)
                    .finally(() => setTimezoneSaving(false));
                }}
                className="h-9 max-w-[210px] rounded-lg bg-[var(--sb-hover)] px-3 text-[13px] text-[var(--sb-text)] outline-none"
                aria-label="时区"
              >
                {(() => {
                  let zones: string[] = [];
                  try {
                    zones = (
                      Intl as unknown as { supportedValuesOf?: (key: string) => string[] }
                    ).supportedValuesOf?.("timeZone") ?? [];
                  } catch {
                    zones = [];
                  }
                  if (zones.length === 0) zones = [timezone, "Asia/Shanghai", "UTC"];
                  if (!zones.includes(timezone)) zones = [timezone, ...zones];
                  return zones.map((zone) => (
                    <option key={zone} value={zone}>{zone}</option>
                  ));
                })()}
              </select>
            </div>
            <div className="mx-5 h-px bg-[var(--sb-border)]" />
            <div className="flex h-14 items-center justify-between gap-4 px-5">
              <div>
                <div className="text-[14px]">{t("settings.microphone")}</div>
                <div className="text-[11.5px] text-[var(--sb-text-3)]">{t("settings.microphoneHint")}</div>
              </div>
              <select
                value={selectedMic}
                onChange={(event) => {
                  const value = event.target.value;
                  setSelectedMic(value);
                  try {
                    if (value) window.localStorage.setItem("superbot-mic-device", value);
                    else window.localStorage.removeItem("superbot-mic-device");
                  } catch {
                    // ignore
                  }
                }}
                className="h-9 max-w-[210px] rounded-lg bg-[var(--sb-hover)] px-3 text-[13px] text-[var(--sb-text)] outline-none"
                aria-label="麦克风"
              >
                <option value="">{t("settings.systemDefault")}</option>
                {micDevices.map((device) => (
                  <option key={device.id} value={device.id}>{device.label}</option>
                ))}
              </select>
            </div>
            <div className="mx-5 h-px bg-[var(--sb-border)]" />
            <div className="flex h-14 items-center justify-between gap-4 px-5">
              <span className="text-[14px]">{t("settings.accent")}</span>
              <button
                type="button"
                onClick={(event) =>
                  setAccentMenuAnchor({ rect: event.currentTarget.getBoundingClientRect() })
                }
                className="flex h-9 min-w-[132px] items-center gap-2 rounded-lg bg-[var(--sb-hover)] px-3 text-[13px] text-[var(--sb-text)]"
                aria-label={t("settings.accent")}
              >
                <span
                  className="h-3.5 w-3.5 rounded-full"
                  style={{
                    background:
                      accent === "green"
                        ? "#0d9655"
                        : accent === "blue"
                          ? "#1680f3"
                          : accent === "violet"
                            ? "#8351e4"
                            : accent === "orange"
                              ? "#ee620d"
                              : "#1c1c1c",
                  }}
                />
                <span className="flex-1 text-left">
                  {accent === "green"
                    ? t("settings.botColor")
                    : accent === "blue"
                      ? "蓝色"
                      : accent === "violet"
                        ? "紫色"
                        : accent === "orange"
                          ? "橙色"
                          : "黑色"}
                </span>
                <ChevronDown className="h-3.5 w-3.5 text-[var(--sb-text-3)]" />
              </button>
            </div>
            <div className="mx-5 h-px bg-[var(--sb-border)]" />
            <div className="flex h-14 items-center justify-between gap-4 px-5">
              <span className="text-[14px]">{t("settings.spellcheck")}</span>
              <button
                type="button"
                className="sb-switch"
                data-on={spellcheck ? "true" : undefined}
                data-tone="blue"
                aria-label="输入时检查拼写"
                onClick={() => setSpellcheck((prev) => !prev)}
              />
            </div>
            {desktopSettings && (
              <>
                <div className="mx-5 h-px bg-[var(--sb-border)]" />
                <div className="flex h-14 items-center justify-between gap-4 px-5">
                  <div>
                    <div className="text-[14px]">{t("settings.hardwareAcceleration")}</div>
                    <div className="text-[11.5px] text-[var(--sb-text-3)]">{t("settings.hardwareHint")}</div>
                  </div>
                  <button
                    type="button"
                    className="sb-switch"
                    data-on={desktopSettings.hardwareAcceleration ? "true" : undefined}
                    data-tone="blue"
                    aria-label="硬件加速"
                    onClick={() => {
                      const next = {
                        ...desktopSettings,
                        hardwareAcceleration: !desktopSettings.hardwareAcceleration,
                      };
                      setDesktopSettings(next);
                      void window.superbotDesktop
                        ?.setDesktopSettings?.({ hardwareAcceleration: next.hardwareAcceleration })
                        .then((saved) => setDesktopSettings(saved))
                        .catch(() => undefined);
                    }}
                  />
                </div>
                <div className="mx-5 h-px bg-[var(--sb-border)]" />
                <div className="flex min-h-[56px] items-center justify-between gap-4 px-5 py-2">
                  <div>
                    <div className="text-[14px]">{t("settings.currentVersion")}</div>
                    <div className="text-[11.5px] text-[var(--sb-text-3)]">
                      {appInfo
                        ? `v${appInfo.version} · ${appInfo.packaged ? t("settings.installed") : t("settings.devMode")}`
                        : "读取中…"}
                      {updateCheck ? ` · ${updateCheck}` : ""}
                    </div>
                  </div>
                  <button
                    type="button"
                    disabled={checkingUpdate}
                    onClick={async () => {
                      setCheckingUpdate(true);
                      setUpdateCheck(null);
                      try {
                        const result = await window.superbotDesktop?.checkForUpdates?.();
                        if (!result) {
                          setUpdateCheck("仅桌面客户端支持");
                        } else if (result.status === "dev") {
                          setUpdateCheck("开发模式无法检查更新");
                        } else if (result.status === "latest") {
                          setUpdateCheck(`已是最新 v${result.version}`);
                        } else if (result.status === "available") {
                          setUpdateCheck(`发现新版本 v${result.latest}`);
                        } else {
                          setUpdateCheck(`检查失败：${result.message ?? "未知错误"}`);
                        }
                      } finally {
                        setCheckingUpdate(false);
                      }
                    }}
                    className="h-9 rounded-lg bg-[var(--sb-hover)] px-3 text-[12px] text-[var(--sb-text)] disabled:opacity-50"
                  >
                    {checkingUpdate ? "…" : t("settings.checkUpdates")}
                  </button>
                </div>
                <div className="mx-5 h-px bg-[var(--sb-border)]" />
                <div className="flex h-14 items-center justify-between gap-4 px-5">
                  <span className="text-[14px]">{t("settings.updateChannel")}</span>
                  <select
                    value={desktopSettings.updateChannel}
                    onChange={(event) => {
                      const next = { ...desktopSettings, updateChannel: event.target.value };
                      setDesktopSettings(next);
                      void window.superbotDesktop
                        ?.setDesktopSettings?.({ updateChannel: next.updateChannel })
                        .then((saved) => setDesktopSettings(saved))
                        .catch(() => undefined);
                    }}
                    className="h-9 min-w-[132px] rounded-lg bg-[var(--sb-hover)] px-3 text-[13px] text-[var(--sb-text)] outline-none"
                    aria-label="更新通道"
                  >
                    <option value="stable">{t("settings.channelStable")}</option>
                    <option value="beta">{t("settings.channelBeta")}</option>
                  </select>
                </div>
              </>
            )}
          </section>

          <div className="mb-2 flex items-center gap-1.5 text-[13px] text-[var(--sb-text-2)]">
            <Bell className="h-3.5 w-3.5" />
            {t("settings.notifications")}
          </div>
          <section className="mb-6 overflow-hidden rounded-2xl border border-[var(--sb-border)] bg-[var(--sb-pane)]">
            <div className="flex h-14 items-center justify-between gap-4 px-5">
              <div>
                <div className="text-[14px]">{t("notif.desktop")}</div>
                <div className="text-[11.5px] text-[var(--sb-text-3)]">{t("notif.desktopDesc")}</div>
              </div>
              <button
                type="button"
                className="sb-switch"
                data-on={notificationPrefs.desktop ? "true" : undefined}
                data-tone="blue"
                aria-label="桌面通知"
                disabled={notificationSaving}
                onClick={() => void updateNotificationPrefs({ desktop: !notificationPrefs.desktop })}
              />
            </div>
            <div className="mx-5 h-px bg-[var(--sb-border)]" />
            <div className="flex min-h-[56px] items-center justify-between gap-4 px-5 py-2">
              <div className="min-w-0">
                <div className="text-[14px]">Web Push</div>
                <div className="text-[11.5px] text-[var(--sb-text-3)]">
                  {pushSupported
                    ? t("settings.pushHint")
                    : t("settings.pushUnsupported")}
                </div>
                {pushMessage && <div className="text-[11px] text-[var(--sb-text-3)]">{pushMessage}</div>}
              </div>
              <div className="flex shrink-0 items-center gap-2">
                {pushSubscribed && (
                  <button
                    type="button"
                    onClick={() => void testPush()}
                    disabled={pushBusy}
                    className="h-9 rounded-lg bg-[var(--sb-hover)] px-3 text-[12px] text-[var(--sb-text)] disabled:opacity-50"
                  >
                    {t("settings.sendTest")}
                  </button>
                )}
                <button
                  type="button"
                  className="sb-switch"
                  data-on={pushSubscribed ? "true" : undefined}
                  data-tone="blue"
                  aria-label="Web Push"
                  disabled={pushBusy || !pushSupported}
                  onClick={() => void (pushSubscribed ? disablePush() : enablePush())}
                />
              </div>
            </div>
            <div className="mx-5 h-px bg-[var(--sb-border)]" />
            <div className="flex h-14 items-center justify-between gap-4 px-5">
              <span className="text-[14px]">{t("notif.sound")}</span>
              <div className="flex items-center gap-2">
                <button
                  type="button"
                  onClick={() => playNotificationSound(notificationPrefs.sound, notificationPrefs.volume)}
                  className="flex h-9 items-center gap-1.5 rounded-lg px-2.5 text-[12px] text-[var(--sb-text-2)] transition hover:bg-[var(--sb-hover)] hover:text-[var(--sb-text)]"
                  aria-label="试听通知声音"
                >
                  <Volume2 className="h-3.5 w-3.5" />
                  {t("notif.preview")}
                </button>
                <select
                  value={notificationPrefs.sound}
                  disabled={notificationSaving}
                  onChange={(event) =>
                    void updateNotificationPrefs({
                      sound: event.target.value as NotificationPreferences["sound"],
                    })
                  }
                  className="h-9 min-w-[104px] rounded-lg bg-[var(--sb-hover)] px-3 text-[13px] text-[var(--sb-text)] outline-none"
                  aria-label="通知声音"
                >
                  <option value="default">{t("sound.default")}</option>
                  <option value="chime">{t("sound.chime")}</option>
                  <option value="soft">{t("sound.soft")}</option>
                  <option value="none">{t("sound.none")}</option>
                </select>
              </div>
            </div>
            <div className="mx-5 h-px bg-[var(--sb-border)]" />
            <div className="flex h-14 items-center justify-between gap-4 px-5">
              <span className="text-[14px]">{t("notif.volume")}</span>
              <div className="flex items-center gap-3">
                <input
                  type="range"
                  min={0}
                  max={100}
                  value={Math.round(notificationPrefs.volume * 100)}
                  disabled={notificationSaving || notificationPrefs.sound === "none"}
                  onChange={(event) =>
                    setNotificationPrefs((prev) => ({
                      ...prev,
                      volume: Number(event.target.value) / 100,
                    }))
                  }
                  onPointerUp={(event) =>
                    void updateNotificationPrefs({
                      volume: Number((event.target as HTMLInputElement).value) / 100,
                    })
                  }
                  onKeyUp={(event) =>
                    void updateNotificationPrefs({
                      volume: Number((event.target as HTMLInputElement).value) / 100,
                    })
                  }
                  className="w-36 accent-[var(--sb-text)]"
                  aria-label="通知音量"
                />
                <span className="w-9 text-right text-[12px] text-[var(--sb-text-3)]">
                  {Math.round(notificationPrefs.volume * 100)}%
                </span>
              </div>
            </div>
            <div className="mx-5 h-px bg-[var(--sb-border)]" />
            <div className="flex h-14 items-center justify-between gap-4 px-5">
              <span className="text-[14px]">{t("notif.messages")}</span>
              <button
                type="button"
                className="sb-switch"
                data-on={notificationPrefs.messages ? "true" : undefined}
                data-tone="blue"
                aria-label="消息通知"
                disabled={notificationSaving}
                onClick={() => void updateNotificationPrefs({ messages: !notificationPrefs.messages })}
              />
            </div>
            <div className="mx-5 h-px bg-[var(--sb-border)]" />
            <div className="flex h-14 items-center justify-between gap-4 px-5">
              <span className="text-[14px]">{t("notif.approvals")}</span>
              <button
                type="button"
                className="sb-switch"
                data-on={notificationPrefs.approvals ? "true" : undefined}
                data-tone="blue"
                aria-label="审批通知"
                disabled={notificationSaving}
                onClick={() => void updateNotificationPrefs({ approvals: !notificationPrefs.approvals })}
              />
            </div>
            <div className="mx-5 h-px bg-[var(--sb-border)]" />
            <div className="flex h-14 items-center justify-between gap-4 px-5">
              <span className="text-[14px]">{t("notif.agents")}</span>
              <button
                type="button"
                className="sb-switch"
                data-on={notificationPrefs.agents ? "true" : undefined}
                data-tone="blue"
                aria-label="子代理与动态通知"
                disabled={notificationSaving}
                onClick={() => void updateNotificationPrefs({ agents: !notificationPrefs.agents })}
              />
            </div>
          </section>

          <div className="mb-2 text-[13px] text-[var(--sb-text-2)]">{t("settings.dataSharing")}</div>
          <section className="mb-6 overflow-hidden rounded-2xl border border-[var(--sb-border)] bg-[var(--sb-pane)]">
            <div className="flex items-center justify-between gap-4 px-5 py-4">
              <div className="min-w-0">
                <div className="text-[14px]">{t("dataSharing.title")}</div>
                <div className="mt-0.5 text-[11.5px] leading-[17px] text-[var(--sb-text-3)]">
                  {t("dataSharing.desc")}
                </div>
              </div>
              <button
                type="button"
                className="sb-switch shrink-0"
                data-on={dataSharing ? "true" : undefined}
                data-tone="blue"
                aria-label="匿名错误报告"
                disabled={dataSharingSaving}
                onClick={() => void toggleDataSharing()}
              />
            </div>
          </section>

          <div className="mb-2 text-[13px] text-[var(--sb-text-2)]">{t("settings.accountSecurity")}</div>
          <section id="account" className="mb-6 rounded-2xl border border-[var(--sb-border)] bg-[var(--sb-pane)] p-5">
            <h2 className="text-[14px] font-medium text-[var(--sb-text)]">{t("settings.changePassword")}</h2>
            <p className="mt-1 text-[12px] leading-[18px] text-[var(--sb-text-2)]">
              {t("settings.passwordNote")}
            </p>
            <div className="mt-4 grid gap-3 md:grid-cols-3">
              <Field label={t("settings.currentPassword")}>
                <input
                  className="field"
                  type="password"
                  autoComplete="current-password"
                  value={passwordForm.current}
                  onChange={(event) =>
                    setPasswordForm((prev) => ({ ...prev, current: event.target.value }))
                  }
                />
              </Field>
              <Field label={t("settings.newPassword")}>
                <input
                  className="field"
                  type="password"
                  autoComplete="new-password"
                  value={passwordForm.next}
                  onChange={(event) =>
                    setPasswordForm((prev) => ({ ...prev, next: event.target.value }))
                  }
                />
              </Field>
              <Field label={t("settings.confirmPassword")}>
                <input
                  className="field"
                  type="password"
                  autoComplete="new-password"
                  value={passwordForm.confirm}
                  onChange={(event) =>
                    setPasswordForm((prev) => ({ ...prev, confirm: event.target.value }))
                  }
                />
              </Field>
            </div>
            {passwordError && (
              <div className="mt-3 rounded-lg border border-danger/25 bg-danger/10 px-3 py-2 text-[11.5px] text-danger">
                {passwordError}
              </div>
            )}
            {passwordNotice && (
              <div className="mt-3 rounded-lg border border-mint/25 bg-mint/10 px-3 py-2 text-[11.5px] text-[var(--sb-text-2)]">
                {passwordNotice}
              </div>
            )}
            <div className="mt-4 flex justify-end">
              <button
                type="button"
                onClick={updatePassword}
                disabled={passwordSaving}
                className="flex items-center gap-1.5 rounded-lg border border-mint/40 bg-mint px-3.5 py-2 text-[11.5px] font-semibold text-ink disabled:opacity-60"
              >
                {passwordSaving && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
                {t("settings.updatePassword")}
              </button>
            </div>
          </section>

          <div className="mb-2 text-[13px] text-[var(--sb-text-2)]">{t("settings.signInMethods")}</div>
          <section id="passkey" className="mb-6 rounded-2xl border border-[var(--sb-border)] bg-[var(--sb-pane)] p-5">
            <div className="flex flex-wrap items-start justify-between gap-3">
              <div>
                <h2 className="text-[14px] font-medium text-[var(--sb-text)]">{t("settings.passkey")}</h2>
                <p className="mt-1 text-[12px] leading-[18px] text-[var(--sb-text-2)]">
                  {t("settings.passkeyNote")}
                </p>
              </div>
              <div className="flex items-center gap-3">
                <span className="font-mono text-[9px] uppercase tracking-[0.12em] text-muted/50">
                  {passkeyCount} keys
                </span>
                <button
                  type="button"
                  disabled={passkeyBusy}
                  onClick={async () => {
                    setPasskeyBusy(true);
                    setPasskeyNotice(null);
                    try {
                      await registerPasskey();
                      const keys = await api.webauthnCredentials();
                      setPasskeyCount(keys.count);
                      setPasskeyCredentials(keys.credentials ?? []);
                      setPasskeyNotice("安全密钥已注册。");
                    } catch (err) {
                      setPasskeyNotice(err instanceof Error ? err.message : "注册失败");
                    } finally {
                      setPasskeyBusy(false);
                    }
                  }}
                  className="sb-btn"
                  data-variant="secondary"
                  data-size="sm"
                >
                  {passkeyBusy && <Loader2 className="h-3 w-3 animate-spin" />}
                  {t("settings.registerPasskey")}
                </button>
              </div>
            </div>
            {passkeyCredentials.length > 0 && (
              <div className="mt-3 space-y-1.5">
                {passkeyCredentials.map((credential) => (
                  <div key={credential.id} className="flex items-center gap-2 rounded-lg border border-[var(--sb-border)] px-3 py-2">
                    {renamingPasskeyID === credential.id ? (
                      <>
                        <input
                          value={renamePasskeyValue}
                          onChange={(event) => setRenamePasskeyValue(event.target.value)}
                          className="field h-8 flex-1"
                          placeholder="安全密钥名称"
                        />
                        <button
                          type="button"
                          className="sb-btn" data-variant="primary" data-size="sm"
                          onClick={async () => {
                            try {
                              const result = await api.renameWebAuthnCredential(credential.id, renamePasskeyValue.trim());
                              setPasskeyCredentials((prev) => prev.map((item) => (item.id === credential.id ? result.credential : item)));
                              setRenamingPasskeyID(null);
                            } catch (err) {
                              toast(err instanceof ApiError ? err.message : "重命名失败", "danger");
                            }
                          }}
                        >
                          保存
                        </button>
                        <button type="button" className="sb-btn" data-variant="secondary" data-size="sm" onClick={() => setRenamingPasskeyID(null)}>
                          取消
                        </button>
                      </>
                    ) : (
                      <>
                        <div className="min-w-0 flex-1">
                          <div className="truncate text-[12.5px]">{credential.name || "安全密钥"}</div>
                          <div className="text-[10.5px] text-[var(--sb-text-3)]">
                            {new Date(credential.created_at).toLocaleString("zh-CN")}
                          </div>
                        </div>
                        <button
                          type="button"
                          className="text-[11.5px] text-[var(--sb-text-2)] hover:text-[var(--sb-text)]"
                          onClick={() => {
                            setRenamingPasskeyID(credential.id);
                            setRenamePasskeyValue(credential.name || "");
                          }}
                        >
                          重命名
                        </button>
                        <button
                          type="button"
                          className="text-[11.5px] text-[var(--sb-text-2)] hover:text-[var(--sb-danger)]"
                          onClick={async () => {
                            const ok = await confirm({
                              title: "删除这个安全密钥？",
                              description: "删除后该密钥将无法用于登录；其他登录方式不受影响。",
                              confirmLabel: "删除",
                              danger: true,
                            });
                            if (!ok) return;
                            try {
                              await api.deleteWebAuthnCredential(credential.id);
                              setPasskeyCredentials((prev) => prev.filter((item) => item.id !== credential.id));
                              setPasskeyCount((prev) => Math.max(0, prev - 1));
                            } catch (err) {
                              toast(err instanceof ApiError ? err.message : "删除失败", "danger");
                            }
                          }}
                        >
                          删除
                        </button>
                      </>
                    )}
                  </div>
                ))}
              </div>
            )}
            {passkeyNotice && (
              <div className="mt-3 rounded-lg border border-[var(--sb-border)] bg-[var(--sb-hover)] px-3 py-2 text-[12px] text-[var(--sb-text-2)]">
                {passkeyNotice}
              </div>
            )}
          </section>

          </div>

          <div className={activeSection === "devices" ? undefined : "hidden"}>
          <section id="devices" className="mb-6">
            <div className="mb-2 text-[13px] text-[var(--sb-text-2)]">{t("settings.devicesTitle")}</div>
            <div className="rounded-2xl border border-[var(--sb-border)] bg-[var(--sb-pane)] px-5 py-4">
              <div className="flex flex-wrap items-start justify-between gap-3">
                <div>
                  <div className="text-[14px] font-medium">{t("settings.runOnThisComputer")}</div>
                  <div className="mt-0.5 max-w-[440px] text-[13px] leading-[19px] text-[var(--sb-text-2)]">
                    在本机运行 superbot device，让 Bot 在你自己的电脑上执行命令。默认不允许 Bot 使用。
                  </div>
                </div>
                <button
                  type="button"
                  onClick={() => {
                    setDeviceError(null);
                    setDeviceOpen(true);
                  }}
                  className="sb-btn"
                  data-variant="secondary"
                  data-size="sm"
                >
                  <Plus className="h-3.5 w-3.5" />
                  添加设备
                </button>
              </div>

              {deviceError && (
                <div className="mt-3 rounded-lg border border-danger/25 bg-danger/10 px-3 py-2 text-[12px] text-danger">
                  {deviceError}
                </div>
              )}
              {deviceNotice && (
                <div className="mt-3 rounded-lg border border-mint/25 bg-mint/10 px-3 py-2 text-[12px] text-[var(--sb-text-2)]">
                  {deviceNotice}
                </div>
              )}
              {deviceToken && (
                <div className="mt-3 rounded-xl border border-amber/25 bg-amber/[0.05] p-3">
                  <div className="flex items-center gap-2">
                    <span className="text-[12px] text-amber">设备 token 只显示一次，请立即保存。</span>
                    <button
                      type="button"
                      onClick={() => setDeviceToken(null)}
                      className="ml-auto text-[var(--sb-text-3)] hover:text-[var(--sb-text)]"
                      aria-label="关闭"
                    >
                      <X className="h-3.5 w-3.5" />
                    </button>
                  </div>
                  <pre className="mt-2 overflow-x-auto rounded-lg bg-[var(--sb-hover)] p-2.5 font-mono text-[10px] leading-relaxed text-[var(--sb-text)]">
                    {`superbot device --server ${typeof window !== "undefined" ? window.location.origin : ""} --token ${deviceToken}`}
                  </pre>
                </div>
              )}
            </div>

            <div className="mb-2 mt-6 text-[13px] text-[var(--sb-text-2)]">{t("settings.manageDevices")}</div>
            <div className="overflow-hidden rounded-2xl border border-[var(--sb-border)] bg-[var(--sb-pane)]">
              {devices.length === 0 ? (
                <div className="px-5 py-4 text-[13px] text-[var(--sb-text-3)]">
                  还没有设备。添加后会得到一条在目标电脑上运行的命令。
                </div>
              ) : (
                devices.map((device, index) => {
                  const online = device.status === "online";
                  return (
                    <div
                      key={device.id}
                      className={`flex flex-wrap items-center gap-3 px-5 py-4 ${
                        index > 0 ? "border-t border-[var(--sb-border)]" : ""
                      }`}
                    >
                      <Laptop className="h-4 w-4 shrink-0 text-[var(--sb-text-2)]" />
                      <div className="min-w-0 flex-1">
                        <div className="truncate text-[14px]">{device.name}</div>
                        <div className="mt-0.5 flex items-center gap-1.5 text-[12px] text-[var(--sb-text-2)]">
                          <span
                            className={`h-1.5 w-1.5 rounded-full ${
                              online ? "bg-[var(--sb-success)]" : "bg-[var(--sb-text-4)]"
                            }`}
                          />
                          电脑 · {online ? "当前在线" : "离线"}
                          <span className="text-[var(--sb-text-3)]">
                            · {device.platform || "device"}
                          </span>
                        </div>
                      </div>
                      <div className="flex items-center gap-2">
                        <span className="text-[12px] text-[var(--sb-text-2)]">允许 Bot 使用</span>
                        <button
                          type="button"
                          className="sb-switch"
                          data-on={device.allow_bot ? "true" : undefined}
                          data-tone="blue"
                          onClick={() => void toggleDevice(device)}
                          aria-label={`${device.allow_bot ? "禁止" : "允许"} Bot 使用 ${device.name}`}
                        />
                        <button
                          type="button"
                          onClick={() => void testDevice(device)}
                          className="sb-btn"
                          data-variant="secondary"
                          data-size="sm"
                        >
                          测试
                        </button>
                        <button
                          type="button"
                          onClick={async () => {
                            try {
                              const result = await api.screenshotDevice(device.id);
                              if (result.error) {
                                setDeviceError(result.error);
                              } else {
                                window.open(result.url, "_blank", "noopener,noreferrer");
                              }
                            } catch (err) {
                              setDeviceError(err instanceof ApiError ? err.message : "截图失败");
                            }
                          }}
                          className="sb-btn"
                          data-variant="secondary"
                          data-size="sm"
                        >
                          {t("settings.screenshot")}
                        </button>
                        <button
                          type="button"
                          onClick={() => void removeDevice(device)}
                          className="text-[var(--sb-text-4)] transition hover:text-danger"
                          aria-label={`删除 ${device.name}`}
                        >
                          <Trash2 className="h-4 w-4" />
                        </button>
                      </div>
                    </div>
                  );
                })
              )}
            </div>
          </section>

          </div>

          <div className={activeSection === "permissions" ? undefined : "hidden"}>
          <section id="permissions" className="mb-5">
            <div className="space-y-4">
              <div className="flex items-center justify-between gap-3 rounded-2xl border border-[var(--sb-border)] bg-[var(--sb-pane)] px-5 py-4">
                <div>
                  <div className="text-[14px] font-medium">自动审核</div>
                  <div className="mt-0.5 text-[13px] text-[var(--sb-text-2)]">
                    根据自定义规则审核 Bot 的操作
                  </div>
                </div>
                <button
                  type="button"
                  className="sb-switch"
                  data-on={autoReview ? "true" : undefined}
                  data-tone="blue"
                  aria-label="自动审核"
                  onClick={() => void toggleAutoReview()}
                />
              </div>

              {(
                [
                  {
                    kind: "always" as const,
                    title: "始终询问",
                    description: "操作与这些规则匹配时，停下来询问",
                    rules: alwaysAskRules,
                  },
                  {
                    kind: "allow" as const,
                    title: "无需询问直接允许",
                    description: "操作与这些规则匹配时，自动继续",
                    rules: allowRules,
                  },
                ]
              ).map((group) => (
                <div key={group.kind}>
                  <div className="flex items-center justify-between gap-3 px-1">
                    <div>
                      <div className="text-[14px] font-medium">{group.title}</div>
                      <div className="mt-0.5 text-[12px] text-[var(--sb-text-2)]">{group.description}</div>
                    </div>
                    <button
                      type="button"
                      className="sb-btn"
                      data-variant="secondary"
                      data-size="sm"
                      onClick={() => setRuleDraft({ kind: group.kind, text: "" })}
                    >
                      添加规则
                    </button>
                  </div>
                  {ruleDraft?.kind === group.kind && (
                    <form
                      className="mt-2 flex items-center gap-2"
                      onSubmit={(event) => {
                        event.preventDefault();
                        addRule(group.kind, ruleDraft.text);
                      }}
                    >
                      <input
                        autoFocus
                        value={ruleDraft.text}
                        onChange={(event) =>
                          setRuleDraft({ kind: group.kind, text: event.target.value })
                        }
                        onKeyDown={(event) => {
                          if (event.key === "Escape") setRuleDraft(null);
                        }}
                        placeholder="例如：允许运行 git push"
                        className="sb-field !h-9"
                      />
                      <button
                        type="submit"
                        className="sb-btn shrink-0"
                        data-variant="primary"
                        data-size="sm"
                        disabled={!ruleDraft.text.trim()}
                      >
                        添加
                      </button>
                    </form>
                  )}
                  <div className="mt-2 overflow-hidden rounded-2xl border border-[var(--sb-border)] bg-[var(--sb-pane)]">
                    {group.rules.length === 0 ? (
                      <div className="px-3.5 py-3 text-[12px] text-[var(--sb-text-3)]">暂无规则</div>
                    ) : (
                      group.rules.map((rule, index) => (
                        <div
                          key={rule.id}
                          className={`group/rule flex items-center gap-2 px-5 py-3 ${
                            index > 0 ? "border-t border-[var(--sb-border)]" : ""
                          }`}
                        >
                          <span className="min-w-0 flex-1 text-[13px] leading-[19px]">{rule.pattern}</span>
                          <button
                            type="button"
                            onClick={() => void removeRule(group.kind, rule.id)}
                            className="flex h-6 w-6 items-center justify-center rounded text-[var(--sb-text-4)] opacity-0 transition hover:bg-[var(--sb-hover)] hover:text-[var(--sb-text)] group-hover/rule:opacity-100"
                            aria-label="删除规则"
                          >
                            <X className="h-3.5 w-3.5" />
                          </button>
                        </div>
                      ))
                    )}
                  </div>
                </div>
              ))}
            </div>
          </section>

          </div>

          <div className={activeSection === "vault" ? undefined : "hidden"}>
          <section id="vault" className="mb-6">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <div className="flex flex-wrap items-center gap-1">
                {[
                  { key: "all", label: "全部" },
                  { key: "login", label: "登录信息" },
                  { key: "payment", label: "支付" },
                  { key: "contact", label: "联系信息" },
                ].map((tab) => {
                  const count =
                    tab.key === "all"
                      ? vaultItems.length
                      : vaultItems.filter((item) => item.kind === tab.key).length;
                  return (
                    <button
                      key={tab.key}
                      type="button"
                      onClick={() =>
                        setVaultTab(tab.key as "all" | "login" | "payment" | "contact")
                      }
                      className={
                        vaultTab === tab.key
                          ? "h-8 rounded-full bg-[var(--sb-hover)] px-3 text-[13px] font-medium text-[var(--sb-text)]"
                          : "h-8 rounded-full px-3 text-[13px] text-[var(--sb-text-2)] transition hover:bg-[var(--sb-hover)] hover:text-[var(--sb-text)]"
                      }
                    >
                      {tab.label} {count}
                    </button>
                  );
                })}
              </div>
              <div className="flex items-center gap-2">
                <label className="flex h-9 w-[200px] items-center gap-2 rounded-full bg-[var(--sb-hover)] px-3">
                  <Search className="h-4 w-4 text-[var(--sb-text-3)]" />
                  <input
                    value={vaultQuery}
                    onChange={(event) => setVaultQuery(event.target.value)}
                    placeholder="搜索保险库"
                    className="min-w-0 flex-1 bg-transparent text-[13px] outline-none placeholder:text-[var(--sb-text-3)]"
                  />
                </label>
                <button
                  type="button"
                  onClick={() => {
                    setVaultError(null);
                    setVaultOpen(true);
                  }}
                  className="sb-btn"
                  data-variant="primary"
                  data-size="sm"
                >
                  {t("settings.newItem")}
                  <ChevronDown className="h-3.5 w-3.5" />
                </button>
              </div>
            </div>

            {vaultError && (
              <div className="mt-3 rounded-lg border border-danger/25 bg-danger/10 px-3 py-2 text-[11.5px] text-danger">
                {vaultError}
              </div>
            )}

            {(() => {
              const needle = vaultQuery.trim().toLowerCase();
              const visible = vaultItems.filter((item) => {
                if (vaultTab !== "all" && item.kind !== vaultTab) return false;
                if (
                  needle &&
                  !`${item.name} ${item.username} ${item.url}`.toLowerCase().includes(needle)
                ) {
                  return false;
                }
                return true;
              });
              if (visible.length === 0) {
                return (
                  <div className="py-20 text-center">
                    <p className="text-[14px] text-[var(--sb-text-2)]">
                      {vaultItems.length === 0
                        ? "还没有保存任何内容。通过新项目添加登录信息、卡片或联系信息。"
                        : "没有找到匹配的内容。"}
                    </p>
                    <p className="mt-10 text-[12px] leading-[18px] text-[var(--sb-text-3)]">
                      superbot 永远看不到已保存卡片的卡号或安全码。它可以查看登录信息和联系信息，以便替你填写。
                    </p>
                  </div>
                );
              }
              return (
                <div className="mt-4 overflow-hidden rounded-2xl border border-[var(--sb-border)] bg-[var(--sb-pane)]">
                  {visible.map((item, index) => (
                    <div
                      key={item.id}
                      className={`flex items-center gap-3 px-5 py-3.5 ${
                        index > 0 ? "border-t border-[var(--sb-border)]" : ""
                      }`}
                    >
                      <div className="min-w-0 flex-1">
                        <div className="flex items-center gap-2">
                          <span className="truncate text-[14px]">{item.name}</span>
                          <span className="rounded-full bg-[var(--sb-hover)] px-1.5 py-0.5 text-[10px] text-[var(--sb-text-2)]">
                            {item.kind === "login"
                              ? "登录信息"
                              : item.kind === "payment"
                                ? "支付"
                                : "联系信息"}
                          </span>
                        </div>
                        <div className="mt-0.5 truncate text-[12px] text-[var(--sb-text-2)]">
                          {item.username || item.url || "—"}
                        </div>
                      </div>
                      <button
                        type="button"
                        onClick={() => {
                          setEditingVaultID(item.id);
                          setVaultForm({
                            kind: item.kind,
                            name: item.name,
                            username: item.username,
                            password: "",
                            url: item.url,
                            notes: "",
                          });
                          setVaultError(null);
                          setVaultOpen(true);
                        }}
                        className="text-[12px] text-[var(--sb-text-2)] transition hover:text-[var(--sb-text)]"
                      >
                        编辑
                      </button>
                      <button
                        type="button"
                        onClick={() => void revealVault(item)}
                        className="text-[12px] text-[var(--sb-text-2)] transition hover:text-[var(--sb-text)]"
                      >
                        查看
                      </button>
                      <button
                        type="button"
                        onClick={() => void removeVault(item)}
                        className="text-[var(--sb-text-4)] transition hover:text-danger"
                        aria-label={`删除 ${item.name}`}
                      >
                        <Trash2 className="h-3.5 w-3.5" />
                      </button>
                    </div>
                  ))}
                </div>
              );
            })()}

            {revealed && (
              <div className="mt-3 rounded-xl border border-[var(--sb-border)] bg-[var(--sb-hover)] p-3">
                <div className="flex items-center gap-2">
                  <span className="text-[12px] text-[var(--sb-text)]">{revealed.item.name}</span>
                  <button
                    type="button"
                    onClick={() => setRevealed(null)}
                    className="ml-auto text-[var(--sb-text-3)] hover:text-[var(--sb-text)]"
                    aria-label="关闭"
                  >
                    <X className="h-3.5 w-3.5" />
                  </button>
                </div>
                <div className="mt-2 space-y-1 font-mono text-[11px] text-[var(--sb-text-2)]">
                  {revealed.item.username && <div>user: {revealed.item.username}</div>}
                  {revealed.password && <div>password: {revealed.password}</div>}
                  {revealed.notes && <div className="whitespace-pre-wrap">notes: {revealed.notes}</div>}
                </div>
              </div>
            )}
          </section>

          </div>

          <div className={activeSection === "usage" ? undefined : "hidden"}>
          <section id="usage" className="mb-6">
            {usage && (
              <>
                <div className="mb-2 text-[13px] text-[var(--sb-text-2)]">我的计划</div>
                <div className="rounded-2xl border border-[var(--sb-border)] bg-[var(--sb-pane)] px-5 py-4">
                  <div className="text-[14px] font-medium">superbot 自托管</div>
                  <div className="mt-0.5 text-[13px] leading-[19px] text-[var(--sb-text-2)]">
                    按实际 token 成本计费，用量每周一 00:00 重置。
                  </div>
                  <div className="mt-4 flex items-center justify-between gap-3 border-t border-[var(--sb-border)] pt-3">
                    <div>
                      <div className="text-[14px] font-medium">账单</div>
                      <div className="mt-0.5 text-[13px] text-[var(--sb-text-2)]">
                        按 Provider 的实际价格记账
                      </div>
                    </div>
                    <button
                      type="button"
                      className="sb-btn"
                      data-variant="secondary"
                      data-size="sm"
                      onClick={() => setProviderOpen(true)}
                    >
                      管理 Provider
                    </button>
                  </div>
                </div>

                <div className="mb-2 mt-6 text-[13px] text-[var(--sb-text-2)]">用量限额</div>
                <div className="rounded-2xl border border-[var(--sb-border)] bg-[var(--sb-pane)] px-5 py-4">
                  <div className="text-[14px] font-medium">每周用量</div>
                  <div className="mt-2 text-[26px] font-medium leading-none tracking-[-0.02em]">
                    {weeklyPercent}
                    <span className="ml-1 text-[12px] font-normal text-[var(--sb-text-2)]">% 已用</span>
                  </div>
                  <div className="mt-3 h-1.5 overflow-hidden rounded-full bg-[var(--sb-hover)]">
                    <div
                      className="h-full rounded-full bg-[#0a84ff]"
                      style={{
                        width: `${weeklyPercent}%`,
                      }}
                    />
                  </div>
                  <div className="mt-2 flex flex-wrap items-center justify-between gap-2 text-[12px] text-[var(--sb-text-3)]">
                    <span>{formatCNY(weeklySpent)} / {weeklyBudget > 0 ? formatCNY(weeklyBudget) : "不限额"}</span>
                    <span>{usage.week_reset_at ? `${new Date(usage.week_reset_at).toLocaleDateString("zh-CN")} 00:00 重置` : "每周一 00:00 重置"}</span>
                  </div>
                </div>

                <div className="mb-2 mt-6 text-[13px] text-[var(--sb-text-2)]">superbot 用量</div>
                <div className="rounded-2xl border border-[var(--sb-border)] bg-[var(--sb-pane)] p-5">
                  <div className="grid grid-cols-2 gap-2 md:grid-cols-4">
                    <UsageStat label="输入 tokens" value={formatNumber(usage.totals.prompt_tokens)} />
                    <UsageStat label="缓存 tokens" value={formatNumber(usage.totals.cached_tokens)} />
                    <UsageStat label="输出 tokens" value={formatNumber(usage.totals.completion_tokens)} />
                    <UsageStat label="成本" value={formatCNY(usage.totals.cost_micros)} />
                  </div>
                  <div className="mt-5">
                    <div className="mb-2 font-mono text-[9px] uppercase tracking-[0.14em] text-muted/50">每日成本</div>
                    <div className="flex h-24 items-end gap-1">
                      {usage.daily.length === 0 && (
                        <div className="w-full py-8 text-center text-[10.5px] text-muted/50">还没有用量记录。</div>
                      )}
                      {usage.daily.map((day) => {
                        const maxCost = Math.max(1, ...usage.daily.map((item) => item.cost_micros));
                        const height = Math.max(4, Math.round((day.cost_micros / maxCost) * 100));
                        return (
                          <div key={day.day} className="group flex min-w-0 flex-1 flex-col items-center gap-1">
                            <div
                              className="w-full rounded-t bg-mint/60 transition group-hover:bg-mint"
                              style={{ height: `${height}%` }}
                              title={`${day.day} · ${formatCNY(day.cost_micros)}`}
                            />
                            <span className="hidden font-mono text-[7px] text-muted/40 sm:block">
                              {day.day.slice(5)}
                            </span>
                          </div>
                        );
                      })}
                    </div>
                  </div>
                  {usage.by_bot.length > 0 && (
                    <div className="mt-4">
                      <div className="mb-2 font-mono text-[9px] uppercase tracking-[0.14em] text-muted/50">按 Bot</div>
                      <div className="space-y-1">
                        {usage.by_bot.slice(0, 6).map((bot) => (
                          <div
                            key={bot.bot_id ?? bot.bot_name}
                            className="flex items-center gap-2 rounded-lg border border-[var(--sb-border)]/50 px-2.5 py-1.5"
                          >
                            <span className="min-w-0 flex-1 truncate text-[11px] text-[var(--sb-text)]">{bot.bot_name}</span>
                            <span className="font-mono text-[9px] text-muted/50">
                              {formatNumber(bot.prompt_tokens + bot.completion_tokens)} tok
                            </span>
                            <span className="font-mono text-[10px] text-[var(--sb-text-2)]/80">{formatCNY(bot.cost_micros)}</span>
                          </div>
                        ))}
                      </div>
                    </div>
                  )}
                </div>
              </>
            )}
          </section>

          <div className="mb-2 text-[13px] text-[var(--sb-text-2)]">套餐与额度</div>
          <section className="mb-6 rounded-2xl border border-[var(--sb-border)] bg-[var(--sb-pane)] p-5">
            {billing ? (
              <>
                <div className="flex flex-wrap items-center justify-between gap-3">
                  <div>
                    <div className="text-[14px] font-medium">{billing.plan === "self_hosted" ? "自托管套餐" : billing.plan}</div>
                    <div className="mt-0.5 text-[12px] text-[var(--sb-text-2)]">
                      本周预算 {formatCNY(billing.weekly_budget_micros)} · 已用 {formatCNY(billing.weekly_spent_micros)}
                    </div>
                  </div>
                  <div className="text-right">
                    <div className="text-[11px] text-[var(--sb-text-3)]">Credits</div>
                    <div className="text-[16px] font-medium">{formatCNY(billing.credits_micros)}</div>
                  </div>
                </div>
                <div className="mt-4 flex flex-wrap items-end gap-2">
                  <label className="text-[11.5px] text-[var(--sb-text-2)]">
                    调整额度（元）
                    <input
                      value={creditAmount}
                      onChange={(event) => setCreditAmount(event.target.value)}
                      className="field mt-1 w-[120px]"
                      placeholder="例如 10"
                      inputMode="decimal"
                    />
                  </label>
                  <label className="min-w-[180px] flex-1 text-[11.5px] text-[var(--sb-text-2)]">
                    备注
                    <input
                      value={creditNote}
                      onChange={(event) => setCreditNote(event.target.value)}
                      className="field mt-1 w-full"
                      placeholder="手动充值 / 调整原因"
                    />
                  </label>
                  <button
                    type="button"
                    disabled={creditSaving || !Number.isFinite(Number(creditAmount)) || Number(creditAmount) === 0}
                    className="sb-btn"
                    data-variant="primary"
                    data-size="sm"
                    onClick={async () => {
                      setCreditSaving(true);
                      try {
                        await api.adjustCredits(Math.round(Number(creditAmount) * 1_000_000), creditNote);
                        setBilling(await api.billing());
                        setCreditAmount("");
                        setCreditNote("");
                        toast("额度已调整", "success");
                      } catch (err) {
                        toast(err instanceof ApiError ? err.message : "调整额度失败", "danger");
                      } finally {
                        setCreditSaving(false);
                      }
                    }}
                  >
                    {creditSaving ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : null}
                    调整额度
                  </button>
                </div>
                {billing.ledger.length > 0 && (
                  <div className="mt-4 space-y-1">
                    {billing.ledger.slice(0, 6).map((entry) => (
                      <div key={entry.id} className="flex items-center gap-2 rounded-lg border border-[var(--sb-border)]/60 px-2.5 py-1.5 text-[11.5px]">
                        <span className="min-w-0 flex-1 truncate text-[var(--sb-text-2)]">{entry.note || entry.kind}</span>
                        <span className={entry.amount_micros >= 0 ? "text-[var(--sb-success)]" : "text-[var(--sb-danger)]"}>
                          {entry.amount_micros >= 0 ? "+" : ""}{formatCNY(entry.amount_micros)}
                        </span>
                        <span className="text-[var(--sb-text-3)]">{formatCNY(entry.balance_after_micros)}</span>
                      </div>
                    ))}
                  </div>
                )}
              </>
            ) : (
              <div className="py-4 text-center text-[12px] text-[var(--sb-text-3)]">加载中…</div>
            )}
          </section>

          </div>

          <div className={activeSection === "updates" ? undefined : "hidden"}>
          <section id="updates" className="mb-6">
            <div className="mb-2 text-[13px] text-[var(--sb-text-2)]">superbot 更新</div>
            <div className="overflow-hidden rounded-2xl border border-[var(--sb-border)] bg-[var(--sb-pane)]">
              <div className="flex items-center justify-between gap-3 px-5 py-4">
                <div>
                  <div className="text-[14px] font-medium">服务端更新</div>
                  <div className="mt-0.5 text-[13px] text-[var(--sb-text-2)]">
                    由部署脚本统一执行；Web 端无需客户端更新
                  </div>
                </div>
                <span className="rounded-full bg-[var(--sb-hover)] px-2.5 py-1 text-[11px] text-[var(--sb-text-2)]">
                  部署控制
                </span>
              </div>
              <div className="flex items-center justify-between gap-3 border-t border-[var(--sb-border)] px-5 py-4">
                <div>
                  <div className="text-[14px] font-medium">版本</div>
                  <div className="mt-0.5 text-[13px] text-[var(--sb-text-2)]">
                    v{packageInfo.version} · Docker Compose
                  </div>
                </div>
                <button
                  type="button"
                  className="sb-btn"
                  data-variant="secondary"
                  data-size="sm"
                  onClick={checkUpdates}
                >
                  {t("settings.checkUpdates")}
                </button>
              </div>
            </div>
            {updateNotice && (
              <div className="mt-2 px-1 text-[12px] text-[var(--sb-danger)]">{updateNotice}</div>
            )}
          </section>

          </div>

          {user.role === "admin" && (
            <section
              id="members"
              className={`mb-5 rounded-2xl border border-[var(--sb-border)] bg-[var(--sb-pane)] p-5 ${
                activeSection === "members" ? "" : "hidden"
              }`}
            >
              <div className="flex flex-wrap items-start justify-between gap-3">
                <div>
                  <h2 className="text-[14px] font-medium text-[var(--sb-text)]">{t("settings.members")}</h2>
                  <p className="mt-1 text-[12px] leading-[18px] text-[var(--sb-text-2)]">
                    多账号共享同一台服务器；Bot、Provider、会话与扩展按账号隔离。
                  </p>
                </div>
                <button
                  type="button"
                  onClick={() => {
                    setUserError(null);
                    setUserOpen(true);
                  }}
                  className="sb-btn"
                  data-variant="secondary"
                  data-size="sm"
                >
                  <Plus className="h-3 w-3" />
                  添加成员
                </button>
              </div>
              {userError && (
                <div className="mt-3 rounded-lg border border-danger/25 bg-danger/10 px-3 py-2 text-[11.5px] text-danger">
                  {userError}
                </div>
              )}
              <div className="mt-3 space-y-1">
                {users.map((member) => (
                  <div key={member.id} className="flex items-center gap-2 rounded-xl border border-[var(--sb-border)] px-4 py-3">
                    <span className="min-w-0 flex-1 truncate text-[14px] text-[var(--sb-text)]">{member.email}</span>
                    <span className="font-mono text-[10px] uppercase tracking-[0.08em] text-[var(--sb-text-3)]">{member.role}</span>
                    {member.id !== user.id && (
                      <button
                        type="button"
                        onClick={() => void removeUser(member)}
                        className="text-muted/40 hover:text-danger"
                        aria-label={`删除 ${member.email}`}
                      >
                        <Trash2 className="h-3.5 w-3.5" />
                      </button>
                    )}
                  </div>
                ))}
              </div>
            </section>
          )}

          <div
            id="providers"
            className={`space-y-4 ${activeSection === "providers" ? "" : "hidden"}`}
          >
            <div className="flex flex-wrap items-start justify-between gap-3">
              <div className="self-center">
                <p className="text-[13px] leading-[19px] text-[var(--sb-text-2)]">
                  Provider 密钥使用 AES-256-GCM 加密存储；API 永远不会把明文返回给浏览器。
                </p>
              </div>
              <button
                type="button"
                onClick={() => setProviderOpen(true)}
                className="sb-btn"
                data-variant="primary"
                data-size="sm"
              >
                <Plus className="h-3.5 w-3.5" />
                {t("settings.newProvider")}
              </button>
            </div>
            {providers.map((provider) => (
              <section
                key={provider.id}
                className="rounded-2xl border border-[var(--sb-border)] bg-[var(--sb-pane)] p-5"
              >
                <div className="flex flex-wrap items-start justify-between gap-3">
                  <div className="flex items-start gap-3">
                    <div className="flex h-10 w-10 items-center justify-center rounded-xl border bg-[var(--sb-hover)] text-[var(--sb-text-2)]">
                      <Settings2 className="h-4.5 w-4.5" />
                    </div>
                    <div>
                      <div className="flex items-center gap-2">
                        <h2 className="text-[14px] font-medium text-[var(--sb-text)]">{provider.name}</h2>
                        <StatusPill tone={provider.enabled ? "live" : "idle"}>{provider.kind}</StatusPill>
                        <span className="inline-flex items-center gap-1 rounded-full border border-[var(--sb-border)] px-1.5 py-0.5 font-mono text-[9.5px] uppercase tracking-[0.1em] text-muted">
                          <KeyRound className="h-2.5 w-2.5" />
                          {provider.has_key ? "key set" : "no key"}
                        </span>
                      </div>
                      <div className="mt-1 font-mono text-[11px] text-[var(--sb-text-3)]">
                        {provider.type} · {provider.base_url || "no base url"}
                      </div>
                    </div>
                  </div>
                  <div className="flex items-center gap-2">
                    <button
                      type="button"
                      onClick={async () => {
                        setProviderTesting(provider.id);
                        try {
                          const result = await api.testProvider(
                            provider.id,
                            models[provider.id]?.[0]?.name,
                          );
                          toast(`连接正常：${result.model}`, "success");
                        } catch (err) {
                          toast(err instanceof ApiError ? err.message : "Provider 测试失败", "danger");
                        } finally {
                          setProviderTesting(null);
                        }
                      }}
                      disabled={providerTesting === provider.id}
                      className="sb-btn"
                      data-variant="secondary"
                      data-size="sm"
                    >
                      {providerTesting === provider.id ? (
                        <Loader2 className="h-3.5 w-3.5 animate-spin" />
                      ) : null}
                      测试
                    </button>
                    <button
                      type="button"
                      onClick={() => {
                        setEditingProvider(provider);
                        setProviderEditForm({
                          name: provider.name,
                          base_url: provider.base_url,
                          api_key: "",
                          enabled: provider.enabled,
                        });
                      }}
                      className="sb-btn"
                      data-variant="secondary"
                      data-size="sm"
                    >
                      编辑
                    </button>
                    <button
                      type="button"
                      onClick={() => {
                        setModelProvider(provider);
                        setModelForm(emptyModelForm);
                      }}
                      className="flex h-8 items-center gap-1 rounded-lg border border-[var(--sb-border)] bg-[var(--sb-hover)]/60 px-3 text-[12px] text-[var(--sb-text-2)] transition hover:border-[var(--sb-border-strong)] hover:text-[var(--sb-text)]"
                    >
                      <Plus className="h-3 w-3" />
                      模型
                    </button>
                    <button
                      type="button"
                      onClick={() => deleteProvider(provider)}
                      className="flex h-7 w-7 items-center justify-center rounded-md text-muted transition hover:bg-danger/10 hover:text-danger"
                      aria-label="删除 Provider"
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                    </button>
                  </div>
                </div>

                {provider.type === "llm" ? (
                  <div className="mt-4 grid gap-2">
                    {(models[provider.id] ?? []).length === 0 && (
                      <div className="rounded-xl border border-dashed border-[var(--sb-border)] px-4 py-5 text-[11.5px] text-muted/60">
                        还没有模型。Bot 创建时需要从这里选择一个模型。
                      </div>
                    )}
                    {(models[provider.id] ?? []).map((model) => (
                      <div
                        key={model.id}
                        className="rounded-xl border border-[var(--sb-border)] bg-[var(--sb-hover)] px-3 py-2.5"
                      >
                        <div className="flex items-center gap-2">
                          <Cpu className="h-3.5 w-3.5 text-[var(--sb-text-2)]/80" />
                          <span className="min-w-0 truncate font-mono text-[12.5px] text-[var(--sb-text)]">{model.name}</span>
                          <span className="ml-auto shrink-0 whitespace-nowrap font-mono text-[10px] text-[var(--sb-text-3)]">
                            {formatNumber(model.context_window)} ctx · {formatNumber(model.max_output)} out
                          </span>
                          <button
                            type="button"
                            onClick={() => deleteModel(provider, model)}
                            className="text-muted/50 transition hover:text-danger"
                            aria-label="删除模型"
                          >
                            <X className="h-3.5 w-3.5" />
                          </button>
                        </div>
                        <div className="mt-1.5 flex flex-wrap items-center gap-1.5 font-mono text-[9.5px] uppercase tracking-[0.08em] text-[var(--sb-text-3)]">
                          {model.supports_tools && <span className="rounded bg-[var(--sb-hover)] px-1 py-0.5">tools</span>}
                          {model.supports_vision && <span className="rounded bg-[var(--sb-hover)] px-1 py-0.5">vision</span>}
                          {model.supports_reasoning && (
                            <span className="rounded bg-[var(--sb-hover)] px-1 py-0.5">reasoning</span>
                          )}
                          <span className="ml-auto text-muted/45">
                            in {formatCNY(model.price_in_micros_per_mtok)} / out{" "}
                            {formatCNY(model.price_out_micros_per_mtok)} per Mtok
                          </span>
                        </div>
                      </div>
                    ))}
                  </div>
                ) : (
                  <div className="mt-4 rounded-xl border border-[var(--sb-border)] bg-[var(--sb-hover)] px-4 py-3 text-[11.5px] text-muted/65">
                    搜索 Provider：Bot 的 <code className="font-mono text-[var(--sb-text-2)]">web_search</code> 工具会使用它。
                  </div>
                )}
              </section>
            ))}
          </div>
          </div>
        </div>
      </main>

      <Menu
        open={themeMenuAnchor !== null}
        anchor={themeMenuAnchor}
        items={[
          {
            key: "system",
            label: "跟随系统",
            checked: mode === "system",
            onSelect: () => setMode("system"),
          },
          { key: "light", label: t("settings.themeLight"), checked: mode === "light", onSelect: () => setMode("light") },
          { key: "dark", label: "深色", checked: mode === "dark", onSelect: () => setMode("dark") },
        ]}
        onClose={() => setThemeMenuAnchor(null)}
        align="right"
        minWidth={184}
      />

      <Menu
        open={accentMenuAnchor !== null}
        anchor={accentMenuAnchor}
        items={([
          ["green", "Bot 颜色"],
          ["blue", "蓝色"],
          ["violet", "紫色"],
          ["orange", "橙色"],
          ["black", "黑色"],
        ] as const).map(([key, label]) => ({
          key,
          label,
          checked: accent === key,
          onSelect: () => setAccent(key as AccentName),
        }))}
        onClose={() => setAccentMenuAnchor(null)}
        align="right"
        minWidth={184}
      />

      {confirmElement}

      {userOpen && (
        <Modal title="添加成员" subtitle="成员只能看到自己的 Bot、会话、Provider 与扩展" onClose={() => setUserOpen(false)}>
          <div className="grid gap-3 md:grid-cols-2">
            <Field label="邮箱">
              <input
                className="field"
                type="email"
                value={userForm.email}
                onChange={(event) => setUserForm((prev) => ({ ...prev, email: event.target.value }))}
              />
            </Field>
            <Field label="显示名称">
              <input
                className="field"
                value={userForm.display_name}
                onChange={(event) => setUserForm((prev) => ({ ...prev, display_name: event.target.value }))}
              />
            </Field>
            <Field label="初始密码（至少 10 位）">
              <input
                className="field"
                type="password"
                value={userForm.password}
                onChange={(event) => setUserForm((prev) => ({ ...prev, password: event.target.value }))}
              />
            </Field>
            <Field label="角色">
              <select
                className="field"
                value={userForm.role}
                onChange={(event) => setUserForm((prev) => ({ ...prev, role: event.target.value }))}
              >
                <option value="member">成员</option>
                <option value="admin">管理员</option>
              </select>
            </Field>
          </div>
          {userError && (
            <div className="mt-3 rounded-lg border border-danger/25 bg-danger/10 px-3 py-2 text-[11.5px] text-danger">
              {userError}
            </div>
          )}
          <div className="mt-5 flex justify-end gap-2">
            <button
              type="button"
              onClick={() => setUserOpen(false)}
              className="rounded-lg border border-[var(--sb-border)] px-3 py-2 text-[11.5px] text-muted hover:text-[var(--sb-text)]"
            >
              取消
            </button>
            <button
              type="button"
              onClick={() => void createUser()}
              disabled={userSaving}
              className="flex items-center gap-1.5 rounded-lg border border-mint/40 bg-mint px-3.5 py-2 text-[11.5px] font-semibold text-ink disabled:opacity-60"
            >
              {userSaving && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
              {t("settings.createMember")}
            </button>
          </div>
        </Modal>
      )}

      {deviceOpen && (
        <Modal
          title="添加设备"
          subtitle="创建后会在目标电脑上运行一个轮询守护进程；token 只显示一次"
          onClose={() => setDeviceOpen(false)}
        >
          <div className="grid gap-3 md:grid-cols-2">
            <Field label="设备名称">
              <input
                className="field"
                value={deviceForm.name}
                onChange={(event) => setDeviceForm((prev) => ({ ...prev, name: event.target.value }))}
                placeholder="例如：我的 Windows 工作站"
              />
            </Field>
            <Field label="平台">
              <select
                className="field"
                value={deviceForm.platform}
                onChange={(event) => setDeviceForm((prev) => ({ ...prev, platform: event.target.value }))}
              >
                <option value="windows">Windows</option>
                <option value="linux">Linux</option>
                <option value="darwin">macOS</option>
              </select>
            </Field>
          </div>
          {deviceError && (
            <div className="mt-3 rounded-lg border border-danger/25 bg-danger/10 px-3 py-2 text-[11.5px] text-danger">
              {deviceError}
            </div>
          )}
          <div className="mt-5 flex justify-end gap-2">
            <button
              type="button"
              onClick={() => setDeviceOpen(false)}
              className="rounded-lg border border-[var(--sb-border)] px-3 py-2 text-[11.5px] text-muted hover:text-[var(--sb-text)]"
            >
              取消
            </button>
            <button
              type="button"
              onClick={() => void createDevice()}
              disabled={deviceSaving}
              className="flex items-center gap-1.5 rounded-lg border border-mint/40 bg-mint px-3.5 py-2 text-[11.5px] font-semibold text-ink disabled:opacity-60"
            >
              {deviceSaving && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
              创建
            </button>
          </div>
        </Modal>
      )}

      {vaultOpen && (
        <Modal
          title={editingVaultID ? "编辑保险库项目" : "添加到保险库"}
          subtitle="密码与备注会加密存储，列表接口不会回显明文；留空表示保持原值"
          onClose={() => {
            setVaultOpen(false);
            setEditingVaultID(null);
          }}
        >
          <div className="grid gap-3 md:grid-cols-2">
            <Field label="类型">
              <select
                className="field"
                value={vaultForm.kind}
                onChange={(event) => setVaultForm((prev) => ({ ...prev, kind: event.target.value }))}
              >
                <option value="login">登录信息</option>
                <option value="payment">支付信息</option>
                <option value="contact">联系信息</option>
              </select>
            </Field>
            <Field label="名称">
              <input
                className="field"
                value={vaultForm.name}
                onChange={(event) => setVaultForm((prev) => ({ ...prev, name: event.target.value }))}
                placeholder="例如：GitHub"
              />
            </Field>
            <Field label="用户名 / 邮箱">
              <input
                className="field"
                value={vaultForm.username}
                onChange={(event) => setVaultForm((prev) => ({ ...prev, username: event.target.value }))}
              />
            </Field>
            <Field label={editingVaultID ? "密码 / 密钥（留空保持不变）" : "密码 / 密钥"}>
              <input
                className="field"
                type="password"
                value={vaultForm.password}
                onChange={(event) => setVaultForm((prev) => ({ ...prev, password: event.target.value }))}
              />
            </Field>
            <Field label="URL" className="md:col-span-2">
              <input
                className="field"
                value={vaultForm.url}
                onChange={(event) => setVaultForm((prev) => ({ ...prev, url: event.target.value }))}
                placeholder="https://…"
              />
            </Field>
            <Field label="备注" className="md:col-span-2">
              <textarea
                className="field min-h-[72px] resize-y"
                value={vaultForm.notes}
                onChange={(event) => setVaultForm((prev) => ({ ...prev, notes: event.target.value }))}
              />
            </Field>
          </div>
          {vaultError && (
            <div className="mt-3 rounded-lg border border-danger/25 bg-danger/10 px-3 py-2 text-[11.5px] text-danger">
              {vaultError}
            </div>
          )}
          <div className="mt-5 flex justify-end gap-2">
            <button
              type="button"
              onClick={() => setVaultOpen(false)}
              className="rounded-lg border border-[var(--sb-border)] px-3 py-2 text-[11.5px] text-muted hover:text-[var(--sb-text)]"
            >
              取消
            </button>
            <button
              type="button"
              onClick={() => void saveVault()}
              disabled={vaultSaving}
              className="flex items-center gap-1.5 rounded-lg border border-mint/40 bg-mint px-3.5 py-2 text-[11.5px] font-semibold text-ink disabled:opacity-60"
            >
              {vaultSaving && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
              保存
            </button>
          </div>
        </Modal>
      )}

      {editingProvider && (
        <UiModal
          open={editingProvider !== null}
          title="编辑 Provider"
          subtitle="留空 API Key 表示保持现有密钥不变"
          onClose={() => setEditingProvider(null)}
          footer={
            <>
              <button
                type="button"
                className="sb-btn"
                data-variant="secondary"
                data-size="sm"
                onClick={() => setEditingProvider(null)}
              >
                取消
              </button>
              <button
                type="button"
                className="sb-btn"
                data-variant="primary"
                data-size="sm"
                onClick={async () => {
                  try {
                    const result = await api.updateProvider(editingProvider.id, {
                      name: providerEditForm.name.trim(),
                      base_url: providerEditForm.base_url.trim(),
                      enabled: providerEditForm.enabled,
                      ...(providerEditForm.api_key.trim()
                        ? { api_key: providerEditForm.api_key.trim() }
                        : {}),
                    });
                    setProviders((prev) =>
                      prev.map((item) => (item.id === result.provider.id ? result.provider : item)),
                    );
                    setEditingProvider(null);
                    toast("Provider 已更新", "success");
                  } catch (err) {
                    toast(err instanceof ApiError ? err.message : "更新 Provider 失败", "danger");
                  }
                }}
              >
                保存
              </button>
            </>
          }
        >
          <div className="grid gap-3">
            <Field label="名称">
              <input
                className="field"
                value={providerEditForm.name}
                onChange={(event) =>
                  setProviderEditForm((prev) => ({ ...prev, name: event.target.value }))
                }
              />
            </Field>
            <Field
              label={editingProvider.kind === "anthropic" ? t("settings.baseUrlOptional") : "Base URL"}
            >
              <input
                className="field"
                value={providerEditForm.base_url}
                onChange={(event) =>
                  setProviderEditForm((prev) => ({ ...prev, base_url: event.target.value }))
                }
                placeholder={editingProvider.kind === "anthropic" ? ANTHROPIC_DEFAULT_BASE_URL : undefined}
              />
            </Field>
            <Field label="API Key（留空保持不变）">
              <input
                className="field"
                type="password"
                autoComplete="new-password"
                value={providerEditForm.api_key}
                onChange={(event) =>
                  setProviderEditForm((prev) => ({ ...prev, api_key: event.target.value }))
                }
                placeholder="sk-..."
              />
            </Field>
            <label className="flex items-center justify-between rounded-xl border border-[var(--sb-border)] px-4 py-3">
              <span className="text-[13px]">启用该 Provider</span>
              <button
                type="button"
                className="sb-switch"
                data-on={providerEditForm.enabled ? "true" : undefined}
                data-tone="blue"
                aria-label="启用 Provider"
                onClick={() =>
                  setProviderEditForm((prev) => ({ ...prev, enabled: !prev.enabled }))
                }
              />
            </label>
          </div>
        </UiModal>
      )}

      {providerOpen && (
        <Modal title={t("settings.newProvider")} subtitle={t("settings.providerKeyNote")} onClose={() => setProviderOpen(false)}>
          {providerForm.type === "llm" && (
            <div className="mb-4 flex flex-wrap gap-1.5">
              {presets.map((preset) => (
                <button
                  key={preset.key}
                  type="button"
                  onClick={() => applyPreset(preset.key)}
                  className={cn(
                    "rounded-lg border px-2.5 py-1.5 font-mono text-[10px] transition",
                    providerForm.preset === preset.key
                      ? "border-mint/40 bg-mint/10 text-[var(--sb-text-2)]"
                      : "border-[var(--sb-border)] text-muted hover:border-mint/25 hover:text-text",
                  )}
                >
                  {preset.label}
                </button>
              ))}
            </div>
          )}
          <div className="grid gap-3 md:grid-cols-2">
            <Field label="名称">
              <input
                className="field"
                value={providerForm.name}
                onChange={(event) => setProviderForm((prev) => ({ ...prev, name: event.target.value }))}
                placeholder="DeepSeek"
              />
            </Field>
            <Field label="类型">
              <select
                className="field"
                value={providerForm.type}
                onChange={(event) => {
                  const type = event.target.value;
                  setProviderForm((prev) => ({
                    ...prev,
                    type,
                    kind: type === "search" ? "bocha" : "openai_compat",
                    name: type === "search" ? "Bocha" : prev.name,
                    preset: "",
                  }));
                }}
              >
                <option value="llm">llm — 对话模型</option>
                <option value="search">search — 网页搜索</option>
              </select>
            </Field>
            <Field label="Provider 类型">
              <select
                className="field"
                value={providerForm.kind}
                onChange={(event) => {
                  const kind = event.target.value;
                  setProviderForm((prev) => ({
                    ...prev,
                    kind,
                    preset: "",
                    // Do not carry another vendor's preset URL over to Anthropic.
                    base_url:
                      kind === "anthropic" && presets.some((item) => item.base_url === prev.base_url)
                        ? ""
                        : prev.base_url,
                  }));
                }}
              >
                {providerForm.type === "search" ? (
                  <>
                    <option value="bocha">bocha</option>
                    <option value="tavily">tavily</option>
                  </>
                ) : (
                  <>
                    <option value="openai_compat">openai_compat</option>
                    <option value="anthropic">Anthropic</option>
                    <option value="mock">mock（本地测试）</option>
                  </>
                )}
              </select>
            </Field>
            <Field
              label={providerForm.kind === "anthropic" ? t("settings.baseUrlOptional") : "Base URL"}
              className="md:col-span-2"
            >
              <input
                className="field font-mono text-[12px]"
                value={providerForm.base_url}
                onChange={(event) => setProviderForm((prev) => ({ ...prev, base_url: event.target.value }))}
                placeholder={
                  providerForm.kind === "anthropic" ? ANTHROPIC_DEFAULT_BASE_URL : "https://api.deepseek.com"
                }
              />
            </Field>
            <Field label="API Key" className="md:col-span-2">
              <input
                className="field font-mono text-[12px]"
                type="password"
                value={providerForm.api_key}
                onChange={(event) => setProviderForm((prev) => ({ ...prev, api_key: event.target.value }))}
                placeholder="sk-…"
              />
            </Field>
          </div>
          <div className="mt-5 flex justify-end gap-2">
            <button
              type="button"
              onClick={() => setProviderOpen(false)}
              className="rounded-lg border border-[var(--sb-border)] px-3 py-2 text-[11.5px] text-muted hover:text-[var(--sb-text)]"
            >
              取消
            </button>
            <button
              type="button"
              onClick={createProvider}
              disabled={saving}
              className="flex items-center gap-1.5 rounded-lg border border-mint/40 bg-mint px-3.5 py-2 text-[11.5px] font-semibold text-ink disabled:opacity-60"
            >
              {saving && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
              {t("settings.saveProvider")}
            </button>
          </div>
        </Modal>
      )}

      {modelProvider && (
        <Modal
          title={`为 ${modelProvider.name} 添加模型`}
          subtitle="价格按人民币 / 1M tokens 填写"
          onClose={() => setModelProvider(null)}
        >
          <div className="grid gap-3 md:grid-cols-2">
            <Field label="模型名">
              <input
                className="field"
                value={modelForm.name}
                onChange={(event) => setModelForm((prev) => ({ ...prev, name: event.target.value }))}
                placeholder="deepseek-flash"
              />
            </Field>
            <Field label="上下文窗口">
              <input
                className="field"
                type="number"
                value={modelForm.context_window}
                onChange={(event) =>
                  setModelForm((prev) => ({ ...prev, context_window: event.target.value }))
                }
              />
            </Field>
            <Field label="最大输出">
              <input
                className="field"
                type="number"
                value={modelForm.max_output}
                onChange={(event) => setModelForm((prev) => ({ ...prev, max_output: event.target.value }))}
              />
            </Field>
            <div className="flex items-end gap-3 pb-2">
              <Toggle
                label="tools"
                checked={modelForm.supports_tools}
                onChange={(checked) => setModelForm((prev) => ({ ...prev, supports_tools: checked }))}
              />
              <Toggle
                label="vision"
                checked={modelForm.supports_vision}
                onChange={(checked) => setModelForm((prev) => ({ ...prev, supports_vision: checked }))}
              />
              <Toggle
                label="reasoning"
                checked={modelForm.supports_reasoning}
                onChange={(checked) => setModelForm((prev) => ({ ...prev, supports_reasoning: checked }))}
              />
            </div>
            <Field label="输入价 ¥/Mtok">
              <input
                className="field"
                type="number"
                step="0.01"
                value={modelForm.price_in}
                onChange={(event) => setModelForm((prev) => ({ ...prev, price_in: event.target.value }))}
              />
            </Field>
            <Field label="缓存输入价 ¥/Mtok">
              <input
                className="field"
                type="number"
                step="0.01"
                value={modelForm.price_cache}
                onChange={(event) =>
                  setModelForm((prev) => ({ ...prev, price_cache: event.target.value }))
                }
              />
            </Field>
            <Field label="输出价 ¥/Mtok">
              <input
                className="field"
                type="number"
                step="0.01"
                value={modelForm.price_out}
                onChange={(event) => setModelForm((prev) => ({ ...prev, price_out: event.target.value }))}
              />
            </Field>
          </div>
          <div className="mt-5 flex justify-end gap-2">
            <button
              type="button"
              onClick={() => setModelProvider(null)}
              className="rounded-lg border border-[var(--sb-border)] px-3 py-2 text-[11.5px] text-muted hover:text-[var(--sb-text)]"
            >
              取消
            </button>
            <button
              type="button"
              onClick={createModel}
              disabled={saving}
              className="flex items-center gap-1.5 rounded-lg border border-mint/40 bg-mint px-3.5 py-2 text-[11.5px] font-semibold text-ink disabled:opacity-60"
            >
              {saving && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
              保存模型
            </button>
          </div>
        </Modal>
      )}
    </div>
  );
}

export default function SettingsPage() {
  return <SettingsScreen />;
}

function Modal({
  title,
  subtitle,
  onClose,
  children,
}: {
  title: string;
  subtitle?: string;
  onClose: () => void;
  children: React.ReactNode;
}) {
  return (
    <UiModal open onClose={onClose} title={title} subtitle={subtitle} size="lg">
      {children}
    </UiModal>
  );
}

function UsageStat({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-xl border border-[var(--sb-border)] bg-[var(--sb-hover)] px-3 py-2.5">
      <div className="font-mono text-[8px] uppercase tracking-[0.12em] text-muted/50">{label}</div>
      <div className="mt-1 font-mono text-[13px] text-[var(--sb-text)]">{value}</div>
    </div>
  );
}

function Field({ label, children, className }: { label: string; children: React.ReactNode; className?: string }) {
  return (
    <div className={className}>
      <label className="sb-field-label" style={{ marginBottom: 0, paddingBottom: 6 }}>
        {label}
      </label>
      {children}
    </div>
  );
}

function Toggle({
  label,
  checked,
  onChange,
}: {
  label: string;
  checked: boolean;
  onChange: (checked: boolean) => void;
}) {
  return (
    <label className="flex items-center gap-1.5 font-mono text-[9.5px] uppercase tracking-[0.1em] text-muted">
      <input
        type="checkbox"
        checked={checked}
        onChange={(event) => onChange(event.target.checked)}
        className="h-3 w-3 accent-[var(--color-mint)]"
      />
      {label}
    </label>
  );
}


function SettingsNavLink({
  id,
  icon,
  label,
}: {
  id: string;
  icon: React.ReactNode;
  label: string;
}) {
  const [active, setActive] = useState(false);

  useEffect(() => {
    const onSection = (event: Event) => {
      setActive((event as CustomEvent<string>).detail === id);
    };
    window.addEventListener("superbot:settings-section", onSection);
    return () => window.removeEventListener("superbot:settings-section", onSection);
  }, [id]);

  useEffect(() => {
    const el = document.getElementById(id);
    if (!el) return;
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries.some((entry) => entry.isIntersecting)) {
          window.dispatchEvent(new CustomEvent("superbot:settings-section", { detail: id }));
        }
      },
      { rootMargin: "-20% 0px -65% 0px", threshold: 0 },
    );
    observer.observe(el);
    return () => observer.disconnect();
  }, [id]);

  return (
    <a
      href={`#${id}`}
      onClick={(event) => {
        event.preventDefault();
        window.dispatchEvent(new CustomEvent("superbot:settings-section", { detail: id }));
      }}
      className="sb-menu-item"
      data-highlighted={active ? "true" : undefined}
    >
      <span className="flex h-4 w-4 items-center justify-center text-[var(--sb-text-2)]">{icon}</span>
      <span className="flex-1">{label}</span>
    </a>
  );
}

function urlBase64ToUint8Array(base64String: string): Uint8Array<ArrayBuffer> {
  const padding = "=".repeat((4 - (base64String.length % 4)) % 4);
  const base64 = (base64String + padding).replace(/-/g, "+").replace(/_/g, "/");
  const raw = window.atob(base64);
  const buffer = new ArrayBuffer(raw.length);
  const output = new Uint8Array(buffer);
  for (let i = 0; i < raw.length; i += 1) output[i] = raw.charCodeAt(i);
  return output;
}
