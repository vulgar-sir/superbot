"""Minimal OpenAI-compatible text-to-speech service backed by Piper.

The voice model is downloaded once from the HuggingFace mirror and cached in
/voices. Speech is generated as WAV; the client plays the returned blob and
does not depend on MP3 specifically.
"""

import json
import os
import shutil
import subprocess
import tempfile
import threading
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

VOICE = os.environ.get("PIPER_VOICE", "zh_CN-huayan-medium")
VOICE_DIR = Path(os.environ.get("PIPER_VOICE_DIR", "/voices"))
MIRROR = os.environ.get("HF_ENDPOINT", "https://hf-mirror.com").rstrip("/")
PORT = int(os.environ.get("PORT", "8091"))
MODEL_PATH = VOICE_DIR / f"{VOICE}.onnx"
CONFIG_PATH = VOICE_DIR / f"{VOICE}.onnx.json"
BUNDLED_CONFIG = Path("/app/voice.json")
VOICE_URL = f"{MIRROR}/rhasspy/piper-voices/resolve/main/zh/zh_CN/huayan/medium/{VOICE}"

lock = threading.Lock()


def download(url, target):
    target.parent.mkdir(parents=True, exist_ok=True)
    partial = target.with_suffix(target.suffix + ".part")
    request = urllib.request.Request(url, headers={"User-Agent": "Mozilla/5.0"})
    with urllib.request.urlopen(request, timeout=300) as response, partial.open("wb") as handle:
        while True:
            chunk = response.read(1 << 20)
            if not chunk:
                break
            handle.write(chunk)
    partial.replace(target)


def ensure_voice():
    if not MODEL_PATH.exists() or MODEL_PATH.stat().st_size < 1_000_000:
        print(f"downloading {MODEL_PATH.name}", flush=True)
        download(VOICE_URL + ".onnx", MODEL_PATH)
    if not CONFIG_PATH.exists() or CONFIG_PATH.stat().st_size < 100:
        if BUNDLED_CONFIG.exists():
            # The mirror's small JSON files can redirect to a host that
            # rejects urllib; the image ships the matching voice config.
            shutil.copyfile(BUNDLED_CONFIG, CONFIG_PATH)
        else:
            print(f"downloading {CONFIG_PATH.name}", flush=True)
            download(VOICE_URL + ".onnx.json", CONFIG_PATH)
    print(f"voice ready: {MODEL_PATH} ({MODEL_PATH.stat().st_size} bytes)", flush=True)


class Handler(BaseHTTPRequestHandler):
    def log_message(self, fmt, *args):
        return

    def send_json(self, status, payload):
        body = json.dumps(payload, ensure_ascii=False).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        if self.path == "/healthz":
            self.send_json(200, {"status": "ok", "voice": VOICE, "ready": MODEL_PATH.exists()})
            return
        self.send_json(404, {"error": {"message": "not found"}})

    def do_POST(self):
        if self.path != "/v1/audio/speech":
            self.send_json(404, {"error": {"message": "not found"}})
            return
        try:
            length = int(self.headers.get("Content-Length", "0"))
            payload = json.loads(self.rfile.read(length) or b"{}")
            text = str(payload.get("input") or "").strip()
            if not text:
                raise ValueError("input is required")
            if len(text) > 4000:
                text = text[:4000]
            with lock:
                with tempfile.NamedTemporaryFile(suffix=".wav", delete=False) as handle:
                    output = handle.name
                try:
                    result = subprocess.run(
                        [
                            "piper",
                            "--model", str(MODEL_PATH),
                            "--config", str(CONFIG_PATH),
                            "-f", output,
                        ],
                        input=text.encode("utf-8"),
                        capture_output=True,
                        timeout=120,
                    )
                    if result.returncode != 0:
                        raise RuntimeError(result.stderr.decode("utf-8", "replace")[-500:])
                    audio = Path(output).read_bytes()
                finally:
                    Path(output).unlink(missing_ok=True)
            if not audio:
                raise RuntimeError("piper returned no audio")
            self.send_response(200)
            self.send_header("Content-Type", "audio/wav")
            self.send_header("Content-Length", str(len(audio)))
            self.end_headers()
            self.wfile.write(audio)
        except Exception as exc:  # noqa: BLE001 - report a proper API error
            self.send_json(500, {"error": {"message": str(exc)}})


if __name__ == "__main__":
    ensure_voice()
    ThreadingHTTPServer(("0.0.0.0", PORT), Handler).serve_forever()
