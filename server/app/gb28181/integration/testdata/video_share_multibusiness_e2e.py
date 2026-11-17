#!/usr/bin/env python3
"""Isolated sharing + real cloud MP4 + loopback SIP/PS-RTP acceptance.

Requires the sibling video_share_full_app_e2e.py and its ordinary CLI arguments.
No external camera/upstream is contacted. Permissions are never repaired here.
Use --self-test for the SIP/RTP wire helpers without starting any application.
"""
import argparse
import datetime as dt
import json
from pathlib import Path
import queue
import re
import secrets
import socket
import sqlite3
import struct
import subprocess
import sys
import threading
import time
import urllib.parse

UPPER = '34020000002000000009'
LOCAL = '34020000002000000008'
PUBLISHED = '34020000001320000010'
SSRC = '0200000999'


def require(value, message):
    if not value:
        raise RuntimeError(message)


def parse_sip(data):
    head, _, body = data.partition(b'\r\n\r\n')
    lines = head.decode('utf-8').split('\r\n')
    headers = {}
    for line in lines[1:]:
        key, sep, value = line.partition(':')
        if sep:
            headers.setdefault(key.lower(), []).append(value.strip())
    return lines[0], headers, body


def sip_message(first, headers, body=b''):
    lines = [first] + [key + ': ' + value for key, value in headers]
    lines += ['Content-Length: ' + str(len(body)), '', '']
    return '\r\n'.join(lines).encode() + body


def rtp_payload(data):
    require(len(data) >= 12 and data[0] >> 6 == 2, 'invalid RTP header')
    offset = 12 + 4 * (data[0] & 15)
    if data[0] & 16:
        require(len(data) >= offset + 4, 'truncated RTP extension')
        offset += 4 + 4 * struct.unpack('!H', data[offset + 2:offset + 4])[0]
    end = len(data) - (data[-1] if data[0] & 32 else 0)
    require(end >= offset, 'truncated RTP payload')
    seq, stamp, ssrc = struct.unpack('!HII', data[2:12])
    return data[1] & 127, ssrc, seq, stamp, data[offset:end]


