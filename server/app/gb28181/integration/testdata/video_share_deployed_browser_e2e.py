#!/usr/bin/env python3
"""Opt-in installed desktop browser check against deployed_e2e private bootstrap.
Run while its real five-minute sharing window is active. QR, cross-site iframe,
real decoded frames and expiry are required; no physical-phone result is implied.
"""
import argparse,base64,html,io,json,re,sys,threading,time
from http.server import BaseHTTPRequestHandler,ThreadingHTTPServer
from pathlib import Path
from video_share_browser_e2e import FRAME_OBSERVER,first_frame,fit,require,redact
from playwright.sync_api import sync_playwright
from PIL import Image
import zxingcpp
parser=argparse.ArgumentParser(description=__doc__)
for name in ('bootstrap','output','browser-executable'):parser.add_argument('--'+name,type=Path,required=True)
args=parser.parse_args()
root=args.output.resolve(); root.mkdir(exist_ok=True,mode=0o700); root.chmod(0o700)
bootstrap=json.loads(args.bootstrap.read_text())
class Outer(BaseHTTPRequestHandler):
 def do_GET(self):
  b=b'<!doctype html><title>UVP cross-site iframe acceptance</title><body></body>'; self.send_response(200); self.send_header('Content-Type','text/html'); self.end_headers(); self.wfile.write(b)
 def log_message(self,*a):pass
outer=ThreadingHTTPServer(('127.0.0.1',0),Outer);threading.Thread(target=outer.serve_forever,daemon=True).start()
report={'status':'RUNNING','scope':'62 deployed instance + physical MERCURY IPC + installed desktop Chrome; no phone claim'}
try:
 with sync_playwright() as p:
  browser=p.chromium.launch(executable_path=str(args.browser_executable),headless=True)
  report['browserVersion']=browser.version
  context=browser.new_context(viewport={'width':1280,'height':800});context.add_init_script(FRAME_OBSERVER)
  page=context.new_page();page.goto(bootstrap['url'],wait_until='domcontentloaded')
  report['firstFrame']=first_frame(page);require(page.evaluate('location.hash===""'),'fragment remains')
  require(page.locator('#share-link').input_value()==bootstrap['url'],'link differs')
  code=page.locator('#share-iframe').input_value();embed=html.unescape(re.search(r'src="([^"]+)"',code).group(1))
  qr=page.locator('.share-tool--qr img');qr.wait_for(state='visible');data=base64.b64decode(qr.get_attribute('src').split(',',1)[1])
  decoded=zxingcpp.read_barcode(Image.open(io.BytesIO(data)));require(decoded and decoded.text==bootstrap['url'],'QR differs')
  report['materials']={'link':True,'iframe':True,'qrDecoded':True,'fragmentCleared':True}
  # Screenshots crop to video so capability link and QR never enter the report.
  page.locator('.share-player').screenshot(path=str(root/'camera-player.png'))
  report['layouts']={}
  for label,w,h in [('portrait',390,844),('landscape',844,390)]:
   page.set_viewport_size({'width':w,'height':h});report['layouts'][label]=fit(page)
  host=context.new_page();host.goto('http://localhost:'+str(outer.server_port))
  host.evaluate("u=>{const f=document.createElement('iframe');f.src=u;f.allow='autoplay; fullscreen';f.style='width:640px;height:400px';document.body.append(f)}",embed)
  frame=host.wait_for_selector('iframe').content_frame();report['crossSiteIframeFrame']=first_frame(frame)
  require(frame.locator('.share-tools, header, footer').count()==0,'embed duplicates sharing tools')
  page.bring_to_front();page.get_by_role('button',name='暂停',exact=True).click();time.sleep(1);report['independentIframeFrame']=first_frame(frame)
  page.get_by_role('button',name='继续播放',exact=True).click();report['resumedFrame']=first_frame(page)
  report['status']='PLAYBACK_PASS_WAITING_EXPIRY';(root/'browser-result.json').write_text(json.dumps(report,indent=2))
  print('PASS desktop Chrome hardware camera playback, QR decode, link, cross-site iframe, responsive layouts, pause/resume',flush=True)
  for target in [page,frame]:
   target.wait_for_function("document.body.innerText.includes('分享已失效')",timeout=340000)
   require(target.locator('video, canvas, .share-tools').count()==0,'expiry retains media/materials')
  report['expiryPageAndIframe']=True;report['status']='PASS';browser.close()
except Exception as e:
 report['status']='FAIL';report['error']=redact(e,bootstrap)
 print('FAIL '+type(e).__name__+'; inspect local report',flush=True)
finally:
 (root/'browser-result.json').write_text(json.dumps(report,indent=2));outer.shutdown()

if report["status"]!="PASS":raise SystemExit(1)
