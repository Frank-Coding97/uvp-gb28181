import importlib.util
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


if __name__ == "__main__":
    unittest.main()