class UpperPeer:
    def __init__(self, sip_port, output):
        self.target = ('127.0.0.1', sip_port)
        self.sip = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        self.sip.bind(('127.0.0.1', 0))
        self.port = self.sip.getsockname()[1]
        self.rtp = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        self.rtp.bind(('127.0.0.1', 0))
        self.rtp_port = self.rtp.getsockname()[1]
        self.sip.settimeout(.25)
        self.rtp.settimeout(.25)
        self.stop = threading.Event()
        self.responses = queue.Queue()
        self.lock = threading.Lock()
        self.packets = self.bytes = 0
        self.last = 0
        self.sequences = set()
        self.timestamps = set()
        self.ps_seen = False
        self.output = output.open('wb')
        self.after_output = None
        self.threads = [threading.Thread(target=self.sip_loop, daemon=True),
                        threading.Thread(target=self.rtp_loop, daemon=True)]
        for thread in self.threads:
            thread.start()

    def sip_loop(self):
        while not self.stop.is_set():
            try:
                data, address = self.sip.recvfrom(65535)
                first, headers, _ = parse_sip(data)
                if first.startswith('SIP/2.0'):
                    self.responses.put((first, headers))
                    continue
                method = first.split()[0]
                if method not in ('REGISTER', 'MESSAGE', 'BYE', 'OPTIONS'):
                    continue
                copied = [(key.title(), value) for key in ('via', 'from', 'to', 'call-id', 'cseq')
                          for value in headers.get(key, [])]
                copied = [(key, value + ';tag=upper-fixture' if key == 'To' and ';tag=' not in value else value)
                          for key, value in copied]
                if method == 'REGISTER':
                    copied += [('Expires', '3600'), ('Contact', headers['contact'][0])]
                self.sip.sendto(sip_message('SIP/2.0 200 OK', copied), address)
            except socket.timeout:
                pass
            except OSError:
                if not self.stop.is_set():
                    raise

    def rtp_loop(self):
        tail = b''
        while not self.stop.is_set():
            try:
                data, _ = self.rtp.recvfrom(65535)
                pt, ssrc, seq, stamp, payload = rtp_payload(data)
                if pt != 96 or ssrc != int(SSRC):
                    continue
                self.output.write(payload)
                self.output.flush()
                with self.lock:
                    if self.after_output is not None:
                        self.after_output.write(payload)
                        self.after_output.flush()
                    self.packets += 1
                    self.bytes += len(payload)
                    self.last = time.monotonic()
                    self.sequences.add(seq)
                    self.timestamps.add(stamp)
                    self.ps_seen |= b'\x00\x00\x01\xba' in tail + payload
                tail = payload[-3:]
            except socket.timeout:
                pass
            except RuntimeError:
                pass
            except OSError:
                if not self.stop.is_set():
                    raise

    def observe_after_revocation(self, path):
        with self.lock:
            self.after_output = path.open('wb')

    def snapshot(self):
        with self.lock:
            return {'packets': self.packets, 'bytes': self.bytes, 'last': self.last,
                    'sequences': len(self.sequences), 'timestamps': len(self.timestamps), 'ps': self.ps_seen}

    def transaction(self, method, body=b'', timeout=35):
        cseq = 1 if method == 'INVITE' else 2
        headers = [('Via', f'SIP/2.0/UDP 127.0.0.1:{self.port};branch=z9hG4bK-{secrets.token_hex(8)}'),
                   ('From', f'<sip:{UPPER}@3402000000>;tag={self.tag}'),
                   ('To', self.to), ('Call-ID', self.call_id), ('CSeq', f'{cseq} {method}'),
                   ('Contact', f'<sip:{UPPER}@127.0.0.1:{self.port}>'), ('Max-Forwards', '70')]
        if body:
            headers += [('Subject', f'{PUBLISHED}:{SSRC},{UPPER}:0'), ('Content-Type', 'application/sdp')]
        target = self.contact if method != 'INVITE' else f'sip:{PUBLISHED}@127.0.0.1:{self.target[1]}'
        packet = sip_message(f'{method} {target} SIP/2.0', headers, body)
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            self.sip.sendto(packet, self.target)
            until = min(deadline, time.monotonic() + .5)
            while time.monotonic() < until:
                try:
                    first, response = self.responses.get(timeout=max(.01, until - time.monotonic()))
                except queue.Empty:
                    break
                if response.get('call-id') != [self.call_id] or response.get('cseq') != [f'{cseq} {method}']:
                    continue
                status = int(first.split()[1])
                if status < 200:
                    continue
                require(status == 200, f'loopback upstream {method} failed: SIP {status}')
                return response
        raise RuntimeError('loopback upstream ' + method + ' timed out')

    def invite(self):
        self.tag = secrets.token_hex(6)
        self.call_id = 'share-e2e-' + secrets.token_hex(8)
        self.to = f'<sip:{PUBLISHED}@3402000000>'
        self.contact = ''
        body = (f'v=0\r\no={UPPER} 0 0 IN IP4 127.0.0.1\r\ns=Play\r\nc=IN IP4 127.0.0.1\r\n'
                f't=0 0\r\nm=video {self.rtp_port} RTP/AVP 96\r\na=recvonly\r\na=rtpmap:96 PS/90000\r\ny={SSRC}\r\n').encode()
        response = self.transaction('INVITE', body)
        self.to = response['to'][0]
        contact = response.get('contact', [f'<sip:{PUBLISHED}@127.0.0.1:{self.target[1]}>'])[0]
        match = re.search(r'<([^>]+)>', contact)
        self.contact = match.group(1) if match else contact.split(';')[0]
        headers = [('Via', f'SIP/2.0/UDP 127.0.0.1:{self.port};branch=z9hG4bK-{secrets.token_hex(8)}'),
                   ('From', f'<sip:{UPPER}@3402000000>;tag={self.tag}'), ('To', self.to),
                   ('Call-ID', self.call_id), ('CSeq', '1 ACK'), ('Max-Forwards', '70')]
        self.sip.sendto(sip_message(f'ACK {self.contact} SIP/2.0', headers), self.target)

    def close(self):
        self.stop.set()
        for thread in self.threads:
            thread.join(timeout=1)
        self.sip.close()
        self.rtp.close()
        self.output.close()
        if self.after_output is not None:
            self.after_output.close()


