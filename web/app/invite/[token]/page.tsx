"use client";

import { useEffect, useState } from "react";
import { useParams, useRouter } from "next/navigation";
import { Loader2 } from "lucide-react";

import { api, ApiError } from "@/lib/api";
import { Logo } from "@/components/Logo";

export default function InvitePage() {
  const params = useParams<{ token: string }>();
  const router = useRouter();
  const [status, setStatus] = useState<"loading" | "ready" | "error">("loading");
  const [message, setMessage] = useState("");

  useEffect(() => {
    api
      .me()
      .then(() => setStatus("ready"))
      .catch(() => router.replace(`/login?next=/invite/${params.token}`));
  }, [params.token, router]);

  return (
    <main className="flex min-h-screen items-center justify-center bg-[var(--sb-bg)] px-6">
      <div className="w-full max-w-[420px] rounded-2xl border border-[var(--sb-border)] bg-[var(--sb-pane)] p-7 text-center">
        <div className="flex justify-center"><Logo compact /></div>
        <h1 className="mt-5 text-[20px] font-semibold">加入账户</h1>
        <p className="mt-2 text-[13px] leading-[20px] text-[var(--sb-text-2)]">
          接受邀请后，你会加入该账户并共享其中的 Bot、会话与任务。
        </p>
        {status === "loading" ? (
          <Loader2 className="mx-auto mt-6 h-5 w-5 animate-spin text-[var(--sb-text-3)]" />
        ) : status === "error" ? (
          <div className="mt-5 rounded-lg bg-[var(--sb-danger-soft)] px-3 py-2 text-[12px] text-[var(--sb-danger)]">{message}</div>
        ) : (
          <button
            type="button"
            className="sb-btn mx-auto mt-6"
            data-variant="primary"
            onClick={async () => {
              try {
                await api.acceptInvitation(params.token);
                router.replace("/");
              } catch (err) {
                setStatus("error");
                setMessage(err instanceof ApiError ? err.message : "接受邀请失败");
              }
            }}
          >
            接受邀请
          </button>
        )}
      </div>
    </main>
  );
}
