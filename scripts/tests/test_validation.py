"""Deterministic validation contract tests. No live DB, browser, or downloads."""

import contextlib
import importlib.util
import io
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch


ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location("validation", ROOT / "scripts/validate.py")
v = importlib.util.module_from_spec(spec)
spec.loader.exec_module(v)
CANARY = "private-canary-do-not-publish"


def event(action, test=None, package="example/config"):
    result = {"Action": action, "Package": package}
    if test is not None:
        result["Test"] = test
    return result


class ParserTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.json = self.root / "go.jsonl"
        self.junit = self.root / "go.xml"
        self.junit.write_text("<testsuites><testsuite/></testsuites>")

    def go(self, events):
        self.json.write_text("".join(json.dumps(item) + "\n" for item in events))
        return v.parse_go(self.json, self.junit)

    def test_function_subtest_package_counts_and_safe_identities(self):
        status, _, counts = self.go([
            event("start"), event("run", "TestParent"),
            event("run", "TestParent/" + CANARY), event("skip", "TestParent/" + CANARY),
            event("pass", "TestParent"), event("run", "TestSkipped"),
            event("skip", "TestSkipped"), event("pass"),
            event("start", package="no/tests"), event("skip", package="no/tests"),
        ])
        self.assertEqual(status, "passed")
        self.assertEqual(counts["top_level"], {"passed": 1, "failed": 0, "skipped": 1, "total": 2})
        self.assertEqual(counts["subtests"]["skipped"], 1)
        self.assertEqual(counts["packages"]["total"], 2)
        self.assertNotIn(CANARY, json.dumps(counts))
        self.assertEqual(counts["skipped_top_level"], [{"package": "example/config", "test": "TestSkipped"}])

    def test_cached_replay_counts_unique_identities(self):
        sequence = [event("run", "TestOne"), event("pass", "TestOne"), event("pass")]
        self.assertEqual(self.go(sequence + sequence)[2]["top_level"]["total"], 1)

    def test_zero_packages_with_no_tests(self):
        self.assertEqual(self.go([event("start"), event("skip")])[0], "zero-selected")

    def test_all_skipped(self):
        self.assertEqual(self.go([event("run", "TestOne"), event("skip", "TestOne"), event("pass")])[0], "skipped")

    def test_compilation_panic_race_package_failure(self):
        for sequence in ([event("fail")], [event("run", "TestPanic"), event("fail", "TestPanic"), event("fail")]):
            with self.subTest(sequence=sequence):
                self.assertEqual(self.go(sequence)[0], "failed")

    def test_incomplete_terminal_tests_and_packages(self):
        for sequence in ([], [event("start")], [event("run", "TestOne"), event("pass")], [event("pass", package="one"), event("start", package="two")]):
            with self.subTest(sequence=sequence), self.assertRaises(v.ValidationError):
                self.go(sequence)

    def test_malformed_truncated_json(self):
        for text in ("{", "[]\n", "null\n", json.dumps(event("pass")), '{"Action":"oops","Package":"x"}\n'):
            self.json.write_text(text)
            with self.subTest(text=text), self.assertRaises(v.ValidationError):
                v.parse_go(self.json, self.junit)

    def test_missing_or_invalid_junit(self):
        self.json.write_text(json.dumps(event("pass")) + "\n")
        for text in ("", "<html/>", "<testsuites>"):
            self.junit.write_text(text)
            with self.subTest(text=text), self.assertRaises(v.ValidationError):
                v.parse_go(self.json, self.junit)
        self.junit.unlink()
        with self.assertRaises(v.ValidationError):
            v.parse_go(self.json, self.junit)

    def tap(self, **overrides):
        counts = dict(tests=3, suites=0, **{"pass": 1}, fail=0, cancelled=0, skipped=1, todo=1)
        counts.update(overrides)
        return "TAP version 13\n" + "\n".join(f"# {k} {n}" for k, n in counts.items()) + "\n"

    def test_tap_native_mixed_counts(self):
        status, _, counts = v.parse_tap(self.tap())
        self.assertEqual(status, "passed")
        self.assertEqual((counts["tests"], counts["skipped"], counts["todo"]), (3, 1, 1))
        self.assertEqual(counts["kind"], "node-native-tap")

    def test_tap_zero_all_skipped_todo_cancelled_failed(self):
        for changes, expected in ((dict(tests=0, **{"pass": 0}, skipped=0, todo=0), "zero-selected"), (dict(**{"pass": 0}, skipped=2), "skipped"), (dict(**{"pass": 0}, cancelled=1), "failed"), (dict(**{"pass": 0}, fail=1), "failed")):
            with self.subTest(changes=changes):
                self.assertEqual(v.parse_tap(self.tap(**changes))[0], expected)

    def test_tap_incomplete_duplicate_inconsistent(self):
        for text in ("", self.tap().replace("# todo 1", ""), self.tap() + "# tests 3\n", self.tap(tests=100)):
            with self.subTest(text=text), self.assertRaises(v.ValidationError):
                v.parse_tap(text)

    def test_unittest_counts_and_dispositions(self):
        for count, disposition, expected in ((4, "OK", "passed"), (0, "OK", "zero-selected"), (4, "OK (skipped=4)", "skipped"), (4, "FAILED (failures=1)", "failed"), (4, "OK (skipped=1)", "passed")):
            self.assertEqual(v.parse_unittest(f"Ran {count} tests in 0.005s\n\n{disposition}\n")[0], expected)
        with self.assertRaises(v.ValidationError):
            v.parse_unittest("OK")


