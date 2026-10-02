#!/usr/bin/env python3
"""Repository-local checks and private, complete validation evidence (stdlib only)."""

from __future__ import annotations

import argparse
import hashlib
import ipaddress
import json
import os
from pathlib import Path
import platform
import re
import shutil
import signal
import stat
import struct
import subprocess
import sys
import time
import urllib.parse
import urllib.request
import uuid
import xml.etree.ElementTree as ET
from datetime import datetime, timezone


ROOT = Path(__file__).resolve().parent.parent
TOOL_PINS = {
    "gotestsum": ("gotest.tools/gotestsum", "v1.13.0"),
    "golangci-lint": (
        "github.com/golangci/golangci-lint/v2", "v2.13.1"
    ),
}
INSTALL_MODULES = {
    "gotestsum": "gotest.tools/gotestsum",
    "golangci-lint": "github.com/golangci/golangci-lint/v2/cmd/golangci-lint",
}
INTEGRATION_PACKAGES = (
    "./internal/database", "./internal/publicmoviepg", "./internal/schedulepg",
    "./internal/enrichment", "./internal/synccontrol", "./internal/syncschedule",
    "./internal/shortlink", "./internal/accounts", "./internal/accountmail", "./cmd/api",
)
CHECK_IDS = (
    "tooling-unit", "format", "go-unit", "web-unit", "go-lint", "web-typecheck",
    "web-lint", "build", "go-race", "go-integration",
)
DEFAULT_CHECKS = CHECK_IDS[1:8]
GO_TEST_CHECKS = {"go-unit", "go-race", "go-integration"}
OFFLINE_GO = {
    "GOENV": "off", "GOTOOLCHAIN": "local", "GOPROXY": "off", "GOSUMDB": "off",
    "GOPRIVATE": "", "GONOPROXY": "none", "GOFLAGS": "-mod=readonly",
}
OPT_INS = (
    "ACCOUNT_BROWSER_HARNESS", "CGR_LIVE_PROXY_FILE", "MEGARAMA_LIVE_PROXY_FILE",
    "CINEVILLE_LIVE_PROXY_FILE", "MK2_LIVE_PROXY_FILE", "CINEWEST_LIVE_PROXY_FILE",
    "GRANDECRAN_LIVE_PROXY_FILE", "NOECINEMAS_LIVE_PROXY_FILE",
)


class ValidationError(Exception):
    """Only finite safe reason codes may leave the runner."""


class Interrupted(Exception):
    def __init__(self, signum):
        self.exit_code = 128 + signum


def now():
    return datetime.now(timezone.utc).isoformat(timespec="milliseconds").replace("+00:00", "Z")


def digest_file(path):
    h = hashlib.sha256()
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            h.update(block)
    return h.hexdigest()


def hash_fields(h, *fields):
    for field in fields:
        value = field if isinstance(field, bytes) else str(field).encode("utf-8", "surrogateescape")
        h.update(struct.pack(">Q", len(value)))
        h.update(value)


def git(root, *args):
    result = subprocess.run(
        ["git", "-C", str(root), *args], stdout=subprocess.PIPE,
        stderr=subprocess.DEVNULL, check=False, timeout=30,
    )
    if result.returncode:
        raise ValidationError("snapshot_unavailable")
    return result.stdout


def source_snapshot(root):
    """Hash HEAD/index/untracked union, including deletion, mode and link text."""
    try:
        head = git(root, "rev-parse", "HEAD").decode().strip()
        if Path(os.fsdecode(git(root, "rev-parse", "--show-toplevel").strip())).resolve() != root.resolve():
            raise ValidationError("snapshot_unavailable")
        branch = git(root, "rev-parse", "--abbrev-ref", "HEAD").decode().strip()
        tracked = set(git(root, "ls-tree", "-r", "--name-only", "-z", "HEAD").split(b"\0")) - {b""}
        index_paths = set(git(root, "ls-files", "-z").split(b"\0")) - {b""}
        untracked = set(git(root, "ls-files", "--others", "--exclude-standard", "-z").split(b"\0")) - {b""}
        index_hash = hashlib.sha256()
        for entry in sorted(git(root, "ls-files", "--stage", "-z").split(b"\0")):
            if entry:
                hash_fields(index_hash, entry)
        content = hashlib.sha256()
        deleted = 0
        for name in sorted(tracked | index_paths | untracked):
            relative = Path(os.fsdecode(name))
            if relative.is_absolute() or ".." in relative.parts:
                raise ValidationError("snapshot_unavailable")
            path = root / relative
            # Never follow a changed parent symlink, even for a tracked file.
            if any(parent.is_symlink() for parent in path.parents if parent != root and root in parent.parents):
                raise ValidationError("snapshot_unavailable")
            try:
                info = path.lstat()
            except FileNotFoundError:
                deleted += 1
                hash_fields(content, name, "deleted")
                continue
            if stat.S_ISLNK(info.st_mode):
                hash_fields(content, name, "symlink", os.readlink(path))
            elif stat.S_ISREG(info.st_mode):
                hash_fields(content, name, "file", bool(info.st_mode & 0o111), digest_file(path))
            else:
                raise ValidationError("snapshot_unavailable")
        return {
            "head": head, "branch": branch, "content_fingerprint": content.hexdigest(),
            "index_fingerprint": index_hash.hexdigest(), "tracked_count": len(tracked | index_paths),
            "untracked_count": len(untracked), "deleted_count": deleted,
        }
    except (OSError, UnicodeError, subprocess.SubprocessError) as exc:
        raise ValidationError("snapshot_unavailable") from exc


