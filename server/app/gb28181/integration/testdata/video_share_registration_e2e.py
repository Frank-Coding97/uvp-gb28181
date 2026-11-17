#!/usr/bin/env python3
"""Opt-in full application REGISTER/Digest + Catalog sharing-default acceptance.

Run beside video_share_full_app_e2e.py with --root (new/empty), --server,
--setup and --baseline. Only private isolated SQLite runtime fixtures for an
already-registered legacy device and a never-registered precreated device are
seeded. Policies, channels and shares must come from real SIP/HTTP product paths.
No media source, INVITE response media, ZLM process or migration-ledger repair.
"""
import argparse
import collections
import datetime as dt
import hashlib
import ipaddress
import json
import os
from pathlib import Path
import queue
import re
import secrets
import signal
import socket
import sqlite3
import sys
import threading
import time
from video_share_full_app_e2e import Check, PERMISSIONS, require

DOMAIN, SERVER = "3402000000", "34020000002000000001"
OLD, PRECREATED = "34020000002000000091", "34020000002000000092"
PREFIX = "/api/gb28181/channel-shares/"
CONFIG = "/api/gb28181/sip/service-config"


def local_advertise_ip(requested):
    if requested is None:
        # UDP connect selects a local route without sending any datagram. The
        # documentation-only destination is never used as a device or SIP peer.
        with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as probe:
            probe.connect(("192.0.2.1", 9))
            requested = probe.getsockname()[0]
    address = ipaddress.ip_address(requested)
    require(address.version == 4 and not address.is_loopback and not address.is_unspecified
            and not address.is_multicast, "advertise-ip must be a concrete local non-loopback IPv4 address")
    # Refuse a guessed remote address: this process must be able to bind it on
    # its own host. All test peers and application listeners remain loopback.
    with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as probe:
        probe.bind((str(address), 0))
    return str(address)


def parse_packet(raw):
    header, _, body = raw.decode("utf-8", "replace").partition("\r\n\r\n")
    lines = header.split("\r\n")
    headers = {}
    for line in lines[1:]:
        name, _, value = line.partition(":")
        headers[name.lower()] = value.strip()
    return lines[0], headers, body


