#!/usr/bin/env python3
"""T01 local decoder probe; synthetic input only, no product share endpoints."""
import argparse
from functools import partial
from http.server import SimpleHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
import shutil
import subprocess
import tempfile

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--sample', required=True, type=Path)
parser.add_argument('--port', default=5189, type=int)
args = parser.parse_args()
sample = args.sample.resolve(strict=True)
fixture = Path(__file__).resolve().parent
assets = fixture.parents[2] / 'web/public/easyplayer'
with tempfile.TemporaryDirectory(prefix='uvp-share-browser-') as directory:
    root = Path(directory)
    (root / 'hls').mkdir()
    shutil.copyfile(fixture / 'testdata/browser.html', root / 'index.html')
    (root / 'easyplayer').symlink_to(assets, target_is_directory=True)
    subprocess.run(['ffmpeg', '-hide_banner', '-loglevel', 'error', '-i', str(sample),
                    '-c', 'copy', '-hls_time', '1', '-hls_playlist_type', 'vod',
                    '-hls_segment_filename', str(root / 'hls/seg-%03d.ts'),
                    str(root / 'hls/index.m3u8')], check=True)

    class Handler(SimpleHTTPRequestHandler):
        def do_GET(self):
            if self.path != '/live.flv':
                return super().do_GET()
            self.send_response(200)
            self.send_header('Content-Type', 'video/x-flv')
            self.send_header('Cache-Control', 'no-store')
            self.end_headers()
            process = subprocess.Popen(['ffmpeg', '-hide_banner', '-loglevel', 'error',
                                        '-re', '-stream_loop', '-1', '-i', str(sample),
                                        '-c', 'copy', '-f', 'flv', 'pipe:1'],
                                       stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
            try:
                while data := process.stdout.read(8192):
                    self.wfile.write(data)
                    self.wfile.flush()
            except (BrokenPipeError, ConnectionResetError):
                pass
            finally:
                process.stdout.close()
                process.terminate()
                try:
                    process.wait(timeout=3)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait()

    server = ThreadingHTTPServer(('127.0.0.1', args.port), partial(Handler, directory=directory))
    print(f'Local synthetic decoder fixture: http://127.0.0.1:{args.port}/', flush=True)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        server.server_close()
