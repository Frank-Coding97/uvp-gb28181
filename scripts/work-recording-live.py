#!/usr/bin/env python3
# Local integration experiment, not a production or customer-node acceptance test.
# Evidence is retained in a fresh temporary directory; credentials are redacted.
import argparse, shutil, sys
import configparser, tempfile, pathlib, socket, uuid, subprocess, time, json, threading, http.server, urllib.request, urllib.parse, signal
parser = argparse.ArgumentParser(description='Isolated macOS ZLM manual MP4 lifecycle experiment; uses a synthetic RTSP source.')
parser.add_argument('--zlm', required=True)
parser.add_argument('--config-template', required=True)
parser.add_argument('--ffmpeg', default='ffmpeg')
parser.add_argument('--ffprobe', default='ffprobe')
parser.add_argument('--scenario', choices=['normal', 'rapid-shared', 'rapid-isolated'], default='normal')
args = parser.parse_args()
if sys.platform != 'darwin' or not pathlib.Path('/usr/bin/sandbox-exec').exists():
    parser.error('this local experiment requires macOS sandbox-exec to restrict outbound traffic to loopback')
for name in ['zlm', 'ffmpeg', 'ffprobe']:
    resolved = shutil.which(getattr(args, name))
    if not resolved:
        parser.error(name + ' executable not found')
    setattr(args, name, resolved)
root = pathlib.Path(tempfile.mkdtemp(prefix='uvp-recorder-controlled-'))
print(str(root), flush=True)

def port():
    with socket.socket() as s:
        s.bind(('127.0.0.1', 0))
        return s.getsockname()[1]
http_port, rtsp_port = (port(), port())
secret = uuid.uuid4().hex
records = []
events = []

class Hook(http.server.BaseHTTPRequestHandler):

    def do_POST(self):
        body = json.loads(self.rfile.read(int(self.headers['Content-Length'])))
        records.append({'received_at': time.time(), 'body': body})
        self.send_response(200)
        self.end_headers()
        self.wfile.write(b'{"code":0}')

    def log_message(self, *args):
        pass
hook = http.server.ThreadingHTTPServer(('127.0.0.1', 0), Hook)
threading.Thread(target=hook.serve_forever, daemon=True).start()
config = configparser.ConfigParser(interpolation=None, strict=False)
config.optionxform = str
if not config.read(args.config_template):
    parser.error('config template could not be read')

def put(section, key, value):
    if section not in config:
        config.add_section(section)
    config[section][key] = str(value)
for section in ['http', 'rtmp', 'rtsp', 'rtp_proxy', 'rtc', 'srt', 'shell', 'onvif']:
    put(section, 'port', 0)
    for key in ['sslport', 'tcpPort']:
        if key in config[section]:
            put(section, key, 0)
put('general', 'listen_ip', '127.0.0.1')
put('general', 'mediaServerId', 'uvp-controlled-test')
put('general', 'streamNoneReaderDelayMS', 3600000)
put('http', 'port', http_port)
put('http', 'rootPath', root / 'www')
put('rtsp', 'port', rtsp_port)
put('api', 'secret', secret)
put('api', 'apiDebug', 0)
put('api', 'snapRoot', root / 'snap')
put('protocol', 'enable_mp4', 0)
put('protocol', 'enable_hls', 0)
put('protocol', 'enable_hls_fmp4', 0)
put('protocol', 'mp4_max_second', 2)
put('protocol', 'mp4_save_path', root / 'record')
put('protocol', 'mp4_as_player', 1)
for key in list(config['hook']):
    if key.startswith('on_'):
        put('hook', key, '')
put('hook', 'enable', 1)
put('hook', 'on_record_mp4', f'http://127.0.0.1:{hook.server_port}/record')
conf = root / 'config.ini'
with conf.open('w') as f:
    config.write(f)
base = f'http://127.0.0.1:{http_port}/index/api/'

def api(name, **params):
    params['secret'] = secret
    begin = time.time()
    req = urllib.request.Request(base + name, data=urllib.parse.urlencode(params).encode())
    with urllib.request.urlopen(req, timeout=4) as response:
        body = json.load(response)
    events.append({'name': name, 'sent_at': begin, 'received_at': time.time(), 'params': {k: v for k, v in params.items() if k != 'secret'}, 'body': body})
    return body
