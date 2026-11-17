#!/usr/bin/env python3
"""Opt-in installed Chromium-family browser acceptance using FRONTEND REST fixture.

Runs the real supplied executable (e.g. Windows Edge), production dist and moving
H264/AAC sample. This is not Go-backend, physical-phone or hardware-camera proof.
All servers bind loopback; artifacts stay inside --root and the browser uses a disposable profile.
"""
import argparse
import base64
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import html
import io
import json
from pathlib import Path
import queue
import re
import subprocess
import sys
import threading
import time
import urllib.request
from video_share_browser_e2e import FRAME_OBSERVER, first_frame, fit, require


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ('root', 'dist', 'sample', 'browser-executable'):
        parser.add_argument('--' + name, required=True, type=Path)
    parser.add_argument('--ffmpeg', default='ffmpeg')
    parser.add_argument('--browser-name', default='Microsoft Edge')
    args = parser.parse_args()
    root = args.root.resolve()
    require(not root.exists() or not any(root.iterdir()), 'root must be new or empty')
    root.mkdir(parents=True, exist_ok=True)
    from playwright.sync_api import sync_playwright
    from PIL import Image
    import zxingcpp
    fixture = None
    outer = None
    origin = None
    report = {'status': 'FAIL', 'scope': 'production frontend + explicit REST fixture + real FFmpeg; installed desktop browser, headless',
              'browser': args.browser_name}
    try:
        fixture = subprocess.Popen([sys.executable, '-u', str(Path(__file__).with_name('video_share_frontend_fixture.py')),
            '--dist', str(args.dist.resolve()), '--sample', str(args.sample.resolve()), '--ffmpeg', args.ffmpeg],
            stdout=subprocess.PIPE, stderr=(root / 'fixture.log').open('wb'), text=True)
        lines = queue.Queue()
        threading.Thread(target=lambda: lines.put(fixture.stdout.readline()), daemon=True).start()
        line = lines.get(timeout=20)
        match = re.search(r'http://127\.0\.0\.1:\d+', line)
        require(match is not None, 'frontend fixture did not start; inspect private log')
        origin = match[0]
        url = origin + '/share.html#id=frontend-fixture&sig=fixture'
        class Outer(BaseHTTPRequestHandler):
            def log_message(self, *_):
                pass
            def do_GET(self):
                data = b'<!doctype html><html><body style="margin:0"></body></html>'
                self.send_response(200)
                self.send_header('Content-Type', 'text/html')
                self.send_header('Content-Length', str(len(data)))
                self.end_headers()
                self.wfile.write(data)
        outer = ThreadingHTTPServer(('127.0.0.1', 0), Outer)
        threading.Thread(target=outer.serve_forever, daemon=True).start()
        with sync_playwright() as playwright:
            browser = playwright.chromium.launch(executable_path=str(args.browser_executable.resolve()), headless=True)
            try:
                report['version'] = browser.version
                context = browser.new_context(viewport={'width': 1280, 'height': 800})
                context.add_init_script(FRAME_OBSERVER)
                context.grant_permissions(['clipboard-read', 'clipboard-write'])
                page = context.new_page()
                page.goto(url, wait_until='domcontentloaded')
                report['firstFrame'] = first_frame(page)
                require(page.evaluate('location.hash === ""'), 'credential remained in URL fragment')
                require(page.locator('#share-link').input_value() == url, 'link differs from issued fixture share')
                code = page.locator('#share-iframe').input_value()
                embedded = html.unescape(re.search(r'src="([^"]+)"', code)[1])
                qr = page.locator('.share-tool--qr img').get_attribute('src')
                decoded = zxingcpp.read_barcode(Image.open(io.BytesIO(base64.b64decode(qr.split(',', 1)[1]))))
                require(decoded is not None and decoded.text == url, 'QR did not decode to issued link')
                for label, value in [('复制分享链接', url), ('复制 iframe 代码', code)]:
                    page.get_by_role('button', name=label, exact=True).click()
                    require(page.evaluate('navigator.clipboard.readText()') == value, 'clipboard material differs')
                report['materials'] = {'link': True, 'iframe': True, 'qrDecoded': True, 'clipboard': True}
                page.screenshot(path=str(root / 'desktop.png'), full_page=True)
                report['layouts'] = {}
                for label, width, height in [('portrait', 390, 844), ('landscape', 844, 390)]:
                    page.set_viewport_size({'width': width, 'height': height})
                    report['layouts'][label] = fit(page)
                    page.screenshot(path=str(root / (label + '.png')), full_page=True)
                embed = context.new_page()
                # localhost and 127.0.0.1 are different sites, as well as origins.
                embed.goto(f'http://localhost:{outer.server_port}/')
                embed.evaluate('''url => { const f = document.createElement('iframe'); f.src = url;
                    f.allow = 'autoplay; fullscreen'; f.style = 'width:640px;height:400px;border:0';
                    document.body.append(f); }''', embedded)
                frame = embed.wait_for_selector('iframe').content_frame()
                frame.wait_for_selector('.share-page--embed')
                report['crossSiteIframeFirstFrame'] = first_frame(frame)
                require(frame.locator('header, footer, .share-tools, .share-actions').count() == 0,
                        'iframe contains duplicated shell or materials')
                embed.screenshot(path=str(root / 'iframe.png'), full_page=True)
                before = frame.evaluate('document.querySelector("video")?.currentTime || window.__shareFrames?.draws')
                page.bring_to_front()
                page.get_by_role('button', name='暂停', exact=True).click()
                time.sleep(1)
                after = frame.evaluate('document.querySelector("video")?.currentTime || window.__shareFrames?.draws')
                require(after > before, 'pausing one viewer stopped the independent iframe')
                page.get_by_role('button', name='继续播放', exact=True).click()
                report['resumedFrame'] = first_frame(page)
                require(page.locator('#share-link').input_value() == url, 'resume changed the share')
                report['viewerIsolation'] = True
                page.set_viewport_size({'width': 1280, 'height': 800})
                page.bring_to_front()
                page.locator('.share-player__screen').hover()
                page.locator('.easyplayer-fullscreen').click()
                page.wait_for_function('document.fullscreenElement !== null')
                page.locator('.share-player__screen').hover()
                page.locator('.easyplayer-fullscreen-exit').click()
                page.wait_for_function('document.fullscreenElement === null')
                report['fullscreen'] = True
                request = urllib.request.Request(origin + '/__fixture/revoke', data=b'{}', method='POST')
                with urllib.request.urlopen(request, timeout=5):
                    pass
                disabled = time.monotonic()
                for target in (page, frame):
                    target.wait_for_function("document.body.innerText.includes('分享已失效')", timeout=30000)
                    require(target.locator('video, canvas, .share-tools').count() == 0, 'revocation retained media or sharing materials')
                report['disabledAfterSeconds'] = round(time.monotonic() - disabled, 3)
                old = context.new_page()
                old.goto(url)
                old.get_by_text('分享已失效，请联系分享人', exact=True).wait_for(timeout=10000)
                page.screenshot(path=str(root / 'revoked.png'), full_page=True)
                report['status'] = 'PASS'
            finally:
                browser.close()
    except Exception as error:
        report['error'] = str(error)[:1200]
    finally:
        if origin:
            try:
                with urllib.request.urlopen(urllib.request.Request(origin + '/__fixture/revoke', data=b'{}', method='POST'), timeout=5):
                    pass
            except OSError:
                pass
        if outer:
            outer.shutdown()
            outer.server_close()
        if fixture:
            fixture.terminate()
            fixture.wait(timeout=10)
            fixture.stdout.close()
        (root / 'results.json').write_text(json.dumps(report, indent=2, ensure_ascii=False), encoding='utf-8')
    print(json.dumps(report, ensure_ascii=True))
    return 0 if report['status'] == 'PASS' else 1


if __name__ == '__main__':
    sys.exit(main())