def candidate_check(args):
    from video_share_full_app_e2e import Check, DEVICE, CHANNEL, STREAM

    class MultiCheck(Check):
        def __init__(self, arguments):
            super().__init__(arguments)
            self.results = {'cloudRecording': {'status': 'NOT_RUN'}, 'cascade': {'status': 'NOT_RUN'}}
            self.peer = None
            self.admin = None
            self.cloud_started = False
            self.cascade_started = False
            self.revoked_at = None

        def start(self, argv, name):
            if name == 'zlm':
                # Configure storage before this process sees any source. Changing
                # record.filePath after publication leaves that source's old path.
                ini = Path(argv[argv.index('-c') + 1]).resolve()
                require(ini.is_relative_to(self.root), 'ZLM fixture config escaped the isolated root')
                record_path = self.root / 'record'
                record_path.mkdir(mode=0o700)
                text = ini.read_text()
                require('[protocol]\n' in text and '[record]' not in text,
                        'unexpected parent ZLM fixture config; cannot prove isolated recording storage')
                require('mp4_save_path=' not in text, 'parent fixture already defines MP4 storage')
                text = text.replace('[protocol]\n', '[protocol]\nmp4_save_path=' + str(record_path) + '\n', 1)
                text += '\n[record]\nfilePath=' + str(record_path) + '\n'
                ini.write_text(text)
            process = super().start(argv, name)
            if name == 'zlm':
                self.wait(self.private_record_paths, 'ZLM private MP4 storage before source publication', 15)
                self.record('ZLM MP4 storage was isolated and verified before publishing the synthetic source')
            return process

        def private_record_paths(self):
            configuration = self.zlm('getServerConfig').get('data')
            if isinstance(configuration, list):
                configuration = configuration[0] if configuration else {}
            require(isinstance(configuration, dict), 'ZLM configuration readback is invalid')
            expected = (self.root / 'record').resolve()
            require(all(configuration.get(key) and Path(configuration[key]).resolve() == expected
                        for key in ('protocol.mp4_save_path', 'record.filePath')),
                    'ZLM MP4 storage readback is not the private recording directory')
            return True

        def zlm(self, api, **params):
            query = urllib.parse.urlencode(dict(secret=self.secret, **params))
            with self.opener.open(f'http://127.0.0.1:{self.zlm_port}/index/api/{api}?' + query, timeout=10) as response:
                value = json.load(response)
            require(value.get('code') == 0, 'isolated ZLM ' + api + ' failed')
            return value

        def recording(self):
            return self.zlm('isRecording', type=1, vhost='__defaultVhost__', app='rtp', stream=STREAM).get('status') is True

        def raw_api(self, path, method='GET', body=None):
            status, result = self.request('/api/gb28181/cascade/platforms' + path, method, body, self.admin)
            require(status == 200 and isinstance(result, dict) and result.get('code', 0) == 0,
                    'cascade configuration HTTP ' + method + ' failed: ' + str(status))
            require(result.get('runtimeSynced', True), 'cascade runtime did not synchronize')
            return result

        def success(self, path, method='GET', body=None, token=None):
            if path == f'/api/gb28181/play/{DEVICE}/{CHANNEL}' and not self.cloud_started:
                self.admin = token
                # This runs after candidate bootstrap has installed its real managed hooks.
                self.cloud_started = True
                try:
                    self.private_record_paths()
                    self.zlm('setServerConfig', **{'protocol.mp4_max_second': 3})
                    super().success(f'/api/gb28181/device-mgmt/channel/{self.channel_pk}/cloud-recording',
                                    'PATCH', {'enabled': True}, token)
                    self.results['cloudRecording'] = {'status': 'STARTING'}
                except Exception as error:
                    self.results['cloudRecording'] = {'status': 'FAIL', 'error': str(error)}
            if (path == f'/api/gb28181/channel-shares/{self.channel_pk}' and method == 'PUT'
                    and body and body.get('enabled') is False):
                try:
                    self.wait(self.recording, 'actual ZLM MP4 recording', 15)
                    self.record('cloud recorder is active on the real ZLM before sharing is disabled')
                    self.results['cloudRecording'] = {'status': 'RECORDING'}
                except Exception as error:
                    self.results['cloudRecording'] = {'status': 'FAIL', 'error': str(error)}
                try:
                    self.setup_cascade()
                except Exception as error:
                    self.results['cascade'] = {'status': 'FAIL', 'error': str(error)}
            result = super().success(path, method, body, token)
            if (path == f'/api/gb28181/channel-shares/{self.channel_pk}' and method == 'PUT'
                    and body and body.get('enabled') is False):
                self.revoked_at = time.monotonic()
                self.revoked_wall = time.time()
                if self.peer:
                    self.peer.observe_after_revocation(self.root / 'cascade-after-revocation.ps')
                    self.rtp_before = self.peer.snapshot()
            return result

        def setup_cascade(self):
            self.peer = UpperPeer(self.sip_port, self.root / 'cascade-received.ps')
            with sqlite3.connect(self.db) as connection:
                device_pk = connection.execute('SELECT id FROM gb_device WHERE device_id=?', (DEVICE,)).fetchone()[0]
            platform = self.raw_api('', 'POST', {
                'name': 'Synthetic local upstream', 'upstreamServerId': UPPER,
                'upstreamDomain': '3402000000', 'host': '127.0.0.1', 'port': self.peer.port,
                'localDeviceId': LOCAL, 'localDomain': '3402000000', 'localSipIp': '127.0.0.1',
                'localSipPort': self.sip_port, 'mediaAdvertiseIp': '127.0.0.1',
                'profileOverride': '2016', 'registerExpires': 3600, 'keepaliveInterval': 60,
                'transport': 'UDP', 'catalogBatchSize': 1, 'maxStreams': 4,
                'autoPushCatalog': False, 'publishPlatform': False, 'publishCivil': False,
                'publishGroup': False, 'rtcpKeepalive': False, 'enabled': False})
            self.platform_id = platform['id']
            self.raw_api(f'/{self.platform_id}/shares', 'PUT', {
                'scope': 'all', 'expectedProjectionRevision': platform['projectionRevision'],
                'devices': [{'sourceDeviceId': device_pk, 'publishedDeviceId': '34020000002000000007',
                             'name': 'Synthetic source'}],
                'channels': [{'sourceDeviceId': device_pk, 'sourceChannelId': self.channel_pk,
                              'publishedChannelId': PUBLISHED, 'name': 'Synthetic live', 'ptzAllowed': False}]})
            self.raw_api(f'/{self.platform_id}/enabled', 'PUT',
                         {'enabled': True, 'expectedRevision': platform['configRevision']})
            def registered():
                value = self.raw_api(f'/{self.platform_id}')
                return value.get('registerAt') is not None and value.get('registerExpiresAt') is not None
            self.wait(registered, 'production SIP REGISTER to the loopback upper platform', 20)
            self.peer.invite()
            def active():
                with sqlite3.connect(self.db) as connection:
                    row = connection.execute('SELECT state FROM gb_cascade_media_session WHERE call_id=?',
                                             (self.peer.call_id,)).fetchone()
                return row is not None and row[0] == 'active'
            self.wait(active, 'production cascade SIP dialog active after ACK', 10)
            self.wait(lambda: self.peer.snapshot()['packets'] >= 5 and self.peer.snapshot()['ps'],
                      'actual PS-over-RTP packets from the real ZLM', 15)
            self.cascade_started = True
            self.results['cascade'] = {'status': 'STREAMING', 'before': self.peer.snapshot()}
            self.record('production SIP INVITE/ACK established a real ZLM PS-over-RTP sender to loopback')

        def decode_frame(self, source, output_name, log_name, offset=None):
            output = self.root / output_name
            progress = self.root / 'logs' / (log_name + '.progress')
            command = [self.args.ffmpeg, '-hide_banner', '-loglevel', 'error', '-nostats',
                       '-progress', str(progress), '-i', str(source)]
            if offset is not None:
                command += ['-ss', str(offset)]
            # The release binary's enumerated video encoder is MJPEG. Request
            # it explicitly while retaining the real input decoder and frame proof.
            command += ['-map', '0:v:0', '-frames:v', '1', '-c:v', 'mjpeg',
                        '-threads:v', '1', '-f', 'image2', '-update', '1', '-y', str(output)]
            self.run(command, log_name)
            values = dict(line.split('=', 1) for line in progress.read_text().splitlines() if '=' in line)
            require(values.get('frame', '').strip() == '1' and values.get('progress') == 'end',
                    'FFmpeg did not decode and output exactly one video frame')
            require(output.is_file() and output.stat().st_size > 100 and
                    output.read_bytes().startswith(b'\xff\xd8'),
                    'decoded JPEG frame is missing, empty or lacks the JPEG SOI marker')
            return output

        def prove_cloud(self):
            require(self.results['cloudRecording']['status'] == 'RECORDING', 'cloud recording never became active')
            require(self.recording(), 'share disable stopped the actual ZLM MP4 recorder')
            super().success(f'/api/gb28181/device-mgmt/channel/{self.channel_pk}/cloud-recording',
                            'PATCH', {'enabled': False}, self.admin)
            self.wait(lambda: not self.recording(), 'cloud recording stopped independently', 10)
            def indexed():
                with sqlite3.connect(self.db) as connection:
                    rows = connection.execute('SELECT file_path,start_time,time_len,file_size FROM gb_recording_file '
                        'WHERE channel_id=? AND source=\'hook\' AND metadata_state=\'complete\' '
                        'AND start_time IS NOT NULL AND time_len IS NOT NULL AND file_size IS NOT NULL '
                        'ORDER BY start_time DESC', (self.channel_pk,)).fetchall()
                complete = []
                for path, start, duration, size in rows:
                    if not path or not start or not duration or not size:
                        continue
                    candidate = Path(path).resolve()
                    if candidate.is_relative_to(self.root) and candidate.is_file():
                        complete.append((str(candidate), start, duration, size))
                return complete
            self.wait(lambda: bool(indexed()), 'real on_record_mp4 hook persisted a private complete file index', 15)
            selected = None
            for path, start, duration, size in indexed():
                began = dt.datetime.fromisoformat(start.replace('Z', '+00:00'))
                if began.tzinfo is None:
                    began = began.replace(tzinfo=dt.timezone.utc)
                # Segment timestamps have whole-second precision. Select a frame
                # at least three seconds after revocation, allowing the next GOP.
                target = max(self.revoked_wall + 3, began.timestamp() + .25)
                offset = target - began.timestamp()
                if 0 <= offset < (duration or 0) - .5 and size and Path(path).is_file():
                    selected = path, offset, duration, size, target - self.revoked_wall
                    break
            require(selected is not None, 'no real MP4 segment covers three seconds after sharing was revoked')
            path, offset, duration, size, delay = selected
            output = self.decode_frame(path, 'cloud-after-revocation.jpg',
                                       'cloud-decode-after-revocation', offset)
            self.results['cloudRecording'] = {'status': 'PASS', 'fileBytes': size, 'durationSeconds': duration,
                'decodedFrameAfterRevocationSeconds': round(delay, 3), 'artifact': output.name}
            self.record('actual recorded MP4 decoded a video frame after sharing was revoked; recorder stopped independently')

        def prove_cascade(self):
            require(self.cascade_started, 'production SIP cascade did not start')
            after = self.peer.snapshot()
            require(after['bytes'] > self.rtp_before['bytes'] and after['last'] > self.revoked_at + 8,
                    'cascade supplied no sustained RTP bytes across share disable')
            require(after['sequences'] > self.rtp_before['sequences'] and after['timestamps'] > self.rtp_before['timestamps'],
                    'cascade RTP sequence/timestamp did not advance across share disable')
            frame = self.decode_frame(self.root / 'cascade-after-revocation.ps',
                                      'cascade-decoded.jpg', 'cascade-decode')
            self.peer.transaction('BYE', timeout=10)
            def closed():
                with sqlite3.connect(self.db) as connection:
                    row = connection.execute('SELECT state FROM gb_cascade_media_session WHERE call_id=?',
                                             (self.peer.call_id,)).fetchone()
                return row is not None and row[0] == 'closed'
            self.wait(closed, 'cascade business dialog closed after BYE', 10)
            baseline = self.peer.snapshot()['bytes']
            time.sleep(1)
            require(self.peer.snapshot()['bytes'] == baseline, 'cascade BYE did not stop its RTP sender')
            self.results['cascade'] = {'status': 'PASS', 'after': after, 'artifact': frame.name, 'decodedAfterDisable': True,
                                      'keptStreamingAfterDisableSeconds': round(after['last'] - self.revoked_at, 3)}
            self.record('actual decodable PS/RTP continued after share disable and stopped only on its own SIP BYE')

        def application(self):
            super().application()
            require(self.revoked_at is not None, 'share disable was not exercised')
            # Keep real recording and RTP running well past the share relay guard interval.
            until = self.revoked_at + 10
            while time.monotonic() < until:
                if self.results['cloudRecording']['status'] == 'RECORDING' and not self.recording():
                    self.results['cloudRecording'] = {'status': 'FAIL', 'error': 'actual ZLM recorder stopped after share disable'}
                time.sleep(min(.5, max(0, until - time.monotonic())))
            for name, proof in [('cascade', self.prove_cascade), ('cloudRecording', self.prove_cloud)]:
                try:
                    proof()
                except Exception as error:
                    previous = self.results[name].get('error', '')
                    self.results[name] = {'status': 'FAIL', 'error': previous or str(error)}
            self.save_report()
            require(all(item['status'] == 'PASS' for item in self.results.values()),
                    'one or more multi-business checks failed; inspect private multi-business-results.json')

        def save_report(self):
            report = {'status': 'PASS' if all(item['status'] == 'PASS' for item in self.results.values()) else 'FAIL',
                      'businesses': self.results, 'checks': self.checks}
            serialized = json.dumps(report, indent=2)
            for value in [self.secret, self.admin, *getattr(self, 'media_capabilities', [])]:
                if value:
                    serialized = serialized.replace(value, '[private]')
            (self.root / 'multi-business-results.json').write_text(serialized)
            print(serialized, flush=True)

        def close(self):
            if self.peer:
                if self.cascade_started:
                    try:
                        self.peer.transaction('BYE', timeout=2)
                    except Exception:
                        pass
                self.peer.close()
            if self.admin and self.cloud_started:
                try:
                    super().success(f'/api/gb28181/device-mgmt/channel/{self.channel_pk}/cloud-recording',
                                    'PATCH', {'enabled': False}, self.admin)
                except Exception:
                    pass
            super().close()

    return MultiCheck(args)


