#!/usr/bin/env python3
"""Opt-in real browser check of an isolated candidate application and production dist.

Use the full_app E2E's --root/--server/--setup/--baseline/--zlm/--ffmpeg/--sample,
plus --dist. The sample must contain moving pictures. Docker uses host networking
and mounts only root/browser; no existing Nginx, business DB or device is touched.
The default image installs pinned Python packages into its disposable container.
--browser-image may name an image with those packages; add --skip-install then.
"""
import argparse
import base64
import http.client
from http.server import SimpleHTTPRequestHandler, ThreadingHTTPServer
import io
import json
from pathlib import Path
import re
import shutil
import socket
import subprocess
import sys
import threading
import time
import urllib.parse
import urllib.request


# Observe actual decoder rendering, independently of the player's play() promise.
FRAME_OBSERVER = """(() => {
  window.__shareFrames = {draws: 0, width: 0, height: 0};
  const observe = (prototype, method, accepts) => {
    if (!prototype || !prototype[method]) return;
    const original = prototype[method];
    prototype[method] = function(...args) {
      const result = original.apply(this, args);
      const c = this.canvas;
      if (c && c.closest && c.closest('.share-player__screen') && c.width > 16 && c.height > 16 && accepts(args)) {
        window.__shareFrames.draws++;
        window.__shareFrames.width = c.width;
        window.__shareFrames.height = c.height;
      }
      return result;
    };
  };
  observe(window.CanvasRenderingContext2D?.prototype, 'drawImage', args =>
    ['VideoFrame', 'HTMLVideoElement', 'ImageBitmap', 'HTMLCanvasElement'].includes(args[0]?.constructor?.name));
  observe(window.CanvasRenderingContext2D?.prototype, 'putImageData', args => args[0]?.width > 16);
  for (const p of [window.WebGLRenderingContext?.prototype, window.WebGL2RenderingContext?.prototype]) {
    observe(p, 'drawArrays', () => true);
    observe(p, 'drawElements', () => true);
  }
})()"""
FRAME_STATE = """() => {
  const root = document.querySelector('.share-player__screen');
  const videos = [...(root?.querySelectorAll('video') || [])].map(v => ({
    width: v.videoWidth, height: v.videoHeight, time: v.currentTime,
    frames: v.getVideoPlaybackQuality?.().totalVideoFrames || v.webkitDecodedFrameCount || 0
  }));
  return {videos, canvas: window.__shareFrames || {draws: 0}};
}"""


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def redact(error, bootstrap):
    message = str(error)
    for key in ("token", "publicId", "signature", "url"):
        value = bootstrap.get(key)
        if value:
            message = message.replace(value, "[private]")
    message = re.sub(r'https?://[^\s<>"\']+', '[url]', message)
    message = re.sub(r'/api/public/video-share/viewers/[^\s/?"\']+',
                     '/api/public/video-share/viewers/[private]', message)
    return message[:1200]


def first_frame(frame):
    # An advancing decoded video frame or repeated decoder-to-canvas drawing is
    # required. Merely receiving HTTP 200 or resolving player.play is insufficient.
    before = frame.evaluate(FRAME_STATE)
    frame.wait_for_function("""before => {
      const root = document.querySelector('.share-player__screen');
      const videos = [...(root?.querySelectorAll('video') || [])];
      return videos.some((v, i) => v.videoWidth > 16 && v.videoHeight > 16 &&
        ((v.getVideoPlaybackQuality?.().totalVideoFrames || v.webkitDecodedFrameCount || 0) >= 2 ||
         v.currentTime > (before.videos[i]?.time || 0) + .1)) ||
        (window.__shareFrames?.draws >= Math.max(3, before.canvas.draws + 2));
    }""", arg=before, timeout=45000)
    return frame.evaluate(FRAME_STATE)


def fit(frame):
    require(frame.evaluate("document.documentElement.scrollWidth <= innerWidth + 2"),
            "share page overflows the viewport horizontally")
    box = frame.locator('.share-player').bounding_box()
    require(box and box['width'] > 100 and box['height'] > 50, "player has no visible layout")
    return {"width": round(box['width']), "height": round(box['height'])}


