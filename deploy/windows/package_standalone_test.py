import hashlib
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
import zipfile

sys.dont_write_bytecode = True

ROOT = Path(__file__).resolve().parent
SCRIPT = ROOT / "package-standalone.py"


class StandalonePackagerTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix="uvp package 中文 # ")
        self.root = Path(self.tmp.name)
        self.inputs = self.root / "inputs"
        self.output = self.root / "trial-package"
        self.zip = self.root / "trial-package.zip"
        self._create_inputs()

    def tearDown(self):
        self.tmp.cleanup()

    def _create_inputs(self):
        self.launcher = self.inputs / "launcher.exe"
        self.backend = self.inputs / "backend.exe"
        self.web = self.inputs / "web"
        self.resource = self.inputs / "resource"
        self.redis = self.inputs / "redis"
        self.media = self.inputs / "media"
        self.licenses = self.inputs / "licenses"
        self.sources = self.inputs / "redistribution-sources"
        self.launcher.parent.mkdir(parents=True)
        self.launcher.write_bytes(b"launcher dummy")
        self.backend.write_bytes(b"backend dummy")
        (self.web / "static").mkdir(parents=True)
        (self.web / "index.html").write_text("<!doctype html>\n", encoding="utf-8")
        (self.web / "static" / "app.js").write_text("console.log('ok');\n", encoding="utf-8")
        (self.resource / "database").mkdir(parents=True)
        (self.resource / "database" / "baseline.sql").write_text("-- fixture\n", encoding="utf-8")
        (self.redis).mkdir(parents=True)
        (self.redis / "redis-server.exe").write_bytes(b"redis dummy")
        (self.redis / "cygwin1.dll").write_bytes(b"cygwin dummy")
        (self.media).mkdir(parents=True)
        (self.media / "MediaServer.exe").write_bytes(b"media dummy")
        (self.media / "config.ini").write_text("[api]\n", encoding="utf-8")
        (self.licenses / "ZLM-LICENSE.txt").parent.mkdir(parents=True)
        (self.licenses / "ZLM-LICENSE.txt").write_text("license material\n", encoding="utf-8")
        (self.licenses / "REDIS-COPYING").write_text("copying material\n", encoding="utf-8")
        (self.sources / "redis-source.tar.gz").parent.mkdir(parents=True)
        (self.sources / "redis-source.tar.gz").write_bytes(b"source archive")

    def _args(self, output=None, output_zip=None, **extra):
        output = output or self.output
        output_zip = output_zip or self.zip
        args = [
            sys.executable,
            str(SCRIPT),
            "--launcher",
            str(self.launcher),
            "--backend",
            str(self.backend),
            "--web-dir",
            str(self.web),
            "--resource-dir",
            str(self.resource),
            "--redis-dir",
            str(self.redis),
            "--media-dir",
            str(self.media),
            "--licenses-dir",
            str(self.licenses),
            "--sources-dir",
            str(self.sources),
            "--version",
            extra.pop("version", "1.2.3-win10"),
            "--source-commit",
            extra.pop("source_commit", "a" * 40),
            "--zlm-revision",
            extra.pop("zlm_revision", "b" * 40),
            "--redis-revision",
            extra.pop("redis_revision", "c" * 40),
            "--license-owner",
            extra.pop("license_owner", "input owner"),
            "--output-dir",
            str(output),
            "--output-zip",
            str(output_zip),
        ]
        for key, value in extra.items():
            args.extend([f"--{key.replace('_', '-')}", str(value)])
        return args

    def _run(self, **kwargs):
        return subprocess.run(
            self._args(**kwargs),
            cwd=ROOT.parent.parent.parent,
            env={**os.environ, "PYTHONDONTWRITEBYTECODE": "1"},
            capture_output=True,
            text=True,
        )

    def test_builds_fresh_directory_and_zip_with_matching_manifest(self):
        result = self._run()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertTrue(self.output.is_dir())
        self.assertTrue(self.zip.is_file())
        self.assertNotIn("config", {p.name for p in self.output.iterdir()})
        self.assertNotIn("data", {p.name for p in self.output.iterdir()})
        self.assertNotIn("logs", {p.name for p in self.output.iterdir()})
        self.assertNotIn("recordings", {p.name for p in self.output.iterdir()})

        current_path = self.output / "current.json"
        current_raw = current_path.read_bytes()
        self.assertFalse(current_raw.startswith(b"\xef\xbb\xbf"))
        self.assertEqual(json.loads(current_raw), {"version": "1.2.3-win10"})
        self.assertEqual(
            json.loads((self.output / "version.json").read_text(encoding="utf-8")),
            {"name": "uvp-gb28181", "version": "1.2.3-win10"},
        )

        release = self.output / "releases" / "1.2.3-win10"
        manifest = json.loads((release / "manifest.json").read_text(encoding="utf-8"))
        self.assertEqual(manifest["format_version"], 1)
        self.assertEqual(manifest["schema_min"], 1)
        self.assertEqual(manifest["schema_max"], 3)
        self.assertEqual(manifest["source_commit"], "a" * 40)
        listed = {item["path"]: item["sha256"] for item in manifest["files"]}
        for required in (
            "backend/uvp-server.exe",
            "redis/redis-server.exe",
            "media/MediaServer.exe",
        ):
            self.assertIn(required, listed)
        for relative, expected in listed.items():
            self.assertNotIn("\\", relative)
            actual = hashlib.sha256((release / Path(relative)).read_bytes()).hexdigest()
            self.assertEqual(actual, expected, relative)
        self.assertTrue((release / "web" / "index.html").is_file())
        self.assertTrue((release / "resource").is_dir())

        provenance = json.loads((self.output / "provenance.json").read_text(encoding="utf-8"))
        self.assertEqual(provenance["source_metadata"]["uvp"]["revision"], "a" * 40)
        self.assertEqual(provenance["source_metadata"]["zlm"]["revision"], "b" * 40)
        self.assertEqual(provenance["source_metadata"]["redis"]["revision"], "c" * 40)
        self.assertFalse(provenance["verification"]["git_verified"])
        self.assertFalse(provenance["verification"]["automated_legal_audit"])
        self.assertEqual(provenance["verification"]["license_owner"], "input owner")
        for component in ("launcher", "backend", "web", "resource", "redis", "media", "licenses", "sources"):
            self.assertTrue(provenance["inputs"][component]["sha256"])
            self.assertTrue(provenance["inputs"][component]["files"])

        with zipfile.ZipFile(self.zip) as archive:
            names = set(archive.namelist())
            self.assertIn("current.json", names)
            self.assertIn("version.json", names)
            self.assertIn("releases/1.2.3-win10/manifest.json", names)
            self.assertIn("licenses/ZLM-LICENSE.txt", names)
            self.assertIn("sources/redis-source.tar.gz", names)
            self.assertNotIn("config/", {name for name in names if name.endswith("/")})
            self.assertEqual(archive.read("current.json"), current_raw)

    def test_rejects_existing_directory_or_zip(self):
        self.output.mkdir()
        result = self._run()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("output directory already exists", result.stderr)

        self.output.rmdir()
        self.zip.write_bytes(b"existing")
        result = self._run()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("output ZIP already exists", result.stderr)

    def test_rejects_invalid_version_and_revision(self):
        for version in ("../escape", "CON", "1.2.3.", "1/2"):
            with self.subTest(version=version):
                result = self._run(version=version)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("version", result.stderr.lower())
        for source_commit in ("HEAD", "a" * 39, "G" * 40):
            with self.subTest(source_commit=source_commit):
                result = self._run(source_commit=source_commit)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("source commit", result.stderr.lower())

    def test_rejects_missing_required_input_and_license(self):
        missing_backend = self._run(backend=self.root / "missing.exe")
        self.assertNotEqual(missing_backend.returncode, 0)
        self.assertIn("backend", missing_backend.stderr.lower())

        empty_licenses = self.root / "empty-licenses"
        empty_licenses.mkdir()
        result = self._run(licenses_dir=empty_licenses)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("license", result.stderr.lower())

    def test_rejects_symlinked_input(self):
        linked = self.web / "linked.js"
        try:
            linked.symlink_to(self.web / "static" / "app.js")
        except (NotImplementedError, OSError) as error:
            self.skipTest(f"symlink unavailable: {error}")
        result = self._run()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("symlink", result.stderr.lower())


if __name__ == "__main__":
    unittest.main()
