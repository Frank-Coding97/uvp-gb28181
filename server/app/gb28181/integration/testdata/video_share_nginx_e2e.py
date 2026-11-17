#!/usr/bin/env python3
"""Opt-in full application acceptance through the shipped frontend Nginx template.

Uses new private state, loopback listeners and the supplied production dist only.
Never reloads the installed Nginx or modifies a running deployment.
"""
import argparse
import json
from pathlib import Path
import socket
import sys
import urllib.error

from video_share_full_app_e2e import Check, ports, require


class NginxCheck(Check):
    def application(self):
        proxy_port = ports(1)[0]
        template = Path(self.args.template).read_text()
        for required in ("listen 56000;", "root /opt/uvp-gb28181/current/frontend/dist;",
                         "http://127.0.0.1:56001", "location ^~ /api/public/video-share/"):
            require(required in template, "production frontend template contract changed")
        template = template.replace("listen 56000;", f"listen 127.0.0.1:{proxy_port};")
        template = template.replace("root /opt/uvp-gb28181/current/frontend/dist;",
                                    "root " + json.dumps(self.args.dist) + ";")
        template = template.replace("http://127.0.0.1:56001", f"http://127.0.0.1:{self.app_port}")
        root = self.root
        config = f'''daemon off;
master_process off;
pid {root}/nginx.pid;
error_log {root}/logs/nginx-error.log warn;
events {{ worker_connections 128; }}
http {{
 default_type application/octet-stream;
 access_log {root}/logs/nginx-access.log combined;
 client_body_temp_path {root}/nginx-body;
 proxy_temp_path {root}/nginx-proxy;
 {template}
}}
'''
        config_path = root / "config/nginx.conf"
        config_path.write_text(config)
        self.run([self.args.nginx, "-t", "-p", str(root), "-c", str(config_path)], "nginx-config")
        self.start([self.args.nginx, "-p", str(root), "-c", str(config_path)], "nginx")

        def listening():
            with socket.create_connection(("127.0.0.1", proxy_port), timeout=1):
                return True

        self.wait(listening, "isolated production Nginx template")
        self.base = f"http://127.0.0.1:{proxy_port}"
        for name in ("share.html", "index.html"):
            with self.opener.open(self.base + "/" + name, timeout=5) as response:
                require(response.status == 200 and response.read() == (Path(self.args.dist) / name).read_bytes(),
                        f"Nginx did not serve the candidate {name}")
        self.record("production dist share and management entrypoints served by real Nginx")
        super().application()
        self.check_logs()

    def check_logs(self):
        # A query sentinel is harmless and must never enter the media access log.
        sentinel = "acceptance-media-query-must-not-be-logged"
        try:
            self.opener.open(self.base + "/api/public/video-share/viewers/invalid/live.flv?probe=" + sentinel,
                             timeout=5).close()
        except urllib.error.HTTPError:
            pass
        access = (self.root / "logs/nginx-access.log").read_text()
        require("GET /share.html " in access, "access log control request was not logged")
        require("/api/public/video-share/viewers/" not in access and sentinel not in access,
                "Nginx access log exposed a viewer media capability")
        errors = (self.root / "logs/nginx-error.log").read_text()
        require("/api/public/video-share/viewers/" not in errors and sentinel not in errors,
                "Nginx error log exposed a viewer media capability")
        for capability in self.media_capabilities:
            require(capability not in access and capability not in errors,
                    "Nginx logs exposed a sharing or upstream credential")
        self.record("production Nginx access/error logs excluded media capabilities with a logged static control request")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("root", "server", "setup", "baseline", "zlm", "ffmpeg", "sample", "nginx", "template", "dist"):
        parser.add_argument("--" + name, required=True)
    parser.add_argument("--rollback-server")
    args = parser.parse_args()
    for name, value in vars(args).items():
        if name != "root" and value is not None:
            setattr(args, name, str(Path(value).resolve(strict=True)))
    require((Path(args.dist) / "share.html").is_file(), "dist must contain production share.html")
    check = None
    try:
        check = NginxCheck(args)
        check.configure()
        check.initialize()
        check.media()
        check.application()
        if args.rollback_server:
            check.rollback()
        print(json.dumps({"status": "PASS", "checks": check.checks, "root": str(check.root)}))
        return 0
    except Exception as error:
        print("FAIL " + str(error), file=sys.stderr)
        return 1
    finally:
        if check is not None:
            check.close()


if __name__ == "__main__":
    sys.exit(main())
