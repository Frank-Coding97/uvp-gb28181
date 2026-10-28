import pathlib
import re
import subprocess
import tempfile
import unittest


ROOT = pathlib.Path(__file__).resolve().parents[2]
BUILD_SCRIPT = ROOT / "deploy/standalone/build-standalone.sh"
CONTROL_SCRIPT = ROOT / "deploy/standalone/uvp-gb28181-ctl.sh"
ZLM_PORTS = {"HTTP_PORT": "52004", "SSL_PORT": "52007", "RTSP_PORT": "52005", "RTMP_PORT": "52006",
             "RTC_PORT": "52008", "RTP_PROXY_PORT": "52014", "RTP_RANGE": "52014-52063", "SIGNALING_PORT": "52009",
             "SIGNALING_SSL_PORT": "52010", "SRT_PORT": "52011", "ONVIF_PORT": "52012", "ICE_PORT": "52013"}


class StandaloneNginxRelocationTests(unittest.TestCase):
    def test_tls_initialization_preserves_existing_zlm_certificate(self):
        source = CONTROL_SCRIPT.read_text(encoding="utf-8")
        function = re.search(r"(?ms)^ensure_tls_material\(\) \{.*?^\}", source).group()
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            zlm = root / "zlm.pem"
            zlm.write_text("old ZLM material")
            script = "\n".join([
                "set -eu", f'ZLM_SSL_PEM="{zlm}"',
                'ensure_self_signed_cert() { echo unexpected; return 1; }',
                function, 'ensure_tls_material',
            ])
            result = subprocess.run(["bash", "-c", script], capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(result.stdout, "")
            self.assertEqual(zlm.read_text(), "old ZLM material")

    def test_disabled_https_port_plan_does_not_advertise_redirect(self):
        source = CONTROL_SCRIPT.read_text(encoding="utf-8")
        function = re.search(r"(?ms)^print_port_plan\(\) \{.*?^\}", source).group()
        # ⛔ 2026-10-20：放行范围改由 port_range_bounds 按实际端口 min/max 计算
        #   （原来是"两个 nginx 口的较小值 ~ RTP 段尾"，客户把端口改高就倒挂）。
        bounds = re.search(r"(?ms)^port_range_bounds\(\) \{.*?^\}", source).group()
        script = "\n".join([
            "set -eu", 'NGINX_HTTPS_ENABLED=0', 'NGINX_HTTPS_PORT=52000', 'NGINX_HTTP_PORT=52001',
            'HTTP_PORT=52002', 'REDIS_PORT=52003', 'REDIS_BIND=127.0.0.1', 'SIP_PORT=52064',
            "C_TAG=''", "C_RST=''",
            *[f'ZLM_{key}={value}' for key, value in ZLM_PORTS.items()],
            'ok() { :; }', 'det() { echo "$*"; }', '_firewall_hint() { :; }',
            bounds, function, 'print_port_plan',
        ])
        result = subprocess.run(["bash", "-c", script], capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("HTTPS 已关闭", result.stdout)
        self.assertNotIn("HTTP 自动跳转", result.stdout)
        self.assertIn("放行范围 52000-52064", result.stdout)

    def test_port_plan_range_is_not_inverted_when_segments_differ(self):
        """客户只把 nginx/后端改到 52000 段、ZLM 留在 51000 段时，范围不能倒挂。

        ⛔ 真事：旧实现「段首 = 两个 nginx 口的较小值，段尾 = RTP 段尾 + SIP 提示」
        在混搭段下会算出 `52000-51064`，并生成 `--dport 52000:51064` —— 防火墙
        直接报错，客户照做则媒体端口一个都没放开。
        """
        source = CONTROL_SCRIPT.read_text(encoding="utf-8")
        function = re.search(r"(?ms)^print_port_plan\(\) \{.*?^\}", source).group()
        bounds = re.search(r"(?ms)^port_range_bounds\(\) \{.*?^\}", source).group()
        zlm = dict(ZLM_PORTS, HTTP_PORT="51004", SSL_PORT="51007", RTSP_PORT="51005",
                   RTMP_PORT="51006", RTC_PORT="51008", RTP_PROXY_PORT="51014",
                   RTP_RANGE="51014-51063", SIGNALING_PORT="51009",
                   SIGNALING_SSL_PORT="51010", SRT_PORT="51011", ONVIF_PORT="51012",
                   ICE_PORT="51013")
        script = "\n".join([
            "set -eu", 'NGINX_HTTPS_ENABLED=0', 'NGINX_HTTPS_PORT=52001', 'NGINX_HTTP_PORT=52000',
            'HTTP_PORT=52002', 'REDIS_PORT=52003', 'REDIS_BIND=127.0.0.1', 'SIP_PORT=51064',
            "C_TAG=''", "C_RST=''",
            *[f'ZLM_{key}={value}' for key, value in zlm.items()],
            'ok() { :; }', 'det() { echo "$*"; }', '_firewall_hint() { :; }',
            bounds, function, 'print_port_plan',
        ])
        result = subprocess.run(["bash", "-c", script], capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("放行范围 51004-52002", result.stdout)
        self.assertNotIn("52000-51064", result.stdout)

    def test_setup_helper_is_copied_once_after_build(self):
        source = BUILD_SCRIPT.read_text(encoding="utf-8")
        copy = 'cp "$SERVER_DIR/bin/uvp-setup" "$PKG/bin/uvp-setup"'
        self.assertEqual(source.count(copy), 1, "setup must not be copied before build")
        self.assertGreater(source.index(copy), source.index('build -ldflags'))

    def test_repeated_start_preserves_custom_port_markers_without_environment(self):
        source = CONTROL_SCRIPT.read_text(encoding="utf-8")
        # ⛔ 2026-10-20：read_port_env 现在依赖 port_baseline_get/port_is_user_set
        #   （"值与基线不同即用户手改"），sync 又会在无基线时调 _warn_legacy_zlm_port
        #   ⇒ 抽函数时漏一个就是 `command not found`，测出来的是"脚本抽错了"而不是真问题。
        functions = "\n".join(re.search(r"(?ms)^" + name + r"\(\) \{.*?^\}", source).group()
                              for name in ["read_env_file", "port_baseline_get", "port_is_user_set",
                                           "read_port_env", "_warn_legacy_zlm_port",
                                           "sync_ports_into_config"])
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            (root / "config").mkdir()
            env_file = root / "config/config.env"
            ports = {"UVP_HTTP_PORT": "52002", "UVP_REDIS_PORT": "52003", "UVP_HTTPS_PORT": "52000", "UVP_NGINX_HTTP_PORT": "52001"}
            # ⭐ 2026-10-20 起 ZLM 的 12 个端口也走同一套标记/基线判据 ⇒
            #   把"用户改过的媒体端口"按真实形态写进文件（值 + 标记），
            #   而不是只给 shell 变量 —— 后者不会被识别成用户设置。
            zlm_lines = "".join(f"UVP_ZLM_{key}={value}\nUVP_USER_SET_UVP_ZLM_{key}=1\n"
                                for key, value in ZLM_PORTS.items())
            env_file.write_text("".join(f"{key}={value}\nUVP_USER_SET_{key}=1\n" for key, value in ports.items())
                                + zlm_lines
                                + "\n# retained setting\nUVP_SIP_TRACE_ENCRYPTION_KEY=test-value\n")
            # ports-sync 的真实写回在 Go 里（不是本用例的范围），这里只记录参数：
            # 要验的是「ctl 有没有把这些键当成用户设置传给 ports-sync」。
            stub = root / "uvp-setup-stub.sh"
            stub.write_text('#!/bin/sh\nprintf "%s\\n" "$*" >> "$STUB_LOG"\nexit 0\n')
            stub.chmod(0o755)
            script = "\n".join([
                "set -eu", f'ROOT="{root}"', f'ENV_FILE="{env_file}"',
                f'PORT_BASELINE_FILE="{root}/config/.port-baseline"',
                f'SETUP_BIN="{stub}"', f'export STUB_LOG="{root}/setup.log"',
                'CONF=/nonexistent-config',
                'HTTP_PORT=52002', 'REDIS_PORT=52003', 'NGINX_HTTPS_PORT=52000', 'NGINX_HTTP_PORT=52001',
                *[f'ZLM_{key}={value}' for key, value in ZLM_PORTS.items()],
                'ok() { :; }', 'log() { :; }', 'det() { :; }', functions, 'sync_ports_into_config',
                'cp "$ENV_FILE" "$ROOT/first.env"', 'sync_ports_into_config',
            ])
            result = subprocess.run(["bash", "-c", script], capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            for key, value in ports.items():
                self.assertIn(f"{key}={value}\n", env_file.read_text())
                self.assertIn(f"UVP_USER_SET_{key}=1\n", env_file.read_text())
            self.assertEqual((root / "first.env").read_text(), env_file.read_text())
            # ⛔ ZLM 端口的落盘由 uvp-setup ports-sync 负责（本用例的 stub 只记录参数），
            #   落盘格式由 server/cmd/uvp-setup/portsync_test.go 钉住；
            #   这里验的是"ctl 有没有把正确的值交出去"。
            calls = (root / "setup.log").read_text().strip().splitlines()
            self.assertEqual(len(calls), 2, calls)
            self.assertIn("--zlm-http 52004", calls[0])
            self.assertIn("--zlm-rtp-range 52014-52063", calls[0])
            for call in calls:
                # ⛔ 12 个 ZLM 键也必须进 --user-set：否则 Go 侧不补标记，
                #   下一次换包这些值就被默认值顶掉（"我明明改过"的现场投诉）。
                for key in list(ports) + [f"UVP_ZLM_{k}" for k in ZLM_PORTS]:
                    self.assertIn(key, call, f"ports-sync 参数里缺 {key}")
                self.assertIn("--baseline", call)

    def test_render_uses_active_assets_and_persistent_state(self):
        source = CONTROL_SCRIPT.read_text(encoding="utf-8")
        function = re.search(r"(?ms)^render_nginx_conf\(\) \{.*?^\}", source).group()
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            active = root / "releases/candidate"
            output = root / "nginx.conf"
            (root / "bin").mkdir()
            helper = root / "bin/uvp-setup"
            helper.write_text("#!/bin/sh\nexit 0\n")
            helper.chmod(0o755)
            script = "\n".join([
                "set -eu", f'ROOT="{root}"', f'ACTIVE_ROOT="{active}"',
                f'NGINX_CONF_TEMPLATE="{ROOT / "deploy/standalone/nginx.conf.template"}"',
                f'NGINX_CONF="{output}"', f'BIN="{root / "bin"}"',
                'HTTP_PORT=52002', 'NGINX_HTTPS_PORT=52000', 'NGINX_HTTP_PORT=52001',
                'resolve_nginx_worker() { printf "nobody nogroup"; }',
                'log() { :; }', 'fail() { exit 1; }', function, 'render_nginx_conf',
            ])
            result = subprocess.run(["bash", "-c", script], capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            rendered = output.read_text()
            self.assertIn(f"include {active}/vendor/nginx/conf/mime.types;", rendered)
            self.assertIn(f"root {active}/resource/public;", rendered)
            self.assertIn(f"ssl_certificate_key  {root}/vendor/nginx/conf/uvp.key;", rendered)
            self.assertIn(f"alias {root}/resource/public/uploads/;", rendered)
            snapshot_alias = f"alias {root}/resource/public/gb-channel-snapshot/;"
            for protocol in ("HTTPS", "HTTP"):
                protocol_config = rendered.split(f"# UVP_{protocol}_BEGIN", 1)[1].split(
                    f"# UVP_{protocol}_END", 1
                )[0]
                self.assertEqual(protocol_config.count(snapshot_alias), 1,
                                 f"{protocol} 快照目录必须映射到持久安装根")
            self.assertIn(f"pid  {root}/run/nginx.pid;", rendered)

    def test_package_discards_build_time_rendered_nginx_config(self):
        source = BUILD_SCRIPT.read_text(encoding="utf-8")
        copy_at = source.index('cp -a "$NGINX_DIR/." "$PKG/vendor/nginx/"')
        cleanup_at = source.find('rm -f "$PKG/vendor/nginx/conf/nginx.conf"', copy_at)
        template_at = source.find('cp "$DEPLOY_DIR/nginx.conf.template" "$PKG/vendor/nginx/conf/nginx.conf.template"', copy_at)

        self.assertNotEqual(cleanup_at, -1, "package must remove the rendered config copied from the build tree")
        self.assertNotEqual(template_at, -1, "package must retain the runtime config template")
        self.assertLess(cleanup_at, template_at)
        self.assertIn('rm -f "$PKG/vendor/nginx/conf/uvp.crt" "$PKG/vendor/nginx/conf/uvp.key"', source)

    def test_start_renders_paths_before_reusing_an_nginx_process(self):
        source = CONTROL_SCRIPT.read_text(encoding="utf-8")
        match = re.search(r"(?ms)^start_nginx\(\) \{(.*?)^\}", source)
        self.assertIsNotNone(match)
        body = match.group(1)

        self.assertLess(body.index("render_nginx_conf"), body.index("nginx_healthy"))


if __name__ == "__main__":
    unittest.main()