class Peer:
    """A real loopback UDP SIP endpoint, limited to registration/catalog evidence."""
    def __init__(self, check, device):
        self.check, self.device = check, device
        self.socket = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        self.socket.bind(("127.0.0.1", 0))
        self.socket.settimeout(0.2)
        self.port, self.sequence, self.catalog_sequence = self.socket.getsockname()[1], 0, 0
        self.lock, self.stopped = threading.Lock(), threading.Event()
        self.responses, self.methods, self.catalog_sn = {}, collections.Counter(), None
        self.subscription = None
        self.thread = threading.Thread(target=self.receive, daemon=True)
        self.thread.start()

    def receive(self):
        while not self.stopped.is_set():
            try:
                raw, sender = self.socket.recvfrom(65535)
            except socket.timeout:
                continue
            except OSError:
                return
            first, headers, body = parse_packet(raw)
            if first.startswith("SIP/2.0 "):
                key = (headers.get("call-id"), headers.get("cseq"))
                with self.lock:
                    destination = self.responses.get(key)
                if destination:
                    destination.put((int(first.split()[1]), headers))
                continue
            method = first.split()[0]
            with self.lock:
                self.methods[method] += 1
                if method == "SUBSCRIBE" and headers.get("event", "").lower().startswith("catalog"):
                    self.subscription = (headers["call-id"], headers["event"])
                if "<CmdType>Catalog</CmdType>" in body:
                    sn = re.search(r"<SN>\s*(\d+)\s*</SN>", body)
                    if sn:
                        self.catalog_sn = int(sn[1])
            # A pull is a failure, never an emulated successful media session.
            code, reason = (488, "No test media") if method == "INVITE" else (200, "OK")
            response = [f"SIP/2.0 {code} {reason}"]
            for name in ("Via", "From", "To", "Call-ID", "CSeq"):
                value = headers.get(name.lower(), "")
                if name == "To" and ";tag=" not in value:
                    value += ";tag=registration-fixture"
                response.append(name + ": " + value)
            if method == "SUBSCRIBE":
                response += ["Expires: " + headers.get("expires", "3600"),
                             f"Contact: <sip:{self.device}@127.0.0.1:{self.port}>"]
            response += ["Content-Length: 0", "", ""]
            try:
                self.socket.sendto("\r\n".join(response).encode(), sender)
            except OSError:
                return

    def request(self, method, body="", extra=(), call_id=None):
        self.sequence += 1
        call_id = call_id or secrets.token_hex(12) + "@registration-e2e"
        uri = f"sip:{SERVER}@{DOMAIN}"
        cseq = f"{self.sequence} {method}"
        headers = [f"{method} {uri} SIP/2.0",
                   f"Via: SIP/2.0/UDP 127.0.0.1:{self.port};rport;branch=z9hG4bK-{secrets.token_hex(12)}",
                   f"From: <sip:{self.device}@{DOMAIN}>;tag=registration-e2e",
                   f"To: <sip:{SERVER}@{DOMAIN}>", f"Call-ID: {call_id}", f"CSeq: {cseq}",
                   "Max-Forwards: 70", "X-GB-Ver: 2.0", f"Contact: <sip:{self.device}@127.0.0.1:{self.port}>"]
        headers += list(extra)
        if body:
            headers.append("Content-Type: Application/MANSCDP+xml")
        headers.append(f"Content-Length: {len(body.encode())}")
        packet = ("\r\n".join(headers) + "\r\n\r\n" + body).encode()
        responses, key = queue.Queue(), (call_id, cseq)
        with self.lock:
            self.responses[key] = responses
        try:
            for _ in range(8):
                self.socket.sendto(packet, ("127.0.0.1", self.check.sip_port))
                try:
                    status, received = responses.get(timeout=1)
                    if status >= 200:
                        return status, received
                except queue.Empty:
                    pass
            raise RuntimeError(f"SIP {method} timed out (private logs retained)")
        finally:
            with self.lock:
                self.responses.pop(key, None)

    def challenge(self, expires=3600):
        call_id = secrets.token_hex(12) + "@registration-e2e"
        status, headers = self.request("REGISTER", extra=[f"Expires: {expires}"], call_id=call_id)
        require(status == 401, f"REGISTER must challenge before authentication, got {status}")
        challenge = dict(re.findall(r'(\w+)="([^"]*)"', headers.get("www-authenticate", "")))
        require(challenge.get("realm") == DOMAIN and challenge.get("nonce"), "invalid native Digest challenge")
        require("qop" not in challenge, "candidate changed Digest contract; update test explicitly")
        return call_id, challenge

    def authorization(self, challenge, password=None):
        md5 = lambda value: hashlib.md5(value.encode()).hexdigest()
        uri = f"sip:{SERVER}@{DOMAIN}"
        password = self.check.sip_password if password is None else password
        response = md5(md5(f"{self.device}:{DOMAIN}:{password}") + ":" +
                       challenge["nonce"] + ":" + md5("REGISTER:" + uri))
        return (f'Digest username="{self.device}", realm="{DOMAIN}", nonce="{challenge["nonce"]}", '
                f'uri="{uri}", response="{response}", algorithm=MD5')

    def register(self, expires=3600):
        call_id, challenge = self.challenge(expires)
        auth = self.authorization(challenge)
        status, _ = self.request("REGISTER", extra=[f"Expires: {expires}", "Authorization: " + auth], call_id=call_id)
        require(status == 200, f"authenticated REGISTER failed: SIP {status}; inspect private logs")
        if expires:
            self.check.wait(lambda: self.check.one("SELECT status FROM gb_device WHERE device_id=?", self.device) == 1,
                            "registered device online")
        return call_id, auth

    def notify(self, channels, event):
        self.check.wait(lambda: self.subscription is not None, "native Catalog SUBSCRIBE")
        with self.lock:
            call_id, subscription_event = self.subscription
        self.catalog_sequence += 1
        label = f"Notify E2E channel {self.catalog_sequence}"
        items = "".join(f"<Item><DeviceID>{channel}</DeviceID><Name>{label}</Name><Event>{event}</Event>"
                        f"<ParentID>{self.device}</ParentID><Parental>0</Parental>"
                        "<CivilCode>340200</CivilCode><Status>ON</Status></Item>" for channel in channels)
        body = (f'<Notify><CmdType>Catalog</CmdType><SN>{1000 + self.catalog_sequence}</SN>'
                f'<DeviceID>{self.device}</DeviceID><DeviceList Num="{len(channels)}">{items}</DeviceList></Notify>')
        status, _ = self.request("NOTIFY", body, ["Event: " + subscription_event,
                                 "Subscription-State: active;expires=3600"], call_id)
        require(status == 200, f"Catalog NOTIFY failed: SIP {status}")
        self.check.wait(lambda: self.check.one("SELECT count(*) FROM gb_channel WHERE device_id=? AND name=?",
                                             self.device, label) == len(channels), "NOTIFY ingestion completed")

    def catalog(self, channels):
        self.catalog_sequence += 1
        label = f"Registration E2E channel {self.catalog_sequence}"
        # The native handler also accepts later completed refreshes using this SN.
        with self.lock:
            sn = self.catalog_sn or 1
        items = "".join(f"<Item><DeviceID>{channel}</DeviceID><Name>{label}</Name>"
                        f"<ParentID>{self.device}</ParentID><Parental>0</Parental>"
                        "<CivilCode>340200</CivilCode><Status>ON</Status></Item>" for channel in channels)
        body = (f'<?xml version="1.0" encoding="UTF-8"?><Response><CmdType>Catalog</CmdType><SN>{sn}</SN>'
                f'<DeviceID>{self.device}</DeviceID><SumNum>{len(channels)}</SumNum>'
                f'<DeviceList Num="{len(channels)}">{items}</DeviceList></Response>')
        status, _ = self.request("MESSAGE", body)
        require(status == 200, f"Catalog MESSAGE failed: SIP {status}")
        self.check.wait(lambda: self.check.one("SELECT count(*) FROM gb_channel WHERE device_id=? AND deleted_at IS NULL",
                                             self.device) == len(channels), "real catalog channels persisted")
        # The SIP 200 intentionally precedes asynchronous ingestion. Check a name
        # update written by every ingest, rather than treating the ACK as completion.
        self.check.wait(lambda: self.check.one("SELECT count(*) FROM gb_channel WHERE device_id=? AND name=?",
                                             self.device, label) == len(channels), "catalog ingestion completed")

    def close(self):
        self.stopped.set()
        self.socket.close()
        self.thread.join(timeout=2)


