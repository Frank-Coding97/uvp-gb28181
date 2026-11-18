#!/usr/bin/env python3
"""Loopback FRONTEND TEST FIXTURE: production dist + anonymous REST stub + real FFmpeg FLV.
Not the Go backend. /share.html#id=frontend-fixture&sig=fixture; POST /__fixture/revoke retires sessions."""
from datetime import datetime, timezone
from http.server import SimpleHTTPRequestHandler, ThreadingHTTPServer
import argparse, json, re, secrets, shutil, signal, subprocess, threading, time
from pathlib import Path
from urllib.parse import urlsplit
def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--dist", type=Path, required=True)
    parser.add_argument("--sample", type=Path, required=True, help="local H264/AAC MP4")
    parser.add_argument("--ffmpeg", default="ffmpeg")
    args = parser.parse_args()
    args.dist, args.sample = args.dist.resolve(), args.sample.resolve()
    if not (args.dist / "share.html").is_file() or not args.sample.is_file():
        parser.error("production dist/share.html and local sample MP4 are required")
    ffmpeg = shutil.which(args.ffmpeg)
    if not ffmpeg:
        parser.error("FFmpeg executable was not found")
    lock, stopped = threading.RLock(), threading.Event()
    expires, sessions, processes, revoked = time.time() + 300, {}, {}, False
    def stamp(value):
        return datetime.fromtimestamp(value, timezone.utc).isoformat().replace("+00:00", "Z")
    def finish(process):
        if process.poll() is None:
            process.terminate()
        try:
            process.wait(timeout=2)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait()
    def retire(token=None):
        nonlocal revoked
        with lock:
            if token is None:
                revoked = True
                sessions.clear()
            else:
                sessions.pop(token, None)
            targets = [p for p, owner in processes.items() if token is None or owner == token]
        for process in targets:
            finish(process)
    def valid(token=None):
        with lock:
            if revoked:
                return False
            if token is None:
                return time.time() < expires
            return token in sessions and (sessions[token] or time.time() < expires)
    class Handler(SimpleHTTPRequestHandler):
        def __init__(self, *a, **kw):
            super().__init__(*a, directory=str(args.dist), **kw)
        def log_message(self, *_):
            pass  # Viewer paths and request bodies never enter access logs.
        def reply(self, status, data=None):
            body = json.dumps({"code": 0 if status < 400 else status, "data": data}).encode()
            self.send_response(status)
            self.send_header("Content-Type", "application/json")
            self.send_header("Cache-Control", "no-store")
            self.send_header("X-Video-Share-Fixture", "frontend-only")
            self.send_header("Content-Length", str(len(body) if status != 204 else 0))
            self.end_headers()
            if status != 204:
                self.wfile.write(body)
        def do_POST(self):
            if self.path == "/__fixture/revoke":
                retire()
                return self.reply(200, {"revoked": True})
            try:
                size = int(self.headers.get("Content-Length", "0"))
                if not 0 < size <= 4096:
                    return self.reply(400)
                body = json.loads(self.rfile.read(size))
                if not isinstance(body, dict):
                    return self.reply(400)
            except (ValueError, OSError):
                return self.reply(400)
            with lock:
                prefix = "/api/public/video-share/"
                if self.path == prefix + "viewers/status":
                    return self.reply(200 if valid(body.get("viewerToken", "")) else 410,
                                      {"expiresAt": stamp(expires), "serverTime": stamp(time.time())})
                if not valid():
                    return self.reply(410)
                if body.get("publicId") != "frontend-fixture" or body.get("signature") != "fixture":
                    return self.reply(410)
                if self.path == prefix + "viewers":
                    token = secrets.token_urlsafe(24)
                    sessions[token] = False
                    return self.reply(200, {"viewerToken": token, "expiresAt": stamp(expires),
                                           "httpFlvPath": prefix + "viewers/" + token + "/live.flv"})
                if self.path == prefix + "resolve":
                    return self.reply(200, {"name": "前端测试样片（匿名 REST fixture）", "online": True,
                                           "hasAudio": True, "expiresAt": stamp(expires), "serverTime": stamp(time.time())})
            self.reply(404)
        def do_DELETE(self):
            match = re.fullmatch(r"/api/public/video-share/viewers/([A-Za-z0-9_-]+)", self.path)
            allowed = bool(match and valid(match[1]))
            if not allowed:
                return self.reply(410)
            retire(match[1])
            self.reply(204)
        def do_GET(self):
            match = re.fullmatch(r"/api/public/video-share/viewers/([A-Za-z0-9_-]+)/live.flv", self.path)
            if not match:
                if urlsplit(self.path).path.startswith("/api/"):
                    return self.reply(404)
                return super().do_GET()
            with lock:
                if not valid(match[1]):
                    return self.reply(410)
                sessions[match[1]] = True
                process = subprocess.Popen([ffmpeg, "-nostdin", "-loglevel", "error", "-re", "-stream_loop", "-1",
                    "-i", str(args.sample), "-map", "0:v:0", "-map", "0:a:0", "-c", "copy", "-f", "flv", "pipe:1"],
                    stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
                processes[process] = match[1]
            try:
                self.send_response(200)
                self.send_header("Content-Type", "video/x-flv")
                self.send_header("Cache-Control", "no-store")
                self.end_headers()
                self.connection.settimeout(2)
                while valid(match[1]) and not stopped.is_set():
                    chunk = process.stdout.read1(8192)
                    if not chunk:
                        break
                    self.wfile.write(chunk)
                    self.wfile.flush()
            except (BrokenPipeError, ConnectionResetError, OSError):
                pass
            finally:
                finish(process)
                process.stdout.close()
                with lock:
                    processes.pop(process, None)
    server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    def interrupt(*_):
        raise KeyboardInterrupt
    signal.signal(signal.SIGTERM, interrupt)
    print(f"FRONTEND TEST FIXTURE (not full backend): http://127.0.0.1:{server.server_port}", flush=True)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        stopped.set()
        retire()
        server.server_close()
if __name__ == "__main__":
    main()
