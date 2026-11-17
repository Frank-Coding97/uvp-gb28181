#!/usr/bin/env python3
"""Real candidate/ZLM synthetic-source acceptance for long HLS and same-version restart.

Uses the sibling full_app Check and its --root/--server/--setup/--baseline/--zlm/
--ffmpeg/--sample CLI. Root must be new/empty. The source is synthetic, not a
hardware camera. Keeps auth enabled, client-IP binding, ordinary TTL=120 seconds
and the issued five-minute share deadline. Runs for at least 125 real seconds;
no clock override, TTL reduction, permission repair or existing DB is used.
"""
import argparse
import datetime as dt
import hashlib
import json
import os
from pathlib import Path
import re
import signal
import socket
import sqlite3
import sys
import time
import urllib.error
import urllib.parse

from video_share_full_app_e2e import Check, DEVICE, CHANNEL, require

CONFIG = '/api/gb28181/sip/service-config'
HOLD_SECONDS = 125


def assert_auth_policy(config):
    policy = config.get('playAuth', {})
    require(policy.get('authEnabled') is True, 'ordinary playback authorization must remain enabled')
    require(policy.get('authBindClientIP') is True, 'ordinary playback client-IP binding must remain enabled')
    require(policy.get('authTTLSeconds') == 120, 'ordinary playback TTL must remain 120 seconds')
    require(config.get('videoShare', {}).get('ttlSeconds') == 300, 'share TTL must remain five minutes')


def instant(value):
    return dt.datetime.fromisoformat(value.replace('Z', '+00:00'))


def private_error(error, private):
    message = str(error)
    for value in sorted((v for v in private if isinstance(v, str) and v), key=len, reverse=True):
        message = message.replace(value, '[private]')
    message = re.sub(r'https?://[^\s<>"\']+', '[url]', message)
    message = re.sub(r'/api/public/video-share/viewers/[^\s/?"\']+',
                     '/api/public/video-share/viewers/[private]', message)
    return message[:1200]


def playlist_segments(body, prefix):
    require(len(body) <= 1 << 20, 'HLS playlist exceeded one MiB')
    text = body.decode('utf-8')
    require(text.lstrip().startswith('#EXTM3U'), 'HLS response is not a playlist')
    require('play_token' not in text, 'public playlist leaked upstream token')
    entries = [line.strip() for line in text.splitlines() if line.strip() and not line.startswith('#')]
    require('#EXTINF:' in text and entries, 'HLS playlist contains no complete segments')
    for entry in entries:
        parsed = urllib.parse.urlsplit(entry)
        require(not parsed.scheme and not parsed.netloc and not parsed.query and not parsed.fragment
                and parsed.path.startswith(prefix), 'HLS segment escaped its anonymous viewer')
    return entries


def read_current_hls(check, viewer):
    prefix = '/api/public/video-share/viewers/' + viewer['viewerToken'] + '/hls/'
    require(viewer.get('hlsPath') == prefix + 'index.m3u8', 'viewer returned an unexpected manifest path')
    with check.opener.open(check.base + viewer['hlsPath'], timeout=10) as response:
        require(response.status == 200, 'anonymous HLS manifest did not return 200')
        entries = playlist_segments(response.read((1 << 20) + 1), prefix)
    # Re-fetch the current playlist on every round; never replay a saved TS URL.
    segment = entries[-1]
    with check.opener.open(check.base + segment, timeout=10) as response:
        require(response.status == 200, 'anonymous HLS segment did not return 200')
        body = response.read((8 << 20) + 1)
    require(3 * 188 <= len(body) <= 8 << 20 and len(body) % 188 == 0,
            'current HLS segment is not bounded MPEG-TS media')
    require(all(body[offset] == 0x47 for offset in range(0, len(body), 188)),
            'current HLS segment lacks TS packet synchronization')
    return {'bytes': len(body), 'sha256': hashlib.sha256(body).hexdigest()}, segment


def media_denied(check, url, description):
    try:
        response = check.opener.open(url, timeout=10)
    except urllib.error.HTTPError as failure:
        response = failure
    with response:
        require(response.status in (400, 401, 403, 404, 410), description + ': media was not denied')


