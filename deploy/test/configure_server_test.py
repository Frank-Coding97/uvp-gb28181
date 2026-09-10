import importlib.util
import re
import unittest
from pathlib import Path


MODULE_PATH = Path(__file__).with_name("configure_server.py")
SPEC = importlib.util.spec_from_file_location("configure_server", MODULE_PATH)
assert SPEC and SPEC.loader
configure_server = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(configure_server)


REPO_ROOT = Path(__file__).resolve().parents[2]


class ConfigureServerTests(unittest.TestCase):
    def test_production_config_trusts_only_the_proxy_chain(self) -> None:
        template = "httpserver:\n  trustedproxies: []\n"
        rendered = configure_server.render_config(
            template,
            {("httpserver", "trustedproxies"): configure_server.TRUSTED_PROXIES},
        )

        self.assertEqual(
            rendered,
            'httpserver:\n  trustedproxies: ["127.0.0.1", "::1", "172.18.0.3"]\n',
        )

    def test_logging_profiles_are_explicit_and_independent_of_appdebug(self) -> None:
        expected = {
            "development": (["file", "stdout"], "console", "console"),
            "direct": (["file"], "json", "json"),
            "container": (["stdout"], "json", "json"),
        }

        for profile, (outputs, textformat, stdoutformat) in expected.items():
            with self.subTest(profile=profile):
                replacements = configure_server.logging_replacements(profile)
                self.assertEqual(replacements[("logs", "outputs")], outputs)
                self.assertEqual(replacements[("logs", "filepath")],
                                 "./resource/logs/uvp-gb28181.log")
                self.assertEqual(replacements[("logs", "textformat")], textformat)
                self.assertEqual(replacements[("logs", "stdoutformat")], stdoutformat)
                self.assertEqual(replacements[("logs", "level")], "info")
                self.assertEqual(replacements[("logs", "modules", "access")], "info")
                self.assertEqual(replacements[("logs", "modules", "scheduler")], "info")
                self.assertFalse(replacements[("logs", "routes")])
                self.assertEqual(replacements[("logs", "maxsize")], 5)
                self.assertEqual(replacements[("logs", "maxbackups")], 7)
                self.assertEqual(replacements[("logs", "maxage")], 15)
                self.assertNotIn(("server", "appdebug"), replacements)

    def test_unknown_logging_profile_is_rejected(self) -> None:
        with self.assertRaises(ValueError):
            configure_server.logging_replacements("appdebug")

    def test_render_config_requires_each_complete_path(self) -> None:
        template = "first:\n  level: old\nsecond:\n  other: old\n"

        with self.assertRaisesRegex(RuntimeError, r"second\.level"):
            configure_server.render_config(
                template,
                {
                    ("first", "level"): "new",
                    ("second", "level"): "missing",
                },
            )

    def test_real_logging_template_renders_container_profile(self) -> None:
        template_path = REPO_ROOT / "server" / "config" / "config.example.yml"
        template = template_path.read_text(encoding="utf-8")
        rendered = configure_server.render_config(
            template,
            configure_server.logging_replacements("container"),
        )

        self.assertIn('outputs: ["stdout"]', rendered)
        self.assertIn('filepath: "./resource/logs/uvp-gb28181.log"', rendered)
        self.assertIn('modules:', rendered)
        self.assertNotIn("ginlogname:", rendered)
        self.assertNotIn("zaplogname:", rendered)
        self.assertNotIn("scheduler:\n  log:", rendered)

    def test_template_keeps_development_default_path_until_profile_is_selected(self) -> None:
        template = (REPO_ROOT / "server" / "config" / "config.example.yml").read_text(
            encoding="utf-8"
        )
        self.assertIn('filepath: "./resource/logs/ginfast.log"', template)
        self.assertEqual(
            configure_server.logging_replacements("development")[("logs", "filepath")],
            "./resource/logs/uvp-gb28181.log",
        )

    def test_compose_has_bounded_stdout_driver_and_preserves_log_mount(self) -> None:
        compose = (Path(__file__).with_name("compose.yml")).read_text(encoding="utf-8")
        self.assertRegex(
            compose,
            re.compile(
                r"(?ms)^    logging:\n"
                r"      driver: json-file\n"
                r"      options:\n"
                r"        max-size: [\"']5m[\"']\n"
                r"        max-file: [\"']8[\"']$"
            ),
        )
        self.assertIn(
            "${UVP_ROOT:-/opt/uvp-gb28181}/data/logs:/app/resource/logs",
            compose,
        )
        self.assertIn("    stop_grace_period: 45s", compose)

    def test_systemd_has_shutdown_window_without_global_journal_quota(self) -> None:
        service = Path(__file__).with_name("uvp-backend.service").read_text(encoding="utf-8")
        self.assertIn("TimeoutStopSec=45s", service)
        self.assertIn("WorkingDirectory=/opt/uvp-gb28181/current/backend", service)
        self.assertIn("ReadWritePaths=/opt/uvp-gb28181", service)
        self.assertNotRegex(service, r"(?m)^(SystemMaxUse|RuntimeMaxUse|SystemKeepFree)=")

    def test_persistent_log_link_and_history_policy_are_documented(self) -> None:
        deploy_script = (Path(__file__).with_name("deploy-uvp.sh")).read_text(encoding="utf-8")
        self.assertIn('ln -sfn "$ROOT/data/logs" "$FINAL_RELEASE/backend/resource/logs"', deploy_script)
        self.assertNotIn('rm -rf "$ROOT/data/logs"', deploy_script)

        notes = Path(__file__).with_name("README.md").read_text(encoding="utf-8")
        for phrase in (
            "development",
            "direct",
            "container",
            "./resource/logs/uvp-gb28181.log",
            "logs.filepath > logs.zaplogname",
            "logs.outputs > logs.console",
            "logs.modules.scheduler > scheduler.log.level",
            "旧 `scheduler` 目录",
            "40 MiB",
            "46 MiB",
            "旧二进制必须匹配旧配置",
        ):
            with self.subTest(phrase=phrase):
                self.assertIn(phrase, notes)


if __name__ == "__main__":
    unittest.main()
