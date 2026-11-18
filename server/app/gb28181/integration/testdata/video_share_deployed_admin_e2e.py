#!/usr/bin/env python3
"""Opt-in admin UI acceptance on one explicitly selected, initially unshared channel.
Requires a private normal login session. Changes only the selected sharing toggle
and restores it to disabled. Checks both card and table entry points.
"""
import argparse,json,urllib.parse,urllib.request
from pathlib import Path
from playwright.sync_api import sync_playwright

p=argparse.ArgumentParser(description=__doc__)
for key in ('base','device','channel'):p.add_argument('--'+key,required=True)
for key in ('session','output','browser-executable'):p.add_argument('--'+key,type=Path,required=True)
p.add_argument('--channel-pk',type=int,required=True)
a=p.parse_args();base=a.base.rstrip('/');session=json.loads(a.session.read_text())
a.output.mkdir(parents=True,exist_ok=True,mode=0o700);a.output.chmod(0o700)
headers={'Authorization':'Bearer '+session['accessToken'],'Content-Type':'application/json'}
path=base+'/api/gb28181/channel-shares/'+str(a.channel_pk)
def request(method='GET',body=None):
    data=None if body is None else json.dumps(body).encode()
    with urllib.request.urlopen(urllib.request.Request(path,headers=headers,method=method,data=data),timeout=15) as r:
        result=json.load(r)
    assert result['code']==0,'sharing request failed'
    return result['data']
report={'status':'RUNNING','layouts':{}}
initial=request();assert not initial['enabled'],'refusing to change an already shared channel'
try:
    with sync_playwright() as pw:
        browser=pw.chromium.launch(executable_path=str(a.browser_executable),headless=True)
        ctx=browser.new_context(viewport={'width':1440,'height':1000})
        cookies=[]
        for kind in ['access','refresh']:
            data={k:v for k,v in session.items() if k.startswith(kind+'Token')}
            cookies.append({'name':'uvp-gb28181-'+kind+'-token','value':urllib.parse.quote(json.dumps(data,separators=(',',':')),safe=''),'url':base})
        ctx.add_cookies(cookies);page=ctx.new_page()
        page.goto(base+'/#/gb28181/device-mgmt/index',wait_until='domcontentloaded')
        page.wait_for_selector('.arco-menu',timeout=90000)
        page.keyboard.press('Escape');page.locator('.driver-overlay').wait_for(state='hidden',timeout=15000)
        page.locator('button[data-asset-kind="channel"]').click()
        search=page.get_by_role('textbox',name='搜索设备名称或通道名称或编码')
        search.fill(a.channel);search.press('Enter')
        for label,mode in [('卡片','card'),('列表','list')]:
            page.get_by_role('button',name=label+'视图',exact=True).click()
            row=page.get_by_role('article' if mode=='card' else 'row').filter(has_text=a.device).filter(has_text=a.channel)
            switch=row.get_by_role('switch',name='开启通道视频分享');switch.wait_for(timeout=45000)
            # The normal first-use guide can start after channel rows arrive.
            page.keyboard.press('Escape');page.locator('.driver-overlay').wait_for(state='hidden',timeout=15000)
            assert switch.count()==1,'selected composite channel identity is not unique'
            assert switch.get_attribute('aria-checked')=='false'
            assert row.get_by_role('button',name='打开通道分享页',exact=True).count()==0
            if mode=='card':
                assert switch.locator('xpath=ancestor::*[contains(@class,"channel-card-switches")]').count()==1,'share switch outside card switches'
                boxes=row.locator('.channel-card-switches .switch-item').evaluate_all('els=>els.map(e=>{const b=e.getBoundingClientRect();return {top:b.top,bottom:b.bottom}})')
                assert len(boxes)==4 and max(b['top'] for b in boxes)-min(b['top'] for b in boxes)<5,'four channel switches do not share a row'
            else:
                assert switch.locator('xpath=ancestor::td').count()==1,'share switch outside table cell'
                assert switch.locator('xpath=ancestor::*[contains(@class,"uvp-table-actions")]').count()==0,'share switch is in action column'
                for title in ['音频','按需直播','云端录像','视频分享']:assert page.get_by_role('columnheader',name=title,exact=True).count()==1
            switch.click();page.get_by_role('button',name='开启分享',exact=True).click()
            icon=row.get_by_role('button',name='打开通道分享页',exact=True);icon.wait_for(timeout=15000)
            action_class='channel-card-actions' if mode=='card' else 'uvp-table-actions'
            assert icon.locator('xpath=ancestor::*[contains(@class,"'+action_class+'")]').count()==1,'share icon outside action area'
            with page.expect_popup() as popup:icon.click()
            share=popup.value;share.wait_for_selector('.share-player__screen',timeout=45000)
            assert share.title()=='UVP-GB28181-视频分享'
            assert page.locator('.arco-modal:visible').count()==0,'opening share page also opened a modal'
            share.close();switch.click();page.get_by_role('button',name='关闭分享',exact=True).click()
            page.wait_for_function('()=>document.querySelector(\'[aria-label="开启通道视频分享"]\')?.getAttribute("aria-checked")==="false"')
            assert row.get_by_role('button',name='打开通道分享页',exact=True).count()==0
            report['layouts'][mode]={'switchPosition':True,'conditionalAction':True,'noModal':True,'directSharePage':True}
            print('PASS '+mode+' channel sharing controls and direct page entry',flush=True)
        browser.close();report['status']='PASS'
except Exception as e:
    report['status']='FAIL';report['errorType']=type(e).__name__
    # Locator diagnostics contain no capability URLs; keep messages out of stdout.
    (a.output/'admin-error.private.txt').write_text(str(e));(a.output/'admin-error.private.txt').chmod(0o600)
finally:
    state=request()
    if state['enabled']:request('PUT',{'enabled':False,'revision':state['revision']})
    report['restoredSharingDisabled']=not request()['enabled']
    (a.output/'admin-result.json').write_text(json.dumps(report,ensure_ascii=False,indent=2))
    print(json.dumps(report,ensure_ascii=False))
if report['status']!='PASS':raise SystemExit(1)
