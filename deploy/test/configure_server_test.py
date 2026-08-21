import importlib.util
import unittest
from pathlib import Path


MODULE_PATH = Path(__file__).with_name("configure_server.py")
SPEC = importlib.util.spec_from_file_location("configure_server", MODULE_PATH)
assert SPEC and SPEC.loader
configure_server = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(configure_server)


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


if __name__ == "__main__":
    unittest.main()