class RegistrationCheck(Check):
    def __init__(self, args):
        super().__init__(args)
        self.peers = []
        self.sip_password = secrets.token_urlsafe(24)
        self.advertise_ip = local_advertise_ip(args.advertise_ip)

    def configure(self):
        super().configure()
        path = self.root / "config/config.yml"
        config = path.read_text()
        require("  device:\n" in config, "base harness GB device configuration changed")
        path.write_text(config.replace("  device:\n", "  device:\n    default_owner_dept_id: 1\n", 1))

    def one(self, sql, *parameters):
        with sqlite3.connect(self.db, timeout=5) as conn:
            row = conn.execute(sql, parameters).fetchone()
            return row[0] if row else None

    def initialize(self):
        self.run([self.args.setup, "db-init", "--baseline", self.args.baseline, "--db", str(self.db), "--quiet"], "baseline")
        self.run([self.args.server, "-migrate-up"], "migrate")
        with sqlite3.connect(self.db) as conn:
            for path, method in PERMISSIONS:
                require(conn.execute("SELECT count(*) FROM sys_casbin_rule WHERE ptype='p' AND v0='role_1' "
                                     "AND v1=? AND v2=?", (path, method)).fetchone()[0] == 1,
                        "native migration did not seed sharing permissions; harness will not repair them")
            conn.execute("INSERT INTO gb_sip_config (id,deployment_mode,listen_ip,advertise_ip,port,domain,server_id,password,hook_ip,sdp_ip,stream_ip) "
                         "VALUES (1,'lan','127.0.0.1',?,?,?,?,?,'127.0.0.1','127.0.0.1','127.0.0.1')",
                         (self.advertise_ip, self.sip_port, DOMAIN, SERVER, self.sip_password))
            for device, registered in ((OLD, True), (PRECREATED, False)):
                conn.execute("INSERT INTO gb_device (device_id,name,status,owner_dept_id,transport,register_time,created_at,updated_at) "
                             "VALUES (?,'Explicit registration-history fixture',0,1,'UDP',?,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)",
                             (device, dt.datetime.now(dt.timezone.utc).isoformat() if registered else None))
            require(conn.execute("SELECT count(*) FROM gb_device_video_share_policy").fetchone()[0] == 0, "unexpected seed policy")
            require(conn.execute("SELECT count(*) FROM gb_channel_video_share").fetchone()[0] == 0, "unexpected seed share")
        self.run([self.args.setup, "reset-admin", "--root", str(self.root), "--db", str(self.db), "--password",
                  self.password, "--no-force-change", "--no-unlock"], "reset-admin")
        self.record("normal setup/migrations, explicit old/precreated history fixtures; no policies/channels/shares seeded")

    def peer(self, device):
        peer = Peer(self, device)
        self.peers.append(peer)
        return peer

    def policy(self, peer):
        with sqlite3.connect(self.db) as conn:
            return conn.execute("SELECT p.default_enabled,p.initialized_at,p.policy_revision FROM gb_device_video_share_policy p "
                                "JOIN gb_device d ON d.id=p.device_pk WHERE d.device_id=?", (peer.device,)).fetchone()

    def global_default(self, enabled, ttl=300):
        config = self.success(CONFIG, token=self.token)
        config["videoShare"] = {"defaultEnabled": enabled, "ttlSeconds": ttl}
        config["syncChannelsOnOnline"] = {"enabled": True}
        config["fixedAddressPlayback"]["autoOnDemandEnabled"] = False
        config["playbackSettings"]["cloudRecordingEnabled"] = False
        self.success(CONFIG, "PUT", config, self.token)
        actual = self.success(CONFIG, token=self.token)["videoShare"]
        require(actual == {"defaultEnabled": enabled, "ttlSeconds": ttl}, "service config did not publish requested defaults")

    def channel(self, code):
        return self.one("SELECT id FROM gb_channel WHERE channel_id=? AND deleted_at IS NULL", code)

    def state(self, code):
        return self.success(PREFIX + str(self.channel(code)), token=self.token)

    def assert_pending(self, code):
        self.wait(lambda: self.state(code).get("pending"), "pending auto-share")
        state = self.state(code)
        require(state["enabled"] and state["pending"] and not state.get("expiresAt"), "auto share must wait without expiry")
        require(self.one("SELECT count(*) FROM gb_channel_video_share WHERE channel_pk=? AND activated_at IS NULL "
                         "AND expires_at IS NULL AND created_by IS NULL AND source='auto'", self.channel(code)) == 1,
                "pending share already activated or attributed to a fabricated creator")
        return state

    def launch_application(self, name):
        self.application_process = self.start([self.args.server], name)
        def listening():
            with socket.create_connection(("127.0.0.1", self.app_port), timeout=1):
                return True
        self.wait(listening, "full application HTTP", 60)
        self.token = self.success("/api/login", "POST", {"username": "admin", "password": self.password})["accessToken"]

    def restart_application(self):
        os.killpg(self.application_process.pid, signal.SIGTERM)
        self.application_process.wait(timeout=15)
        self.processes.remove(self.application_process)
        self.launch_application("server-restarted")

    def rejected_registration(self):
        peer = self.peer("34020000002000000014")
        call_id, challenge = peer.challenge()
        require(self.policy(peer) is None, "unauthenticated challenge created default share policy")
        status, _ = peer.request("REGISTER", extra=["Expires: 3600", "Authorization: " +
                                 peer.authorization(challenge, "deliberately-invalid-password")], call_id=call_id)
        require(status == 401 and self.policy(peer) is None, "failed Digest initialized sharing policy")
        security_path = "/api/gb28181/security/policy"
        original = self.success(security_path, token=self.token)
        short = dict(original, nonceTtl=1)
        try:
            self.success(security_path, "PUT", short, self.token)
            call_id, challenge = peer.challenge()
            # Exercise an actually expired server-issued nonce, without forging it
            # or seeding policy rows. Only this isolated runtime's TTL is shortened.
            time.sleep(2)
            status, _ = peer.request("REGISTER", extra=["Expires: 3600", "Authorization: " +
                                     peer.authorization(challenge)], call_id=call_id)
            require(status == 401 and self.policy(peer) is None, "expired nonce initialized sharing policy")
        finally:
            self.success(security_path, "PUT", original, self.token)
        call_id, challenge = peer.challenge(expires=0)
        status, _ = peer.request("REGISTER", extra=["Expires: 0", "Authorization: " +
                                 peer.authorization(challenge)], call_id=call_id)
        require(status == 500, f"unregister of a nonexistent device must preserve native SIP 500, got {status}")
        require(self.policy(peer) is None, "authenticated unregister created first-register share policy")
        require(self.one("SELECT count(*) FROM gb_device WHERE device_id=?", peer.device) == 0,
                "rejected registration or nonexistent unregister created a device")
        require(self.one("SELECT count(*) FROM gb_channel WHERE device_id=?", peer.device) == 0,
                "rejected registration or unregister fabricated channels")
        self.record("challenge, bad Digest, actually expired nonce and authenticated unregister create no device sharing policy")

    def application(self):
        self.launch_application("server")
        initial = self.success(CONFIG, token=self.token)["videoShare"]
        require(initial == {"defaultEnabled": False, "ttlSeconds": 300}, "initial sharing defaults must be off/five minutes")
        self.global_default(False)
        off = self.peer("34020000002000000011")
        off.register()
        require(self.policy(off) and self.policy(off)[0] == 0, "default-off REGISTER did not snapshot disabled")
        require(self.one("SELECT count(*) FROM gb_channel WHERE device_id=?", off.device) == 0,
                "registration without Catalog unexpectedly created channels")
        off_snapshot = self.policy(off)
        self.global_default(True)
        self.rejected_registration()
        off.catalog(["34020000001320000011"])
        require(not self.state("34020000001320000011")["enabled"], "delayed Catalog used live global flag instead of off snapshot")
        self.record("real UDP 401/Digest/200 default-off registration stays off after global enable and delayed Catalog")
        enabled = self.peer("34020000002000000012")
        enabled_call_id, enabled_auth = enabled.register()
        enabled_snapshot = self.policy(enabled)
        require(enabled_snapshot and enabled_snapshot[0] == 1, "default-on REGISTER did not snapshot enabled")
        require(self.one("SELECT count(*) FROM gb_channel WHERE device_id=?", enabled.device) == 0, "unexpected pre-Catalog channels")
        self.global_default(False)
        # A different CSeq/branch is a new transaction, not a same-transaction
        # retransmission; an already consumed qopless nonce must be rejected.
        status, _ = enabled.request("REGISTER", extra=["Expires: 3600", "Authorization: " + enabled_auth],
                                    call_id=enabled_call_id)
        require(status == 401 and self.policy(enabled) == enabled_snapshot,
                "replayed Digest updated the device sharing snapshot")
        enabled.register(expires=0)
        require(self.policy(enabled) == enabled_snapshot, "unregister modified existing device sharing snapshot")
        enabled.register()
        self.restart_application()
        require(self.success(CONFIG, token=self.token)["videoShare"] == {"defaultEnabled": False, "ttlSeconds": 300},
                "sharing configuration did not survive application restart")
        channels = ["34020000001320000021", "34020000001320000022"]
        enabled.catalog(channels)
        for code in channels:
            self.assert_pending(code)
        require(self.policy(enabled) == enabled_snapshot, "delayed Catalog rewrote first-register snapshot")
        self.record("used-nonce replay and unregister preserve existing snapshot; global-off and real restart before delayed Catalog preserve first-register policy")
        self.global_default(True)
        old, precreated = self.peer(OLD), self.peer(PRECREATED)
        old.register()
        old.catalog(["34020000001320000091"])
        require(self.policy(old) is None, "legacy already-registered device was reclassified as new")
        require(not self.state("34020000001320000091")["enabled"], "legacy device new channel auto-enabled")
        precreated.register()
        pre_snapshot = self.policy(precreated)
        require(pre_snapshot and pre_snapshot[0] == 1, "precreated never-registered device missed first-register snapshot")
        precreated.catalog(["34020000001320000092"])
        self.assert_pending("34020000001320000092")
        self.record("legacy registered row remains off; precreated never-registered row initializes at first real Digest registration")
        self.global_default(False)
        enabled.register()
        enabled.catalog(channels)
        for code in channels:
            self.assert_pending(code)
        pending = self.state(channels[0])
        path = PREFIX + str(self.channel(channels[0]))
        issued = self.success(path + "/link", "POST", {"revision": pending["revision"]}, self.token)
        expiry = dt.datetime.fromisoformat(issued["expiresAt"].replace("Z", "+00:00"))
        issued_at = dt.datetime.fromisoformat(issued["serverTime"].replace("Z", "+00:00"))
        require(295 <= (expiry - issued_at).total_seconds() <= 300, "pending activation did not use global five-minute TTL")
        require(self.one("SELECT count(*) FROM gb_channel_video_share WHERE channel_pk=? AND activated_at IS NOT NULL "
                         "AND created_by IS NOT NULL AND expires_at IS NOT NULL", self.channel(channels[0])) == 1,
                "link acquisition did not atomically activate and attribute share")
        closed_path = PREFIX + str(self.channel(channels[1]))
        closed = self.state(channels[1])
        closed = self.success(closed_path, "PUT", {"enabled": False, "revision": closed["revision"]}, self.token)
        self.global_default(True, 180)
        enabled.register()
        channels.append("34020000001320000023")
        enabled.catalog(channels)
        after = self.state(channels[0])
        require(after["expiresAt"] == issued["expiresAt"] and after["generation"] == issued["generation"]
                and after["revision"] == issued["revision"], "re-register/Catalog/global TTL change renewed the issued link")
        again = self.success(path + "/link", "POST", {"revision": after["revision"]}, self.token)
        require(again["signature"] == issued["signature"] and again["expiresAt"] == issued["expiresAt"], "repeat link acquisition renewed credentials")
        require(not self.state(channels[1])["enabled"] and self.state(channels[1])["revision"] == closed["revision"],
                "re-registration or Catalog reopened a manually closed share")
        self.assert_pending(channels[2])
        device_pk = self.one("SELECT id FROM gb_device WHERE device_id=?", enabled.device)
        self.success(f"/api/gb28181/device-mgmt/device/{device_pk}/subscriptions/catalog", "PATCH",
                     {"enabled": True, "expiresSeconds": 3600}, self.token)
        # Establish the dialog through the actual protected API and real UDP
        # SUBSCRIBE/200; an unsolicited NOTIFY ACK alone is not ingestion proof.
        for event in ("ADD", "UPDATE", "ADD"):
            enabled.notify(channels[:2], event)
            after_notify = self.state(channels[0])
            require(all(after_notify[key] == issued[key] for key in ("expiresAt", "generation", "revision")),
                    "Catalog NOTIFY renewed an issued share")
            closed_notify = self.state(channels[1])
            require(not closed_notify["enabled"] and closed_notify["revision"] == closed["revision"],
                    "Catalog NOTIFY reopened a manually closed share")
        notified = "34020000001320000024"
        enabled.notify([notified], "ADD")
        self.assert_pending(notified)
        self.record("real SUBSCRIBE/200 and repeated ADD/UPDATE NOTIFY neither reopen closed shares nor renew active links; NOTIFY-discovered channel inherits snapshot")
        require(self.policy(enabled) == enabled_snapshot, "renewed REGISTER rewrote enabled policy")
        off.register()
        off.catalog(["34020000001320000011", "34020000001320000013"])
        require(self.policy(off) == off_snapshot and not self.state("34020000001320000013")["enabled"],
                "default-off existing device new channel inherited a later global flag")
        old.register()
        old.catalog(["34020000001320000091", "34020000001320000093"])
        require(self.policy(old) is None and not self.state("34020000001320000093")["enabled"], "legacy renewal auto-enabled sharing")
        self.global_default(False, 180)
        precreated.register()
        precreated.catalog(["34020000001320000092", "34020000001320000094"])
        require(self.policy(precreated) == pre_snapshot, "precreated first-register policy was not immutable")
        self.assert_pending("34020000001320000094")
        self.record("renewed REGISTER and refreshed Catalog neither reopen closed shares nor renew issued links; later channels inherit immutable device policy")
        # Default-off allows explicit protected management, without preparing media.
        off_code = "34020000001320000011"
        off_state = self.state(off_code)
        manual = self.success(PREFIX + str(self.channel(off_code)), "PUT",
                              {"enabled": True, "revision": off_state["revision"]}, self.token)
        require(manual["enabled"], "global default-off incorrectly prohibited manual sharing")
        self.record("global default-off still permits authorized manual channel sharing")
        time.sleep(1)
        for peer in self.peers:
            with peer.lock:
                require(peer.methods["INVITE"] == 0, "automatic share/pending/acquisition unexpectedly initiated media INVITE")
        require(self.one("SELECT count(*) FROM gb_play_attempt") == 0, "default sharing unexpectedly created a live playback attempt")
        self.record("zero observed SIP INVITE and zero real playback attempts; no media source or playback simulator used")

    def close(self):
        try:
            super().close()
        finally:
            for peer in self.peers:
                peer.close()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("root", "server", "setup", "baseline"):
        parser.add_argument("--" + name, required=True)
    parser.add_argument("--advertise-ip", help="local non-loopback IPv4; default uses a no-packet UDP route probe")
    args = parser.parse_args()
    for name in ("server", "setup", "baseline"):
        setattr(args, name, str(Path(getattr(args, name)).resolve(strict=True)))
    check = None
    try:
        check = RegistrationCheck(args)
        check.configure()
        check.initialize()
        check.application()
        print(json.dumps({"status": "PASS", "checks": check.checks, "root": str(check.root),
                          "scope": "full candidate and restart, real UDP REGISTER/Digest, Catalog and subscribed NOTIFY; no media"}))
        return 0
    except Exception as error:
        # No credentials, Digest values, raw SIP or bearer links are printed.
        print("FAIL " + str(error), file=sys.stderr)
        return 1
    finally:
        if check is not None:
            check.close()


if __name__ == "__main__":
    sys.exit(main())
