import base64
import importlib.util
import json
import os
import re
import tempfile
import unittest
from pathlib import Path


MODULE_PATH = Path(__file__).parents[1] / "standalone" / "make-config.py"
SPEC = importlib.util.spec_from_file_location("make_config", MODULE_PATH)
assert SPEC and SPEC.loader
make_config = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(make_config)


class MakeConfigTests(unittest.TestCase):
    def test_build_trusts_only_the_local_proxy_chain(self) -> None:
        source = Path(__file__).parents[2] / "server" / "config" / "config.example.yml"
        with tempfile.TemporaryDirectory() as directory:
            target = Path(directory) / "config.yml"
            make_config.build(source, target, "51010", "51011")
            rendered = target.read_text(encoding="utf-8")

        self.assertIn(
            'trustedproxies: ["127.0.0.1", "::1", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"]',
            rendered,
        )

    def test_build_injects_configured_upgrade_release_public_key(self) -> None:
        template = Path(__file__).parents[2] / "server" / "config" / "config.example.yml"
        release_source = "https://updates.example.test/uvp/channels/stable/manifest.json"
        key_id = "release-" + os.urandom(6).hex()
        public_key = base64.b64encode(os.urandom(32)).decode("ascii")
        trusted_keys = json.dumps({key_id: public_key})
        with tempfile.TemporaryDirectory() as directory:
            source = Path(directory) / "config.example.yml"
            target = Path(directory) / "config.yml"
            source.write_text(
                template.read_text(encoding="utf-8").replace(
                    '  release_source: ""',
                    '  release_source: "%s"' % release_source,
                ),
                encoding="utf-8",
            )
            make_config.build(
                source, target, "51010", "51011",
                upgrade_trusted_keys_json=trusted_keys,
            )
            rendered = target.read_text(encoding="utf-8")

        self.assertIn(
            'trusted_keys: {"%s":"%s"}' % (key_id, public_key),
            rendered,
        )
        self.assertIn('release_source: "%s"' % release_source, rendered)

    def test_build_rejects_invalid_upgrade_release_public_key(self) -> None:
        source = Path(__file__).parents[2] / "server" / "config" / "config.example.yml"
        with tempfile.TemporaryDirectory() as directory:
            target = Path(directory) / "config.yml"
            with self.assertRaises(SystemExit):
                make_config.build(
                    source, target, "51010", "51011",
                    upgrade_trusted_keys_json='{"release-bad":"not-a-public-key"}',
                )

    def test_build_preserves_trusted_keys_from_source_without_override(self) -> None:
        source = Path(__file__).parents[2] / "server" / "config" / "config.example.yml"
        source_public_key = base64.b64encode(os.urandom(32)).decode("ascii")
        configured = source.read_text(encoding="utf-8").replace(
            "  trusted_keys: {}",
            '  trusted_keys: {"release-source":"%s"}' % source_public_key,
        )
        with tempfile.TemporaryDirectory() as directory:
            configured_source = Path(directory) / "config.example.yml"
            target = Path(directory) / "config.yml"
            configured_source.write_text(configured, encoding="utf-8")
            make_config.build(configured_source, target, "51010", "51011")
            rendered = target.read_text(encoding="utf-8")

        self.assertIn("trusted_keys: {\"release-source\":", rendered)

    def test_build_disables_debug_surface(self) -> None:
        """出厂配置必须关掉 appdebug —— 开着就注册免鉴权的 /viewCache。"""
        source = Path(__file__).parents[2] / "server" / "config" / "config.example.yml"
        with tempfile.TemporaryDirectory() as directory:
            target = Path(directory) / "config.yml"
            make_config.build(source, target, "51010", "51011")
            rendered = target.read_text(encoding="utf-8")

        self.assertRegex(rendered, r"(?m)^\s*appdebug:\s*false\b")
        self.assertNotRegex(rendered, r"(?m)^\s*appdebug:\s*true\b")

    def test_build_replaces_placeholder_jwt_sign_key(self) -> None:
        """模板里的 CHANGE_ME 占位符绝不能随包发出去。"""
        source = Path(__file__).parents[2] / "server" / "config" / "config.example.yml"
        with tempfile.TemporaryDirectory() as directory:
            target = Path(directory) / "config.yml"
            make_config.build(source, target, "51010", "51011")
            rendered = target.read_text(encoding="utf-8")

        match = re.search(r'(?m)^\s*jwttokensignkey:\s*"([^"]+)"', rendered)
        self.assertIsNotNone(match, "生成结果里找不到 jwttokensignkey")
        jwt_sign_key = match.group(1)
        self.assertNotIn("CHANGE_ME", jwt_sign_key)
        self.assertFalse(make_config.sign_key_looks_placeholder(jwt_sign_key))
        self.assertGreaterEqual(len(jwt_sign_key), 32)

    def test_build_rejects_explicit_placeholder_jwt_sign_key(self) -> None:
        """显式传入的占位密钥同样要拒绝，不能"传了就当合法"。"""
        source = Path(__file__).parents[2] / "server" / "config" / "config.example.yml"
        with tempfile.TemporaryDirectory() as directory:
            target = Path(directory) / "config.yml"
            with self.assertRaises(SystemExit):
                make_config.build(
                    source, target, "51010", "51011",
                    jwt_sign_key="CHANGE_ME_TO_A_RANDOM_SECRET",
                )

    def test_build_keeps_strong_sign_key_from_source(self) -> None:
        """源配置已是强密钥时原样保留 —— 重复出包不能偷偷换掉它。"""
        source = Path(__file__).parents[2] / "server" / "config" / "config.example.yml"
        strong_key = os.urandom(32).hex()
        configured = source.read_text(encoding="utf-8").replace(
            '"CHANGE_ME_TO_A_RANDOM_SECRET"', '"%s"' % strong_key,
        )
        with tempfile.TemporaryDirectory() as directory:
            configured_source = Path(directory) / "config.example.yml"
            target = Path(directory) / "config.yml"
            configured_source.write_text(configured, encoding="utf-8")
            make_config.build(configured_source, target, "51010", "51011")
            rendered = target.read_text(encoding="utf-8")

        self.assertIn('jwttokensignkey: "%s"' % strong_key, rendered)


if __name__ == "__main__":
    unittest.main()