zlog = (root / 'zlm.log').open('w')
flog = (root / 'ffmpeg.log').open('w')
zlm = None
ff = None
try:
    profile = '(version 1)(allow default)(deny network-outbound)(allow network-outbound (remote ip "localhost:*"))'
    zlm = subprocess.Popen(['/usr/bin/sandbox-exec', '-p', profile, args.zlm, '-c', str(conf), '-l', '1', '-t', '2', '--log-dir', str(root / 'logs')], cwd=root, stdout=zlog, stderr=subprocess.STDOUT)
    for i in range(60):
        try:
            if api('getApiList').get('code') == 0:
                break
        except Exception:
            time.sleep(0.1)
    else:
        raise RuntimeError('ZLM not ready')
    (root / 'listeners.txt').write_text(subprocess.run(['lsof', '-nP', '-a', '-p', str(zlm.pid), '-i'], capture_output=True, text=True).stdout)
    effective = api('getServerConfig')['data'][0]
    assert effective['general.mediaServerId'] == 'uvp-controlled-test'
    assert str(effective['protocol.enable_mp4']) == '0'
    for event in events:
        if event.get('name') == 'getServerConfig':
            event['body'] = json.loads(json.dumps(event['body']).replace(secret, '[REDACTED_SECRET]'))
    stream = 'controlled-' + uuid.uuid4().hex[:12]
    ff = subprocess.Popen([args.ffmpeg, '-hide_banner', '-loglevel', 'warning', '-re', '-f', 'lavfi', '-i', 'testsrc2=size=320x240:rate=25', '-c:v', 'libx264', '-preset', 'ultrafast', '-tune', 'zerolatency', '-g', '25', '-an', '-t', '40', '-f', 'rtsp', '-rtsp_transport', 'tcp', f'rtsp://127.0.0.1:{rtsp_port}/rtp/{stream}'], stdout=flog, stderr=subprocess.STDOUT)
    target = {'vhost': '__defaultVhost__', 'app': 'rtp', 'stream': stream, 'type': 1}
    for i in range(60):
        ready = api('getMediaList', app='rtp', stream=stream)
        if ready.get('data'):
            break
        time.sleep(0.1)
    else:
        raise RuntimeError('media not ready')
    assert api('isRecording', **target)['status'] is False
    assert not records and (not list(root.rglob('*.mp4')))
    job_directories = {}
    if args.scenario == 'rapid-isolated':
        for job in ['A', 'B']:
            candidate = root / 'work-recordings' / str(uuid.uuid4())
            listing = api('getMP4RecordFile', customized_path=str(candidate), **target)
            assert listing['code'] == 0
            resolved = pathlib.Path(listing['data']['rootPath'])
            assert resolved.is_absolute() and resolved.is_relative_to(candidate), 'node ignored customized_path'
            assert not listing['data']['paths'], 'new job directory must be empty'
            job_directories[job] = candidate
    if args.scenario != 'normal':
        time.sleep(1 - time.time() % 1 + 0.05)
    for job in ['A', 'B']:
        events.append({'job': job, 'event': 'before_start', 'at': time.time()})
        directory = {'customized_path': str(job_directories[job])} if args.scenario == 'rapid-isolated' else {}
        answer = api('startRecord', max_second=2, **target, **directory)
        assert answer['code'] == 0 and answer['result']
        assert api('isRecording', **target)['status'] is True
        time.sleep(5.5 if args.scenario == 'normal' else 0.15)
        answer = api('stopRecord', **target)
        assert answer['code'] == 0 and answer['result']
        assert api('isRecording', **target)['status'] is False
        events.append({'job': job, 'event': 'after_stop', 'at': time.time()})
        time.sleep(3.5 if args.scenario == 'normal' else 0.05)
        assert ff.poll() is None, 'publisher exited during stop observation'
        assert api('getMediaList', app='rtp', stream=stream).get('data'), 'source disappeared after stop'
        assert api('isRecording', **target)['status'] is False
        events.append({'job': job, 'event': 'source_still_live_after_stop', 'at': time.time(), 'observed_hooks': len(records)})
    time.sleep(1)
    probes = []
    for file in root.rglob('*.mp4'):
        result = subprocess.run([args.ffprobe, '-v', 'error', '-show_entries', 'format=duration,size:stream=codec_name,width,height', '-of', 'json', str(file)], capture_output=True, text=True, check=True, timeout=15)
        decoded = subprocess.run([args.ffmpeg, '-v', 'error', '-i', str(file), '-f', 'null', '-'], capture_output=True, text=True, timeout=15)
        assert decoded.returncode == 0 and (not decoded.stderr), 'MP4 decode failed: ' + file.name
        probes.append({'path': str(file), 'probe': json.loads(result.stdout), 'decode_exit_code': decoded.returncode})
    assert records and probes
    hook_paths = {str(pathlib.Path(x['body']['file_path']).resolve()) for x in records}
    assert hook_paths == {str(pathlib.Path(x['path']).resolve()) for x in probes}
    if args.scenario != 'normal':
        start_seconds = {int(event['at']) for event in events if event.get('event') == 'before_start'}
        assert len(start_seconds) == 1, 'rapid recordings did not start within the same second'
    if args.scenario == 'rapid-shared':
        assert len(records) == 2 and len(hook_paths) == 1, 'expected default-path collision was not reproduced'
    else:
        assert len(records) == len(hook_paths), 'distinct recording callbacks collided on a path'
    if args.scenario == 'rapid-isolated':
        listed_paths = set()
        for directory in job_directories.values():
            dates = api('getMP4RecordFile', customized_path=str(directory), **target)
            assert dates['code'] == 0
            for period in dates['data']['paths']:
                listing = api('getMP4RecordFile', customized_path=str(directory), period=period, **target)
                assert listing['code'] == 0
                listed_paths.update(str((pathlib.Path(listing['data']['rootPath']) / name).resolve()) for name in listing['data']['paths'])
        assert listed_paths == hook_paths, 'custom directory reconciliation missed or crossed job files'
    (root / 'probes.json').write_text(json.dumps(probes, indent=2))
    print(json.dumps({'status': 'expected_collision_reproduced' if args.scenario == 'rapid-shared' else 'control_sequence_passed', 'scenario': args.scenario, 'files': len(probes), 'hooks': len(records), 'directory': str(root)}), flush=True)
except Exception as e:
    print(json.dumps({'status': 'failed', 'error': str(e), 'directory': str(root)}), flush=True)
    raise
finally:
    for process in [ff, zlm]:
        if process and process.poll() is None:
            process.send_signal(signal.SIGINT)
            try:
                process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait()
    events.append({'event': 'process_exit_codes', 'ffmpeg': ff.returncode if ff else None, 'zlm': zlm.returncode if zlm else None})
    hook.shutdown()
    hook.server_close()
    zlog.close()
    flog.close()
    (root / 'events.json').write_text(json.dumps(events, indent=2).replace(secret, '[REDACTED_SECRET]'))
    (root / 'hooks.json').write_text(json.dumps(records, indent=2))
    config['api']['secret'] = '[REDACTED_SECRET]'
    with conf.open('w') as f:
        config.write(f)
