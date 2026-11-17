"""Tests for gate.py: python3 -m unittest deploy/images/computer/gate_test.py"""

import os
import socket
import sys
import threading
import time
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import gate  # noqa: E402

TOKEN = b"secret-token"


def keep_alive_server():
    """A service that, like Chromium's DevTools, answers every request on a
    connection and keeps it open whatever the request said."""
    server = socket.socket()
    server.bind(("127.0.0.1", 0))
    server.listen(8)
    seen = []

    def serve(conn):
        buf = b""
        while True:
            try:
                data = conn.recv(65536)
            except OSError:
                return
            if not data:
                return
            buf += data
            while b"\r\n\r\n" in buf:
                head, buf = buf.split(b"\r\n\r\n", 1)
                seen.append(head)
                body = b'{"ok":true}'
                conn.sendall(b"HTTP/1.1 200 OK\r\nContent-Type: application/json\r\n"
                             b"Content-Length: %d\r\n\r\n%s" % (len(body), body))

    def accept():
        while True:
            conn, _ = server.accept()
            threading.Thread(target=serve, args=(conn,), daemon=True).start()

    threading.Thread(target=accept, daemon=True).start()
    return server.getsockname()[1], seen


def start_gate(upstream_port):
    listener = socket.socket()
    listener.bind(("127.0.0.1", 0))
    port = listener.getsockname()[1]
    listener.close()
    threading.Thread(target=gate.serve, args=(port, upstream_port, TOKEN), daemon=True).start()
    time.sleep(0.2)
    return port


def exchange(port, payload, wait=1.0):
    conn = socket.create_connection(("127.0.0.1", port))
    conn.settimeout(wait)
    conn.sendall(payload)
    data = b""
    try:
        while True:
            chunk = conn.recv(65536)
            if not chunk:
                break
            data += chunk
    except socket.timeout:
        pass
    conn.close()
    return data


class RewriteHeadTest(unittest.TestCase):
    def test_refuses_missing_or_wrong_token(self):
        head = b"GET /json/version HTTP/1.1\r\nHost: bot:9223"
        self.assertEqual(gate.rewrite_head(head, TOKEN, b"127.0.0.1:9222"), (b"", False))
        wrong = head + b"\r\nX-Superbot-Gate: nope"
        self.assertEqual(gate.rewrite_head(wrong, TOKEN, b"127.0.0.1:9222"), (b"", False))
        self.assertEqual(gate.rewrite_head(head + b"\r\nX-Superbot-Gate: " + TOKEN, b"", b"x"), (b"", False))

    def test_strips_token_and_rewrites_host(self):
        head = b"GET /json/version HTTP/1.1\r\nHost: bot:9223\r\nX-Superbot-Gate: " + TOKEN + b"\r\nConnection: keep-alive"
        out, upgrade = gate.rewrite_head(head, TOKEN, b"127.0.0.1:9222")
        self.assertFalse(upgrade)
        self.assertNotIn(TOKEN, out)
        self.assertIn(b"Host: 127.0.0.1:9222\r\n", out)
        self.assertIn(b"Connection: close\r\n", out)
        self.assertNotIn(b"keep-alive", out)

    def test_keeps_websocket_upgrade(self):
        head = (b"GET /devtools/browser/x HTTP/1.1\r\nHost: bot:9223\r\nConnection: Upgrade\r\n"
                b"Upgrade: websocket\r\nX-Superbot-Gate: " + TOKEN)
        out, upgrade = gate.rewrite_head(head, TOKEN, b"127.0.0.1:9222")
        self.assertTrue(upgrade)
        self.assertIn(b"Connection: Upgrade", out)

    def test_rejects_malformed_and_smuggling(self):
        self.assertEqual(gate.rewrite_head(b"garbage", TOKEN, b"x"), (None, False))
        two_hosts = b"GET / HTTP/1.1\r\nHost: a\r\nHost: b\r\nX-Superbot-Gate: " + TOKEN
        self.assertEqual(gate.rewrite_head(two_hosts, TOKEN, b"x"), (None, False))
        two_tokens = b"GET / HTTP/1.1\r\nX-Superbot-Gate: " + TOKEN + b"\r\nX-Superbot-Gate: " + TOKEN
        self.assertEqual(gate.rewrite_head(two_tokens, TOKEN, b"x"), (None, False))


class GateConnectionTest(unittest.TestCase):
    def test_one_checked_request_per_connection(self):
        upstream, seen = keep_alive_server()
        port = start_gate(upstream)
        self.assertIn(b"403 Forbidden", exchange(port, b"GET /json HTTP/1.1\r\nHost: x\r\n\r\n"))
        self.assertEqual(seen, [])
        # An authenticated request followed by an unauthenticated one on the
        # same connection: only the first reaches the service.
        reply = exchange(port, b"GET /json/version HTTP/1.1\r\nHost: x\r\nX-Superbot-Gate: " + TOKEN +
                         b"\r\n\r\nGET /json/list HTTP/1.1\r\nHost: x\r\n\r\n")
        self.assertIn(b"200 OK", reply)
        self.assertIn(b"Connection: close", reply)
        self.assertTrue(reply.endswith(b'{"ok":true}'), reply)
        time.sleep(0.2)
        self.assertEqual(len(seen), 1, seen)
        self.assertTrue(seen[0].startswith(b"GET /json/version"))
        self.assertNotIn(TOKEN, seen[0])


if __name__ == "__main__":
    unittest.main()