def offline_environment(integration=False):
    env = dict(os.environ)
    env.update(OFFLINE_GO)
    env.update({"npm_config_offline": "true", "NUXT_TELEMETRY_DISABLED": "1", "PYTHONDONTWRITEBYTECODE": "1", "NO_COLOR": "1"})
    for key in list(env):
        if key.startswith("GOTESTSUM_") or key in OPT_INS:
            env.pop(key, None)
    if not integration:
        env.pop("TEST_DATABASE_URL", None)
    return env


def normalized_exit(returncode):
    return 128 - returncode if returncode < 0 else returncode


def artifact(root, path, complete):
    record = {"path": str(path.relative_to(root)), "exists": path.is_file(), "complete": complete}
    if record["exists"]:
        record.update(size_bytes=path.stat().st_size, sha256=digest_file(path))
    else:
        record.update(size_bytes=None, sha256=None)
    return record


def parse_go(json_path, junit_path):
    """Count unique functions/subtests and terminal package events, never prose."""
    tests, packages, started_tests, started_packages = {}, {}, set(), set()
    try:
        with json_path.open(encoding="utf-8") as stream:
            for line in stream:
                if not line.endswith("\n"):
                    raise ValueError("truncated")
                event = json.loads(line)
                if not isinstance(event, dict):
                    raise ValueError("event")
                action, package, test = event.get("Action"), event.get("Package"), event.get("Test")
                if not isinstance(package, str) or action not in ("start", "run", "pause", "cont", "output", "bench", "pass", "fail", "skip"):
                    raise ValueError("event")
                started_packages.add(package)
                if test is not None:
                    if not isinstance(test, str):
                        raise ValueError("test")
                    started_tests.add((package, test))
                if action in ("pass", "fail", "skip"):
                    if test is None:
                        packages[package] = action
                    else:
                        tests[(package, test)] = action
        if not packages or started_packages != set(packages) or started_tests != set(tests):
            raise ValueError("incomplete")
        if ET.parse(junit_path).getroot().tag not in ("testsuites", "testsuite"):
            raise ValueError("junit")
    except (OSError, ValueError, TypeError, ET.ParseError) as exc:
        raise ValidationError("go_evidence_incomplete") from exc
    counts = {"kind": "go-test2json", "top_level": {}, "subtests": {}, "packages": {}}
    for category in ("top_level", "subtests", "packages"):
        counts[category] = {"passed": 0, "failed": 0, "skipped": 0, "total": 0}
    names = {"pass": "passed", "fail": "failed", "skip": "skipped"}
    identities = {"failed_top_level": [], "skipped_top_level": []}
    for (package, test), outcome in tests.items():
        category = "subtests" if "/" in test else "top_level"
        counts[category][names[outcome]] += 1
        counts[category]["total"] += 1
        if category == "top_level" and outcome in ("fail", "skip"):
            # Dynamic subtest names and arbitrary output must never enter metadata.
            if re.fullmatch(r"[A-Za-z0-9_.\-/]+", package) and test.startswith(("Test", "Example", "Fuzz")) and test.isidentifier():
                identities[names[outcome] + "_top_level"].append({"package": package, "test": test})
    for outcome in packages.values():
        counts["packages"][names[outcome]] += 1
        counts["packages"]["total"] += 1
    counts.update({key: sorted(value, key=lambda item: (item["package"], item["test"])) for key, value in identities.items()})
    top = counts["top_level"]
    if counts["packages"]["failed"] or top["failed"] or counts["subtests"]["failed"]:
        return "failed", "test_failure", counts
    if not top["total"]:
        return "zero-selected", "no_tests_selected", counts
    if top["skipped"] == top["total"]:
        return "skipped", "all_tests_skipped", counts
    return "passed", "complete", counts


