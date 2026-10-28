import pathlib
import re
import unittest


ROOT = pathlib.Path(__file__).resolve().parents[2]
TEMPLATES = (
    ROOT / "deploy" / "standalone" / "nginx.conf.template",
    ROOT / "deploy" / "standalone-windows" / "nginx.conf.template",
    ROOT / "deploy" / "compose" / "nginx" / "nginx.conf.template",
)
TEST_EDGE = ROOT / "deploy" / "test" / "nginx.conf"


class DeviceIngressNginxTemplateTests(unittest.TestCase):
    def test_test_edge_uses_the_same_http_device_ingress_contract(self):
        text = TEST_EDGE.read_text(encoding="utf-8")
        self.assertIn('set $uvp_device_http_base "http://$http_host";', text)
        self.assertIn("if ($uvp_device_proxy_trusted = 0)", text)
        self.assertIn("if ($http_x_forwarded_proto ~ ^(?<uvp_device_external_scheme>https?)$)", text)
        self.assertIn("location /gb28181/", text)
        self.assertIn("location ~ ^/openapi(?:/|$)", text)
        for route in (
            "~ ^/api/gb28181/cloud-recordings/downloads/[^/]+/content$",
            "= /api/system/upgrade/import",
            "/api/",
            "/gb28181/",
        ):
            self.assertIn(
                "proxy_set_header X-UVP-Device-Base-URL $uvp_device_http_base;",
                self._location(text, route),
            )
        self.assertIn(
            "proxy_set_header X-UVP-Device-Base-URL $uvp_device_http_base;",
            self._openapi(text),
        )

    def test_device_ingress_is_explicit_and_overwrites_client_header(self):
        for path in TEMPLATES:
            with self.subTest(template=path):
                text = path.read_text(encoding="utf-8")
                self.assertIn("geo $remote_addr $uvp_device_proxy_trusted", text)
                self.assertIn("map $http_host $uvp_device_host_without_port", text)
                self.assertIn("X-UVP-Device-Base-URL", text)
                self.assertIn("$uvp_device_http_base", text)
                self.assertIn("$uvp_device_https_base", text)
                self.assertIn("~^1:(?<uvp_device_external_scheme>https?)$", text)
                self.assertIn('""      "http://$http_host";', text)
                self.assertIn('""      "http://$uvp_device_host_without_port:@PLAIN_HTTP_PORT@";', text)
                self.assertIn('~^\\[(?<uvp_device_ipv6>', text)
                for trusted_source in ("127.0.0.1", "::1", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"):
                    self.assertIn(trusted_source, text)

    def test_every_device_route_has_no_spa_fallback_and_sets_the_right_base(self):
        for path in TEMPLATES:
            with self.subTest(template=path):
                text = path.read_text(encoding="utf-8")
                https = self._server(text, "HTTPS")
                http = self._server(text, "HTTP")
                for block in (https, http):
                    self.assertIn("location ~ ^/openapi(?:/|$)", block)
                for route in ("/api/", "/gb28181/"):
                    self.assertIn(
                        "proxy_set_header X-UVP-Device-Base-URL $uvp_device_https_base;",
                        self._location(https, route),
                    )
                    self.assertIn(
                        "proxy_set_header X-UVP-Device-Base-URL $uvp_device_http_base;",
                        self._location(http, route),
                    )
                self.assertIn(
                    "proxy_set_header X-UVP-Device-Base-URL $uvp_device_https_base;",
                    self._openapi(https),
                )
                self.assertIn(
                    "proxy_set_header X-UVP-Device-Base-URL $uvp_device_http_base;",
                    self._openapi(http),
                )
                self.assertIn(
                    "proxy_set_header X-UVP-Device-Base-URL $uvp_device_https_base;",
                    self._location(https, "= /api/system/upgrade/import"),
                )

    @staticmethod
    def _server(text: str, label: str) -> str:
        port = "HTTPS_PORT" if label == "HTTPS" else "PLAIN_HTTP_PORT"
        marker = f"listen      @{port}@"
        listen = text.index(marker)
        start = text.rfind("    server {", 0, listen)
        end = text.find("\n    server {", listen)
        return text[start:] if end == -1 else text[start:end]

    @staticmethod
    def _location(server: str, route: str) -> str:
        match = re.search(r"(?ms)^\s*location (?:\^~ )?" + re.escape(route) + r" \{(.*?)^\s*\}", server)
        if match is None:
            raise AssertionError(f"missing location {route}")
        return match.group(1)

    @staticmethod
    def _openapi(server: str) -> str:
        match = re.search(r"(?ms)^\s*location ~ \^/openapi\(\?:/\|\$\) \{(.*?)^\s*\}", server)
        if match is None:
            raise AssertionError("missing OpenAPI location")
        return match.group(1)


if __name__ == "__main__":
    unittest.main()