def check_hooks(check):
    query = urllib.parse.urlencode({'secret': check.secret})
    with check.opener.open(f'http://127.0.0.1:{check.zlm_port}/index/api/getServerConfig?' + query,
                           timeout=10) as response:
        result = json.load(response)
    require(result.get('code') == 0 and isinstance(result.get('data'), list) and result['data'],
            'real ZLM configuration readback failed')
    config = result['data'][0]
    hook = urllib.parse.urlsplit(config.get('hook.on_play', ''))
    require(str(config.get('hook.enable')) == '1', 'real ZLM playback hooks are disabled')
    require(hook.hostname == '127.0.0.1' and hook.port == check.app_port
            and hook.path.endswith('/index/hook/on_play'), 'real ZLM on_play does not target the candidate')


def start_application(check, name):
    process = check.start([check.args.server], name)
    def ready():
        with socket.create_connection(('127.0.0.1', check.app_port), timeout=1):
            return True
    check.wait(ready, name + ' listener', 60)
    login = check.success('/api/login', 'POST', {'username': 'admin', 'password': check.password})
    require(login.get('mustChangePassword') is not True, 'fixture admin unexpectedly requires password change')
    return process, login['accessToken']


def protected_config(check, admin):
    before = check.success(CONFIG, token=admin)
    assert_auth_policy(before)
    altered = json.loads(json.dumps(before))
    altered['videoShare']['ttlSeconds'] = 180
    for method, body in (('GET', None), ('PUT', altered)):
        status, result = check.request(CONFIG, method, body)
        require(status in (401, 403), 'anonymous configuration ' + method + ' was not denied')
        require(not isinstance(result, dict) or not result.get('data'),
                'anonymous configuration response disclosed protected data')
    after = check.success(CONFIG, token=admin)
    require(after == before, 'anonymous configuration PUT changed protected configuration')
    return after


def persisted_share(check):
    with sqlite3.connect(check.db) as connection:
        row = connection.execute('SELECT public_id,generation,activated_at,expires_at,revision,enabled '
                                 'FROM gb_channel_video_share WHERE channel_pk=?', (check.channel_pk,)).fetchone()
    require(row is not None and row[-1] == 1, 'issued share is missing or disabled in its original database')
    return row


def binary_digest(path):
    digest = hashlib.sha256()
    with open(path, 'rb') as binary:
        for chunk in iter(lambda: binary.read(1 << 20), b''):
            digest.update(chunk)
    return digest.hexdigest()


