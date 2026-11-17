#!/usr/bin/env python3
"""Opt-in full UVP process check against dedicated synthetic media only.

Build server and uvp-setup from the same candidate, then supply absolute paths.
The root must be new/empty on a supported local filesystem (not /tmp tmpfs).
No existing business database, hand-filled migration ledger or SIP peer is used.
Permissions come only from the product migrations.
"""
import argparse
import datetime as dt
import json
import http.client
import os
from pathlib import Path
import secrets
import signal
import socket
import sqlite3
import subprocess
import sys
import time
import threading
import urllib.error
import urllib.parse
import urllib.request

DEVICE = "34020000002000000002"
CHANNEL = "34020000001320000001"
STREAM = DEVICE + "_" + CHANNEL
PERMISSIONS = [
    ("/api/gb28181/channel-shares/:id", "GET"),
    ("/api/gb28181/channel-shares/:id", "PUT"),
    ("/api/gb28181/channel-shares/:id/link", "POST"),
    ("/api/gb28181/channel-shares/:id/regenerate", "POST"),
]


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def ports(count):
    held = []
    try:
        for _ in range(count):
            sock = socket.socket()
            sock.bind(("127.0.0.1", 0))
            held.append(sock)
        return [sock.getsockname()[1] for sock in held]
    finally:
        for sock in held:
            sock.close()


