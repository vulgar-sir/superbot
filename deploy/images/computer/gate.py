"""Token gate in front of the computer's remote-control endpoints.

Chromium's DevTools port (CDP) and the noVNC/websockify server listen on
loopback only. This gate is the only thing listening on the container
network: it accepts a connection only when the first HTTP request on it
carries the computer's secret in the X-Superbot-Gate header, removes the
header and forwards the connection to the loopback service. Other computers
on the sandbox network therefore cannot drive this computer's browser or
screen.

The worker and the API derive the secret from the server key and the bot id
(internal/sandbox/gate.go); the worker passes it to the container as
SUPERBOT_GATE_TOKEN. Without a token every connection is refused.

Usage: gate.py LISTEN_PORT:UPSTREAM_PORT [LISTEN_PORT:UPSTREAM_PORT ...]
"""

import hmac
import os
import socket
import sys
import threading

HEADER = b"x-superbot-gate"
MAX_HEAD = 64 * 1024
HEAD_TIMEOUT = 15.0
UPSTREAM_TIMEOUT = 60.0
FORBIDDEN = (
    b"HTTP/1.1 403 Forbidden\r\nContent-Type: text/plain\r\n"
    b"Content-Length: 10\r\nConnection: close\r\n\r\nforbidden\n"
)
BAD_REQUEST = (
    b"HTTP/1.1 400 Bad Request\r\nContent-Type: text/plain\r\n"
    b"Content-Length: 12\r\nConnection: close\r\n\r\nbad request\n"
)


def read_head(sock):
    """Read up to the end of the first request head. Returns (head, rest) or
    (None, b"") when the peer closed early or sent an oversized head."""
    data = b""
    while True:
        end = data.find(b"\r\n\r\n")
        if end >= 0:
            return data[:end], data[end + 4:]
        if len(data) > MAX_HEAD:
            return None, b""
        chunk = sock.recv(16384)
        if not chunk:
            return None, b""
        data += chunk


def rewrite_head(head, token, upstream_host):
    """Check the token and return (head to forward, is a WebSocket upgrade):
    (None, False) for a malformed request, (b"", False) for a refused one.

    The gate header is removed and Host is replaced with the loopback
    address: Chromium refuses DevTools requests whose Host is a name. Only
    the first request of a connection is checked, so a plain HTTP request
    is sent with "Connection: close" and the gate relays exactly one
    response (see relay_response): every further request needs a new,
    checked connection. WebSocket upgrades keep their headers."""
    lines = head.split(b"\r\n")
    if not lines or lines[0].count(b" ") < 2:
        return None, False
    out = [lines[0]]
    supplied = None
    host_seen = False
    upgrade = False
    for line in lines[1:]:
        name, sep, value = line.partition(b":")
        if not sep or not name.strip():
            return None, False
        key = name.strip().lower()
        if key == HEADER:
            if supplied is not None:
                return None, False
            supplied = value.strip()
            continue
        if key == b"host":
            if host_seen:
                return None, False
            host_seen = True
            out.append(b"Host: " + upstream_host)
            continue
        if key == b"connection":
            tokens = [part.strip().lower() for part in value.split(b",")]
            if b"upgrade" in tokens:
                upgrade = True
                out.append(line)
            continue
        if key == b"keep-alive":
            continue
        out.append(line)
    if not host_seen:
        out.append(b"Host: " + upstream_host)
    if not upgrade:
        out.append(b"Connection: close")
    if not token or supplied is None or not hmac.compare_digest(supplied, token):
        return b"", False
    return b"\r\n".join(out) + b"\r\n\r\n", upgrade


def relay_response(upstream, client, method):
    """Copy one HTTP response to the client, marked "Connection: close", and
    stop: services such as Chromium's DevTools keep the connection open
    whatever the request said, and a second request on it would pass the
    gate unchecked."""
    head, body = read_head(upstream)
    if head is None:
        return
    lines = head.split(b"\r\n")
    status = lines[0].split(b" ")
    code = int(status[1]) if len(status) > 1 and status[1].isdigit() else 0
    out = [lines[0]]
    length = None
    for line in lines[1:]:
        name, _, value = line.partition(b":")
        key = name.strip().lower()
        if key in (b"connection", b"keep-alive"):
            continue
        if key == b"content-length" and value.strip().isdigit():
            length = int(value.strip())
        out.append(line)
    out.append(b"Connection: close")
    client.sendall(b"\r\n".join(out) + b"\r\n\r\n")
    if method == b"HEAD" or code in (204, 304) or 100 <= code < 200:
        return
    if length is None:
        # No length: the body ends when the service closes the connection.
        if body:
            client.sendall(body)
        pipe(upstream, client)
        return
    remaining = length
    if body:
        chunk = body[:remaining]
        client.sendall(chunk)
        remaining -= len(chunk)
    while remaining > 0:
        data = upstream.recv(min(65536, remaining))
        if not data:
            break
        client.sendall(data)
        remaining -= len(data)