def execute(args):
    check = None
    private = []
    report = {'status': 'FAIL', 'scope': 'Real candidate processes and ZLM; synthetic moving source, not hardware',
              'ordinaryTokenTTLSeconds': 120, 'clientIPBinding': True, 'issuedShareTTLSeconds': 300}
    try:
        check = Check(args)
        private.extend((check.secret, check.password))
        check.configure()
        check.initialize()
        check.media()
        candidate_digest = binary_digest(args.server)
        process, admin = start_application(check, 'candidate-before-restart')
        private.append(admin)
        protected_config(check, admin)
        check_hooks(check)
        check.record('ordinary playback auth enabled, IP-bound and TTL 120; real ZLM on_play hook enabled')
        check.record('anonymous protected configuration GET and valid PUT denied without mutation')

        path = f'/api/gb28181/channel-shares/{check.channel_pk}'
        state = check.success(path, token=admin)
        require(state['enabled'] is False, 'new share did not default off')
        enabled = check.success(path, 'PUT', {'enabled': True, 'revision': state['revision']}, admin)
        link = check.success(path + '/link', 'POST', {'revision': enabled['revision']}, admin)
        private.extend((link['publicId'], link['signature']))
        require(295 <= (instant(link['expiresAt']) - instant(link['serverTime'])).total_seconds() <= 300,
                'issued share did not retain the global five-minute TTL')
        credential = {'publicId': link['publicId'], 'signature': link['signature']}
        saved = persisted_share(check)
        keys = list(check.root.rglob('video-share.key'))
        require(len(keys) == 1, 'persistent share signing key was not found uniquely')
        saved_key = keys[0].read_bytes()

        ordinary = check.success(f'/api/gb28181/play/{DEVICE}/{CHANNEL}', 'POST', token=admin)
        ordinary_url = ordinary.get('httpFlvUrl') or ordinary.get('urls', {}).get('httpFlv')
        parsed = urllib.parse.urlsplit(ordinary_url or '')
        tokens = urllib.parse.parse_qs(parsed.query).get('play_token', [])
        require(parsed.scheme == 'http' and parsed.hostname == '127.0.0.1'
                and parsed.port == check.zlm_port and len(tokens) == 1,
                'ordinary playback returned no isolated authorized URL')
        private.extend(tokens)
        with check.opener.open(ordinary_url, timeout=10) as response:
            require(response.status == 200 and response.read(3) == b'FLV',
                    'ordinary authenticated playback did not return real FLV')
        media_denied(check, urllib.parse.urlunsplit(parsed._replace(query='')),
                     'unsigned direct media with real hooks')

        viewer = check.success('/api/public/video-share/viewers', 'POST', credential)
        private.append(viewer['viewerToken'])
        check.read_hls(viewer)  # Wait only for initial live HLS startup, with real media validation.
        require((instant(link['expiresAt']) - dt.datetime.now(dt.timezone.utc)).total_seconds()
                > HOLD_SECONDS + 75, 'insufficient original share lifetime for honest hold and restart')
        started = time.monotonic()
        rounds, byte_count, resource_ids, digests = 0, 0, set(), set()
        last_progress = started
        while True:
            require(all(proc.poll() is None for proc in check.processes), 'process exited during long HLS observation')
            media, old_segment = read_current_hls(check, viewer)
            rounds += 1
            byte_count += media['bytes']
            resource_ids.add(old_segment)
            digests.add(media['sha256'])
            elapsed = time.monotonic() - started
            if elapsed >= HOLD_SECONDS:
                break
            if time.monotonic() - last_progress >= 30:
                print(f'PROGRESS anonymous HLS {elapsed:.1f}s, {rounds} validated current segments', flush=True)
                last_progress = time.monotonic()
            time.sleep(min(2, HOLD_SECONDS - elapsed))
        require(len(resource_ids) > 1 and len(digests) > 1, 'long HLS observation replayed unchanged cached media')
        require(persisted_share(check) == saved, 'continuous playback changed the issued share lifecycle')
        assert_auth_policy(check.success(CONFIG, token=admin))
        check_hooks(check)
        media_denied(check, ordinary_url, 'original ordinary token after its configured lifetime')
        report['longHLS'] = {'elapsedSeconds': round(elapsed, 3), 'validatedSegments': rounds,
                             'bytes': byte_count, 'distinctResources': len(resource_ids),
                             'distinctPayloads': len(digests)}
        check.record(f'same anonymous viewer fetched real current TS segments for {elapsed:.3f}s beyond ordinary TTL')

        # Only the candidate app is stopped. Keep its DB, key, ZLM and synthetic
        # publisher intact; restart the identical candidate binary at the same port.
        os.killpg(process.pid, signal.SIGTERM)
        process.wait(timeout=20)
        require(process.returncode == 0, 'candidate did not exit cleanly after SIGTERM')
        check.processes.remove(process)
        require(binary_digest(args.server) == candidate_digest, 'candidate binary changed before same-version restart')
        process, admin = start_application(check, 'candidate-after-restart')
        private.append(admin)
        require(keys[0].read_bytes() == saved_key, 'restart replaced the persisted share signing key')
        require(persisted_share(check) == saved, 'restart mutated the original share identity, revision or expiry')
        protected_config(check, admin)
        check_hooks(check)
        status, _ = check.request('/api/public/video-share/viewers/status', 'POST',
                                  {'viewerToken': viewer['viewerToken']})
        require(status == 410, 'old anonymous viewer remained redeemable after app restart')
        check.expect_gone(viewer['hlsPath'], 'old viewer HLS manifest survived app restart')
        check.expect_gone(old_segment, 'old viewer HLS segment survived app restart')
        metadata = check.success('/api/public/video-share/resolve', 'POST', credential)
        require(metadata['expiresAt'] == link['expiresAt'], 'restarting extended the public share expiry')
        replacement = check.success('/api/public/video-share/viewers', 'POST', credential)
        private.append(replacement['viewerToken'])
        require(replacement['viewerToken'] != viewer['viewerToken'], 'new process reused an old viewer capability')
        require(replacement['expiresAt'] == link['expiresAt'], 're-exchanging a share extended the original expiry')
        check.read_hls(replacement)
        require(persisted_share(check) == saved, 'post-restart playback renewed the persisted share')
        report['restart'] = {'signal': 'SIGTERM', 'exitCode': 0, 'candidateSHA256': candidate_digest,
                             'sameDatabase': True, 'sameSigningKey': True, 'unchangedExpiresAt': True,
                             'oldViewerRejected': True, 'originalShareReexchangedAndPlayed': True,
                             'anonymousConfigurationReadWriteDenied': True}
        check.record('clean same-candidate SIGTERM restart rejected old viewer and played original unextended share')
        report['status'] = 'PASS'
        return 0
    except Exception as error:
        report['error'] = private_error(error, private)
        print('FAIL ' + report['error'], file=sys.stderr)
        return 1
    finally:
        if check is not None:
            report['checks'] = check.checks
            output = check.root / 'restart-results.json'
            output.write_text(json.dumps(report, ensure_ascii=False, indent=2))
            output.chmod(0o600)
            check.close()
        print(json.dumps(report, ensure_ascii=False), flush=True)


