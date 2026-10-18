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
        script = "\n".join([
            "set -eu", 'NGINX_HTTPS_ENABLED=0', 'NGINX_HTTPS_PORT=52000', 'NGINX_HTTP_PORT=52001',
            'HTTP_PORT=52002', 'REDIS_PORT=52003', 'REDIS_BIND=127.0.0.1', "C_TAG=''", "C_RST=''",
            *[f'ZLM_{key}={value}' for key, value in ZLM_PORTS.items()],
            'ok() { :; }', 'det() { echo "$*"; }', '_firewall_hint() { :; }', function, 'print_port_plan',
        ])
        result = subprocess.run(["bash", "-c", script], capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("HTTPS 已关闭", result.stdout)
        self.assertNotIn("HTTP 自动跳转", result.stdout)

    def test_setup_helper_is_copied_once_after_build(self):
        source = BUILD_SCRIPT.read_text(encoding="utf-8")
        copy = 'cp "$SERVER_DIR/bin/uvp-setup" "$PKG/bin/uvp-setup"'
        self.assertEqual(source.count(copy), 1, "setup must not be copied before build")
        self.assertGreater(source.index(copy), source.index('build -ldflags'))

    def test_repeated_start_preserves_custom_port_markers_without_environment(self):
        source = CONTROL_SCRIPT.read_text(encoding="utf-8")
        functions = "\n".join(re.search(r"(?ms)^" + name + r"\(\) \{.*?^\}", source).group()
                              for name in ["read_env_file", "read_port_env", "sync_ports_into_config"])
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            (root / "config").mkdir()
            env_file = root / "config/config.env"
            ports = {"UVP_HTTP_PORT": "52002", "UVP_REDIS_PORT": "52003", "UVP_HTTPS_PORT": "52000", "UVP_NGINX_HTTP_PORT": "52001"}
            env_file.write_text("".join(f"{key}={value}\nUVP_USER_SET_{key}=1\n" for key, value in ports.items())
                                + "\n# retained setting\nUVP_SIP_TRACE_ENCRYPTION_KEY=test-value\n")
            script = "\n".join([
                "set -eu", f'ROOT="{root}"', f'ENV_FILE="{env_file}"', 'CONF=/nonexistent-config',
                'HTTP_PORT=52002', 'REDIS_PORT=52003', 'NGINX_HTTPS_PORT=52000', 'NGINX_HTTP_PORT=52001',
                *[f'ZLM_{key}={value}' for key, value in ZLM_PORTS.items()],
                'ok() { :; }', functions, 'sync_ports_into_config',
                'cp "$ENV_FILE" "$ROOT/first.env"', 'sync_ports_into_config',
            ])
            result = subprocess.run(["bash", "-c", script], capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            for key, value in ports.items():
                self.assertIn(f"{key}={value}\n", env_file.read_text())
                self.assertIn(f"UVP_USER_SET_{key}=1\n", env_file.read_text())
            for key, value in ZLM_PORTS.items():
                self.assertIn(f"UVP_ZLM_{key}={value}\n", env_file.read_text())
            self.assertEqual((root / "first.env").read_text(), env_file.read_text())

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