def self_test():
    import tempfile
    with tempfile.TemporaryDirectory(prefix='uvp-wire-check-') as temporary:
        fake = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        fake.bind(('127.0.0.1', 0))
        fake.settimeout(3)
        peer = UpperPeer(fake.getsockname()[1], Path(temporary) / 'received.ps')
        errors = queue.Queue()
        try:
            register = sip_message('REGISTER sip:upper SIP/2.0', [
                ('Via', 'SIP/2.0/UDP 127.0.0.1:1;branch=z9hG4bK-test'),
                ('From', '<sip:local@domain>;tag=test'), ('To', '<sip:local@domain>'),
                ('Call-ID', 'register-test'), ('CSeq', '1 REGISTER'), ('Contact', '<sip:local@127.0.0.1>')])
            fake.sendto(register, ('127.0.0.1', peer.port))
            first, headers, _ = parse_sip(fake.recv(65535))
            require(first == 'SIP/2.0 200 OK' and headers['expires'] == ['3600'], 'REGISTER helper failed')

            def remote():
                try:
                    invite, address = fake.recvfrom(65535)
                    first, headers, body = parse_sip(invite)
                    require(first.startswith('INVITE ') and f'm=video {peer.rtp_port}'.encode() in body,
                            'INVITE body or target failed')
                    copied = [(key.title(), value) for key in ('via', 'from', 'to', 'call-id', 'cseq')
                              for value in headers[key]]
                    fake.sendto(sip_message('SIP/2.0 100 Trying', copied), address)
                    copied = [(key, value + ';tag=answer' if key == 'To' else value) for key, value in copied]
                    copied.append(('Contact', f'<sip:local@127.0.0.1:{fake.getsockname()[1]}>'))
                    fake.sendto(sip_message('SIP/2.0 200 OK', copied), address)
                    ack, ack_headers, _ = parse_sip(fake.recv(65535))
                    require(ack.startswith('ACK ') and ';tag=answer' in ack_headers['to'][0] and
                            ack_headers['cseq'] == ['1 ACK'], 'ACK helper failed')
                    bye, bye_headers, _ = parse_sip(fake.recv(65535))
                    require(bye.startswith('BYE ') and bye_headers['cseq'] == ['2 BYE'], 'BYE helper failed')
                    reply = [(key.title(), value) for key in ('via', 'from', 'to', 'call-id', 'cseq')
                             for value in bye_headers[key]]
                    fake.sendto(sip_message('SIP/2.0 200 OK', reply), address)
                except Exception as error:
                    errors.put(error)
            thread = threading.Thread(target=remote)
            thread.start()
            peer.invite()
            peer.transaction('BYE', timeout=3)
            thread.join(timeout=3)
            require(not thread.is_alive() and errors.empty(), 'SIP transaction self-test failed')
            for seq, payload in enumerate((b'\x00\x00', b'\x01\xbaPS-video')):
                packet = struct.pack('!BBHII', 0x80, 96, seq, seq * 3600, int(SSRC)) + payload
                fake.sendto(packet, ('127.0.0.1', peer.rtp_port))
            deadline = time.monotonic() + 2
            while peer.snapshot()['packets'] < 2 and time.monotonic() < deadline:
                time.sleep(.01)
            observed = peer.snapshot()
            require(observed['packets'] == 2 and observed['ps'] and observed['timestamps'] == 2,
                    'RTP depacketization/filter/PS boundary helper failed')
            extension = struct.pack('!BBHIIHH', 0x90, 96, 2, 7200, int(SSRC), 0x1000, 1) + b'abcdpayload'
            require(rtp_payload(extension)[-1] == b'payload', 'RTP extension skip failed')
            require(parse_sip(register)[1]['content-length'] == ['0'], 'SIP byte length failed')
            print('PASS local SIP REGISTER/INVITE/100/200/ACK/BYE and RTP/PS wire helpers')
        finally:
            peer.close()
            fake.close()
    return 0


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--self-test', action='store_true')
    for name in ('root', 'server', 'setup', 'baseline', 'zlm', 'ffmpeg', 'sample'):
        parser.add_argument('--' + name)
    args = parser.parse_args()
    if args.self_test:
        return self_test()
    for name in ('root', 'server', 'setup', 'baseline', 'zlm', 'ffmpeg', 'sample'):
        require(getattr(args, name), '--' + name + ' is required')
        value = Path(getattr(args, name)).absolute()
        if name != 'root':
            value = value.resolve(strict=True)
        setattr(args, name, str(value))
    check = None
    try:
        check = candidate_check(args)
        check.configure()
        check.initialize()
        check.media()
        check.application()
        return 0
    except Exception as error:
        message = str(error)
        if check:
            for value in [check.secret, check.admin, *getattr(check, 'media_capabilities', [])]:
                if value:
                    message = message.replace(value, '[private]')
            check.save_report()
        print('FAIL ' + message, file=sys.stderr)
        return 1
    finally:
        if check:
            check.close()


if __name__ == '__main__':
    sys.exit(main())
