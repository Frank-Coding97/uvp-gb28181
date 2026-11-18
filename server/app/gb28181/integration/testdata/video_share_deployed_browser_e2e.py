#!/usr/bin/env python3
"""Opt-in installed desktop browser check against deployed_e2e private bootstrap.
Run while its real five-minute sharing window is active. QR, cross-site iframe,
real decoded frames and expiry are required; no physical-phone result is implied.
"""
import argparse,base64,datetime as dt,html,io,json,re,sys,threading,time,urllib.parse
from http.server import BaseHTTPRequestHandler,ThreadingHTTPServer
from pathlib import Path
from video_share_browser_e2e import FRAME_OBSERVER,FRAME_STATE,first_frame,fit,require,redact
from playwright.sync_api import sync_playwright,TimeoutError as PlaywrightTimeoutError
from PIL import Image
import zxingcpp
parser=argparse.ArgumentParser(description=__doc__)
for name in ('bootstrap','output','browser-executable'):parser.add_argument('--'+name,type=Path,required=True)
parser.add_argument('--session',type=Path,required=True)
for name in ('device','channel'):parser.add_argument('--'+name,required=True)
parser.add_argument('--channel-pk',type=int,required=True)
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
  require(page.title()=='UVP-GB28181-视频分享','share tab title differs')
  require('UVP-GB28181' in page.locator('.share-brand').inner_text(),'share product brand differs')
  require(page.locator('.share-brand img').evaluate('img=>img.complete && img.naturalWidth>0'),'share logo failed to load')
  require(page.locator('#share-link').input_value()==bootstrap['url'],'link differs')
  code=page.locator('#share-iframe').input_value();embed=html.unescape(re.search(r'src="([^"]+)"',code).group(1))
  qr=page.locator('.share-tool--qr img');qr.wait_for(state='visible');data=base64.b64decode(qr.get_attribute('src').split(',',1)[1])
  decoded=zxingcpp.read_barcode(Image.open(io.BytesIO(data)));require(decoded and decoded.text==bootstrap['url'],'QR differs')
  report['materials']={'link':True,'iframe':True,'qrDecoded':True,'fragmentCleared':True}
  link_box=page.locator('.share-tool').filter(has=page.locator('#share-link')).bounding_box()
  iframe_box=page.locator('.share-tool').filter(has=page.locator('#share-iframe')).bounding_box()
  require(link_box and iframe_box and iframe_box['y']>=link_box['y']+link_box['height']-1,'link and iframe do not occupy separate rows')
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
  expiry=dt.datetime.fromisoformat(bootstrap['expiresAt'].replace('Z','+00:00'))
  while dt.datetime.now(dt.timezone.utc)<expiry+dt.timedelta(seconds=14):page.wait_for_timeout(1000)
  for target in [page,frame]:
   before=target.evaluate(FRAME_STATE);target.wait_for_timeout(2000);after=target.evaluate(FRAME_STATE)
   advanced=after['canvas']['draws']>before['canvas']['draws'] or any(v['time']>before['videos'][i]['time']+.1 for i,v in enumerate(after['videos']) if i<len(before['videos']))
   require(advanced,'existing playback stopped when link expired')
  fresh=context.new_page();fresh.goto(bootstrap['url'],wait_until='domcontentloaded')
  fresh.get_by_text('分享已失效',exact=False).first.wait_for(timeout=20000)
  require(fresh.locator('video, canvas').count()==0,'expired link created a fresh player');fresh.close()
  report['expiryAdmissionAndContinuation']=True
  print('PASS natural expiry: main page and iframe keep rendering new frames; fresh visit refused',flush=True)
  admin_context=browser.new_context(viewport={'width':1440,'height':1000});admin_context.add_init_script(FRAME_OBSERVER)
  session=json.loads(args.session.read_text());cookies=[]
  for kind in ('access','refresh'):
   value={k:v for k,v in session.items() if k.startswith(kind+'Token')}
   cookies.append({'name':'uvp-gb28181-'+kind+'-token','value':urllib.parse.quote(json.dumps(value,separators=(',',':')),safe=''),'url':bootstrap['origin']})
  admin_context.add_cookies(cookies);admin=admin_context.new_page()
  switch_requests=[]
  admin_context.on('request',lambda request:switch_requests.append(True) if request.method=='PUT' and urllib.parse.urlsplit(request.url).path=='/api/gb28181/channel-shares/'+str(args.channel_pk) else None)
  admin.goto(bootstrap['origin']+'/#/gb28181/device-mgmt/index',wait_until='domcontentloaded')
  admin.wait_for_selector('.arco-menu',timeout=90000)
  admin.keyboard.press('Escape');admin.locator('.driver-overlay').wait_for(state='hidden',timeout=15000)
  def click_after_guide(target):
   target.wait_for(state='visible',timeout=45000)
   for attempt in range(5):
    admin.keyboard.press('Escape');admin.locator('.driver-overlay').wait_for(state='hidden',timeout=15000)
    try:
     target.click(timeout=3000);return
    except PlaywrightTimeoutError:
     if not admin.locator('.driver-overlay:visible').count() or attempt==4:raise
  click_after_guide(admin.locator('button[data-asset-kind="channel"]'))
  click_after_guide(admin.get_by_role('button',name='卡片视图',exact=True))
  search=admin.get_by_role('textbox',name='搜索设备名称或通道名称或编码');search.fill(args.channel);search.press('Enter')
  row=admin.get_by_role('article').filter(has_text=args.device).filter(has_text=args.channel)
  switch=row.get_by_role('switch',name='开启通道视频分享');switch.wait_for(timeout=45000)
  admin.keyboard.press('Escape');admin.locator('.driver-overlay').wait_for(state='hidden',timeout=15000)
  require(switch.get_attribute('aria-checked')=='true','expired sharing switch was not left enabled')
  link_endpoint='/api/gb28181/channel-shares/'+str(args.channel_pk)+'/link'
  with admin.expect_popup() as popup,admin.expect_response(lambda response:response.request.method=='POST' and urllib.parse.urlsplit(response.url).path==link_endpoint and response.status==200) as renewed_response:
   click_after_guide(row.get_by_role('button',name='打开通道分享页',exact=True))
  renewed_link=renewed_response.value.json()['data']
  new_expiry=dt.datetime.fromisoformat(renewed_link['expiresAt'].replace('Z','+00:00'))
  new_time=dt.datetime.fromisoformat(renewed_link['serverTime'].replace('Z','+00:00'))
  require(295<=(new_expiry-new_time).total_seconds()<=300,'renewed link did not use the global five-minute lifetime')
  renewed_page=popup.value;report['renewedFirstFrame']=first_frame(renewed_page)
  require(renewed_page.locator('#share-link').input_value()!=bootstrap['url'],'expired icon opened the old link')
  require(switch.get_attribute('aria-checked')=='true' and not switch_requests,'renewal changed the share switch')
  require(admin.locator('.arco-modal:visible').count()==0,'renewal opened an extra modal')
  require('请关闭后重新开启' not in admin.locator('body').inner_text(),'renewal retained the old warning')
  for target in [page,frame]:
   before=target.evaluate(FRAME_STATE);target.wait_for_timeout(2000);after=target.evaluate(FRAME_STATE)
   advanced=after['canvas']['draws']>before['canvas']['draws'] or any(v['time']>before['videos'][i]['time']+.1 for i,v in enumerate(after['videos']) if i<len(before['videos']))
   require(advanced,'renewing the link stopped an old playback page')
  report['expiredIconRenewal']={'singleClick':True,'switchStayedEnabled':True,'switchWriteRequests':len(switch_requests),'newLinkPlayed':True,'oldPagesContinued':True,'globalFiveMinuteTTL':True}
  print('PASS expired icon: one click opened a fresh playing link, no switch request; old page and iframe continued',flush=True)
  for target in [page,frame,renewed_page]:
   target.wait_for_function("document.body.innerText.includes('分享已失效')",timeout=120000)
   require(target.locator('video, canvas, .share-tools').count()==0,'manual disable retains media/materials')
  report['manualDisableAfterRenewal']=True;report['status']='PASS';browser.close()
except Exception as e:
 report['status']='FAIL';report['error']=redact(e,bootstrap)
 print('FAIL '+type(e).__name__+'; inspect local report',flush=True)
finally:
 (root/'browser-result.json').write_text(json.dumps(report,indent=2));outer.shutdown()

if report["status"]!="PASS":raise SystemExit(1)