class Check:
    def __init__(self, args):
        self.args = args
        root = Path(args.root).absolute()
        require(not root.is_symlink(), "root must not be a symlink")
        self.root = root.resolve()
        require(not self.root.exists() or not any(self.root.iterdir()), "root must be new or empty")
        self.root.mkdir(parents=True, exist_ok=True, mode=0o700)
        self.root.chmod(0o700)
        for name in ("config", "data", "logs", "state", "resource/public/uploads"):
            (self.root / name).mkdir(parents=True, exist_ok=True, mode=0o700)
        self.processes = []
        self.logs = []
        self.checks = []
        self.media_readers = []
        self.app_port, self.zlm_port, self.rtmp_port, self.sip_port, self.peer_port = ports(5)
        self.base = f"http://127.0.0.1:{self.app_port}"
        self.db = self.root / "data/uvp.db"
        self.secret = secrets.token_urlsafe(32)
        self.password = "E2e!" + secrets.token_urlsafe(24)
        self.uuid = "share-e2e-" + secrets.token_hex(8)
        self.env = {k: v for k, v in os.environ.items() if not k.startswith("UVP_UPGRADE_")}
        self.opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))

    def record(self, name):
        self.checks.append(name)
        print("PASS " + name, flush=True)

    def run(self, argv, name):
        log = self.root / "logs" / (name + ".log")
        with log.open("wb") as out:
            result = subprocess.run(argv, cwd=self.root, env=self.env, stdout=out,
                                    stderr=subprocess.STDOUT, timeout=90)
        require(result.returncode == 0, f"{name} failed; inspect private log {log}")

    def start(self, argv, name):
        out = (self.root / "logs" / (name + ".log")).open("wb")
        self.logs.append(out)
        proc = subprocess.Popen(argv, cwd=self.root, env=self.env, stdout=out,
                                stderr=subprocess.STDOUT, start_new_session=True)
        self.processes.append(proc)
        return proc

    def wait(self, action, description, timeout=30):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            require(all(p.poll() is None for p in self.processes),
                    f"process exited while waiting for {description}; inspect private logs")
            try:
                if action():
                    return
            except (OSError, ValueError, urllib.error.URLError):
                pass
            time.sleep(0.2)
        raise RuntimeError(f"timed out waiting for {description}; inspect private logs")

    def request(self, path, method="GET", body=None, token=None):
        headers = {"Content-Type": "application/json"}
        if token:
            headers["Authorization"] = "Bearer " + token
        data = None if body is None else json.dumps(body).encode()
        req = urllib.request.Request(self.base + path, data=data, headers=headers, method=method)
        try:
            response = self.opener.open(req, timeout=20)
        except urllib.error.HTTPError as error:
            response = error
        with response:
            raw = response.read()
            try:
                result = json.loads(raw) if raw else None
            except (ValueError, UnicodeDecodeError):
                raise RuntimeError(f"{method} {path.split('/viewers')[0]} returned non-JSON HTTP {response.status}") from None
            return response.status, result

    def success(self, path, method="GET", body=None, token=None):
        status, result = self.request(path, method, body, token)
        require(status == 200 and isinstance(result, dict) and result.get("code") == 0,
                f"{method} {path.split('/viewers/')[0]} failed: HTTP {status}, "
                f"code={result.get('code') if isinstance(result, dict) else 'non-json'}")
        return result["data"]

    def observe_flv(self, response):
        state = {"last_byte": time.monotonic(), "bytes": 0,
                 "ended": threading.Event(), "stop": threading.Event(), "lock": threading.Lock()}
        def consume():
            try:
                while not state["stop"].is_set():
                    chunk = response.read1(32 * 1024)
                    if not chunk:
                        break
                    with state["lock"]:
                        state["last_byte"] = time.monotonic()
                        state["bytes"] += len(chunk)
            except http.client.IncompleteRead as error:
                if error.partial:
                    with state["lock"]:
                        state["last_byte"] = time.monotonic()
                        state["bytes"] += len(error.partial)
            except (OSError, ValueError):
                pass
            finally:
                response.close()
                state["ended"].set()
        reader = threading.Thread(target=consume, daemon=True)
        self.media_readers.append((response, reader, state))
        reader.start()
        return state

    def flv_snapshot(self, state):
        with state["lock"]:
            return state["bytes"], state["last_byte"]

    def expect_gone(self, path, description):
        try:
            response = self.opener.open(self.base + path, timeout=5)
        except urllib.error.HTTPError as error:
            response = error
        with response:
            require(response.status == 410, description + f": HTTP {response.status}")

    def read_hls(self, viewer):
        path = viewer.get("hlsPath")
        prefix = "/api/public/video-share/viewers/" + viewer["viewerToken"] + "/hls/"
        require(isinstance(path, str) and path == prefix + "index.m3u8",
                "HLS manifest URL escaped its anonymous viewer")
        segments = []
        def playlist_ready():
            with self.opener.open(self.base + path, timeout=10) as response:
                require(response.status == 200, "anonymous HLS manifest did not return HTTP 200")
                body = response.read((1 << 20) + 1)
                require(len(body) <= 1 << 20, "HLS manifest exceeded one MiB")
                text = body.decode("utf-8")
                require(text.lstrip().startswith("#EXTM3U"), "response is not an HLS manifest")
                require("play_token" not in text and self.secret not in text,
                        "HLS manifest leaked upstream authorization")
                entries = [line.strip() for line in text.splitlines()
                           if line.strip() and not line.startswith("#")]
                if "#EXTINF:" not in text or not entries:
                    return False
                for entry in entries:
                    parsed = urllib.parse.urlsplit(entry)
                    require(not parsed.scheme and not parsed.netloc and not parsed.query
                            and not parsed.fragment and parsed.path.startswith(prefix),
                            "HLS segment URL escaped its anonymous viewer")
                segments[:] = entries
                return True
        self.wait(playlist_ready, "real anonymous HLS media playlist", 30)
        segment = segments[0]
        with self.opener.open(self.base + segment, timeout=10) as response:
            require(response.status == 200, "anonymous HLS segment did not return HTTP 200")
            body = response.read((8 << 20) + 1)
            require(3 * 188 <= len(body) <= 8 << 20 and len(body) % 188 == 0,
                    "HLS segment is not a bounded MPEG-TS payload")
            require(all(body[offset] == 0x47 for offset in range(0, len(body), 188)),
                    "HLS segment lacks MPEG-TS packet synchronization")
        self.record("anonymous HLS returned a real playlist and MPEG-TS segment through viewer-bound relay paths")
        return path, segment

    def configure(self):
        config = f'''server:
  appdebug: false
  cachetype: memory
  syslog: true
  notcheckuser: []
captcha:
  open: false
httpserver:
  port: "127.0.0.1:{self.app_port}"
  serverrootpath: /public
  serverroot: ./resource/public
  read_timeout: 30
  write_timeout: 30
  idle_timeout: 60
  handler_timeout: 30
  trustedproxies: []
processauthority:
  state_dir: {json.dumps(str(self.root / 'state'))}
token:
  jwttokensignkey: {secrets.token_urlsafe(48)}
  jwttokenexpire: 43200
  jwttokenrefreshexpire: 2592000
  cachekeyprefix: {self.uuid}
  iscache: false
logs:
  outputs: [stdout]
  level: info
  stdoutformat: json
casbin:
  autoloadpolicyseconds: 120
  tableprefix: sys_
  tablename: casbin_rule
  modelconfig: |
    [request_definition]
    r = sub, obj, act, dom
    [policy_definition]
    p = sub, obj, act, dom
    [role_definition]
    g = _, _, _
    [policy_effect]
    e = some(where (p.eft == allow))
    [matchers]
    m = g(r.sub, p.sub, r.dom) && keyMatch2(r.obj, p.obj) && regexMatch(r.act, p.act) && (r.dom == p.dom || p.dom == "*")
gormv2:
  usedbtype: sqlite
  sqlite:
    isinitglobalgormsqlite: 1
    loglevel: warn
    slowthreshold: 30
    write:
      database: {json.dumps(str(self.db))}
upload:
  upload_type: local
  local_path: ./resource/public/uploads
  max_size: 10
openapi:
  enabled: false
gb28181:
  enabled: true
  sip:
    transport: [udp, tcp]
  zlm:
    host: 127.0.0.1
    playbackhost: 127.0.0.1
    httpport: {self.zlm_port}
    secret: {self.secret}
    mediaserverid: {self.uuid}
  media:
    hookhost: 127.0.0.1
    hookport: {self.app_port}
    hookrequiretls: false
    streamnonereadertimeout: 60
  play:
    fixed_address_enabled: true
    auto_on_demand_enabled: false
    reconcile_interval_sec: 0
    auth:
      enabled: true
      bind_client_ip: true
      ttl_seconds: 120
  video_share:
    default_enabled: false
    ttl_seconds: 300
  subscribe:
    global_items: []
  device:
    keepalive_timeout_count: 3
    keepalive_grace_seconds: 15
    offline_scan_interval: 30
  recording:
    plan_enabled: false
    reconcile_interval_sec: 0
    catalog_reconcile_interval_sec: 0
'''
        (self.root / "config/config.yml").write_text(config)
        (self.root / "version.json").write_text(json.dumps({"name": "UVP share acceptance", "version": "development"}))

    def initialize(self):
        self.run([self.args.setup, "db-init", "--baseline", self.args.baseline,
                  "--db", str(self.db), "--quiet"], "baseline")
        self.record("production setup baseline")
        self.run([self.args.server, "-migrate-up"], "migrate")
        with sqlite3.connect(self.db) as conn:
            for path, method in PERMISSIONS:
                require(conn.execute("SELECT count(*) FROM sys_api WHERE path=? AND method=?",
                                     (path, method)).fetchone()[0] == 1, "migration missing protected share API")
                require(conn.execute("SELECT count(*) FROM sys_casbin_rule WHERE ptype='p' "
                                     "AND v0='role_1' AND v1=? AND v2=?", (path, method)).fetchone()[0] == 1,
                        "migration missing admin share policy; harness never repairs permissions")
            # Runtime fixture only: this does not exercise the SIP setup wizard.
            conn.execute("INSERT INTO gb_sip_config "
                         "(id,deployment_mode,listen_ip,advertise_ip,port,domain,server_id,password,hook_ip,sdp_ip,stream_ip) "
                         "VALUES (1,'lan','127.0.0.1','127.0.0.1',?,'3402000000','34020000002000000001',?,'127.0.0.1','127.0.0.1','127.0.0.1')",
                         (self.sip_port, secrets.token_urlsafe(24)))
            cursor = conn.execute("INSERT INTO gb_device "
                         "(device_id,name,status,owner_dept_id,transport,ip,port,keepalive_time,keepalive_interval,register_expire_at,created_at,updated_at) "
                         "VALUES (?,'Synthetic share E2E',1,1,'UDP','127.0.0.1',?,CURRENT_TIMESTAMP,3600,datetime('now','+1 day'),CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)",
                         (DEVICE, self.peer_port))
            self.device_pk = cursor.lastrowid
            cursor = conn.execute("INSERT INTO gb_channel "
                                  "(device_id,channel_id,name,status,owner_dept_id,stream_id,current_ssrc,on_demand_live,created_at,updated_at) "
                                  "VALUES (?,?,'Synthetic share E2E',1,1,?,'0200000001',0,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)",
                                  (DEVICE, CHANNEL, STREAM))
            self.channel_pk = cursor.lastrowid
        self.record("normal migrations and real share permission seeds")
        self.run([self.args.setup, "reset-admin", "--root", str(self.root), "--db", str(self.db),
                  "--password", self.password, "--no-force-change", "--no-unlock"], "reset-admin")

    def media(self):
        ini = f'''[general]
listen_ip=127.0.0.1
mediaServerId={self.uuid}
[api]
secret={self.secret}
[http]
port={self.zlm_port}
sslport=0
rootPath={self.root}/resource/public
dirMenu=0
forwarded_ip_header=
[rtmp]
port={self.rtmp_port}
sslport=0
[rtsp]
port=0
sslport=0
[rtc]
port=0
tcpPort=0
signalingPort=0
signalingSslPort=0
icePort=0
iceTcpPort=0
[srt]
port=0
[shell]
port=0
[rtp_proxy]
port=0
[hook]
enable=0
[protocol]
enable_hls=1
hls_demand=0
hls_save_path={self.root}/resource/public
enable_mp4=0
enable_rtsp=1
enable_rtmp=1
[hls]
segDur=1
segNum=6
segDelay=0
'''
        ini_path = self.root / "config/zlm.ini"
        ini_path.write_text(ini)
        self.start([self.args.zlm, "-c", str(ini_path), "-l", "3", "-t", "1",
                    "--log-dir", str(self.root / "logs/zlm")], "zlm")
        def online():
            query = urllib.parse.urlencode({"secret": self.secret, "schema": "rtsp",
                                            "vhost": "__defaultVhost__", "app": "rtp", "stream": STREAM})
            with self.opener.open(f"http://127.0.0.1:{self.zlm_port}/index/api/isMediaOnline?" + query, timeout=2) as r:
                return json.load(r).get("online") is True
        def listening():
            with socket.create_connection(("127.0.0.1", self.rtmp_port), timeout=1):
                return True
        self.wait(listening, "isolated RTMP listener")
        self.start([self.args.ffmpeg, "-hide_banner", "-loglevel", "error", "-re", "-stream_loop", "-1",
                    "-i", self.args.sample, "-c", "copy", "-f", "flv",
                    f"rtmp://127.0.0.1:{self.rtmp_port}/rtp/{STREAM}"], "synthetic-source")
        self.wait(online, "synthetic source RTSP media schema")
        self.record("isolated real ZLM synthetic live source")

    def application(self):
        self.application_process = self.start([self.args.server], "server")
        def ready():
            with socket.create_connection(("127.0.0.1", self.app_port), timeout=1):
                return True
        self.wait(ready, "full application HTTP listener", 60)
        login = self.success("/api/login", "POST", {"username": "admin", "password": self.password})
        token = login["accessToken"]
        self.management_token = token
        require(login.get("mustChangePassword") is not True, "reset-admin did not disable fixture password change")
        self.record("real admin HTTP login")
        path = f"/api/gb28181/channel-shares/{self.channel_pk}"
        unauthenticated, _ = self.request(path)
        require(unauthenticated in (401, 403), "management endpoint allowed anonymous access")
        state = self.success(path, token=token)
        require(not state["enabled"], "new channel share must default off")
        invalid_status, _ = self.request(path, "PUT",
                                         {"enabled": True, "revision": state["revision"], "ttlSeconds": 600}, token)
        require(invalid_status == 400, "channel-specific expiry override must be rejected")
        unchanged = self.success(path, token=token)
        require(not unchanged["enabled"] and unchanged["revision"] == state["revision"],
                "rejected channel expiry override changed sharing state")
        enabled = self.success(path, "PUT", {"enabled": True, "revision": state["revision"]}, token)
        require(enabled["enabled"], "share did not enable")
        for suffix in ("/link", "/regenerate"):
            invalid_status, _ = self.request(path + suffix, "POST",
                                             {"revision": enabled["revision"], "ttlMinutes": 10}, token)
            require(invalid_status == 400, "link operations must reject channel-specific expiry override")
            unchanged = self.success(path, token=token)
            require(unchanged["enabled"] and unchanged["revision"] == enabled["revision"],
                    "rejected link operation changed sharing state")
        self.record("all three management writes rejected channel expiry overrides without lifecycle changes")
        link = self.success(path + "/link", "POST", {"revision": enabled["revision"]}, token)
        credential = {"publicId": link["publicId"], "signature": link["signature"]}
        expiry = dt.datetime.fromisoformat(link["expiresAt"].replace("Z", "+00:00"))
        server_time = dt.datetime.fromisoformat(link["serverTime"].replace("Z", "+00:00"))
        require(295 <= (expiry - server_time).total_seconds() <= 300, "share TTL is not global five minutes")
        self.success("/api/public/video-share/resolve", "POST", credential)
        self.record("protected enable/acquire and anonymous resolve with five-minute TTL")
        ordinary = self.success(f"/api/gb28181/play/{DEVICE}/{CHANNEL}", "POST", token=token)
        ordinary_url = ordinary.get("httpFlvUrl") or ordinary.get("urls", {}).get("httpFlv")
        require(isinstance(ordinary_url, str) and ordinary_url,
                "ordinary authenticated playback returned no FLV URL")
        parsed = urllib.parse.urlsplit(ordinary_url)
        require(parsed.scheme == "http" and parsed.hostname == "127.0.0.1"
                and parsed.port == self.zlm_port
                and urllib.parse.parse_qs(parsed.query).get("play_token"),
                "ordinary FLV URL lacks isolated media authorization")
        ordinary_response = self.opener.open(ordinary_url, timeout=20)
        require(ordinary_response.status == 200 and ordinary_response.read(3) == b"FLV",
                "ordinary authenticated playback did not return real FLV")
        ordinary_reader = self.observe_flv(ordinary_response)
        self.wait(lambda: self.flv_snapshot(ordinary_reader)[0] > 0,
                  "ordinary authenticated FLV bytes before share disable", 10)
        self.record("ordinary backend-authorized FLV connection was established before sharing was disabled")
        hls_viewer = self.success("/api/public/video-share/viewers", "POST", credential)
        hls_manifest, hls_segment = self.read_hls(hls_viewer)
        viewer = self.success("/api/public/video-share/viewers", "POST", credential)
        self.media_capabilities = [viewer["viewerToken"], hls_viewer["viewerToken"],
                                   link["signature"], self.secret]
        media_path = viewer["httpFlvPath"]
        require(media_path.startswith("/api/public/video-share/viewers/"), "media URL escaped anonymous relay")
        response = self.opener.open(self.base + media_path, timeout=20)
        require(response.status == 200 and response.read(3) == b"FLV", "anonymous relay did not return real FLV")
        self.record("full production viewer preparation, origin proof, authorization hook and FLV relay")
        anonymous_reader = self.observe_flv(response)
        self.success("/api/public/video-share/viewers/status", "POST", {"viewerToken": viewer["viewerToken"]})
        current = self.success(path, token=token)
        require(not anonymous_reader["ended"].is_set(), "FLV connection ended before share disable")
        require(not ordinary_reader["ended"].is_set(), "ordinary FLV ended before share disable")
        ordinary_before, _ = self.flv_snapshot(ordinary_reader)
        self.success(path, "PUT", {"enabled": False, "revision": current["revision"]}, token)
        revoked_at = time.monotonic()
        require(anonymous_reader["ended"].wait(30), "existing FLV continued beyond thirty-second revocation budget")
        _, last_byte = self.flv_snapshot(anonymous_reader)
        require(last_byte - revoked_at <= 30, "new FLV bytes arrived after revocation budget")
        self.record(f"existing FLV connection stopped within thirty seconds of disable ({max(0, last_byte - revoked_at):.3f}s)")
        for endpoint, body in (("resolve", credential), ("viewers", credential),
                               ("viewers/status", {"viewerToken": viewer["viewerToken"]})):
            status, _ = self.request("/api/public/video-share/" + endpoint, "POST", body)
            require(status == 410, f"disabled share still allowed {endpoint}: HTTP {status}")
        self.expect_gone(media_path, "disabled share still allowed old FLV session")
        self.expect_gone(hls_manifest, "disabled share still allowed old HLS manifest")
        self.expect_gone(hls_segment, "disabled share still allowed old HLS segment")
        self.record("share disable rejected the previously playable HLS manifest and segment with HTTP 410")
        require(not ordinary_reader["ended"].is_set(), "share disable interrupted ordinary FLV")
        ordinary_after, last_ordinary_byte = self.flv_snapshot(ordinary_reader)
        require(ordinary_after > ordinary_before and last_ordinary_byte > revoked_at,
                "ordinary FLV supplied no new bytes across share disable")
        self.wait(lambda: self.flv_snapshot(ordinary_reader)[0] > ordinary_after,
                  "ordinary FLV progress after anonymous media rejection", 10)
        require(not ordinary_reader["ended"].is_set(), "ordinary FLV ended after anonymous media rejection")
        self.record("the same ordinary authenticated FLV connection kept delivering bytes across share disable and anonymous rejection")
        ordinary_reader["stop"].set()
        require(ordinary_reader["ended"].wait(5), "ordinary FLV observation did not close")
        self.record("disable revokes old link, new viewers, existing viewer status and media")
        query = urllib.parse.urlencode({"secret": self.secret, "schema": "rtsp", "vhost": "__defaultVhost__", "app": "rtp", "stream": STREAM})
        with self.opener.open(f"http://127.0.0.1:{self.zlm_port}/index/api/isMediaOnline?" + query, timeout=5) as live:
            require(json.load(live).get("online") is True, "share disable stopped the independent live source")
        self.record("share disable preserved the independent live source")

    def check_deleted_source_identity(self, token, path):
        for target, target_pk in (("channel", self.channel_pk), ("device", self.device_pk)):
            current = self.success(path, token=token)
            link = self.success(path, "PUT", {"enabled": True, "revision": current["revision"]}, token)
            credential = {"publicId": link["publicId"], "signature": link["signature"]}
            self.success("/api/public/video-share/resolve", "POST", credential)
            with sqlite3.connect(self.db) as conn:
                conn.row_factory = sqlite3.Row
                channel = dict(conn.execute("SELECT * FROM gb_channel WHERE id=?", (self.channel_pk,)).fetchone())
                device = dict(conn.execute("SELECT * FROM gb_device WHERE id=?", (self.device_pk,)).fetchone())
            self.success(f"/api/gb28181/device-mgmt/{target}/{target_pk}", "DELETE", token=token)
            # Reuse the exact source identity before any post-delete resolve.
            # Explicit private fixture imports must never revive a signed link.
            with sqlite3.connect(self.db) as conn:
                retired = conn.execute("SELECT enabled,revoked_at,revision FROM gb_channel_video_share WHERE channel_pk=?",
                                       (self.channel_pk,)).fetchone()
                require(retired and retired[0] == 0 and retired[1] and retired[2] == link["revision"] + 1,
                        "source deletion did not permanently revoke its share in the same transaction")
                require(conn.execute("SELECT count(*) FROM gb_channel WHERE id=?", (self.channel_pk,)).fetchone()[0] == 0,
                        "deletion did not remove the source channel")
                restore = [("gb_channel", channel)]
                if target == "device":
                    require(conn.execute("SELECT count(*) FROM gb_device WHERE id=?", (self.device_pk,)).fetchone()[0] == 0,
                            "deletion did not remove the source device")
                    restore.insert(0, ("gb_device", device))
                for table, row in restore:
                    columns = ",".join('"' + column + '"' for column in row)
                    placeholders = ",".join("?" for _ in row)
                    conn.execute(f"INSERT INTO {table} ({columns}) VALUES ({placeholders})", tuple(row.values()))
            status, _ = self.request("/api/public/video-share/resolve", "POST", credential)
            require(status == 410, "reusing the deleted source identity revived its old share")
            self.record(f"real {target} deletion permanently revoked its share before identical private-fixture identity restoration")

    def rollback(self):
        # Sharing is revoked and media observers are closed by application().
        # Keep the same migrated database, key, ZLM and synthetic source intact.
        keys = list(self.root.rglob("video-share.key"))
        require(len(keys) == 1, "persistent signing key was not found uniquely")
        key = keys[0]
        saved_key = key.read_bytes()
        os.killpg(self.application_process.pid, signal.SIGTERM)
        self.application_process.wait(timeout=15)
        self.processes.remove(self.application_process)
        self.start([self.args.rollback_server], "rollback-server")
        def ready():
            with socket.create_connection(("127.0.0.1", self.app_port), timeout=1):
                return True
        self.wait(ready, "prior-version application listener", 60)
        login = self.success("/api/login", "POST", {"username": "admin", "password": self.password})
        result = self.success(f"/api/gb28181/play/{DEVICE}/{CHANNEL}", "POST", token=login["accessToken"])
        media = result.get("httpFlvUrl") or result.get("urls", {}).get("httpFlv")
        require(isinstance(media, str) and media, "prior version returned no ordinary FLV URL")
        with self.opener.open(media, timeout=10) as response:
            require(response.status == 200 and response.read(3) == b"FLV", "ordinary playback failed after rollback")
        with sqlite3.connect(self.db) as conn:
            require(conn.execute("SELECT count(*) FROM gb_channel_video_share WHERE enabled<>0").fetchone()[0] == 0,
                    "rollback reopened a revoked share")
            require(conn.execute("SELECT count(*) FROM gb_channel_video_share").fetchone()[0] == 1,
                    "rollback destroyed share records")
        require(key.read_bytes() == saved_key, "rollback replaced the persistent share key")
        self.record("prior version restarted on migrated DB and ordinary authenticated FLV still plays")
        self.record("rollback retained revoked share records and signing key without destructive Down")

    def close(self):
        for _, _, state in self.media_readers:
            state["stop"].set()
        for proc in reversed(self.processes):
            if proc.poll() is None:
                os.killpg(proc.pid, signal.SIGTERM)
                try:
                    proc.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    os.killpg(proc.pid, signal.SIGKILL)
                    proc.wait(timeout=5)
        for response, reader, _ in self.media_readers:
            reader.join(timeout=5)
            response.close()
        for log in self.logs:
            log.close()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("root", "server", "setup", "baseline", "zlm", "ffmpeg", "sample"):
        parser.add_argument("--" + name, required=True)
    parser.add_argument("--rollback-server", help="optional prior-version binary for isolated rollback check")
    args = parser.parse_args()
    for name in ("server", "setup", "baseline", "zlm", "ffmpeg", "sample"):
        value = Path(getattr(args, name)).resolve(strict=True)
        setattr(args, name, str(value))
    if args.rollback_server:
        args.rollback_server = str(Path(args.rollback_server).resolve(strict=True))
    check = None
    try:
        check = Check(args)
        check.configure()
        check.initialize()
        check.media()
        check.application()
        check.check_deleted_source_identity(check.management_token,
                                            f"/api/gb28181/channel-shares/{check.channel_pk}")
        if args.rollback_server:
            check.rollback()
        print(json.dumps({"status": "PASS", "checks": check.checks, "root": str(check.root)}))
        return 0
    except Exception as error:
        # Tokens, passwords and complete request bodies are intentionally not printed.
        print("FAIL " + str(error), file=sys.stderr)
        return 1
    finally:
        if check is not None:
            check.close()


if __name__ == "__main__":
    sys.exit(main())