def parse_tap(text):
    keys = ("tests", "suites", "pass", "fail", "cancelled", "skipped", "todo")
    counts = {}
    for key in keys:
        matches = re.findall(rf"^# {key} (\d+)\s*$", text, re.MULTILINE)
        if len(matches) != 1:
            raise ValidationError("tap_evidence_incomplete")
        counts[key] = int(matches[0])
    if counts["tests"] != sum(counts[k] for k in keys[2:]):
        raise ValidationError("tap_evidence_incomplete")
    counts["kind"] = "node-native-tap"
    if counts["fail"] or counts["cancelled"]:
        return "failed", "test_failure", counts
    if not counts["tests"]:
        return "zero-selected", "no_tests_selected", counts
    if not counts["pass"]:
        return "skipped", "all_tests_skipped_or_todo", counts
    return "passed", "complete", counts


def parse_unittest(text):
    matches = re.findall(r"^Ran (\d+) tests? in [\d.]+s\s*$", text, re.MULTILINE)
    if len(matches) != 1:
        raise ValidationError("unittest_evidence_incomplete")
    count = int(matches[0])
    disposition = re.search(r"^(OK|FAILED)(?: \(([^\n]*)\))?\s*$", text, re.MULTILINE)
    if not disposition:
        raise ValidationError("unittest_evidence_incomplete")
    skipped = re.search(r"skipped=(\d+)", disposition[2] or "")
    counts = {"kind": "python-unittest", "tests": count, "skipped": int(skipped[1]) if skipped else 0}
    if disposition[1] != "OK":
        return "failed", "test_failure", counts
    if not count:
        return "zero-selected", "no_tests_selected", counts
    if counts["skipped"] == count:
        return "skipped", "all_tests_skipped", counts
    return "passed", "complete", counts


def check_command(root, directory, check_id, packages=(), go_run=None):
    if check_id in GO_TEST_CHECKS:
        args = [str(root / "api/bin/gotestsum"), "--format", "pkgname", "--jsonfile", str(directory / f"{check_id}.jsonl"), "--junitfile", str(directory / f"{check_id}.junit.xml"), "--", "-tags=nodynamic"]
        if check_id == "go-race":
            args.append("-race")
        args.extend(INTEGRATION_PACKAGES if check_id == "go-integration" else packages or ("./...",))
        if check_id == "go-integration":
            args.extend(["-run", "Integration$", "-count=1"])
        elif go_run is not None:
            args.extend(["-run", go_run])
        return args, root / "api"
    if check_id == "go-lint":
        return [str(root / "api/bin/golangci-lint"), "run"], root / "api"
    if check_id == "tooling-unit":
        return [sys.executable, "-m", "unittest", "discover", "-s", "scripts/tests"], root
    if check_id in ("format", "build"):
        return ["make", "fmt-check" if check_id == "format" else "build"], root
    return ["npm", "--prefix", "web", "run", {"web-unit": "test:unit", "web-typecheck": "typecheck", "web-lint": "lint"}[check_id]], root


def database_environment(value, base):
    """Accept only explicit loopback URI, never put credentials in argv/report."""
    if not value or not value.strip():
        raise ValidationError("database_not_configured")
    try:
        if re.search(r"%(?![0-9a-fA-F]{2})", value) or any(c.isspace() for c in value):
            raise ValueError("uri")
        parsed = urllib.parse.urlsplit(value)
        host = parsed.hostname
        if parsed.scheme not in ("postgres", "postgresql") or parsed.fragment or not host:
            raise ValueError("uri")
        if host != "localhost" and not ipaddress.ip_address(host).is_loopback:
            raise ValueError("host")
        user = urllib.parse.unquote(parsed.username or "")
        database = urllib.parse.unquote(parsed.path.removeprefix("/"))
        if not user or not database or "/" in database or not parsed.path.startswith("/"):
            raise ValueError("identity")
        query = urllib.parse.parse_qs(parsed.query, keep_blank_values=True, strict_parsing=True)
        if set(query) - {"sslmode"} or any(len(v) != 1 for v in query.values()):
            raise ValueError("options")
        sslmode = query.get("sslmode", ["prefer"])[0]
        if sslmode not in ("disable", "allow", "prefer", "require", "verify-ca", "verify-full"):
            raise ValueError("sslmode")
        port = parsed.port if parsed.port is not None else 5432
        if not 1 <= port <= 65535:
            raise ValueError("port")
        password = urllib.parse.unquote(parsed.password or "")
        if any("\0" in v for v in (user, database, password, host)):
            raise ValueError("nul")
    except (ValueError, UnicodeError) as exc:
        raise ValidationError("database_uri_unsupported") from exc
    env = {key: val for key, val in base.items() if not key.startswith("PG")}
    env.update({
        "PGHOST": host, "PGPORT": str(port), "PGDATABASE": database, "PGUSER": user,
        "PGPASSWORD": password, "PGSSLMODE": sslmode, "PGPASSFILE": os.devnull,
        "PGSERVICEFILE": os.devnull, "PGSYSCONFDIR": os.devnull, "PGCONNECT_TIMEOUT": "5",
        "PGOPTIONS": "-c statement_timeout=5000 -c default_transaction_read_only=on -c search_path=pg_catalog",
    })
    return env


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def browser_http_probe():
    # No proxy, redirects, cookie processor, persistent body, or write endpoint.
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
    probes = (
        ("http://127.0.0.1:18089/healthz", "health"),
        ("http://127.0.0.1:18089/api/__browser/mailbox?email=probe@example.test", "mailbox"),
        ("http://127.0.0.1:13009/api/v1/auth/session", "session"),
    )
    try:
        for url, kind in probes:
            req = urllib.request.Request(url, headers={"X-Browser-Harness": "1"} if kind == "mailbox" else {})
            with opener.open(req, timeout=3) as response:
                if response.status != 200:
                    raise ValidationError("browser_fixture_unavailable")
                if kind != "health":
                    if "no-store" not in response.headers.get("Cache-Control", "").lower():
                        raise ValidationError("browser_fixture_unavailable")
                    body = response.read(65537)
                    if len(body) > 65536:
                        raise ValidationError("browser_fixture_unavailable")
                    data = json.loads(body)
                    if not isinstance(data, dict):
                        raise ValidationError("browser_fixture_unavailable")
                    if kind == "mailbox" and not isinstance(data.get("messages"), list):
                        raise ValidationError("browser_fixture_unavailable")
                    if kind == "session" and (data.get("enabled") is not True or data.get("state") != "anonymous" or "account" not in data or data["account"] is not None):
                        raise ValidationError("browser_fixture_unavailable")
    except (OSError, ValueError) as exc:
        raise ValidationError("browser_fixture_unavailable") from exc


