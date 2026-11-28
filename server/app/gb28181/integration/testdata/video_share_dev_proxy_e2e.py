#!/usr/bin/env python3
"""Opt-in Vite development entry and proxy routing acceptance.

The HTTP backend is an explicit routing fixture, not playback or authorization
proof. Uses loopback listeners, random ports and an isolated Vite process.
"""
import argparse
import http.server
import json
import os
from pathlib import Path
import signal
import socket
import subprocess
import tempfile
import threading
import time
import urllib.request

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--web', type=Path, default=Path(__file__).resolve().parents[5] / 'web')
args = parser.parse_args()
root = args.web.resolve(strict=True)
logs = Path(tempfile.mkdtemp(prefix='uvp-share-vite-'))
seen = []
class Handler(http.server.BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass
    def do_POST(self):
        body = self.rfile.read(int(self.headers.get('Content-Length', '0')))
        seen.append((self.command, self.path, body))
        payload = json.dumps({'code': 0, 'data': {'fixture': 'dev-proxy-only'}}).encode()
        self.send_response(200)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)
    def do_GET(self):
        seen.append((self.command, self.path, b''))
        payload = b'FLV\x01dev-proxy-routing-fixture'
        self.send_response(200)
        self.send_header('Content-Type', 'video/x-flv')
        self.send_header('Cache-Control', 'no-store, private')
        self.send_header('Content-Length', str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)

server = http.server.ThreadingHTTPServer(('127.0.0.1', 0), Handler)
thread = threading.Thread(target=server.serve_forever, daemon=True)
thread.start()
with socket.socket() as held:
    held.bind(('127.0.0.1', 0))
    port = held.getsockname()[1]
env = os.environ.copy()
env['VITE_APP_BASE_URL'] = f'http://127.0.0.1:{server.server_port}'
log = (logs / 'vite.log').open('w')
proc = subprocess.Popen(['node', 'node_modules/vite/bin/vite.js', '--host', '127.0.0.1', '--port', str(port), '--strictPort'], cwd=root, env=env, stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
base = f'http://127.0.0.1:{port}'
opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
try:
    deadline = time.monotonic() + 45
    while True:
        if proc.poll() is not None:
            raise RuntimeError('Vite exited; see private log')
        try:
            response = opener.open(base + '/share.html', timeout=1)
            break
        except OSError:
            if time.monotonic() > deadline:
                raise
            time.sleep(.2)
    with response:
        html = response.read().decode()
        assert response.status == 200 and '/src/share/main.ts' in html and '/@vite/client' in html
    with opener.open(base + '/src/share/main.ts', timeout=20) as response:
        assert response.status == 200 and b'ShareApp' in response.read()
    with opener.open(base + '/index.html', timeout=10) as response:
        assert response.status == 200 and b'/src/main.ts' in response.read()
    request = urllib.request.Request(base + '/api/public/video-share/resolve', method='POST', data=b'{"publicId":"routing-fixture","signature":"not-a-real-secret"}', headers={'Content-Type':'application/json'})
    with opener.open(request, timeout=5) as response:
        assert json.load(response)['data']['fixture'] == 'dev-proxy-only'
    with opener.open(base + '/api/public/video-share/viewers/routing-fixture/live.flv', timeout=5) as response:
        assert response.status == 200 and response.read() == b'FLV\x01dev-proxy-routing-fixture'
        assert response.headers['Cache-Control'] == 'no-store, private'
    assert [(m,p) for m,p,b in seen] == [('POST','/api/public/video-share/resolve'),('GET','/api/public/video-share/viewers/routing-fixture/live.flv')]
    assert json.loads(seen[0][2])['publicId'] == 'routing-fixture'
    print('PASS actual Vite development share.html and index.html, transformed share entry, POST anonymous resolve and FLV media proxy routing/cache headers; explicit HTTP fixture, not playback/authentication evidence')
finally:
    if proc.poll() is None:
        os.killpg(proc.pid, signal.SIGTERM)
        proc.wait(timeout=10)
    server.shutdown()
    server.server_close()
    thread.join(timeout=3)
    log.close()

print('Vite diagnostic log: ' + str(logs / 'vite.log'))
