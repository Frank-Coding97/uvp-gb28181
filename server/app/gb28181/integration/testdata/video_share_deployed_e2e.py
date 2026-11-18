#!/usr/bin/env python3
"""Opt-in acceptance of an existing deployment, one explicitly selected channel.
Requires an authenticated session JSON and initial sharing disabled. It exercises
only that channel's sharing lifecycle and restores it to disabled in finally.
No global configuration, device, source stream or database row is modified here.
Credentials and media URLs remain in chmod-0600 files in the supplied directory.
"""
import argparse,datetime as dt,json,os,subprocess,threading,time,urllib.parse,urllib.request
from pathlib import Path
from video_share_full_app_e2e import Check,require

class Deployed(Check):
    def __init__(self,args):
        self.args=args; self.root=args.output.resolve(); self.root.mkdir(exist_ok=True,mode=0o700)
        self.root.chmod(0o700); self.base=args.base.rstrip('/'); self.secret='not-loaded'
        self.opener=urllib.request.build_opener(urllib.request.ProxyHandler({}))
        self.media_readers=[]; self.processes=[]; self.checks=[]
        self.token=json.loads(args.session.read_text())['accessToken']; self.path='/api/gb28181/channel-shares/'+str(args.channel_pk)
        self.report={'status':'RUNNING','base':self.base,'channelPk':args.channel_pk,'checks':self.checks,'measurements':{}}
    def private(self,name,data):
        p=self.root/name; p.write_text(json.dumps(data)); p.chmod(0o600)
    def record(self,name):
        super().record(name); self.save()
    def save(self):
        (self.root/'acceptance-result.json').write_text(json.dumps(self.report,ensure_ascii=False,indent=2))
    def status(self): return self.success(self.path,token=self.token)
    def enabled(self,on):
        state=self.status(); return self.success(self.path,'PUT',{'enabled':on,'revision':state['revision']},self.token)
    def link(self):
        state=self.status(); link=self.success(self.path+'/link','POST',{'revision':state['revision']},self.token)
        return link,{'publicId':link['publicId'],'signature':link['signature']}
    def wait(self, action, description, timeout=30):
        deadline=time.monotonic()+(100 if description=="real anonymous HLS media playlist" else timeout)
        while time.monotonic()<deadline:
            try:
                if action():return
            except (OSError,ValueError) as e:
                failure={'type':type(e).__name__,'http':getattr(e,'code',None),'description':description}
                self.report['lastWaitError']=failure
                history=self.report.setdefault('waitErrors',[])
                if not history or failure!=history[-1]:
                    history.append(failure);del history[:-12]
                self.save()
            time.sleep(.2)
        raise RuntimeError('timed out waiting for '+description)
    def reader(self,url):
        r=self.opener.open(url,timeout=20); require(r.status==200 and r.read(3)==b'FLV','expected real FLV')
        state=self.observe_flv(r); self.wait(lambda:self.flv_snapshot(state)[0]>65536,'progressing media',15); return state
    def decode(self,path):
        output=self.root/'decoded-frame.jpg'; log=self.root/'ffmpeg.private.log'
        env=os.environ.copy(); env['LD_LIBRARY_PATH']=str(self.args.ffmpeg.parent.parent/'lib')+':'+env.get('LD_LIBRARY_PATH','')
        with log.open('wb') as out:
            p=subprocess.run([str(self.args.ffmpeg),'-hide_banner','-loglevel','info','-y','-i',self.base+path,'-frames:v','1',str(output)],env=env,stdout=out,stderr=subprocess.STDOUT,timeout=35)
        require(p.returncode==0 and output.stat().st_size>1000,'real camera video failed to decode; inspect private ffmpeg log')
        log.chmod(0o600)
        # Output only media characteristics, never the input capability URL.
        lines=log.read_text(errors='replace').splitlines(); self.report['media']=[x.strip() for x in lines if 'Stream #' in x and ('Video:' in x or 'Audio:' in x)]
        self.record('hardware camera FLV decoded into a real JPEG frame')
    def gone(self,cred,viewer,paths):
        for suffix,body in [('resolve',cred),('viewers',cred),('viewers/status',{'viewerToken':viewer['viewerToken']})]:
            code,_=self.request('/api/public/video-share/'+suffix,'POST',body); require(code==410,'retired anonymous endpoint must return 410')
        for p in paths:self.expect_gone(p,'retired anonymous media must return 410')
    def observe_hls(self,viewer):
        state={'stop':threading.Event(),'segments':0,'last':None,'error':None,'paths':set(),'targetDuration':0}
        def consume():
            try:
                while not state['stop'].is_set():
                    with self.opener.open(self.base+viewer['hlsPath'],timeout=10) as response:
                        text=response.read(1<<20).decode()
                    durations=[float(line.split(':',1)[1]) for line in text.splitlines() if line.startswith('#EXT-X-TARGETDURATION:')]
                    if durations:state['targetDuration']=durations[0]
                    paths=[line.strip() for line in text.splitlines() if line.strip() and not line.startswith('#')]
                    require(paths,'continuous HLS manifest has no media')
                    path=paths[-1]
                    require(path.startswith('/api/public/video-share/viewers/'+viewer['viewerToken']+'/hls/'),'HLS escaped viewer relay')
                    with self.opener.open(self.base+path,timeout=10) as response:
                        data=response.read((8<<20)+1)
                    require(3*188<=len(data)<=8<<20 and len(data)%188==0 and data[0]==0x47,'continuous HLS segment is not MPEG-TS')
                    if path not in state['paths']:
                        state['paths'].add(path); state['segments']+=1
                    state['last']=time.monotonic()
                    state['stop'].wait(2)
            except Exception as error:
                state['error']=type(error).__name__
        thread=threading.Thread(target=consume,daemon=True); thread.start()
        return state,thread
    def run_acceptance(self):
        initial=self.status(); require(not initial['enabled'],'selected channel is already shared; refusing to revoke it')
        self.private('initial-share.private.json',initial)
        config=self.success('/api/gb28181/sip/service-config',token=self.token)
        require(config['videoShare']=={'defaultEnabled':False,'ttlSeconds':300},'expected existing-instance defaults false/300')
        self.record('deployed global defaults are off and five minutes; old channel remains unshared')
        hls_state=None; hls_thread=None
        try:
            ordinary=self.success('/api/gb28181/play/'+self.args.device+'/'+self.args.channel,'POST',token=self.token)
            self.private('ordinary-play.private.json',ordinary)
            normal=self.reader(ordinary['httpFlvUrl']); self.record('normal authenticated camera playback is delivering FLV')
            self.enabled(True); link,cred=self.link(); require(self.link()[0]['expiresAt']==link['expiresAt'],'reacquiring extended expiry')
            meta=self.success('/api/public/video-share/resolve','POST',cred)
            viewer=self.success('/api/public/video-share/viewers','POST',cred)
            anon=self.reader(self.base+viewer['httpFlvPath'])
            decoder=self.success('/api/public/video-share/viewers','POST',cred); self.decode(decoder['httpFlvPath'])
            hls_viewer=self.success('/api/public/video-share/viewers','POST',cred); self.private('hls-viewer.private.json',hls_viewer); hls,segment=self.read_hls(hls_viewer)
            self.enabled(False); revoked=time.monotonic(); require(anon['ended'].wait(30),'revocation exceeded 30 seconds')
            delay=max(0,self.flv_snapshot(anon)[1]-revoked); require(delay<=30,'revoked bytes arrived after 30s')
            self.report['measurements']['revocationLastByteSeconds']=round(delay,3)
            self.gone(cred,viewer,[viewer['httpFlvPath'],hls,segment])
            before=self.flv_snapshot(normal)[0]; self.wait(lambda:self.flv_snapshot(normal)[0]>before,'ordinary playback survives share disable',10)
            require(not normal['ended'].is_set(),'normal playback was interrupted')
            self.record('manual disable stopped anonymous FLV within budget; old link/viewer/FLV/HLS return 410; same normal stream continues')
            self.enabled(True); link,cred=self.link()
            require(self.request('/api/public/video-share/resolve','POST',{'publicId':meta.get('publicId', 'invalid'),'signature':'invalid'})[0]==410,'invalid share accepted')
            viewer=self.success('/api/public/video-share/viewers','POST',cred)
            anon=self.reader(self.base+viewer['httpFlvPath'])
            hls_viewer=self.success('/api/public/video-share/viewers','POST',cred); self.private('hls-viewer.private.json',hls_viewer); hls,segment=self.read_hls(hls_viewer)
            hls_state,hls_thread=self.observe_hls(hls_viewer)
            expiry=dt.datetime.fromisoformat(link['expiresAt'].replace('Z','+00:00')); now=dt.datetime.fromisoformat(link['serverTime'].replace('Z','+00:00'))
            ttl=(expiry-now).total_seconds(); require(295<=ttl<=300,'five minute expiry not applied')
            public=self.args.public_base.rstrip('/')
            url=public+'/share.html#'+urllib.parse.urlencode({'id':link['publicId'],'sig':link['signature']})
            self.private('browser-bootstrap.private.json',{'url':url,'origin':public,'expiresAt':link['expiresAt']})
            self.record('five-minute share ready for anonymous deployed browser acceptance')
            while dt.datetime.now(dt.timezone.utc)<expiry+dt.timedelta(seconds=12):
                require(not anon['ended'].is_set(),'FLV ended before natural expiry continuation check')
                require(not hls_state['error'],'continuous HLS failed: '+str(hls_state['error']))
                time.sleep(1)
            for endpoint in ('resolve','viewers'):
                require(self.request('/api/public/video-share/'+endpoint,'POST',cred)[0]==410,'expired link admitted a new visit')
            self.success('/api/public/video-share/viewers/status','POST',{'viewerToken':viewer['viewerToken']})
            state=self.status();require(state['enabled'] and state['expired'],'expired share must retain the explicit disable control')
            before_anon=self.flv_snapshot(anon)[0]; before_hls=hls_state['segments']
            self.wait(lambda:hls_state['error'] or hls_state['segments']>before_hls,'new HLS segment after natural expiry',max(30,2*hls_state['targetDuration']+10))
            require(not anon['ended'].is_set() and self.flv_snapshot(anon)[0]>before_anon,'existing FLV stopped at link expiry')
            require(not hls_state['error'] and hls_state['segments']>before_hls,'existing HLS failed to fetch new post-expiry segments')
            self.report['measurements']['postExpiryHlsNewSegments']=hls_state['segments']-before_hls
            before=self.flv_snapshot(normal)[0]; self.wait(lambda:self.flv_snapshot(normal)[0]>before,'ordinary playback survives expiry',10)
            self.record('real five-minute expiry rejected new visits while active FLV/HLS and ordinary playback continued')
            # The independent browser must click the expired share icon. Do not
            # call /link here until the UI has performed that renewal itself.
            renewed=[]
            def renewed_from_ui():
                require(not anon['ended'].is_set(),'old FLV stopped while waiting for icon renewal')
                require(not hls_state['error'],'old HLS stopped while waiting for icon renewal')
                current=self.status()
                require(current['enabled'],'renewal unexpectedly toggled sharing off')
                if current.get('expiresAt')!=link['expiresAt']:
                    renewed[:]=[current];return True
                return False
            self.wait(renewed_from_ui,'expired share icon renewal by the browser',150)
            new_link,new_cred=self.link()
            require(new_link['publicId']!=link['publicId'],'renewal reused the expired public identifier')
            require(new_link['generation']==link['generation'],'ordinary renewal changed the sharing authorization generation')
            require(new_link['expiresAt']==renewed[0]['expiresAt'],'reacquiring renewed link extended its expiry')
            self.success('/api/public/video-share/resolve','POST',new_cred)
            for endpoint in ('resolve','viewers'):
                require(self.request('/api/public/video-share/'+endpoint,'POST',cred)[0]==410,'renewal revived the expired link')
            new_viewer=self.success('/api/public/video-share/viewers','POST',new_cred)
            new_anon=self.reader(self.base+new_viewer['httpFlvPath'])
            before_anon=self.flv_snapshot(anon)[0];before_hls=hls_state['segments']
            self.wait(lambda:hls_state['error'] or hls_state['segments']>before_hls,'new HLS segment after link renewal',max(30,2*hls_state['targetDuration']+10))
            require(not anon['ended'].is_set() and self.flv_snapshot(anon)[0]>before_anon,'link renewal interrupted old FLV')
            require(not hls_state['error'] and hls_state['segments']>before_hls,'link renewal interrupted old HLS')
            require(not new_anon['ended'].is_set(),'renewed link did not retain live playback')
            self.report['measurements']['postRenewalHlsNewSegments']=hls_state['segments']-before_hls
            self.record('expired icon renewed the public link without toggling sharing; old FLV/HLS continued and old link stayed invalid')
            time.sleep(20)
            hls_state['stop'].set();hls_thread.join(12)
            self.enabled(False);revoked=time.monotonic();require(anon['ended'].wait(30),'post-expiry manual disable exceeded 30 seconds')
            require(new_anon['ended'].wait(30),'manual disable did not stop the renewed viewer')
            self.report['measurements']['postExpiryDisableLastByteSeconds']=round(max(0,self.flv_snapshot(anon)[1]-revoked),3)
            self.gone(cred,viewer,[viewer['httpFlvPath'],hls,segment])
            self.gone(new_cred,new_viewer,[new_viewer['httpFlvPath']])
            self.record('manual disable after renewal stopped both old and new viewers and invalidated both links')
            self.report['status']='PASS'
        finally:
            if hls_state is not None:hls_state['stop'].set()
            if hls_thread is not None:hls_thread.join(12)
            try:
                if self.status()['enabled']: self.enabled(False)
                self.report['restoredSharingDisabled']=not self.status()['enabled']
            finally:
                for response,thread,state in self.media_readers:
                    state['stop'].set(); thread.join(3)
                    if thread.is_alive(): response.close()
                self.save()

def main():
    p=argparse.ArgumentParser(description=__doc__)
    for n in ['base','public-base','device','channel']:p.add_argument('--'+n,required=True)
    for n in ['session','output','ffmpeg']:p.add_argument('--'+n,type=Path,required=True)
    p.add_argument('--channel-pk',type=int,required=True); a=p.parse_args(); c=Deployed(a)
    try:c.run_acceptance()
    except Exception as e:
        c.report['status']='FAIL'; c.report['errorType']=type(e).__name__; c.report['error']=__import__('re').sub(r'https?://[^ ]+', '[private-url]',str(e))[:600]; c.save(); print('FAIL '+type(e).__name__+'; inspect private report/logs',flush=True); raise SystemExit(1)
if __name__=='__main__':main()