class RepositoryCase(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        (self.root / "api/bin").mkdir(parents=True)
        (self.root / "tmp").mkdir()
        (self.root / "web/node_modules/.bin").mkdir(parents=True)
        (self.root / ".gitignore").write_text("api/bin/\ntmp/\nweb/node_modules/\n.generated/\n.env\n")
        (self.root / "api/go.mod").write_text("module example\n\ngo 1.25.13\n")
        (self.root / "source").write_text("first")
        self.git("init", "-q")
        self.git("add", ".")
        self.git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.test", "commit", "-qm", "fixture")

    def git(self, *args):
        result = subprocess.run(["git", "-C", str(self.root), *args], capture_output=True, check=True)
        return result.stdout.decode().strip()

    def write_executable(self, name, body):
        path = self.root / "api/bin" / name
        path.write_text(f"#!{sys.executable}\n" + body)
        path.chmod(0o755)
        return path

    def quiet(self):
        return contextlib.redirect_stdout(io.StringIO())


class SnapshotTests(RepositoryCase):
    def fingerprint(self):
        return v.source_snapshot(self.root)["content_fingerprint"]

    def test_same_size_content_change(self):
        before = self.fingerprint()
        (self.root / "source").write_text("other")
        self.assertNotEqual(before, self.fingerprint())

    def test_untracked_add_delete(self):
        before = self.fingerprint()
        (self.root / "new").write_text("new")
        self.assertNotEqual(before, self.fingerprint())
        (self.root / "new").unlink()
        self.assertEqual(before, self.fingerprint())

    def test_deleted_staged_index_separate(self):
        before = v.source_snapshot(self.root)
        (self.root / "source").unlink()
        deleted = v.source_snapshot(self.root)
        self.assertEqual(deleted["deleted_count"], 1)
        self.assertNotEqual(before["content_fingerprint"], deleted["content_fingerprint"])
        self.git("add", "source")
        staged = v.source_snapshot(self.root)
        self.assertNotEqual(staged["index_fingerprint"], deleted["index_fingerprint"])
        self.assertEqual(staged["content_fingerprint"], deleted["content_fingerprint"])

    def test_executable_and_symlink_text(self):
        before = self.fingerprint()
        (self.root / "source").chmod(0o755)
        self.assertNotEqual(before, self.fingerprint())
        (self.root / "source").unlink()
        (self.root / "source").symlink_to("/outside/not-read")
        linked = self.fingerprint()
        (self.root / "source").unlink()
        (self.root / "source").symlink_to("/outside/also-not-read")
        self.assertNotEqual(linked, self.fingerprint())

    def test_ignored_changes_and_tracked_ignored_file(self):
        before = self.fingerprint()
        (self.root / "api/bin/x").write_text("ignored")
        (self.root / ".env").write_text(CANARY)
        self.assertEqual(before, self.fingerprint())
        (self.root / "api/bin/tracked").write_text("first")
        self.git("add", "-f", "api/bin/tracked")
        before = self.fingerprint()
        (self.root / "api/bin/tracked").write_text("other")
        self.assertNotEqual(before, self.fingerprint())

    def test_nul_safe_newline_path(self):
        before = self.fingerprint()
        (self.root / "with\nnewline").write_text("x")
        self.assertNotEqual(before, self.fingerprint())

    def test_linked_worktree(self):
        linked = self.root / "tmp/linked"
        self.git("worktree", "add", "--detach", str(linked), "HEAD")
        self.assertTrue((linked / ".git").is_file())
        self.assertEqual(self.fingerprint(), v.source_snapshot(linked)["content_fingerprint"])

    def test_snapshot_failure_closed(self):
        with patch.object(v, "git", side_effect=v.ValidationError("snapshot_unavailable")), self.quiet():
            run = v.Run(self.root, ("go-unit",))
            self.assertEqual(run.run(), 1)
            selected = next(c for c in run.report["checks"] if c["selected"])
            self.assertEqual(selected["status"], "blocked")
            self.assertIsNone(selected["returncode"])


class RunnerTests(RepositoryCase):
    def setUp(self):
        super().setUp()
        pins = {"nuxt": "4.5.2", "vue-tsc": "3.3.9", "oxlint": "1.78.0", "@biomejs/biome": "2.5.14"}
        (self.root / "web/package.json").write_text(json.dumps({"devDependencies": pins}))
        for package, version in pins.items():
            directory = self.root / "web/node_modules" / package
            directory.mkdir(parents=True)
            (directory / "package.json").write_text(json.dumps({"version": version}))
            executable = "biome" if package.startswith("@") else package
            path = self.root / "web/node_modules/.bin" / executable
            path.write_text("#!/bin/sh\nexit 0\n")
            path.chmod(0o755)
        # All execution is synthetic, including pin/cache/DB probes. Ledger ignored.
        self.write_executable("go", '''import json, os, sys
from pathlib import Path
args = sys.argv[1:]
root = Path(__file__).resolve().parents[2]
with (root / 'tmp/ledger').open('a') as f: f.write(json.dumps(['go', args]) + '\\n')
if args[:2] == ['version', '-m']:
    name = Path(args[2]).name
    module, pin = {'gotestsum': ('gotest.tools/gotestsum', 'v1.13.0'), 'golangci-lint': ('github.com/golangci/golangci-lint/v2', 'v2.13.1')}[name]
    print(args[2] + ': go1.26.4\\n\\tmod\\t' + module + '\\t' + os.environ.get('FAKE_PIN', pin))
elif args == ['version']: print('go version go1.26.4 linux/amd64')
elif args and args[0] == 'env': print(json.dumps({'CGO_ENABLED': '1', 'CC': 'cc'}))
elif args and args[0] == 'list': sys.exit(int(os.environ.get('GRAPH_EXIT', '0')))
elif args and args[0] == 'install': sys.exit(int(os.environ.get('INSTALL_EXIT', '0')))
''')
        self.write_executable("gotestsum", '''import json, os, sys
from pathlib import Path
if sys.argv[1:] == ['--version']:
    print('gotestsum version v1.13.0'); sys.exit(0)
args = sys.argv[1:]
root = Path(__file__).resolve().parents[2]
with (root / 'tmp/ledger').open('a') as f: f.write(json.dumps(['gotestsum', args]) + '\\n')
mode = os.environ.get('FAKE_EVIDENCE', 'pass')
events = [{'Action': 'start', 'Package': 'example/config'}]
if mode != 'zero':
    events.extend([{'Action': 'run', 'Package': 'example/config', 'Test': 'TestOne'}, {'Action': 'skip' if mode == 'skip' else 'pass', 'Package': 'example/config', 'Test': 'TestOne'}])
events.append({'Action': 'pass', 'Package': 'example/config'})
jsonfile = Path(args[args.index('--jsonfile') + 1])
xmlfile = Path(args[args.index('--junitfile') + 1])
if mode != 'missing': jsonfile.write_text('bad' if mode == 'bad' else ''.join(json.dumps(e) + '\\n' for e in events))
if mode != 'missing': xmlfile.write_text('<testsuites/>' if mode != 'badxml' else '<html/>')
print('private-canary-do-not-publish')
print('private-canary-do-not-publish', file=sys.stderr)
sys.exit(int(os.environ.get('CHILD_EXIT', '0')))
''')
        self.write_executable("golangci-lint", "import sys\nprint('golangci-lint version 2.13.1')\nsys.exit(0)\n")
        self.write_executable("node", "import sys\nprint('v22.23.1') if sys.argv[1:] == ['--version'] else None\n")
        self.write_executable("npm", '''import sys
if sys.argv[1:] == ['--version']: print('10.9.8')
elif sys.argv[-1] == 'test:unit': print('# tests 1\\n# suites 0\\n# pass 1\\n# fail 0\\n# cancelled 0\\n# skipped 0\\n# todo 0')
''')
        self.write_executable("make", "import os, sys\nsys.exit(int(os.environ.get('MAKE_EXIT', '0')))\n")
        self.write_executable("psql", "import os\nprint(os.environ.get('PSQL_RESULT', '180003|t'))\n")
        self.write_executable("google-chrome", "print('Google Chrome 141.0.7390.54')\n")
        env = {"PATH": str(self.root / "api/bin") + os.pathsep + os.environ["PATH"], "PYTHONDONTWRITEBYTECODE": "1"}
        self.environment = patch.dict(os.environ, env)
        self.environment.start()
        self.addCleanup(self.environment.stop)

    def execute(self, selection=("go-unit",), **kwargs):
        with self.quiet():
            run = v.Run(self.root, selection, **kwargs)
            code = run.run()
        return run, code

    def selected(self, run, check_id="go-unit"):
        return next(c for c in run.report["checks"] if c["id"] == check_id)

    def test_pass_reports_commands_versions_counts_artifacts(self):
        run, code = self.execute()
        self.assertEqual(code, 0)
        check = self.selected(run)
        self.assertEqual(check["returncode"], 0)
        self.assertEqual(check["cwd"], str(self.root / "api"))
        self.assertEqual(check["test_counts"]["top_level"]["total"], 1)
        self.assertEqual(len(check["artifacts"]), 4)
        for artifact in check["artifacts"]:
            self.assertTrue(artifact["complete"])
            self.assertEqual(len(artifact["sha256"]), 64)
        serialized = run.report_path.read_text()
        self.assertNotIn(CANARY, serialized)
        self.assertIn(CANARY, (run.directory / "go-unit.stdout.log").read_text())
        self.assertFalse(run.report["repository"]["source_changed"])
        self.assertEqual(run.report["tools"]["gotestsum"]["module_version"], "v1.13.0")

    def test_real_child_failure_survives_report_work(self):
        with patch.dict(os.environ, {"CHILD_EXIT": "7"}):
            run, code = self.execute()
        self.assertEqual(code, 7)
        self.assertEqual(self.selected(run)["returncode"], 7)
        self.assertEqual(run.report["status"], "failed")

    def test_zero_selected_keeps_observed_zero(self):
        with patch.dict(os.environ, {"FAKE_EVIDENCE": "zero"}):
            run, code = self.execute(go_run="^NoSuchTest$")
        self.assertEqual(code, 1)
        self.assertEqual(self.selected(run)["returncode"], 0)
        self.assertEqual(self.selected(run)["status"], "zero-selected")
        self.assertEqual(run.report["selection"]["go_run"], "^NoSuchTest$")

    def test_all_skipped_never_passes(self):
        with patch.dict(os.environ, {"FAKE_EVIDENCE": "skip"}):
            run, code = self.execute()
        self.assertEqual(code, 1)
        self.assertEqual(run.report["status"], "skipped")

    def test_missing_invalid_json_xml_fail_closed_actual_zero(self):
        for mode in ("missing", "bad", "badxml"):
            with self.subTest(mode=mode), patch.dict(os.environ, {"FAKE_EVIDENCE": mode}):
                run, code = self.execute()
                self.assertEqual(code, 1)
                self.assertEqual(self.selected(run)["returncode"], 0)
                self.assertEqual(run.report["status"], "failed")

    def test_build_panic_race_failed_exit_even_without_evidence(self):
        for check_id, rc in (("go-unit", "2"), ("go-race", "66")):
            with self.subTest(check_id=check_id), patch.dict(os.environ, {"CHILD_EXIT": rc, "FAKE_EVIDENCE": "missing"}):
                run, code = self.execute((check_id,))
                self.assertEqual(code, int(rc))
                self.assertEqual(run.report["status"], "failed")

    def test_selected_go_only_requires_neither_node_db_nor_linter(self):
        run, code = self.execute()
        self.assertEqual(code, 0)
        ids = [p["id"] for p in run.report["preflight"]]
        for name in ("node", "npm", "database", "chrome", "golangci-lint"):
            self.assertNotIn(name, ids)

    def test_lint_requires_no_reporter(self):
        (self.root / "api/bin/gotestsum").unlink()
        run, code = self.execute(("go-lint",))
        self.assertEqual(code, 0)
        self.assertNotIn("gotestsum", [p["id"] for p in run.report["preflight"]])

    def test_wrong_pin_blocked_unlaunched(self):
        with patch.dict(os.environ, {"FAKE_PIN": "v0.0.0"}):
            run, code = self.execute()
        check = self.selected(run)
        self.assertEqual((code, check["status"]), (1, "blocked"))
        self.assertIsNone(check["returncode"])
        self.assertIsNone(check["started_at"])

    def test_unverifiable_development_and_replaced_tool_identity(self):
        for output in ("no build info", "fake: go1.26.4\n\tmod\tgotest.tools/gotestsum\t(devel)\n", "fake: go1.26.4\n\tmod\twrong/module\tv1.13.0\n", "fake: go1.26.4\n\tmod\tgotest.tools/gotestsum\tv1.13.0\n\t=>\tlocal\n"):
            with self.subTest(output=output), patch.object(v.Run, "probe_command", return_value=output):
                run = v.Run(self.root, ())
                self.assertFalse(run.probe("gotestsum", lambda r: run.tool(r, "gotestsum")))

    def test_missing_pin_no_download_fallback(self):
        (self.root / "api/bin/gotestsum").unlink()
        run, code = self.execute()
        self.assertEqual(code, 1)
        self.assertNotIn("install", (self.root / "tmp/ledger").read_text())

    def test_graph_cache_block(self):
        with patch.dict(os.environ, {"GRAPH_EXIT": "1"}):
            run, code = self.execute()
        self.assertEqual(code, 1)
        self.assertIn("cache_unavailable", json.dumps(run.report))

    def test_format_does_not_probe_go_graph(self):
        with patch.dict(os.environ, {"GRAPH_EXIT": "1"}):
            run, code = self.execute(("format",))
        self.assertEqual(code, 0)
        self.assertFalse(any(p["id"].startswith("go-graph") for p in run.report["preflight"]))

    def test_race_cgo_and_compiler_blockers(self):
        run = v.Run(self.root, ())
        for config in ({"CGO_ENABLED": "0", "CC": "cc"}, {"CGO_ENABLED": "1", "CC": "__missing_compiler__"}):
            with self.subTest(config=config), patch.object(run, "probe_command", return_value=json.dumps(config)), self.assertRaises(v.ValidationError):
                run.race({"id": "race"})

    def test_missing_frontend_dependency_selected_only(self):
        (self.root / "web/node_modules/nuxt/package.json").unlink()
        run, code = self.execute(("web-unit", "web-typecheck"))
        self.assertEqual(code, 1)
        self.assertEqual(self.selected(run, "web-unit")["status"], "passed")
        self.assertEqual(self.selected(run, "web-typecheck")["status"], "blocked")

    def test_prerequisite_dedup_and_build_last(self):
        run, code = self.execute(v.DEFAULT_CHECKS)
        self.assertEqual(code, 0)
        ids = [p["id"] for p in run.report["preflight"]]
        self.assertEqual(len(ids), len(set(ids)))
        self.assertEqual(len([p for p in ids if p.startswith("go-graph-")]), 1)
        checks = [c for c in run.report["checks"] if c["selected"]]
        self.assertEqual(checks[-1]["id"], "build")
        for previous, following in zip(checks, checks[1:]):
            self.assertLessEqual(previous["finished_at"], following["started_at"])

    def test_failed_cheap_checks_continue_but_build_blocked(self):
        with patch.dict(os.environ, {"CHILD_EXIT": "9"}):
            run, code = self.execute(v.DEFAULT_CHECKS)
        self.assertEqual(code, 9)
        self.assertEqual(self.selected(run, "web-lint")["status"], "passed")
        self.assertEqual(self.selected(run, "build")["reason"], "cheap_checks_unsuccessful")
        self.assertIsNone(self.selected(run, "build")["returncode"])

    def test_explicit_build_no_lint_test_tools(self):
        (self.root / "api/bin/gotestsum").unlink()
        (self.root / "api/bin/golangci-lint").unlink()
        run, code = self.execute(("build",))
        self.assertEqual(code, 0)

    def test_preflight_no_coverage(self):
        run, code = self.execute(kind="preflight")
        self.assertEqual(code, 0)
        self.assertEqual(run.report["coverage"], "not_executed")
        self.assertEqual(self.selected(run)["reason"], "preflight_only")
        self.assertIsNone(self.selected(run)["returncode"])
        self.assertNotIn('"gotestsum", ["--format"', (self.root / "tmp/ledger").read_text())

    def test_private_permissions_unique_runs(self):
        run1, _ = self.execute()
        run2, _ = self.execute()
        self.assertNotEqual(run1.directory, run2.directory)
        self.assertEqual(run1.directory.stat().st_mode & 0o777, 0o700)
        self.assertEqual(run1.report_path.stat().st_mode & 0o777, 0o600)
        for suffix in ("stdout", "stderr"):
            self.assertEqual((run1.directory / f"go-unit.{suffix}.log").stat().st_mode & 0o777, 0o600)

    def test_source_change_blocks_boundary_pass(self):
        original = v.Run.execute_check
        def mutate(run, check):
            original(run, check)
            (self.root / "source").write_text("other")
        with patch.object(v.Run, "execute_check", mutate):
            run, code = self.execute()
        self.assertEqual(code, 1)
        self.assertTrue(run.report["repository"]["source_changed"])
        self.assertEqual(run.report["status"], "blocked")

    def test_artifact_collision_does_not_overwrite(self):
        run = v.Run(self.root, ())
        path = run.directory / "collision.stdout.log"
        path.write_text("keep")
        record = {"id": "collision", "started_at": None}
        run.process(record, ["true"], self.root)
        self.assertEqual(path.read_text(), "keep")
        self.assertIsNone(record["returncode"])

    def test_private_directory_collision_fails_closed(self):
        fixed_time = v.datetime.now(v.timezone.utc)
        with patch.object(v.uuid, "uuid4") as uuid:
            uuid.return_value.hex = "fixed"
            with patch.object(v, "datetime") as date:
                date.now.return_value = fixed_time
                with self.assertRaises(FileExistsError):
                    v.Run(self.root, ())
                    v.Run(self.root, ())

    def test_artifact_parent_symlink_rejected_before_writing(self):
        target = self.root / "tmp/elsewhere"
        target.mkdir()
        (self.root / "tmp/validation").symlink_to(target)
        with self.assertRaises(v.ValidationError):
            v.Run(self.root, ())
        self.assertEqual(list(target.iterdir()), [])

    def test_missing_command_records_null_exit_and_times(self):
        run = v.Run(self.root, ())
        record = {"id": "missing", "started_at": None, "finished_at": None}
        run.process(record, ["/__validation_no_such_command__"], self.root)
        self.assertIsNone(record["returncode"])
        self.assertIsNone(record["started_at"])
        self.assertIsNone(record["finished_at"])

    def test_probe_timeout_stops_child_preserves_returncode(self):
        run = v.Run(self.root, ())
        child = unittest.mock.Mock()
        child.wait.side_effect = [subprocess.TimeoutExpired(["fake"], 10), -signal.SIGTERM, -signal.SIGTERM]
        child.returncode = -signal.SIGTERM
        record = {"id": "timeout"}
        with patch.object(v.subprocess, "Popen", return_value=child), patch.object(v.os, "killpg") as kill:
            self.assertEqual(run.process(record, ["fake"], self.root, timeout=10), "probe_timeout")
        self.assertEqual(kill.call_args_list, [unittest.mock.call(child.pid, signal.SIGTERM), unittest.mock.call(child.pid, signal.SIGKILL)])
        self.assertEqual(record["returncode"], -signal.SIGTERM)
        self.assertEqual(record["normalized_exit"], 143)
        self.assertFalse(record["artifacts"][0]["complete"])

    def test_report_write_error_fails_closed(self):
        with patch.object(v, "ROOT", self.root), patch.object(v.Run, "save", side_effect=OSError(CANARY)), contextlib.redirect_stderr(io.StringIO()) as stderr:
            self.assertEqual(v.main(["run", "--check", "go-unit"]), 1)
        self.assertNotIn(CANARY, stderr.getvalue())

    def test_interrupt_terminates_process_group_finalizes(self):
        script = self.write_executable("interrupt", "import os, signal, time\nos.kill(os.getppid(), signal.SIGTERM)\ntime.sleep(60)\n")
        original = v.check_command
        def command(root, directory, check_id, *args):
            return ([str(script)], root) if check_id == "web-lint" else original(root, directory, check_id, *args)
        with patch.object(v, "check_command", command):
            run, code = self.execute(("web-lint",))
        self.assertEqual(code, 143)
        self.assertIsNone(run.child)
        self.assertIsNotNone(self.selected(run, "web-lint")["returncode"])
        self.assertEqual(run.report["runner_exit_code"], 143)

    def test_install_exact_modules_gobin_and_no_app_mutation(self):
        before = (self.root / "api/go.mod").read_bytes()
        with self.quiet():
            self.assertEqual(v.install_tools(tuple(v.TOOL_PINS), self.root), 0)
        ledger = [json.loads(line) for line in (self.root / "tmp/ledger").read_text().splitlines()]
        installs = [args for name, args in ledger if args[0] == "install"]
        self.assertEqual(installs, [["install", "gotest.tools/gotestsum@v1.13.0"], ["install", "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.1"]])
        self.assertEqual(before, (self.root / "api/go.mod").read_bytes())

    def test_install_explicit_local_gobin_and_setup_toolchain(self):
        original = v.Run.process
        environments = []
        def process(run, record, argv, cwd, env=None, timeout=None):
            if record["id"].startswith("install-"):
                environments.append(env)
            return original(run, record, argv, cwd, env, timeout)
        with patch.object(v.Run, "process", process), self.quiet():
            self.assertEqual(v.install_tools(("gotestsum",), self.root), 0)
        self.assertEqual(environments[0]["GOBIN"], str(self.root / "api/bin"))
        self.assertEqual(environments[0]["GOTOOLCHAIN"], "auto")
        self.assertEqual(environments[0]["GOENV"], "off")

    def test_install_failure_preserves_actual_exit(self):
        with self.quiet(), patch.dict(os.environ, {"INSTALL_EXIT": "17"}):
            self.assertEqual(v.install_tools(("gotestsum",), self.root), 17)

    def test_database_readonly_argv_private_identity(self):
        with patch.dict(os.environ, {"TEST_DATABASE_URL": f"postgres://privateuser:{CANARY}@127.0.0.1:5432/privatedb?sslmode=disable"}):
            run, code = self.execute(("go-integration",), kind="preflight")
        self.assertEqual(code, 0)
        record = next(p for p in run.report["preflight"] if p["id"] == "database")
        self.assertIn("SELECT current_setting", record["argv"][-1])
        self.assertNotIn(CANARY, run.report_path.read_text())
        self.assertNotIn("privateuser", run.report_path.read_text())
        self.assertNotIn("privatedb", run.report_path.read_text())
        self.assertEqual(run.report["environment"]["database_server_major"], 18)

    def test_database_permission_version_timeout_and_auth_blocks(self):
        for output, reason in (("180003|f", "database_permission_missing"), ("170001|t", "database_version_mismatch"), (CANARY, "database_unavailable")):
            with patch.dict(os.environ, {"TEST_DATABASE_URL": "postgres://user@localhost/db", "PSQL_RESULT": output}):
                run, code = self.execute(("go-integration",), kind="preflight")
            self.assertEqual(code, 1)
            self.assertIn(reason, run.report_path.read_text())
            self.assertNotIn(CANARY, run.report_path.read_text())

    def test_database_missing_psql_and_timeout_safe(self):
        run = v.Run(self.root, ())
        with patch.dict(os.environ, {"TEST_DATABASE_URL": "postgres://user@localhost/db"}), patch.object(run, "executable", side_effect=v.ValidationError("tool_missing")):
            self.assertFalse(run.probe("database", run.database))
        self.assertEqual(run.probes["database"]["reason"], "tool_missing")
        run = v.Run(self.root, ())
        with patch.dict(os.environ, {"TEST_DATABASE_URL": "postgres://user@localhost/db"}), patch.object(run, "probe_command", side_effect=v.ValidationError("probe_timeout")):
            self.assertFalse(run.probe("database", run.database))
        self.assertEqual(run.probes["database"]["reason"], "database_unavailable")

    def test_missing_database_config_blocks_not_skips(self):
        with patch.dict(os.environ, {"TEST_DATABASE_URL": ""}):
            run, code = self.execute(("go-integration",), kind="preflight")
        self.assertEqual(code, 1)
        self.assertEqual(self.selected(run, "go-integration")["status"], "blocked")
        self.assertIsNone(self.selected(run, "go-integration")["returncode"])

    def test_console_does_not_publish_private_child_output(self):
        with contextlib.redirect_stdout(io.StringIO()) as stdout:
            run = v.Run(self.root, ("go-unit",))
            self.assertEqual(run.run(), 0)
        self.assertNotIn(CANARY, stdout.getvalue())

    def test_browser_http_block_truthful_no_scenarios(self):
        with patch.object(v, "browser_http_probe", side_effect=v.ValidationError("browser_fixture_unavailable")):
            run, code = self.execute((), kind="preflight", browser=True)
        self.assertEqual(code, 1)
        self.assertTrue(all(c["returncode"] is None for c in run.report["checks"]))
        ids = [p["id"] for p in run.report["preflight"]]
        self.assertNotIn("npm", ids)
        self.assertNotIn("go", ids)
        chrome = next(p for p in run.report["preflight"] if p["id"] == "chrome")
        self.assertEqual(chrome["argv"][-1], "--version")

    def test_missing_chrome_and_websocket_blocks_browser_only(self):
        original = v.Run.probe_command
        def probe(run, record, *args, **kwargs):
            if record["id"] == "node-websocket":
                raise v.ValidationError("probe_failed")
            return original(run, record, *args, **kwargs)
        with patch.dict(os.environ, {"CHROME_BIN": "__missing_chrome__"}), patch.object(v.Run, "probe_command", probe), patch.object(v, "browser_http_probe"):
            run, code = self.execute((), kind="preflight", browser=True)
        self.assertEqual(code, 1)
        self.assertEqual(run.probes["chrome"]["reason"], "tool_missing")
        self.assertEqual(run.probes["node-websocket"]["status"], "blocked")


class EnvironmentTests(unittest.TestCase):
    def test_offline_override_and_optin_removal(self):
        inherited = {**{k: CANARY for k in v.OPT_INS}, "TEST_DATABASE_URL": CANARY, "GOFLAGS": "-tags=live", "GOPROXY": "direct", "GONOPROXY": "*", "GOTESTSUM_JSONFILE": CANARY, "GOTESTSUM_PACKAGES": CANARY}
        with patch.dict(os.environ, inherited):
            env = v.offline_environment()
            integration = v.offline_environment(True)
        self.assertEqual({k: env[k] for k in v.OFFLINE_GO}, v.OFFLINE_GO)
        self.assertEqual(env["npm_config_offline"], "true")
        for key in v.OPT_INS + ("TEST_DATABASE_URL", "GOTESTSUM_JSONFILE", "GOTESTSUM_PACKAGES"):
            self.assertNotIn(key, env)
        self.assertEqual(integration["TEST_DATABASE_URL"], CANARY)

    def test_database_rejects_missing_malformed_remote_options(self):
        for uri in (None, "", "postgres://user@remote/db", "postgres://user@localhost/", "postgres://localhost/db", "postgres://u@localhost/d?host=remote", "postgres://u@localhost/d?sslmode=disable&sslmode=require", "postgres://u@localhost/d#fragment", "postgres://u@localhost/d?sslmode=bogus", "postgres://u@localhost/d%zz", "postgres://u@localhost:0/d", "postgres://u@localhost:99999/d"):
            with self.subTest(uri=uri), self.assertRaises(v.ValidationError):
                v.database_environment(uri, {})

    def test_database_decoded_fields_clear_libpq_password_lookup(self):
        env = v.database_environment("postgresql://some%20user:p%40ss@[::1]:55439/accountstest?sslmode=disable", {"PGSERVICE": CANARY, "PGHOSTADDR": CANARY, "PGOPTIONS": CANARY, "PATH": "keep"})
        self.assertEqual(env["PGUSER"], "some user")
        self.assertEqual(env["PGPASSWORD"], "p@ss")
        self.assertEqual(env["PGHOST"], "::1")
        self.assertNotIn("PGHOSTADDR", env)
        self.assertNotIn("PGSERVICE", env)
        self.assertEqual(env["PGPASSFILE"], os.devnull)
        self.assertIn("default_transaction_read_only=on", env["PGOPTIONS"])

    def test_signal_exit_normalization(self):
        self.assertEqual(v.normalized_exit(-signal.SIGTERM), 143)
        self.assertEqual(v.normalized_exit(7), 7)


class BrowserTests(unittest.TestCase):
    def test_read_only_probe_shapes_no_body_or_cookie_persistence(self):
        class Response:
            status = 200
            headers = {"Cache-Control": "no-store", "Set-Cookie": CANARY}
            def __init__(self, body): self.body = body
            def __enter__(self): return self
            def __exit__(self, *args): return False
            def read(self, limit): return self.body
        opener = unittest.mock.Mock()
        opener.open.side_effect = [Response(b"ok"), Response(b'{"messages":[]}'), Response(b'{"enabled":true,"state":"anonymous","account":null}')]
        with patch.object(v.urllib.request, "build_opener", return_value=opener):
            self.assertIsNone(v.browser_http_probe())
        requests = [call.args[0] for call in opener.open.call_args_list]
        self.assertEqual([r.get_method() for r in requests], ["GET"] * 3)
        self.assertEqual(requests[1].get_header("X-browser-harness"), "1")
        self.assertTrue(all(r.get_header("Cookie") is None for r in requests))
        self.assertNotIn("shutdown", str([r.full_url for r in requests]))

    def test_redirect_forbidden(self):
        self.assertIsNone(v.NoRedirect().redirect_request(None, None, 302, "", {}, "http://elsewhere"))


class CommandContractTests(unittest.TestCase):
    def test_canonical_integration_selection_and_flags(self):
        args, cwd = v.check_command(ROOT, ROOT / "tmp/synthetic", "go-integration")
        self.assertEqual(cwd, ROOT / "api")
        self.assertEqual(args[args.index("--") + 1:], ["-tags=nodynamic", *v.INTEGRATION_PACKAGES, "-run", "Integration$", "-count=1"])
        self.assertEqual(len(v.INTEGRATION_PACKAGES), 10)

    def test_cli_usage_errors_no_arbitrary_execution(self):
        for args in (["run", "--check", "shell"], ["run", "--browser-accounts"], ["run", "--go-run", "Rating"], ["run", "--check", "go-integration", "--go-run", "Rating"], ["run", "--check", "go-unit", "--go-package", "./../elsewhere"], ["run", "--check", "go-unit", "--go-package", "/tmp/foo"], ["run", "--command", CANARY]):
            with self.subTest(args=args), contextlib.redirect_stderr(io.StringIO()) as stderr, self.assertRaises(SystemExit) as caught:
                v.main(args)
            self.assertEqual(caught.exception.code, 2)
            self.assertNotIn(CANARY, stderr.getvalue())

    def test_canonical_order_browser_only_and_explicit_addition(self):
        cases = ((["run", "--check", "build", "--check", "go-unit"], ("go-unit", "build"), False), (["preflight", "--browser-accounts"], (), True), (["preflight", "--browser-accounts", "--check", "web-unit"], ("web-unit",), True))
        for args, selection, browser in cases:
            with patch.object(v, "Run") as run:
                run.return_value.run.return_value = 0
                self.assertEqual(v.main(args), 0)
                self.assertEqual(run.call_args.args[1], selection)
                self.assertEqual(run.call_args.kwargs["browser"], browser)

    def test_make_and_ci_contract_preserves_existing_actions_and_checks(self):
        make = (ROOT / "Makefile").read_text()
        self.assertIn("check:\n\tPYTHONDONTWRITEBYTECODE=1 python3 scripts/validate.py run\n", make)
        self.assertNotIn("go run github.com/golangci", make)
        for target, check in (("test-go", "go-unit"), ("test-race", "go-race"), ("test-integration", "go-integration"), ("lint", "go-lint")):
            self.assertIn(f"{target}:\n\tPYTHONDONTWRITEBYTECODE=1 python3 scripts/validate.py run --check {check}", make)
        workflow = (ROOT / ".github/workflows/go.yml").read_text()
        for text in ("name: Go CI / checks", "name: Go CI / integration", "branches: [dev, main]", "contents: read", "actions/checkout@v5", "actions/setup-go@v6", "go vet ./...", "golang/govulncheck-action@v1", "golangci/golangci-lint-action@v9.3.0", "version: " + v.TOOL_PINS["golangci-lint"][1], "postgres:18-alpine", "command -v psql"):
            self.assertIn(text, workflow)
        self.assertEqual(workflow.count("run: go mod download"), 2)
        self.assertEqual(workflow.count("install-tools --tool gotestsum"), 2)
        self.assertIn("run: make test-go", workflow)
        self.assertIn("run: make test-race", workflow)
        self.assertIn("run: make test-integration", workflow)
        self.assertNotIn("upload-artifact", workflow)
        frontend = (ROOT / ".github/workflows/frontend.yml").read_text()
        self.assertLess(frontend.index("run typecheck"), frontend.index("run build"))
        release = (ROOT / ".github/workflows/release-tests.yml").read_text()
        self.assertIn("unittest discover -s scripts/tests", release)

    def test_make_failed_gofmt_substitution_not_hidden(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "api").mkdir()
            tools = root / "tools"
            tools.mkdir()
            for name, body in (("gofmt", "exit 19"), ("npm", "exit 0")):
                path = tools / name
                path.write_text("#!/bin/sh\n" + body + "\n")
                path.chmod(0o755)
            env = dict(os.environ, PATH=str(tools) + os.pathsep + os.environ["PATH"])
            result = subprocess.run(["make", "-f", str(ROOT / "Makefile"), "fmt-check"], cwd=root, env=env, capture_output=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("19", result.stderr.decode())

    def test_make_leaves_route_to_cli_without_recursive_aggregate(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            tools = root / "tools"
            tools.mkdir()
            python = tools / "python3"
            python.write_text(f"#!{sys.executable}\nimport json, os, sys\nfrom pathlib import Path\nwith Path('ledger').open('a') as f: f.write(json.dumps(sys.argv[1:]) + '\\n')\nsys.exit(int(os.environ.get('STUB_EXIT', '0')))\n")
            python.chmod(0o755)
            env = dict(os.environ, PATH=str(tools) + os.pathsep + os.environ["PATH"])
            for target, tail in (("check", ["run"]), ("preflight", ["preflight"]), ("test-go", ["run", "--check", "go-unit"]), ("test-race", ["run", "--check", "go-race"]), ("test-integration", ["run", "--check", "go-integration"]), ("lint", ["run", "--check", "go-lint"])):
                result = subprocess.run(["make", "-f", str(ROOT / "Makefile"), target], cwd=root, env=env, capture_output=True)
                self.assertEqual(result.returncode, 0)
                entries = (root / "ledger").read_text().splitlines()
                self.assertEqual(json.loads(entries[-1]), ["scripts/validate.py", *tail])
            env["STUB_EXIT"] = "17"
            result = subprocess.run(["make", "-f", str(ROOT / "Makefile"), "test-go"], cwd=root, env=env, capture_output=True)
            self.assertEqual(result.returncode, 2)
            self.assertIn("17", result.stderr.decode())


if __name__ == "__main__":
    unittest.main()
