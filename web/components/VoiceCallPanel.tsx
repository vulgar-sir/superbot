"use client";

import { useEffect, useRef, useState } from "react";
import { MessageSquare, Mic, MicOff, Phone, Settings2, Volume2 } from "lucide-react";

import { BotAvatar } from "./BotAvatar";
import { api } from "@/lib/api";

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

export function VoiceCallPanel({
  name,
  botID,
  latestBotText,
  onSend,
  onClose,
  onOpenSettings,
}: {
  name: string;
  botID: string;
  latestBotText: string;
  onSend: (text: string) => void | Promise<void>;
  onClose: () => void;
  onOpenSettings?: () => void;
}) {
  const [listening, setListening] = useState(false);
  const [speaking, setSpeaking] = useState(false);
  const [transcript, setTranscript] = useState("");
  const [showTranscript, setShowTranscript] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const recognitionRef = useRef<SpeechRecognitionLike | null>(null);
  const lastSpokenRef = useRef(latestBotText);
  const onSendRef = useRef(onSend);
  useEffect(() => {
    onSendRef.current = onSend;
  }, [onSend]);
  const [serverSTT, setServerSTT] = useState(false);
  const [serverTTS, setServerTTS] = useState(false);
  const [webrtcAvailable, setWebrtcAvailable] = useState(false);
  const pcRef = useRef<RTCPeerConnection | null>(null);
  const dcRef = useRef<RTCDataChannel | null>(null);
  const audioContextRef = useRef<AudioContext | null>(null);
  const processorRef = useRef<ScriptProcessorNode | null>(null);
  const sourceRef = useRef<MediaStreamAudioSourceNode | null>(null);
  const gainRef = useRef<GainNode | null>(null);
  const serverListeningRef = useRef(false);
  const mediaRecorderRef = useRef<MediaRecorder | null>(null);
  const mediaStreamRef = useRef<MediaStream | null>(null);
  const audioPlayerRef = useRef<HTMLAudioElement | null>(null);
  const [voiceName] = useState(() => {
    try {
      return window.localStorage.getItem("superbot-voice") ?? "";
    } catch {
      return "";
    }
  });
  const [rate] = useState(() => {
    try {
      return Number(window.localStorage.getItem("superbot-voice-rate") ?? "1") || 1;
    } catch {
      return 1;
    }
  });
  const [language] = useState(() => {
    try {
      return window.localStorage.getItem("superbot-voice-language") ?? "auto";
    } catch {
      return "auto";
    }
  });
  const langCode =
    language === "zh"
      ? "zh-CN"
      : language === "en"
        ? "en-US"
        : navigator.language?.startsWith("zh")
          ? "zh-CN"
          : "en-US";

  useEffect(() => {
    api
      .voiceStatus()
      .then((status) => {
        setServerSTT(status.stt.configured);
        setServerTTS(status.tts.configured);
        setWebrtcAvailable(Boolean((status as any).webrtc?.configured));
      })
      .catch(() => undefined);
  }, []);

  useEffect(() => {
    lastSpokenRef.current = latestBotText;
    return () => {
      recognitionRef.current?.stop();
      if (typeof window !== "undefined" && window.speechSynthesis) {
        window.speechSynthesis.cancel();
      }
    };
  }, []);

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  useEffect(() => {
    if (!latestBotText || latestBotText === lastSpokenRef.current) return;
    lastSpokenRef.current = latestBotText;
    if (serverTTS) {
      let cancelled = false;
      let objectURL = "";
      setSpeaking(true);
      api
        .speakVoice(latestBotText.slice(0, 4000))
        .then((blob) => {
          if (cancelled) return;
          objectURL = URL.createObjectURL(blob);
          const audio = new Audio(objectURL);
          audioPlayerRef.current = audio;
          audio.onended = () => setSpeaking(false);
          audio.onerror = () => setSpeaking(false);
          void audio.play().catch(() => setSpeaking(false));
        })
        .catch(() => {
          if (!cancelled) setSpeaking(false);
        });
      return () => {
        cancelled = true;
        audioPlayerRef.current?.pause();
        audioPlayerRef.current = null;
        if (objectURL) URL.revokeObjectURL(objectURL);
      };
    }
    if (typeof window === "undefined" || !window.speechSynthesis) return;
    window.speechSynthesis.cancel();
    const utterance = new SpeechSynthesisUtterance(latestBotText.slice(0, 4000));
    utterance.lang = langCode;
    utterance.rate = rate;
    if (voiceName) {
      const voice = window.speechSynthesis.getVoices().find((item) => item.name === voiceName);
      if (voice) utterance.voice = voice;
    }
    utterance.onstart = () => setSpeaking(true);
    utterance.onend = () => setSpeaking(false);
    utterance.onerror = () => setSpeaking(false);
    window.speechSynthesis.speak(utterance);
  }, [latestBotText, langCode, rate, voiceName, serverTTS]);

  const cleanupWebRTC = () => {
    processorRef.current?.disconnect();
    sourceRef.current?.disconnect();
    gainRef.current?.disconnect();
    processorRef.current = null;
    sourceRef.current = null;
    gainRef.current = null;
    void audioContextRef.current?.close().catch(() => undefined);
    audioContextRef.current = null;
    dcRef.current?.close();
    dcRef.current = null;
    pcRef.current?.close();
    pcRef.current = null;
    mediaStreamRef.current?.getTracks().forEach((track) => track.stop());
    mediaStreamRef.current = null;
    serverListeningRef.current = false;
    setListening(false);
  };

  const startWebRTCListening = async () => {
    if (!navigator.mediaDevices?.getUserMedia || typeof RTCPeerConnection === "undefined") {
      return startServerListening();
    }
    try {
      const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
      mediaStreamRef.current = stream;
      const pc = new RTCPeerConnection();
      pcRef.current = pc;
      const channel = pc.createDataChannel("audio");
      dcRef.current = channel;
      const processMessage = (raw: string) => {
        try {
          const payload = JSON.parse(raw);
          (window as any).__webrtcMessages = [...((window as any).__webrtcMessages || []), payload];
          if (payload.type === "transcript" && typeof payload.text === "string" && payload.text.trim()) {
            setTranscript(payload.text);
            (window as any).__voiceSendAttempts = [...((window as any).__voiceSendAttempts || []), payload.text.trim()];
            Promise.resolve(onSendRef.current(payload.text.trim()))
              .then(() => {
                (window as any).__voiceSendOk = [...((window as any).__voiceSendOk || []), payload.text.trim()];
              })
              .catch((err) => {
                (window as any).__voiceSendErrors = [...((window as any).__voiceSendErrors || []), String(err)];
              });
          } else if (payload.type === "error") {
            setError(String(payload.message || "WebRTC 语音错误"));
          }
        } catch {
          // ignore malformed frames
        }
      };
      channel.onopen = () => {
        (window as any).__webrtcOpen = true;
      };
      channel.onmessage = (event) => {
        if (typeof event.data === "string") {
          processMessage(event.data);
        } else if (event.data instanceof Blob) {
          void event.data.text().then(processMessage);
        } else if (event.data instanceof ArrayBuffer) {
          processMessage(new TextDecoder().decode(event.data));
        }
      };
      pc.onconnectionstatechange = () => {
        if (pc.connectionState === "failed" || pc.connectionState === "disconnected") {
          setError("WebRTC 连接中断，已回退到分片模式");
          cleanupWebRTC();
        }
      };
      const offer = await pc.createOffer();
      await pc.setLocalDescription(offer);
      const answer = await api.webrtcOffer(offer.sdp || "");
      await pc.setRemoteDescription({ type: "answer", sdp: answer.sdp });
      const audioContext = new AudioContext();
      audioContextRef.current = audioContext;
      const source = audioContext.createMediaStreamSource(stream);
      sourceRef.current = source;
      const processor = audioContext.createScriptProcessor(4096, 1, 1);
      processorRef.current = processor;
      const gain = audioContext.createGain();
      gain.gain.value = 0;
      gainRef.current = gain;
      processor.onaudioprocess = (event) => {
        if (!dcRef.current || dcRef.current.readyState !== "open") return;
        const input = event.inputBuffer.getChannelData(0);
        const ratio = audioContext.sampleRate / 16000;
        const length = Math.floor(input.length / ratio);
        const pcm = new Int16Array(length);
        for (let index = 0; index < length; index += 1) {
          const sample = Math.max(-1, Math.min(1, input[Math.floor(index * ratio)]));
          pcm[index] = sample < 0 ? sample * 0x8000 : sample * 0x7fff;
        }
        const bytes = new Uint8Array(pcm.buffer);
        let binary = "";
        for (let index = 0; index < bytes.length; index += 1) binary += String.fromCharCode(bytes[index]);
        dcRef.current.send(JSON.stringify({ type: "audio", data: btoa(binary), sample_rate: 16000 }));
      };
      source.connect(processor);
      processor.connect(gain);
      gain.connect(audioContext.destination);
      serverListeningRef.current = true;
      setListening(true);
    } catch (err) {
      cleanupWebRTC();
      setError(err instanceof Error ? err.message : "WebRTC 启动失败");
    }
  };

  const stopServerListening = () => {
    cleanupWebRTC();
    serverListeningRef.current = false;
    if (mediaRecorderRef.current && mediaRecorderRef.current.state !== "inactive") {
      mediaRecorderRef.current.stop();
    }
    mediaRecorderRef.current = null;
    mediaStreamRef.current?.getTracks().forEach((track) => track.stop());
    mediaStreamRef.current = null;
    setListening(false);
  };

  const startServerListening = async () => {
    if (!navigator.mediaDevices?.getUserMedia) {
      setError("当前环境不支持麦克风录音。");
      return;
    }
    try {
      const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
      mediaStreamRef.current = stream;
      serverListeningRef.current = true;
      setListening(true);
      const recordChunk = () => {
        if (!serverListeningRef.current) return;
        const recorder = new MediaRecorder(stream);
        mediaRecorderRef.current = recorder;
        const chunks: Blob[] = [];
        recorder.ondataavailable = (event) => {
          if (event.data.size > 0) chunks.push(event.data);
        };
        recorder.onstop = async () => {
          if (!serverListeningRef.current) return;
          const blob = new Blob(chunks, { type: recorder.mimeType || "audio/webm" });
          if (blob.size > 0) {
            try {
              const result = await api.transcribeVoice(
                new File([blob], `call-${Date.now()}.webm`, { type: blob.type }),
              );
              const text = result.text.trim();
              if (text) {
                setTranscript(text);
                void onSendRef.current(text);
              }
            } catch (err) {
              setError(err instanceof Error ? err.message : "语音转写失败");
            }
          }
          if (serverListeningRef.current) window.setTimeout(recordChunk, 200);
        };
        recorder.start();
        window.setTimeout(() => {
          if (recorder.state !== "inactive") recorder.stop();
        }, 4000);
      };
      recordChunk();
    } catch (err) {
      setError(err instanceof Error ? err.message : "麦克风权限被拒绝。");
    }
  };

  const startListening = () => {
    setError(null);
    if (webrtcAvailable) {
      void startWebRTCListening();
      return;
    }
    if (serverSTT) {
      void startServerListening();
      return;
    }
    const globalWindow = window as any;
    const Recognition = globalWindow.SpeechRecognition || globalWindow.webkitSpeechRecognition;
    if (!Recognition) {
      setError("当前浏览器不支持语音识别。");
      return;
    }
    const recognition: SpeechRecognitionLike = new Recognition();
    recognition.lang = langCode;
    recognition.continuous = true;
    recognition.interimResults = true;
    recognition.onresult = (event: any) => {
      let finalText = "";
      let interimText = "";
      for (let i = event.resultIndex; i < event.results.length; i += 1) {
        const result = event.results[i];
        if (result.isFinal) finalText += result[0].transcript;
        else interimText += result[0].transcript;
      }
      setTranscript(interimText || finalText);
      if (finalText.trim()) {
        setTranscript("");
        void onSend(finalText.trim());
      }
    };
    recognition.onend = () => {
      setListening(false);
      if (recognitionRef.current === recognition) {
        try {
          recognition.start();
          setListening(true);
        } catch {
          // user closed the call
        }
      }
    };
    recognition.onerror = (event: any) => {
      setListening(false);
      setError(event?.error === "not-allowed" ? "麦克风权限被拒绝。" : "语音识别失败。");
    };
    recognitionRef.current = recognition;
    setListening(true);
    recognition.start();
  };

  const stopListening = () => {
    recognitionRef.current?.stop();
    recognitionRef.current = null;
    stopServerListening();
    setListening(false);
  };

  const endCall = () => {
    stopListening();
    audioPlayerRef.current?.pause();
    audioPlayerRef.current = null;
    if (window.speechSynthesis) window.speechSynthesis.cancel();
    onClose();
  };

  return (
    <div className="fixed left-1/2 top-[74px] z-50 flex -translate-x-1/2 flex-col items-center">
      <div className="flex items-center gap-3 rounded-full border border-[var(--sb-border)] bg-[var(--sb-pane)] px-4 py-2.5 shadow-[var(--sb-shadow-modal)]">
        <BotAvatar name={name} id={botID} size={40} />
        <div className="flex h-6 items-center gap-1.5 px-1" aria-hidden>
          {[0, 1, 2, 3, 4, 5, 6, 7, 8, 9].map((index) => (
            <span
              key={index}
              className="sb-wave-dot"
              style={{
                animationDelay: `${index * 80}ms`,
                background: speaking ? "var(--sb-success)" : "var(--sb-text-4)",
              }}
            />
          ))}
        </div>
        <button
          type="button"
          className="sb-icon-btn !h-10 !w-10"
          onClick={onOpenSettings}
          aria-label="显示设置"
          title="语音设置"
        >
          <Settings2 className="h-4 w-4" />
        </button>
        <button
          type="button"
          className="sb-icon-btn !h-10 !w-10"
          onClick={() => setShowTranscript((prev) => !prev)}
          aria-label="显示文字记录"
          title="文字记录"
          data-variant={showTranscript ? undefined : "plain"}
        >
          <MessageSquare className="h-4 w-4" />
        </button>
        <button
          type="button"
          className="sb-icon-btn !h-10 !w-10"
          onClick={listening ? stopListening : startListening}
          aria-label={listening ? "静音" : "取消静音"}
          title={listening ? "静音" : "取消静音"}
          data-variant={listening ? undefined : "plain"}
        >
          {listening ? <Mic className="h-4 w-4" /> : <MicOff className="h-4 w-4" />}
        </button>
        <button
          type="button"
          onClick={endCall}
          className="flex h-11 w-11 items-center justify-center rounded-full bg-[var(--sb-danger)] text-white transition hover:brightness-105"
          aria-label="挂断"
          title="挂断"
        >
          <Phone className="h-5 w-5 rotate-[135deg]" />
        </button>
      </div>

      {showTranscript && (
        <div className="mt-2 w-[360px] max-w-[90vw] rounded-2xl border border-[var(--sb-border)] bg-[var(--sb-pane)] p-4 text-left shadow-[var(--sb-shadow-popup)]">
          <div className="flex items-center gap-2 text-[12px] text-[var(--sb-text-2)]">
            <Volume2 className="h-3.5 w-3.5" />
            {speaking ? "Bot 正在说话" : listening ? "正在聆听…" : "语音通话"}
          </div>
          <div className="mt-2 min-h-[56px] whitespace-pre-wrap text-[13px] leading-[20px] text-[var(--sb-text)]">
            {transcript || latestBotText || (listening ? "请说话…" : "点击麦克风开始说话")}
          </div>
          {error && (
            <div className="mt-2 rounded-lg bg-[var(--sb-danger-soft)] px-2.5 py-1.5 text-[11px] text-[var(--sb-danger)]">
              {error}
            </div>
          )}
        </div>
      )}
    </div>
  );
}
