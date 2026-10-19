import base64
import importlib.util
import json
import os
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


if __name__ == "__main__":
    unittest.main()
