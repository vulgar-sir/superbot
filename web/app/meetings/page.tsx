"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { ArrowLeft, CalendarDays, Loader2, Mic, Sparkles, Square, Trash2, Upload } from "lucide-react";

import { api, ApiError } from "@/lib/api";
import type { MeetingItem, User } from "@/lib/types";
import { Rail } from "@/components/Rail";
import { useToast } from "@/components/ui/Toast";
import { cn, timeAgo } from "@/lib/format";

type SpeechRecognitionLike = {
  lang: string;
  continuous: boolean;
  interimResults: boolean;
  start: () => void;
  stop: () => void;
  onresult: ((event: any) => void) | null;
  onend: (() => void) | null;
  onerror: ((event: any) => void) | null;
};

export default function MeetingsPage() {
  const router = useRouter();
  const { toast } = useToast();
  const [user, setUser] = useState<User | null>(null);
  const [meetings, setMeetings] = useState<MeetingItem[]>([]);
  const [active, setActive] = useState<MeetingItem | null>(null);
  const [title, setTitle] = useState("");
  const [transcript, setTranscript] = useState("");
  const [summary, setSummary] = useState("");
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [summarizing, setSummarizing] = useState(false);
  const [transcribing, setTranscribing] = useState(false);
  const [sttAvailable, setSttAvailable] = useState(false);
  const [captionsOn, setCaptionsOn] = useState(false);
  const [interimCaption, setInterimCaption] = useState("");
  const recognitionRef = useRef<SpeechRecognitionLike | null>(null);
  const audioInputRef = useRef<HTMLInputElement>(null);
  const [recording, setRecording] = useState(false);
  const [recordSeconds, setRecordSeconds] = useState(0);
  const recorderRef = useRef<MediaRecorder | null>(null);
  const recordStreamRef = useRef<MediaStream | null>(null);
  const recordChunksRef = useRef<Blob[]>([]);
  const recordTimerRef = useRef<number | null>(null);
  const recordingRef = useRef(false);
  const recordCancelRef = useRef(false);
  const transcribeQueueRef = useRef<Promise<void>>(Promise.resolve());

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [me, result, capabilities] = await Promise.all([
        api.me(),
        api.listMeetings(),
        api.capabilities().catch(() => ({ capabilities: [] })),
      ]);
      setUser(me.user);
      setMeetings(result.meetings);
      setSttAvailable(
        capabilities.capabilities.some((item) => item.key === "voice" && item.status === "available"),
      );
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) router.replace("/login");
      else toast(err instanceof ApiError ? err.message : "读取会议失败", "danger");
    } finally {
      setLoading(false);
    }
  }, [router, toast]);

  useEffect(() => {
    void load();
  }, [load]);

  const open = async (meeting: MeetingItem) => {
    try {
      const detail = await api.getMeeting(meeting.id);
      setActive(detail.meeting);
      setTitle(detail.meeting.title);
      setTranscript(detail.meeting.transcript || "");
      setSummary(detail.meeting.summary);
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "读取会议详情失败", "danger");
    }
  };

  const create = async () => {
    if (!title.trim() || !transcript.trim()) return;
    setSaving(true);
    try {
      const result = await api.createMeeting({ title: title.trim(), transcript });
      setMeetings((prev) => [result.meeting, ...prev]);
      setActive(result.meeting);
      setSummary(result.meeting.summary);
      toast("会议记录已保存", "success");
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "保存会议失败", "danger");
    } finally {
      setSaving(false);
    }
  };

  const transcribeAudio = (file: File) => {
    const run = async () => {
      setTranscribing(true);
      try {
        const result = await api.transcribeVoice(file);
        setTranscript((prev) => (prev.trim() ? `${prev.trim()}
${result.text}` : result.text));
      } catch (err) {
        toast(err instanceof ApiError ? err.message : "音频转写失败", "danger");
      } finally {
        setTranscribing(false);
      }
    };
    transcribeQueueRef.current = transcribeQueueRef.current.then(run, run);
    return transcribeQueueRef.current;
  };

  const stopRecording = useCallback((cancel = false) => {
    if (recordTimerRef.current !== null) window.clearInterval(recordTimerRef.current);
    recordTimerRef.current = null;
    recordCancelRef.current = cancel;
    recordingRef.current = false;
    const recorder = recorderRef.current;
    if (cancel) {
      recorderRef.current = null;
      recordStreamRef.current?.getTracks().forEach((track) => track.stop());
      recordStreamRef.current = null;
      setRecording(false);
      setRecordSeconds(0);
      return;
    }
    if (recorder && recorder.state !== "inactive") recorder.stop();
  }, []);

  const startRecording = async () => {
    if (!navigator.mediaDevices?.getDisplayMedia) {
      toast("当前环境不支持录制（需要 HTTPS 或桌面客户端）", "danger");
      return;
    }
    try {
      const stream = await navigator.mediaDevices.getDisplayMedia({
        video: true,
        audio: true,
      });
      const audioTracks = stream.getAudioTracks();
      if (audioTracks.length === 0) {
        stream.getTracks().forEach((track) => track.stop());
        toast("共享时没有勾选音频，请重新选择并共享标签页音频", "danger");
        return;
      }
      const audioStream = new MediaStream(audioTracks);
      recordStreamRef.current = stream;
      recordCancelRef.current = false;
      recordingRef.current = true;

      const finish = (streamToStop: MediaStream) => {
        if (recordTimerRef.current !== null) window.clearInterval(recordTimerRef.current);
        recordTimerRef.current = null;
        streamToStop.getTracks().forEach((track) => track.stop());
        recordStreamRef.current = null;
        recorderRef.current = null;
        setRecording(false);
      };

      const recordChunk = () => {
        const recorder = new MediaRecorder(audioStream);
        recorderRef.current = recorder;
        const chunks: Blob[] = [];
        recorder.ondataavailable = (event) => {
          if (event.data.size > 0) chunks.push(event.data);
        };
        recorder.onstop = () => {
          const cancelled = recordCancelRef.current;
          const blob = new Blob(chunks, { type: recorder.mimeType || "audio/webm" });
          if (!cancelled && blob.size > 0) {
            void transcribeAudio(
              new File([blob], `meeting-${Date.now()}.webm`, { type: blob.type }),
            );
          }
          if (!cancelled && recordingRef.current && sttAvailable) {
            window.setTimeout(recordChunk, 0);
            return;
          }
          finish(stream);
        };
        recorder.start();
        if (sttAvailable) {
          window.setTimeout(() => {
            if (recorder.state !== "inactive") recorder.stop();
          }, 20_000);
        }
      };

      recordChunk();
      setRecording(true);
      setRecordSeconds(0);
      const started = Date.now();
      recordTimerRef.current = window.setInterval(() => {
        const elapsed = Math.floor((Date.now() - started) / 1000);
        setRecordSeconds(elapsed);
      }, 500);
    } catch (err) {
      recordStreamRef.current?.getTracks().forEach((track) => track.stop());
      recordStreamRef.current = null;
      toast(err instanceof Error ? err.message : "无法开始录制", "danger");
    }
  };

  const stopCaptions = useCallback(() => {
    recognitionRef.current?.stop();
    recognitionRef.current = null;
    setCaptionsOn(false);
    setInterimCaption("");
  }, []);

  const startCaptions = () => {
    const Constructor =
      (window as unknown as { SpeechRecognition?: new () => SpeechRecognitionLike }).SpeechRecognition ??
      (window as unknown as { webkitSpeechRecognition?: new () => SpeechRecognitionLike }).webkitSpeechRecognition;
    if (!Constructor) {
      toast("当前环境不支持浏览器语音识别", "danger");
      return;
    }
    try {
      const recognition = new Constructor();
      recognition.lang = navigator.language?.startsWith("zh") ? "zh-CN" : "en-US";
      recognition.continuous = true;
      recognition.interimResults = true;
      recognition.onresult = (event: any) => {
        let finalText = "";
        let interimText = "";
        for (let index = event.resultIndex; index < event.results.length; index += 1) {
          const result = event.results[index];
          if (result.isFinal) finalText += result[0].transcript;
          else interimText += result[0].transcript;
        }
        if (finalText.trim()) {
          setTranscript((prev) => (prev.trim() ? `${prev.trim()}
${finalText.trim()}` : finalText.trim()));
        }
        setInterimCaption(interimText);
      };
      recognition.onerror = (event: any) => {
        toast(`语音识别失败：${event?.error || "unknown"}`, "danger");
        setCaptionsOn(false);
        setInterimCaption("");
      };
      recognition.onend = () => {
        setCaptionsOn(false);
        setInterimCaption("");
      };
      recognitionRef.current = recognition;
      recognition.start();
      setCaptionsOn(true);
    } catch (err) {
      toast(err instanceof Error ? err.message : "无法启动实时字幕", "danger");
    }
  };

  useEffect(() => () => recognitionRef.current?.stop(), []);

  const summarize = async () => {
    if (!active) return;
    setSummarizing(true);
    try {
      const result = await api.summarizeMeeting(active.id);
      setSummary(result.meeting.summary);
      setMeetings((prev) => prev.map((item) => (item.id === active.id ? { ...item, summary: result.meeting.summary } : item)));
      toast("摘要已生成", "success");
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "生成摘要失败", "danger");
    } finally {
      setSummarizing(false);
    }
  };

  const remove = async () => {
    if (!active) return;
    try {
      await api.deleteMeeting(active.id);
      const next = meetings.filter((item) => item.id !== active.id);
      setMeetings(next);
      setActive(null);
      setTitle("");
      setTranscript("");
      setSummary("");
      toast("会议已删除", "success");
    } catch (err) {
      toast(err instanceof ApiError ? err.message : "删除会议失败", "danger");
    }
  };

  if (!user) return <div className="flex h-screen items-center justify-center"><Loader2 className="h-5 w-5 animate-spin" /></div>;

  return (
    <div className="flex h-screen w-full overflow-hidden bg-[var(--sb-bg)]">
      <Rail user={user} onLogout={() => void api.logout().finally(() => router.replace("/login"))} />
      <main className="flex min-w-0 flex-1 flex-col pb-16 md:pb-0">
        <header className="sb-page-header flex h-[61px] shrink-0 items-center gap-3 border-b border-[var(--sb-border)] px-6">
          <Link href="/calendar" className="sb-icon-btn" aria-label="返回日历">
            <ArrowLeft className="h-4 w-4" />
          </Link>
          <h1 className="text-[17px] font-semibold">会议记录</h1>
          <span className="text-[11px] text-[var(--sb-text-3)]">上传 transcript，用主 Bot 模型生成摘要与行动项</span>
        </header>

        <div className="flex min-h-0 flex-1">
          <aside className="hidden w-[280px] shrink-0 overflow-y-auto border-r border-[var(--sb-border)] p-3 sm:block">
            {loading ? (
              <div className="flex justify-center py-10"><Loader2 className="h-4 w-4 animate-spin" /></div>
            ) : meetings.length === 0 ? (
              <div className="px-2 py-8 text-center text-[12px] text-[var(--sb-text-3)]">还没有会议记录</div>
            ) : (
              <div className="space-y-1">
                {meetings.map((meeting) => (
                  <button
                    key={meeting.id}
                    type="button"
                    onClick={() => void open(meeting)}
                    className={cn(
                      "w-full rounded-xl px-3 py-2.5 text-left",
                      active?.id === meeting.id ? "bg-[var(--sb-hover)]" : "hover:bg-[var(--sb-hover)]",
                    )}
                  >
                    <span className="block truncate text-[13px]">{meeting.title}</span>
                    <span className="mt-0.5 block text-[10px] text-[var(--sb-text-3)]">
                      {meeting.summary ? "已有摘要" : "待摘要"} · {timeAgo(meeting.starts_at)}
                    </span>
                  </button>
                ))}
              </div>
            )}
          </aside>

          <section className="min-w-0 flex-1 overflow-y-auto px-6 py-5">
            <div className="mx-auto max-w-[900px] space-y-4">
              <section className="rounded-2xl border border-[var(--sb-border)] bg-[var(--sb-pane)] p-4">
                <div className="flex flex-wrap items-center gap-2">
                  <input value={title} onChange={(event) => setTitle(event.target.value)} placeholder="会议标题" className="field min-w-[220px] flex-1" />
                  <input
                    ref={audioInputRef}
                    type="file"
                    accept="audio/*"
                    className="hidden"
                    onChange={(event) => {
                      const file = event.target.files?.[0];
                      event.target.value = "";
                      if (file) void transcribeAudio(file);
                    }}
                  />
                  {recording ? (
                    <button type="button" onClick={() => stopRecording(false)} className="sb-btn" data-variant="primary" data-size="sm">
                      <Square className="h-3.5 w-3.5" />
                      停止并转写 {Math.floor(recordSeconds / 60)}:{String(recordSeconds % 60).padStart(2, "0")}
                      {sttAvailable ? " · 实时字幕" : ""}
                    </button>
                  ) : (
                    <button type="button" onClick={() => void startRecording()} disabled={transcribing} className="sb-btn" data-variant="secondary" data-size="sm">
                      <Mic className="h-3.5 w-3.5" />
                      录制会议音频
                      {sttAvailable ? " · 实时字幕" : ""}
                    </button>
                  )}
                  <button
                    type="button"
                    onClick={() => (captionsOn ? stopCaptions() : startCaptions())}
                    className="sb-btn"
                    data-variant={captionsOn ? "primary" : "secondary"}
                    data-size="sm"
                  >
                    <Mic className="h-3.5 w-3.5" />
                    {captionsOn ? "停止字幕" : "麦克风实时字幕"}
                  </button>
                  <button type="button" onClick={() => audioInputRef.current?.click()} disabled={transcribing} className="sb-btn" data-variant="secondary" data-size="sm">
                    {transcribing ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Upload className="h-3.5 w-3.5" />}
                    上传音频转写
                  </button>
                  <button type="button" onClick={() => void create()} disabled={saving || !title.trim() || !transcript.trim()} className="sb-btn" data-variant="primary" data-size="sm">
                    {saving ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <CalendarDays className="h-3.5 w-3.5" />}
                    保存记录
                  </button>
                  {active && (
                    <button type="button" onClick={() => void remove()} className="sb-icon-btn" aria-label="删除会议">
                      <Trash2 className="h-4 w-4" />
                    </button>
                  )}
                </div>
                <textarea
                  value={transcript}
                  onChange={(event) => setTranscript(event.target.value)}
                  placeholder="粘贴会议 transcript…"
                  className="field mt-3 min-h-[220px] w-full resize-y font-mono text-[12px] leading-5"
                />
                {captionsOn && (
                  <div className="mt-2 rounded-lg bg-[var(--sb-hover)] px-3 py-2 text-[12px] text-[var(--sb-text-2)]">
                    实时字幕：{interimCaption || "正在听…"}
                  </div>
                )}
              </section>

              {active && (
                <section className="rounded-2xl border border-[var(--sb-border)] bg-[var(--sb-pane)] p-4">
                  <div className="flex items-center justify-between gap-2">
                    <div className="text-[13px] font-medium">摘要与行动项</div>
                    <button type="button" onClick={() => void summarize()} disabled={summarizing} className="sb-btn" data-variant="primary" data-size="sm">
                      {summarizing ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Sparkles className="h-3.5 w-3.5" />}
                      生成摘要
                    </button>
                  </div>
                  {summary ? (
                    <pre className="mt-3 whitespace-pre-wrap text-[13px] leading-6">{summary}</pre>
                  ) : (
                    <div className="mt-4 text-[12px] text-[var(--sb-text-3)]">尚未生成摘要</div>
                  )}
                </section>
              )}
            </div>
          </section>
        </div>
      </main>
    </div>
  );
}