class Run:
    """One invocation, one private directory. No report reuse or scheduling."""

    def __init__(self, root, selection, kind="validation", packages=(), go_run=None, browser=False):
        self.root = root
        self.selection = selection
        self.packages = packages
        self.go_run = go_run
        self.browser = browser
        self.env = offline_environment("go-integration" in selection)
        self.started = time.monotonic()
        self.child = None
        self.interrupted = None
        self.probes = {}
        run_id = datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%S%fZ") + "-" + uuid.uuid4().hex
        parent = root / "tmp/validation"
        if parent.is_symlink() or (root / "tmp").is_symlink():
            raise ValidationError("artifact_directory_unavailable")
        parent.mkdir(parents=True, exist_ok=True, mode=0o700)
        self.directory = parent / run_id
        self.directory.mkdir(mode=0o700)
        self.report_path = self.directory / "report.json"
        self.report = {
            "schema_version": 1, "kind": kind, "run_id": run_id, "started_at": now(),
            "finished_at": None, "duration_ms": None, "status": "blocked", "runner_exit_code": None,
            "repository": {"root": str(root), "start": None, "end": None, "source_changed": None},
            "tools": {}, "environment": {
                "os": platform.system(), "architecture": platform.machine(), "python": platform.python_version(),
                "ci": os.environ.get("CI", "").lower() in ("1", "true"), "identity": "root" if os.geteuid() == 0 else "non-root",
                "go": OFFLINE_GO, "npm_offline": True, "nuxt_telemetry_disabled": True,
            },
            "selection": {"checks": list(selection), "go_packages": list(packages), "go_run": go_run, "browser_accounts": browser},
            "preflight": [], "checks": [], "report_path": str(self.report_path.relative_to(root)), "artifacts": [],
        }
        for check_id in CHECK_IDS:
            argv, cwd = check_command(root, self.directory, check_id, packages, go_run)
            self.report["checks"].append({
                "id": check_id, "selected": check_id in selection, "argv": argv, "cwd": str(cwd),
                "status": "skipped", "reason": "not_selected", "returncode": None, "normalized_exit": None,
                "started_at": None, "finished_at": None, "duration_ms": None, "test_counts": None,
                "prerequisites": [], "artifacts": [],
            })
        self.save()

    def save(self):
        temporary = self.directory / "report.partial"
        with temporary.open("w", encoding="utf-8") as stream:
            os.chmod(temporary, 0o600)
            json.dump(self.report, stream, indent=2, ensure_ascii=True)
            stream.write("\n")
            stream.flush()
            os.fsync(stream.fileno())
        temporary.replace(self.report_path)

    def process(self, record, argv, cwd, env=None, timeout=None):
        record.update(argv=argv, cwd=str(cwd))
        started = time.monotonic()
        paths = [self.directory / f"{record['id']}.{suffix}.log" for suffix in ("stdout", "stderr")]
        reason = "complete"
        complete = False
        try:
            with paths[0].open("xb") as stdout, paths[1].open("xb") as stderr:
                for path in paths:
                    os.chmod(path, 0o600)
                self.child = subprocess.Popen(argv, cwd=cwd, env=env or self.env, stdout=stdout, stderr=stderr, start_new_session=True)
                record["started_at"] = now()
                try:
                    record["returncode"] = self.child.wait(timeout=timeout)
                    complete = True
                except subprocess.TimeoutExpired:
                    reason = "probe_timeout"
                    self.stop_child()
                    record["returncode"] = self.child.returncode
                except Interrupted:
                    reason = "interrupted"
                    self.stop_child()
                    record["returncode"] = self.child.returncode
                    record["status"] = "failed"
                    raise
        except OSError:
            reason = "command_unavailable"
            record.setdefault("returncode", None)
        finally:
            self.child = None
            if record.get("started_at") is not None:
                record.update(finished_at=now(), duration_ms=round((time.monotonic() - started) * 1000))
            rc = record.get("returncode")
            record["normalized_exit"] = normalized_exit(rc) if rc is not None else None
            record["artifacts"] = [artifact(self.root, path, complete) for path in paths]
            record["reason"] = reason
            self.save()
        return reason

    def stop_child(self):
        if self.child is None:
            return
        try:
            os.killpg(self.child.pid, signal.SIGTERM)
            self.child.wait(timeout=2)
        except subprocess.TimeoutExpired:
            pass
        except ProcessLookupError:
            pass
        # Parent may exit while a grandchild ignores TERM. Kill remaining group.
        try:
            os.killpg(self.child.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        self.child.wait()

    def probe(self, name, action):
        if name in self.probes:
            return self.probes[name]["status"] == "passed"
        record = {"id": name, "status": "blocked", "reason": "prerequisite_unavailable", "argv": None, "cwd": str(self.root), "started_at": now(), "finished_at": None, "duration_ms": None, "returncode": None, "normalized_exit": None, "artifacts": []}
        self.probes[name] = record
        self.report["preflight"].append(record)
        started = time.monotonic()
        try:
            action(record)
            record.update(status="passed", reason="complete")
        except ValidationError as exc:
            record.update(status="blocked", reason=str(exc))
        except (OSError, ValueError, subprocess.SubprocessError):
            record.update(status="blocked", reason="prerequisite_unavailable")
        finally:
            record.update(finished_at=now(), duration_ms=round((time.monotonic() - started) * 1000))
            self.save()
        return record["status"] == "passed"

    def probe_command(self, record, argv, cwd=None, env=None):
        reason = self.process(record, argv, cwd or self.root, env, timeout=10 if record["id"] == "database" else 120)
        if reason != "complete" or record["returncode"] != 0:
            raise ValidationError("probe_timeout" if reason == "probe_timeout" else "probe_failed")
        return (self.directory / f"{record['id']}.stdout.log").read_text(encoding="utf-8")

    def executable(self, name):
        path = shutil.which(name, path=self.env.get("PATH"))
        if path is None:
            raise ValidationError("tool_missing")
        self.report["tools"].setdefault(name, {})["path"] = path
        return path

    def version(self, record, name, args, pattern, expected=None):
        path = self.executable(name)
        output = self.probe_command(record, [path, *args])
        match = re.search(pattern, output)
        if not match:
            raise ValidationError("tool_version_unverifiable")
        version = match[1]
        self.report["tools"][name]["version"] = version
        if expected and version != expected:
            raise ValidationError("tool_version_mismatch")
        return version

    def go_version(self, record):
        version = self.version(record, "go", ["version"], r"\bgo(\d+\.\d+\.\d+)\b")
        minimum = re.search(r"^go (\d+\.\d+\.\d+)$", (self.root / "api/go.mod").read_text(), re.MULTILINE)
        if not minimum or tuple(map(int, version.split("."))) < tuple(map(int, minimum[1].split("."))):
            raise ValidationError("tool_version_mismatch")

    def tool(self, record, name):
        path = self.root / "api/bin" / name
        module, pin = TOOL_PINS[name]
        if not path.is_file() or not os.access(path, os.X_OK):
            raise ValidationError("tool_missing")
        output = self.probe_command(record, [self.executable("go"), "version", "-m", str(path)])
        build = re.search(r":\s+go(\d+\.\d+\.\d+)\b", output)
        found = re.search(r"^\s*mod\s+(\S+)\s+(\S+)", output, re.MULTILINE)
        if not found or not build or "\n\t=>" in output:
            raise ValidationError("tool_version_unverifiable")
        if found[1] != module or found[2] != pin:
            raise ValidationError("tool_version_mismatch")
        self.report["tools"][name] = {"path": str(path), "expected_version": pin, "module": module, "module_version": found[2], "build_go_version": build[1]}
        # Separate captured command records, not overwritten logs.
        def readable(child):
            text = self.probe_command(child, [str(path), "--version" if name == "gotestsum" else "version"])
            pattern = r"(?<![A-Za-z0-9.])v?" + re.escape(pin.removeprefix("v")) + r"(?![A-Za-z0-9.])"
            if not re.search(pattern, text) or "devel" in text.lower():
                raise ValidationError("tool_version_mismatch")
        if not self.probe(name + "-version", readable):
            raise ValidationError("tool_version_mismatch")

    def frontend_package(self, record, package, executable):
        manifest = json.loads((self.root / "web/package.json").read_text())
        expected = {**manifest.get("dependencies", {}), **manifest.get("devDependencies", {})}[package]
        installed = json.loads((self.root / "web/node_modules" / package / "package.json").read_text())
        if installed.get("version") != expected:
            raise ValidationError("frontend_dependency_mismatch")
        path = self.root / "web/node_modules/.bin" / executable
        if not path.is_file() or not os.access(path, os.X_OK):
            raise ValidationError("frontend_dependency_missing")
        self.report["tools"][package] = {"path": str(path), "version": expected}

    def graph(self, record, packages):
        try:
            self.probe_command(record, [self.executable("go"), "list", "-mod=readonly", "-tags=nodynamic", "-deps", "-test", *packages], self.root / "api")
        except ValidationError as exc:
            raise ValidationError("cache_unavailable") from exc

    def race(self, record):
        output = self.probe_command(record, [self.executable("go"), "env", "-json", "CGO_ENABLED", "CC"])
        config = json.loads(output)
        if config.get("CGO_ENABLED") != "1":
            raise ValidationError("cgo_disabled")
        # Go-configured compiler may include flags. No shell is ever used.
        import shlex
        compiler = shlex.split(config.get("CC", ""))
        if not compiler or not shutil.which(compiler[0], path=self.env.get("PATH")):
            raise ValidationError("c_compiler_missing")

    def database(self, record):
        self.report["environment"]["database_configured"] = bool(os.environ.get("TEST_DATABASE_URL", "").strip())
        env = database_environment(os.environ.get("TEST_DATABASE_URL"), self.env)
        psql = self.executable("psql")
        args = [psql, "-X", "-w", "-q", "-A", "-t", "-v", "ON_ERROR_STOP=1", "-c", "SELECT current_setting('server_version_num'), has_database_privilege(current_user, current_database(), 'CREATE');"]
        try:
            output = self.probe_command(record, args, env=env)
        except ValidationError as exc:
            raise ValidationError("database_unavailable") from exc
        match = re.fullmatch(r"\s*(\d+)\|([tf])\s*", output)
        if not match:
            raise ValidationError("database_unavailable")
        major = int(match[1]) // 10000
        self.report["environment"]["database_server_major"] = major
        if major != 18:
            raise ValidationError("database_version_mismatch")
        if match[2] != "t":
            raise ValidationError("database_permission_missing")

    def browser_prerequisites(self):
        node = self.probe("node", lambda r: self.version(r, "node", ["--version"], r"^v(\d+\.\d+\.\d+)\s*$", "22.23.1"))
        def websocket(record):
            self.probe_command(record, [self.executable("node"), "-e", "process.exit(typeof WebSocket === 'function' ? 0 : 1)"])
        ws = self.probe("node-websocket", websocket) if node else False
        def chrome(record):
            path = self.executable(os.environ.get("CHROME_BIN") or "google-chrome")
            text = self.probe_command(record, [path, "--version"])
            match = re.search(r"\b(\d+\.\d+\.\d+\.\d+)\b", text[:1024])
            if not match:
                raise ValidationError("tool_version_unverifiable")
            self.report["environment"].update(browser_version=match[1], browser_ports=[18089, 13009])
        chrome_ok = self.probe("chrome", chrome)
        services = self.probe("browser-services", lambda r: browser_http_probe())
        return node and ws and chrome_ok and services

    def prerequisites(self, check):
        names = []
        def need(name, action):
            names.append(name)
            return self.probe(name, action)
        check_id = check["id"]
        okay = True
        if check_id in ("format", "build"):
            okay &= need("make", lambda r: self.executable("make"))
        if check_id == "tooling-unit":
            for name in ("bash", "jq"):
                okay &= need(name, lambda r, n=name: self.executable(n))
        if check_id == "format":
            okay &= need("gofmt", lambda r: self.executable("gofmt"))
        if check_id in GO_TEST_CHECKS | {"go-lint", "build"}:
            go_ok = need("go", self.go_version)
            okay &= go_ok
            if go_ok:
                if check_id in GO_TEST_CHECKS:
                    okay &= need("gotestsum", lambda r: self.tool(r, "gotestsum"))
                if check_id == "go-lint":
                    okay &= need("golangci-lint", lambda r: self.tool(r, "golangci-lint"))
                packages = INTEGRATION_PACKAGES if check_id == "go-integration" else self.packages or ("./...",)
                key = "go-graph-" + hashlib.sha256("\0".join(packages).encode()).hexdigest()[:12]
                okay &= need(key, lambda r: self.graph(r, packages))
                if check_id == "go-race":
                    okay &= need("race", self.race)
            if check_id == "go-integration":
                okay &= need("database", self.database)
        if check_id.startswith("web-") or check_id in ("format", "build"):
            okay &= need("node", lambda r: self.version(r, "node", ["--version"], r"^v(\d+\.\d+\.\d+)\s*$", "22.23.1"))
            okay &= need("npm", lambda r: self.version(r, "npm", ["--version"], r"^(\d+\.\d+\.\d+)\s*$", "10.9.8"))
            dependencies = {
                "format": (("@biomejs/biome", "biome"),), "web-unit": (),
                "web-lint": (("oxlint", "oxlint"),), "build": (("nuxt", "nuxt"),),
                "web-typecheck": (("nuxt", "nuxt"), ("vue-tsc", "vue-tsc")),
            }[check_id]
            for package, executable in dependencies:
                okay &= need("package-" + executable, lambda r, p=package, e=executable: self.frontend_package(r, p, e))
        check["prerequisites"] = names
        return okay

    def execute_check(self, check):
        reason = self.process(check, check["argv"], Path(check["cwd"]))
        rc = check["returncode"]
        if rc is None:
            check.update(status="blocked", reason=reason)
        else:
            check.update(status="failed" if rc else "passed", reason="command_failed" if rc else "complete")
            try:
                if check["id"] in GO_TEST_CHECKS:
                    result = parse_go(self.directory / f"{check['id']}.jsonl", self.directory / f"{check['id']}.junit.xml")
                elif check["id"] in ("web-unit", "tooling-unit"):
                    text = "\n".join((self.directory / f"{check['id']}.{suffix}.log").read_text(encoding="utf-8") for suffix in ("stdout", "stderr"))
                    result = parse_tap(text) if check["id"] == "web-unit" else parse_unittest(text)
                else:
                    result = None
                if result:
                    status, evidence_reason, counts = result
                    check["test_counts"] = counts
                    if not rc:
                        check.update(status=status, reason=evidence_reason)
            except ValidationError as exc:
                check.update(status="failed", reason=str(exc))
            if check["id"] in GO_TEST_CHECKS:
                complete = check["test_counts"] is not None
                check["artifacts"].extend(artifact(self.root, self.directory / f"{check['id']}.{suffix}", complete) for suffix in ("jsonl", "junit.xml"))
        self.save()
        print(f"{check['id']}: {check['status']} (exit {rc if rc is not None else 'not launched'})")

    def finish(self, exit_override=None):
        try:
            end = source_snapshot(self.root)
            self.report["repository"]["end"] = end
            start = self.report["repository"]["start"]
            changed = start is None or any(start[key] != end[key] for key in ("head", "content_fingerprint", "index_fingerprint"))
            self.report["repository"]["source_changed"] = changed
        except ValidationError:
            changed = True
            self.report["repository"].update(source_changed=True, reason="end_snapshot_unavailable")
        selected = [c for c in self.report["checks"] if c["selected"]]
        if self.report["kind"] == "preflight":
            status = "passed" if all(p["status"] == "passed" for p in self.report["preflight"]) and not changed else "blocked"
            self.report["coverage"] = "not_executed"
        else:
            states = [c["status"] for c in selected]
            status = next((s for s in ("failed", "blocked", "zero-selected", "skipped") if s in states), "passed")
            if changed and status != "failed":
                status = "blocked"
        actual_failure = next((c["normalized_exit"] for c in selected if c["returncode"] is not None and c["returncode"] != 0), None)
        code = exit_override if exit_override is not None else actual_failure or (0 if status == "passed" else 1)
        if exit_override is not None:
            status = "blocked" if status != "failed" else status
        self.report.update(status=status, runner_exit_code=code, finished_at=now(), duration_ms=round((time.monotonic() - self.started) * 1000))
        self.report["artifacts"] = [a for r in self.report["preflight"] + self.report["checks"] for a in r["artifacts"]]
        self.save()
        print(f"{self.report['kind']}: {status}; report: {self.report['report_path']}")
        return code

    def run(self):
        previous = {}
        def interrupt(signum, frame):
            raise Interrupted(signum)
        for sig in (signal.SIGINT, signal.SIGTERM):
            previous[sig] = signal.signal(sig, interrupt)
        try:
            def snapshot(record):
                if sys.version_info < (3, 12):
                    raise ValidationError("python_version_mismatch")
                self.report["repository"]["start"] = source_snapshot(self.root)
            ready = self.probe("source-snapshot", snapshot)
            for check in self.report["checks"]:
                if not check["selected"]:
                    continue
                if not ready:
                    check.update(status="blocked", reason="snapshot_unavailable")
                    continue
                if not self.prerequisites(check):
                    check.update(status="blocked", reason="prerequisite_unavailable")
                elif self.report["kind"] == "preflight":
                    check.update(status="skipped", reason="preflight_only")
                elif check["id"] == "build" and any(c["selected"] and c["id"] in DEFAULT_CHECKS and c["id"] != "build" and c["status"] != "passed" for c in self.report["checks"]):
                    check.update(status="blocked", reason="cheap_checks_unsuccessful")
                else:
                    self.execute_check(check)
                self.save()
            if self.browser:
                self.browser_prerequisites()
            return self.finish()
        except Interrupted as exc:
            self.interrupted = exc.exit_code
            for check in self.report["checks"]:
                if check["selected"] and check["status"] == "skipped" and check["reason"] == "not_selected":
                    check.update(status="blocked", reason="interrupted")
            return self.finish(exc.exit_code)
        finally:
            for sig, handler in previous.items():
                signal.signal(sig, handler)


def install_tools(tools, root=ROOT):
    """Setup only: downloads/toolchain acquisition explicitly permitted here."""
    directory = root / "api/bin"
    if directory.is_symlink() or (root / "api").is_symlink():
        raise ValidationError("tool_directory_unavailable")
    directory.mkdir(exist_ok=True)
    env = dict(os.environ, GOBIN=str(directory.resolve()), GOENV="off", GOFLAGS="", GOTOOLCHAIN="auto")
    run = Run(root, (), kind="preflight")
    if not run.probe("source-snapshot", lambda r: run.report["repository"].update(start=source_snapshot(root))):
        return run.finish()
    previous = {}
    def interrupt(signum, frame):
        raise Interrupted(signum)
    for sig in (signal.SIGINT, signal.SIGTERM):
        previous[sig] = signal.signal(sig, interrupt)
    try:
        for name in tools:
            # Setup compilation can take longer than read-only probes.
            record = {"id": "install-" + name, "status": "blocked", "returncode": None, "artifacts": []}
            run.report["preflight"].append(record)
            reason = run.process(record, ["go", "install", INSTALL_MODULES[name] + "@" + TOOL_PINS[name][1]], root / "api", env=env)
            rc = record["returncode"]
            record.update(status="passed" if reason == "complete" and rc == 0 else "blocked")
            if record["status"] != "passed":
                return run.finish(normalized_exit(rc) if rc else 1)
        # Verify exact embedded identity and readable versions with same helpers.
        for name in tools:
            run.probe(name, lambda r, n=name: run.tool(r, n))
        return run.finish()
    except Interrupted as exc:
        return run.finish(exc.exit_code)
    finally:
        for sig, handler in previous.items():
            signal.signal(sig, handler)


class SafeParser(argparse.ArgumentParser):
    def error(self, message):
        self.print_usage(sys.stderr)
        self.exit(2, "validation: invalid selection or option; see --help\n")


def parser():
    cli = SafeParser(description=__doc__)
    commands = cli.add_subparsers(dest="command", required=True)
    install = commands.add_parser("install-tools", help="Online setup into ignored api/bin")
    install.add_argument("--tool", action="append", choices=tuple(TOOL_PINS))
    for name in ("preflight", "run"):
        sub = commands.add_parser(name, help="Read-only prerequisites" if name == "preflight" else "Checks with private structured evidence")
        sub.add_argument("--check", action="append", choices=CHECK_IDS)
        sub.add_argument("--go-package", action="append", help="Relative package/pattern; single go-unit or go-race only")
        sub.add_argument("--go-run", help="Go test name regex; single go-unit or go-race only")
        if name == "preflight":
            sub.add_argument("--browser-accounts", action="store_true", help="Existing default account fixture only; starts nothing")
    return cli


def main(argv=None):
    os.umask(0o077)
    cli = parser()
    args = cli.parse_args(argv)
    try:
        if args.command == "install-tools":
            return install_tools(tuple(dict.fromkeys(args.tool or TOOL_PINS)))
        browser = getattr(args, "browser_accounts", False)
        selection = tuple(c for c in CHECK_IDS if c in (args.check or (() if browser else DEFAULT_CHECKS)))
        if args.go_package or args.go_run is not None:
            if len(selection) != 1 or selection[0] not in ("go-unit", "go-race"):
                cli.error("selector scope")
            for package in args.go_package or ():
                if not re.fullmatch(r"\./[A-Za-z0-9_./-]+", package) or ".." in package.removesuffix("/...").split("/") or "//" in package:
                    cli.error("package")
            if args.go_run is not None and (len(args.go_run) > 512 or any(ord(c) < 32 for c in args.go_run)):
                cli.error("filter")
        return Run(ROOT, selection, kind="preflight" if args.command == "preflight" else "validation", packages=tuple(args.go_package or ()), go_run=args.go_run, browser=browser).run()
    except (ValidationError, OSError, ValueError, subprocess.SubprocessError):
        # Never leak paths/credentials/child output via exception formatting.
        print("validation: blocked; evidence or prerequisite unavailable", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