def inside(args):
    from playwright.sync_api import sync_playwright
    from PIL import Image
    import zxingcpp

    root = Path(args.bootstrap).resolve().parent
    bootstrap = json.loads(Path(args.bootstrap).read_text())
    report = {"status": "FAIL", "browsers": {}, "scope": "Desktop browser engines; no real iOS or Edge claim"}
    active = []
    with sync_playwright() as playwright:
        try:
            for name in ('chromium', 'firefox', 'webkit'):
                result = {"status": "FAIL"}
                report['browsers'][name] = result
                browser = None
                try:
                    engine = getattr(playwright, name)
                    browser = engine.launch(headless=True, **({'args': ['--no-sandbox']} if name == 'chromium' else {}))
                    context = browser.new_context(viewport={"width": 1280, "height": 800})
                    context.add_init_script(FRAME_OBSERVER)
                    if name == 'chromium':
                        context.grant_permissions(['clipboard-read', 'clipboard-write'])
                    page = context.new_page()
                    page.goto(bootstrap['url'], wait_until='domcontentloaded')
                    result['firstFrame'] = first_frame(page)
                    require(page.evaluate('location.hash === ""'), "share credential remained in URL fragment")
                    link = page.locator('#share-link').input_value()
                    require(link == bootstrap['url'], "share link differs from issued credential")
                    code = page.locator('#share-iframe').input_value()
                    match = re.search(r'src="([^"]+)"', code)
                    require(match is not None, "iframe material has no source")
                    import html
                    embed_url = html.unescape(match.group(1))
                    original = urllib.parse.parse_qs(urllib.parse.urlsplit(link).fragment)
                    embedded = urllib.parse.parse_qs(urllib.parse.urlsplit(embed_url).fragment)
                    require(embedded == dict(original, embed=['1']), "iframe changes the authorized share")
                    qr = page.locator('.share-tool--qr img')
                    qr.wait_for(state='visible')
                    qr_bytes = base64.b64decode(qr.get_attribute('src').split(',', 1)[1])
                    decoded = zxingcpp.read_barcode(Image.open(io.BytesIO(qr_bytes)))
                    require(decoded is not None and decoded.text == link, "QR does not decode to the issued share link")
                    result['materials'] = {"link": True, "iframe": True, "qrDecoded": True}
                    if name == 'chromium':
                        page.get_by_role('button', name='复制分享链接', exact=True).click()
                        require(page.evaluate('navigator.clipboard.readText()') == link, "copy link failed")
                        page.get_by_role('button', name='复制 iframe 代码', exact=True).click()
                        require(page.evaluate('navigator.clipboard.readText()') == code, "copy iframe failed")
                        result['materials']['clipboard'] = True
                    page.screenshot(path=str(root / (name + '-desktop.png')), full_page=True)
                    layouts = {}
                    for label, width, height in [('portrait', 390, 844), ('landscape', 844, 390)]:
                        page.set_viewport_size({"width": width, "height": height})
                        layouts[label] = fit(page)
                        page.screenshot(path=str(root / (name + '-' + label + '.png')), full_page=True)
                    result['layouts'] = layouts
                    embed = context.new_page()
                    embed.set_viewport_size({"width": 640, "height": 400})
                    embed.goto(bootstrap['origin'] + '/__browser-fixture', wait_until='domcontentloaded')
                    embed.evaluate("""url => {
                      const iframe = document.createElement('iframe');
                      iframe.src = url; iframe.allow = 'autoplay; fullscreen';
                      iframe.style = 'width:100%;height:360px;border:0'; document.body.append(iframe);
                    }""", embed_url)
                    iframe = embed.wait_for_selector('iframe').content_frame()
                    iframe.wait_for_selector('.share-page--embed')
                    result['iframeFirstFrame'] = first_frame(iframe)
                    require(iframe.locator('header, footer, .share-tools, .share-actions').count() == 0,
                            "embedded page displays duplicate share materials or shell")
                    fit(iframe)
                    embed.screenshot(path=str(root / (name + '-iframe.png')), full_page=True)
                    result['status'] = 'READY'
                    active.append((name, browser, context, page, iframe))
                    browser = None
                except Exception as error:
                    text = redact(error, bootstrap)
                    result.update(status='SKIP' if name != 'chromium' and 'Executable doesn' in text else 'FAIL', error=text)
                finally:
                    if browser is not None:
                        browser.close()
            require(any(name == 'chromium' for name, *_ in active), "Chromium playback/material checks did not pass")
            request = urllib.request.Request(bootstrap['origin'] + bootstrap['managementPath'],
                headers={'Authorization': 'Bearer ' + bootstrap['token'], 'Content-Type': 'application/json'})
            with urllib.request.urlopen(request, timeout=10) as response:
                current = json.load(response)['data']
            request.method = 'PUT'
            request.data = json.dumps({'enabled': False, 'revision': current['revision']}).encode()
            with urllib.request.urlopen(request, timeout=10) as response:
                require(json.load(response)['code'] == 0, 'disable failed')
            disabled_at = time.monotonic()
            for name, browser, context, page, iframe in active:
                result = report['browsers'][name]
                try:
                    for frame in (page, iframe):
                        frame.wait_for_function("document.body.innerText.includes('分享已失效')", timeout=30000)
                    require(time.monotonic() - disabled_at <= 30, 'share page missed thirty-second revocation budget')
                    result['disabledAfterSeconds'] = round(time.monotonic() - disabled_at, 3)
                    old = context.new_page()
                    old.goto(bootstrap['url'], wait_until='domcontentloaded')
                    old.get_by_text('分享已失效，请联系分享人', exact=True).wait_for(timeout=10000)
                    require(old.locator('.share-tools').count() == 0, 'revoked link still offers share materials')
                    page.screenshot(path=str(root / (name + '-revoked.png')), full_page=True)
                    result['status'] = 'PASS'
                except Exception as error:
                    result.update(status='FAIL', error=redact(error, bootstrap))
            if report['browsers'].get('chromium', {}).get('status') == 'PASS' and all(
                    value['status'] in ('PASS', 'SKIP') for value in report['browsers'].values()):
                report['status'] = 'PASS'
        except Exception as error:
            report['error'] = redact(error, bootstrap)
        finally:
            for _, browser, *_ in active:
                browser.close()
    (root / 'browser-results.json').write_text(json.dumps(report, indent=2, ensure_ascii=False))
    print(json.dumps(report, ensure_ascii=False))
    return 0 if report['status'] == 'PASS' else 1


