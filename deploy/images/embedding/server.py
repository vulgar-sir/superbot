"""Minimal OpenAI-compatible embeddings service backed by fastembed.

The memory subsystem calls POST /v1/embeddings and expects a 1024-dimension
vector. Keeping this service self-hosted avoids an external embedding API key;
the model is downloaded from the configured HuggingFace mirror on first start.
"""

import json
import os
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

from fastembed import TextEmbedding

MODEL = os.environ.get("EMBEDDING_MODEL", "intfloat/multilingual-e5-large")
CACHE_DIR = os.environ.get("FASTEMBED_CACHE_PATH", "/models")
THREADS = int(os.environ.get("EMBEDDING_THREADS", "4"))
PORT = int(os.environ.get("PORT", "8090"))

model = TextEmbedding(model_name=MODEL, cache_dir=CACHE_DIR, threads=THREADS)
lock = threading.Lock()


def embed(texts):
    with lock:
        vectors = list(model.embed(texts))
    return [[float(value) for value in vector] for vector in vectors]


class Handler(BaseHTTPRequestHandler):
    def log_message(self, fmt, *args):  # keep container logs quiet
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
            self.send_json(200, {"status": "ok", "model": MODEL, "dimension": 1024})
            return
        self.send_json(404, {"error": {"message": "not found"}})

    def do_POST(self):
        if self.path != "/v1/embeddings":
            self.send_json(404, {"error": {"message": "not found"}})
            return
        try:
            length = int(self.headers.get("Content-Length", "0"))
            payload = json.loads(self.rfile.read(length) or b"{}")
            raw_input = payload.get("input")
            if isinstance(raw_input, str):
                inputs = [raw_input]
            elif isinstance(raw_input, list) and all(isinstance(item, str) for item in raw_input):
                inputs = raw_input
            else:
                raise ValueError("input must be a string or an array of strings")
            if not inputs:
                raise ValueError("input is required")
            vectors = embed(inputs)
            data = [
                {"object": "embedding", "index": index, "embedding": vector}
                for index, vector in enumerate(vectors)
            ]
            self.send_json(200, {
                "object": "list",
                "model": payload.get("model") or MODEL,
                "data": data,
                "usage": {"prompt_tokens": 0, "total_tokens": 0},
            })
        except Exception as exc:  # noqa: BLE001 - return a proper API error
            self.send_json(400, {"error": {"message": str(exc)}})


if __name__ == "__main__":
    ThreadingHTTPServer(("0.0.0.0", PORT), Handler).serve_forever()