def self_test():
    good = {'playAuth': {'authEnabled': True, 'authBindClientIP': True, 'authTTLSeconds': 120},
            'videoShare': {'ttlSeconds': 300}}
    assert_auth_policy(good)
    for key, bad in (('authEnabled', False), ('authBindClientIP', False), ('authTTLSeconds', 121)):
        changed = json.loads(json.dumps(good))
        changed['playAuth'][key] = bad
        try:
            assert_auth_policy(changed)
        except RuntimeError:
            pass
        else:
            raise RuntimeError('policy prerequisite accepted ' + key)
    prefix = '/api/public/video-share/viewers/private/hls/'
    require(playlist_segments(('#EXTM3U\n#EXTINF:1,\n' + prefix + 'current\n').encode(), prefix)
            == [prefix + 'current'], 'current segment parser self-check failed')
    for entry in ('http://foreign.invalid/segment', '/other/segment', prefix + 'segment?play_token=secret'):
        try:
            playlist_segments(('#EXTM3U\n#EXTINF:1,\n' + entry + '\n').encode(), prefix)
        except RuntimeError:
            pass
        else:
            raise RuntimeError('current segment parser accepted escaped capability')
    redacted = private_error('https://private.invalid/?token=capability /api/public/video-share/viewers/viewer-secret/hls/x capability',
                             ['capability', 'viewer-secret'])
    require('private.invalid' not in redacted and 'capability' not in redacted
            and 'viewer-secret' not in redacted, 'private error redaction self-check failed')
    require(HOLD_SECONDS > 120, 'real hold must exceed ordinary token lifetime')
    print('PASS local parser, policy prerequisites and redaction self-checks; no application/media acceptance executed')
    return 0


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ('root', 'server', 'setup', 'baseline', 'zlm', 'ffmpeg', 'sample'):
        parser.add_argument('--' + name)
    parser.add_argument('--self-test', action='store_true', help='run local helper checks only, without process/media claims')
    args = parser.parse_args()
    if args.self_test:
        return self_test()
    for name in ('root', 'server', 'setup', 'baseline', 'zlm', 'ffmpeg', 'sample'):
        if not getattr(args, name):
            parser.error('--' + name + ' is required')
        value = Path(getattr(args, name)).absolute()
        if name != 'root':
            value = value.resolve(strict=True)
            require(value.is_file(), '--' + name + ' must name a file')
        setattr(args, name, str(value))
    return execute(args)


if __name__ == '__main__':
    sys.exit(main())