def host(args):
    from video_share_full_app_e2e import Check, ports

    check = Check(args)
    proxy = None
    try:
        check.configure()
        check.initialize()
        check.media()
        check.start([args.server], 'server')
        def ready():
            with socket.create_connection(('127.0.0.1', check.app_port), timeout=1):
                return True
        check.wait(ready, 'candidate application listener', 60)
        login = check.success('/api/login', 'POST', {'username': 'admin', 'password': check.password})
        require(login.get('mustChangePassword') is not True, 'fixture admin requires password change')
        token = login['accessToken']
        path = f'/api/gb28181/channel-shares/{check.channel_pk}'
        state = check.success(path, token=token)
        require(not state['enabled'], 'new channel sharing did not default off')
        enabled = check.success(path, 'PUT', {'enabled': True, 'revision': state['revision']}, token)
        link = check.success(path + '/link', 'POST', {'revision': enabled['revision']}, token)
        port = ports(1)[0]
        origin = f'http://127.0.0.1:{port}'

        class Handler(SimpleHTTPRequestHandler):
            def __init__(self, *values, **options):
                super().__init__(*values, directory=args.dist, **options)

            def log_message(self, *_):
                pass  # URL paths may contain private viewer tokens.

            def do_GET(self):
                if self.path.startswith('/api/'):
                    return self.relay()
                if self.path == '/__browser-fixture':
                    content = b'<!doctype html><html><body style="margin:0"></body></html>'
                    self.send_response(200)
                    self.send_header('Content-Type', 'text/html; charset=utf-8')
                    self.send_header('Content-Length', str(len(content)))
                    self.end_headers()
                    self.wfile.write(content)
                    return
                super().do_GET()

            def do_POST(self):
                self.relay()

            def do_PUT(self):
                self.relay()

            def do_DELETE(self):
                self.relay()

            def relay(self):
                require(self.path.startswith('/api/'), 'unexpected proxy path')
                connection = http.client.HTTPConnection('127.0.0.1', check.app_port, timeout=40)
                try:
                    length = int(self.headers.get('Content-Length', '0'))
                    body = self.rfile.read(length) if length else None
                    headers = {key: value for key, value in self.headers.items()
                               if key.lower() not in ('host', 'connection', 'accept-encoding')}
                    connection.request(self.command, self.path, body, headers)
                    response = connection.getresponse()
                    self.send_response(response.status)
                    for key, value in response.getheaders():
                        if key.lower() not in ('connection', 'transfer-encoding', 'keep-alive'):
                            self.send_header(key, value)
                    self.send_header('Connection', 'close')
                    self.end_headers()
                    self.close_connection = True
                    while data := response.read1(65536):
                        self.wfile.write(data)
                        self.wfile.flush()
                except (BrokenPipeError, ConnectionResetError):
                    pass
                finally:
                    connection.close()

        proxy = ThreadingHTTPServer(('127.0.0.1', port), Handler)
        proxy.daemon_threads = True
        threading.Thread(target=proxy.serve_forever, daemon=True).start()
        private = check.root / 'browser'
        private.mkdir(mode=0o700)
        shutil.copyfile(__file__, private / 'browser_check.py')
        bootstrap = {'url': origin + '/share.html#' + urllib.parse.urlencode({
                         'id': link['publicId'], 'sig': link['signature']}),
                     'origin': origin, 'managementPath': path, 'token': token,
                     'publicId': link['publicId'], 'signature': link['signature']}
        bootstrap_path = private / 'bootstrap.json'
        bootstrap_path.write_text(json.dumps(bootstrap))
        bootstrap_path.chmod(0o600)
        install = '' if args.skip_install else (
            'python3 -m pip install --disable-pip-version-check -q '
            'playwright==1.56.0 zxing-cpp==2.3.0 Pillow==11.3.0 && ')
        command = ['docker', 'run', '--rm', '--network', 'host', '--shm-size=512m',
                   '--memory=1536m', '--cpus=1', '--pids-limit=512',
                   '--mount', f'type=bind,src={private},dst=/share-check',
                   '--entrypoint', 'bash', args.browser_image, '-lc', install +
                   'python3 /share-check/browser_check.py --inside-browser --bootstrap /share-check/bootstrap.json']
        with (private / 'container.log').open('wb') as log:
            result = subprocess.run(command, stdout=log, stderr=subprocess.STDOUT, timeout=600)
        require((private / 'browser-results.json').is_file(), 'browser produced no result; inspect private container.log')
        report = json.loads((private / 'browser-results.json').read_text())
        print(json.dumps(report, ensure_ascii=False))
        require(result.returncode == 0 and report['status'] == 'PASS', 'browser acceptance failed; inspect private results')
        check.record('real browser first frame, share materials, layouts, iframe and disable revocation')
        return 0
    except Exception as error:
        # Never print bootstrap credentials or the private fragment-bearing URL.
        print('FAIL ' + redact(error, locals().get('bootstrap', {})), file=sys.stderr)
        return 1
    finally:
        if proxy is not None:
            proxy.shutdown()
            proxy.server_close()
        check.close()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--inside-browser', action='store_true')
    parser.add_argument('--bootstrap')
    for name in ('root', 'server', 'setup', 'baseline', 'zlm', 'ffmpeg', 'sample', 'dist'):
        parser.add_argument('--' + name)
    parser.add_argument('--browser-image', default='mcr.microsoft.com/playwright/python:v1.56.0-noble')
    parser.add_argument('--skip-install', action='store_true')
    args = parser.parse_args()
    if args.inside_browser:
        require(args.bootstrap, '--bootstrap is required inside browser mode')
        return inside(args)
    for name in ('root', 'server', 'setup', 'baseline', 'zlm', 'ffmpeg', 'sample', 'dist'):
        require(getattr(args, name), '--' + name + ' is required')
        value = Path(getattr(args, name)).absolute()
        if name != 'root':
            value = value.resolve(strict=True)
        setattr(args, name, str(value))
    require((Path(args.dist) / 'share.html').is_file(), '--dist must be the candidate production build')
    return host(args)


if __name__ == '__main__':
    sys.exit(main())
