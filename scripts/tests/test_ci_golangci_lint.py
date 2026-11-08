"""Exercise CI installation with synthetic archives and a loopback HTTP server."""

import hashlib
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import io
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tarfile
import tempfile
import threading
import unittest


ROOT = Path(__file__).resolve().parents[2]
INSTALLER = ROOT / "scripts/install-ci-golangci-lint.sh"
VERSION = "2.13.1"


class CILinterInstallerTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="ci linter ")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.cache = self.root / "cache"
        self.bin = self.root / "bin"
        self.cache.mkdir()
        self.stub = self.root / "stub"
        self.stub.mkdir()
        self.installer = self.root / "installer.sh"
        self.marker = self.root / "executed"
        self.calls = self.root / "curl.jsonl"
        self.api_calls = self.root / "gh.jsonl"
        self.api_body = self.root / "api-archive"
        self.env = dict(os.environ, PATH=f"{self.stub}:{os.environ['PATH']}")
        self.env["REAL_CURL"] = shutil.which("curl") or "curl"
        self.env["CURL_CALLS"] = str(self.calls)
        self.env["BINARY_MARKER"] = str(self.marker)
        self.env["GH_CALLS"] = str(self.api_calls)
        self.env["GH_FIXTURE_BODY"] = str(self.api_body)
        self.env["GH_FIXTURE_EXIT"] = "1"
        self.stub.joinpath("uname").write_text(
            '#!/bin/sh\nif [ "$1" = -s ]; then echo Linux; else echo x86_64; fi\n'
        )
        # Preserve the production retry/time limits. Only redirect the external
        # HTTPS URL to our HTTP fixture, recording the original arguments.
        self.stub.joinpath("curl").write_text('''#!/usr/bin/env python3
import json, os, sys
args = sys.argv[1:]
with open(os.environ["CURL_CALLS"], "a") as stream:
    stream.write(json.dumps(args) + "\\n")
if "CURL_FORCE_ERROR" in os.environ:
    print("504", end="")
    sys.exit(int(os.environ["CURL_FORCE_ERROR"]))
args = ["=http" if arg == "=https" else arg for arg in args]
args[-1] = os.environ["FIXTURE_URL"]
os.execv(os.environ["REAL_CURL"], ["curl", *args])
''')
        self.stub.joinpath("gh").write_text('''#!/usr/bin/env python3
import json, os, sys
from pathlib import Path
with open(os.environ["GH_CALLS"], "a") as stream:
    stream.write(json.dumps(sys.argv[1:]) + "\\n")
status = int(os.environ["GH_FIXTURE_EXIT"])
if status == 0:
    sys.stdout.buffer.write(Path(os.environ["GH_FIXTURE_BODY"]).read_bytes())
sys.exit(status)
''')
        for stub in self.stub.iterdir():
            stub.chmod(0o755)
        self.statuses = []
        self.requests = 0
        self.payload = b""
        owner = self

        class Handler(BaseHTTPRequestHandler):
            def do_GET(self):
                owner.requests += 1
                status = owner.statuses.pop(0) if owner.statuses else 200
                self.send_response(status)
                body = owner.payload if status == 200 else b""
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                self.wfile.write(body)

            def log_message(self, *_):
                pass

        self.server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()
        self.addCleanup(self.stop_server)
        self.env["FIXTURE_URL"] = f"http://127.0.0.1:{self.server.server_port}/archive"
        self.make_archive()

    def stop_server(self):
        self.server.shutdown()
        self.thread.join()
        self.server.server_close()

    def make_archive(self, version=VERSION, arch="amd64"):
        name = f"golangci-lint-{VERSION}-linux-{arch}"
        self.archive = self.cache / f"{name}.tar.gz"
        binary = (
            '#!/bin/sh\nprintf executed > "$BINARY_MARKER"\n'
            f"echo 'golangci-lint has version {version} built with go1.26.0'\n"
        ).encode()
        buffer = io.BytesIO()
        with tarfile.open(fileobj=buffer, mode="w:gz") as archive:
            info = tarfile.TarInfo(f"{name}/golangci-lint")
            info.size = len(binary)
            info.mode = 0o755
            archive.addfile(info, io.BytesIO(binary))
        self.payload = buffer.getvalue()
        self.api_body.write_bytes(self.payload)
        checksum = hashlib.sha256(self.payload).hexdigest()
        # Substitute only the trusted digest in an isolated script copy. The
        # production installer has no environment-based checksum/URL override.
        source = INSTALLER.read_text()
        source = re.sub(r"checksum=[0-9a-f]{64}", f"checksum={checksum}", source)
        self.installer.write_text(source)

    def run_installer(self):
        return subprocess.run(
            ["bash", str(self.installer), str(self.cache), str(self.bin)],
            env=self.env, capture_output=True, text=True, timeout=70,
        )

    def assert_no_install_or_partials(self):
        self.assertFalse(self.bin.joinpath("golangci-lint").exists())
        self.assertEqual(list(self.cache.glob(".download.*")), [])
        self.assertEqual(list(self.bin.glob(".golangci-lint.*")), [])

    def test_504_recovers_then_verified_cache_works_without_network(self):
        self.statuses = [504, 200]
        result = self.run_installer()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.requests, 2)
        self.assertEqual(self.archive.read_bytes(), self.payload)
        self.assertTrue(self.bin.joinpath("golangci-lint").is_file())
        self.assertTrue(self.marker.exists())
        args = json.loads(self.calls.read_text())
        for flag, value in (("--retry", "4"), ("--retry-max-time", "180"),
                            ("--connect-timeout", "10"), ("--max-time", "45")):
            self.assertEqual(args[args.index(flag) + 1], value)
        self.assertEqual(args[-1], f"https://github.com/golangci/golangci-lint/releases/download/v{VERSION}/golangci-lint-{VERSION}-linux-amd64.tar.gz")
        self.calls.unlink()
        self.marker.unlink()
        self.env["FIXTURE_URL"] = "http://127.0.0.1:1/unreachable"
        result = self.run_installer()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertFalse(self.calls.exists())
        self.assertFalse(self.api_calls.exists())
        self.assertTrue(self.marker.exists())

    def test_corrupt_cache_is_rejected_before_execution_without_download(self):
        self.archive.write_bytes(b"corrupt")
        result = self.run_installer()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Cached linter checksum mismatch", result.stderr)
        self.assertFalse(self.marker.exists())
        self.assertFalse(self.calls.exists())
        self.assertFalse(self.api_calls.exists())
        self.assert_no_install_or_partials()

    def test_corrupt_download_is_neither_executed_nor_cached(self):
        self.payload = b"corrupt"
        result = self.run_installer()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Downloaded linter checksum mismatch", result.stderr)
        self.assertEqual(self.requests, 1)
        self.assertFalse(self.api_calls.exists())
        self.assertFalse(self.marker.exists())
        self.assertFalse(self.archive.exists())
        self.assert_no_install_or_partials()

    def test_wrong_executable_version_is_rejected(self):
        self.make_archive(version="2.13.10")
        result = self.run_installer()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Installed linter version mismatch", result.stderr)
        self.assert_no_install_or_partials()

    def test_404_is_not_retried(self):
        self.statuses = [404]
        result = self.run_installer()
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.requests, 1)
        self.assertFalse(self.api_calls.exists())
        self.assertFalse(self.archive.exists())
        self.assert_no_install_or_partials()

    def test_persistent_504_exhausts_five_attempts_and_cleans_up(self):
        self.statuses = [504] * 5
        result = self.run_installer()
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.requests, 5)
        self.assertEqual(len(self.api_calls.read_text().splitlines()), 1)
        self.assertFalse(self.archive.exists())
        self.assert_no_install_or_partials()

    def test_api_fallback_recovers_persistent_504_and_caches_verified_archive(self):
        self.statuses = [504] * 5
        self.env["GH_FIXTURE_EXIT"] = "0"
        result = self.run_installer()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.requests, 5)
        self.assertEqual(self.archive.read_bytes(), self.payload)
        self.assertTrue(self.marker.exists())
        self.assertEqual(json.loads(self.api_calls.read_text()), [
            "api", "--hostname", "github.com",
            "repos/golangci/golangci-lint/releases/assets/522396872",
            "--header", "Accept: application/octet-stream",
        ])
        self.calls.unlink()
        self.api_calls.unlink()
        self.marker.unlink()
        self.env["GH_FIXTURE_EXIT"] = "1"
        result = self.run_installer()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertFalse(self.calls.exists())
        self.assertFalse(self.api_calls.exists())
        self.assertTrue(self.marker.exists())

    def test_api_fallback_corruption_is_rejected_before_execution_or_caching(self):
        self.env["CURL_FORCE_ERROR"] = "22"
        self.env["GH_FIXTURE_EXIT"] = "0"
        self.api_body.write_bytes(b"corrupt")
        result = self.run_installer()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Downloaded linter checksum mismatch", result.stderr)
        self.assertFalse(self.marker.exists())
        self.assertFalse(self.archive.exists())
        self.assert_no_install_or_partials()

    def test_api_fallback_uses_arm64_asset_and_rejects_wrong_version(self):
        self.stub.joinpath("uname").write_text(
            '#!/bin/sh\nif [ "$1" = -s ]; then echo Linux; else echo aarch64; fi\n'
        )
        self.make_archive(version="2.13.10", arch="arm64")
        self.env["CURL_FORCE_ERROR"] = "22"
        self.env["GH_FIXTURE_EXIT"] = "0"
        result = self.run_installer()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Installed linter version mismatch", result.stderr)
        self.assertIn("repos/golangci/golangci-lint/releases/assets/522396912", json.loads(self.api_calls.read_text()))
        self.assert_no_install_or_partials()

    def test_api_timeout_preserves_failure_and_cleans_partial_download(self):
        self.env["CURL_FORCE_ERROR"] = "22"
        self.stub.joinpath("timeout").write_text(
            '#!/bin/sh\n[ "$1" = 90s ] || exit 2\nprintf partial\nexit 124\n'
        )
        self.stub.joinpath("timeout").chmod(0o755)
        result = self.run_installer()
        self.assertEqual(result.returncode, 124)
        self.assertFalse(self.archive.exists())
        self.assertFalse(self.marker.exists())
        self.assert_no_install_or_partials()

    def test_arm64_uses_matching_archive(self):
        self.stub.joinpath("uname").write_text(
            '#!/bin/sh\nif [ "$1" = -s ]; then echo Linux; else echo aarch64; fi\n'
        )
        self.make_archive(arch="arm64")
        self.archive.write_bytes(self.payload)
        result = self.run_installer()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertFalse(self.calls.exists())

    def test_installer_and_cache_versions_match_validation_pin(self):
        sys.path.insert(0, str(ROOT / "scripts"))
        self.addCleanup(sys.path.pop, 0)
        import validate
        self.assertEqual(validate.TOOL_PINS["golangci-lint"][1], f"v{VERSION}")
        self.assertIn(f"version={VERSION}\n", INSTALLER.read_text())
        action = ROOT.joinpath(".github/actions/setup-golangci-lint/action.yml").read_text()
        self.assertIn(f"-v{VERSION}-${{{{ hashFiles('scripts/install-ci-golangci-lint.sh') }}}}", action)

    def test_prs_and_base_branch_warmer_share_verified_cache_setup(self):
        action = ROOT.joinpath(".github/actions/setup-golangci-lint/action.yml").read_text()
        self.assertLess(action.index("- name: Install verified linter"), action.index("- name: Save verified linter archive"))
        self.assertNotIn("continue-on-error:", action)
        for name in ("go.yml", "ci-tools.yml"):
            workflow = ROOT.joinpath(".github/workflows", name).read_text()
            self.assertIn("uses: ./.github/actions/setup-golangci-lint\n", workflow)
        warmer = ROOT.joinpath(".github/workflows/ci-tools.yml").read_text()
        self.assertIn("  push:\n    branches: [dev, main]\n", warmer)
        self.assertIn("contents: read", warmer)
        self.assertNotIn("pull_request", warmer)


if __name__ == "__main__":
    unittest.main()