def request_body_length(head):
    """Content-Length of a request (0 without one); None for a chunked body,
    which neither DevTools nor noVNC sends and the gate does not relay."""
    length = 0
    for line in head.split(b"\r\n")[1:]:
        name, _, value = line.partition(b":")
        key = name.strip().lower()
        if key == b"transfer-encoding":
            return None
        if key == b"content-length":
            if not value.strip().isdigit():
                return None
            length = int(value.strip())
    return length if length <= 16 << 20 else None


def linger_close(client):
    """Close after the client has read the response: end our side, then drain
    what it may still send, so the close is not a reset that discards the
    response it has not read yet."""
    try:
        client.shutdown(socket.SHUT_WR)
        client.settimeout(2)
        drained = 0
        while drained < 1 << 20:
            data = client.recv(65536)
            if not data:
                break
            drained += len(data)
    except OSError:
        pass


def pipe(src, dst):
    try:
        while True:
            data = src.recv(65536)
            if not data:
                break
            dst.sendall(data)
    except OSError:
        pass
    finally:
        try:
            dst.shutdown(socket.SHUT_WR)
        except OSError:
            pass


def handle(client, upstream_port, token):
    upstream = None
    try:
        client.settimeout(HEAD_TIMEOUT)
        head, rest = read_head(client)
        if head is None:
            return
        forwarded, upgrade = rewrite_head(head, token, b"127.0.0.1:%d" % upstream_port)
        if forwarded is None:
            client.sendall(BAD_REQUEST)
            return
        if not forwarded:
            client.sendall(FORBIDDEN)
            return
        if upgrade:
            client.settimeout(None)
            upstream = socket.create_connection(("127.0.0.1", upstream_port), timeout=10)
            upstream.settimeout(None)
            upstream.sendall(forwarded + rest)
            threading.Thread(target=pipe, args=(client, upstream), daemon=True).start()
            # Once the service is done with the connection, so is the gate;
            # the close below also ends the client-to-service copy.
            pipe(upstream, client)
            return
        # A plain request: forward it and its body, nothing that follows (a
        # pipelined second request would otherwise reach the service
        # unchecked), and relay its one response.
        length = request_body_length(head)
        if length is None:
            client.sendall(BAD_REQUEST)
            return
        body = rest[:length]
        while len(body) < length:
            data = client.recv(min(65536, length - len(body)))
            if not data:
                return
            body += data
        upstream = socket.create_connection(("127.0.0.1", upstream_port), timeout=10)
        upstream.settimeout(UPSTREAM_TIMEOUT)
        upstream.sendall(forwarded + body)
        relay_response(upstream, client, head.split(b" ", 1)[0].upper())
        linger_close(client)
    except (OSError, ValueError):
        pass
    finally:
        for sock in (client, upstream):
            if sock is not None:
                try:
                    sock.close()
                except OSError:
                    pass


def serve(listen_port, upstream_port, token):
    server = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    server.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    server.bind(("0.0.0.0", listen_port))
    server.listen(64)
    while True:
        try:
            client, _ = server.accept()
        except OSError:
            continue
        threading.Thread(target=handle, args=(client, upstream_port, token), daemon=True).start()


def main(argv):
    token = os.environ.get("SUPERBOT_GATE_TOKEN", "").strip().encode()
    if not token:
        print("SUPERBOT_GATE_TOKEN is not set; refusing every connection", file=sys.stderr, flush=True)
    pairs = []
    for arg in argv:
        listen, _, upstream = arg.partition(":")
        pairs.append((int(listen), int(upstream)))
    if not pairs:
        print(__doc__, file=sys.stderr)
        return 2
    threads = [
        threading.Thread(target=serve, args=(listen, upstream, token), daemon=True)
        for listen, upstream in pairs
    ]
    for thread in threads:
        thread.start()
    for thread in threads:
        thread.join()
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
