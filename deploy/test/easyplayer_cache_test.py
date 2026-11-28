"""Check player cache behavior with a real Nginx when UVP_TEST_NGINX is supplied."""
import os
import gzip
from pathlib import Path
import re
import shutil
import socket
import subprocess
import tempfile
import time
import unittest
import urllib.request
import urllib.error

ROOT = Path(__file__).resolve().parents[2]
TEMPLATES = [ROOT / 'deploy' / item / 'nginx.conf.template' for item in
             ['standalone', 'standalone-windows', 'compose/nginx']]
NGINX = os.environ.get('UVP_TEST_NGINX') or shutil.which('nginx')


def cache_blocks(source):
    locations = re.findall(r'        location [^\n]+ \{\n.*?\n        \}', source, re.S)
    return [block for block in locations if '/easyplayer/' in block or 'location = /index.html' in block]


def gzip_policy(source):
    return '\n'.join(re.findall(r'^    gzip(?:_[a-z_]+)?\s+[^;]+;', source, re.M))


class PlayerCacheTests(unittest.TestCase):
    def test_all_packages_define_versioned_legacy_and_entry_policies(self):
        for template in TEMPLATES:
            with self.subTest(template=template):
                blocks = cache_blocks(template.read_text())
                self.assertEqual(len(blocks), 3)
                self.assertIn('immutable', '\n'.join(blocks))
                self.assertEqual('\n'.join(blocks).count('"no-cache"'), 2)
                self.assertIn('application/wasm', gzip_policy(template.read_text()))

    @unittest.skipUnless(NGINX, 'set UVP_TEST_NGINX to run real response checks')
    def test_nginx_serves_cache_headers_and_missing_resources_are_404(self):
        for template in TEMPLATES:
            with self.subTest(template=template), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                public = root / 'public'
                version = '0123456789abcdef'
                asset = public / 'easyplayer' / version / 'EasyPlayer-pro.wasm'
                asset.parent.mkdir(parents=True)
                payload = b'\x00asm' + bytes(range(256)) * 64
                asset.write_bytes(payload)
                (public / 'easyplayer/EasyPlayer-pro.js').write_text('legacy')
                (public / 'index.html').write_text('entry')
                with socket.socket() as listener:
                    listener.bind(('127.0.0.1', 0))
                    port = listener.getsockname()[1]
                config = root / 'nginx.conf'
                config.write_text('daemon off;\nmaster_process off;\nerror_log stderr;\n'
                                  + f'pid {root}/nginx.pid;\nevents {{}}\nhttp {{ access_log off;\n'
                                  + gzip_policy(template.read_text())
                                  + '\ntypes { application/wasm wasm; }\nserver {\n'
                                  + f'listen 127.0.0.1:{port};\nroot {public};\n'
                                  + '\n'.join(cache_blocks(template.read_text())) + '\n} }\n')
                process = subprocess.Popen([NGINX, '-p', directory, '-c', str(config)],
                                           stdout=subprocess.DEVNULL, stderr=subprocess.PIPE)
                try:
                    for _ in range(100):
                        if process.poll() is not None:
                            self.fail(process.stderr.read().decode())
                        try:
                            with socket.create_connection(('127.0.0.1', port), timeout=0.1):
                                break
                        except OSError:
                            time.sleep(0.03)
                    def request(path):
                        return urllib.request.urlopen(urllib.request.Request(
                            f'http://127.0.0.1:{port}' + path, method='HEAD'), timeout=3)
                    with request(f'/easyplayer/{version}/EasyPlayer-pro.wasm') as response:
                        cache = ', '.join(response.headers.get_all('Cache-Control', []))
                        self.assertIn('max-age=', cache)
                        self.assertIn('immutable', cache)
                    resource_url = f'http://127.0.0.1:{port}/easyplayer/{version}/EasyPlayer-pro.wasm'
                    for encoding in ['gzip', 'identity']:
                        compressed_request = urllib.request.Request(resource_url, headers={'Accept-Encoding': encoding})
                        with urllib.request.urlopen(compressed_request, timeout=3) as response:
                            body = response.read()
                            self.assertEqual(response.headers['Content-Type'], 'application/wasm')
                            self.assertIn('Accept-Encoding', response.headers['Vary'])
                            if encoding == 'gzip':
                                self.assertEqual(response.headers['Content-Encoding'], 'gzip')
                                self.assertLess(len(body), len(payload))
                                self.assertEqual(gzip.decompress(body), payload)
                            else:
                                self.assertIsNone(response.headers.get('Content-Encoding'))
                                self.assertEqual(body, payload)
                    for path in ['/index.html', '/easyplayer/EasyPlayer-pro.js']:
                        with request(path) as response:
                            self.assertIn('no-cache', response.headers['Cache-Control'])
                            self.assertNotIn('immutable', response.headers['Cache-Control'])
                    with self.assertRaises(urllib.error.HTTPError) as error:
                        request(f'/easyplayer/{version}/missing.js')
                    self.assertEqual(error.exception.code, 404)
                finally:
                    process.terminate()
                    process.communicate(timeout=5)


if __name__ == '__main__':
    unittest.main()
