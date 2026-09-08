import json
import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
VALIDATION_WORKFLOW = ROOT / ".github/workflows/release-pr.yml"
PUBLICATION_WORKFLOW = ROOT / ".github/workflows/release.yml"
BASE_SHA = "a" * 40
HEAD_SHA = "b" * 40
MERGE_SHA = "c" * 40
REPOSITORY = "MathisVerstrepen/MesSeances"
LEGACY_TOOL = '''import argparse

def main(argv):
    parser = argparse.ArgumentParser()
    commands = parser.add_subparsers(dest="command", required=True)
    publish = commands.add_parser("publish")
    publish.add_argument("--repository", required=True)
    publish.add_argument("--pull-request-number", required=True, type=int)
    parser.parse_args(argv)
    raise AssertionError("legacy publication must not execute")
'''


def step_scripts(workflow: str, step_name: str) -> list[str]:
    lines = workflow.splitlines()
    scripts = []
    marker = f"      - name: {step_name}"
    for index, line in enumerate(lines):
        if line != marker:
            continue
        run_index = lines.index("        run: |", index + 1)
        block = []
        for block_line in lines[run_index + 1 :]:
            if block_line and not block_line.startswith("          "):
                break
            block.append(block_line[10:] if block_line else "")
        scripts.append("\n".join(block))
    return scripts


def assert_order(test: unittest.TestCase, text: str, *fragments: str) -> None:
    positions = [text.index(fragment) for fragment in fragments]
    test.assertEqual(positions, sorted(positions))


class ReleaseWorkflowBootstrapTests(unittest.TestCase):
    def test_validation_prefers_base_and_constrains_absent_base_fallback(self):
        workflow = VALIDATION_WORKFLOW.read_text(encoding="utf-8")
        selector = step_scripts(workflow, "Select release automation source")
        self.assertEqual(len(selector), 1)
        assert_order(
            self,
            selector[0],
            '[[ "$(git -C base-automation rev-parse HEAD)" == "$BASE_SHA" ]]',
            '[[ "$EVENT_NAME" == pull_request ]]',
            '[[ "$BASE_REF" == main ]]',
            '[[ "$HEAD_REF" == dev ]]',
            '[[ "$BASE_REPOSITORY" == "$EVENT_REPOSITORY" ]]',
            '[[ "$HEAD_REPOSITORY" == "$EVENT_REPOSITORY" ]]',
            '[[ "$HEAD_FORK" == false ]]',
            'if [[ -f "$tool_path" && ! -L "$tool_path" ]]',
            "printf 'source=base",
            "exit 0",
            'if [[ -e "$tool_path" || -L "$tool_path" ]]',
            "printf 'source=head",
        )
        self.assertIn('[[ "$HEAD_SHA" =~ $sha_pattern ]]', selector[0])
        assert_order(
            self,
            workflow,
            "ref: ${{ steps.automation.outputs.ref }}",
            "- name: Verify selected release automation",
            "- name: Set up Python",
            "python release-automation/scripts/release_automation.py validate-pr",
        )

    def test_finalize_and_promotion_use_identical_fail_closed_selection(self):
        workflow = PUBLICATION_WORKFLOW.read_text(encoding="utf-8")
        selectors = step_scripts(workflow, "Select release automation source")
        self.assertEqual(len(selectors), 2)
        self.assertEqual(selectors[0], selectors[1])
        selector = selectors[0]
        self.assertNotIn("source=head", workflow)
        self.assertNotIn("source=base", workflow)
        self.assertNotIn("tool_path=base-automation", selector)
        assert_order(
            self,
            selector,
            '[[ "$(git -C base-automation rev-parse HEAD)" == "$BASE_SHA" ]]',
            '[[ "$PR_MERGED" == true ]]',
            '[[ "$BASE_REF" == main ]]',
            '[[ "$HEAD_REF" == dev ]]',
            '[[ "$HEAD_FORK" == false ]]',
            '[[ "$WORKFLOW_SHA" =~ $sha_pattern ]]',
            '[[ "$WORKFLOW_SHA" == "$MERGE_SHA" ]]',
            'pr_json="$(gh api',
            '.base.sha == $base_sha and .head.sha == $head_sha',
            '.merge_commit_sha == $merge_sha',
            'main_json="$(gh api',
            '.protected == true and .commit.sha == $merge_sha',
            "printf 'source=merge",
        )
        self.assertEqual(
            workflow.count("ref: ${{ steps.automation.outputs.ref }}"), 2
        )
        self.assertEqual(
            workflow.count("- name: Verify selected release automation"), 2
        )
        self.assertEqual(workflow.count("- name: Set up Python"), 2)
        sections = (
            workflow.split("  finalize:\n", 1)[1].split("\n  build:\n", 1)[0],
            workflow.split("  promote:\n", 1)[1],
        )
        for section in sections:
            self.assertIn("WORKFLOW_SHA: ${{ github.workflow_sha }}", section)
            self.assertEqual(section.count("persist-credentials: false"), 2)
            assert_order(
                self,
                section,
                "printf 'source=merge",
                "ref: ${{ steps.automation.outputs.ref }}",
                "- name: Verify selected release automation",
                "- name: Set up Python",
                "python release-automation/scripts/release_automation.py",
            )

    def test_permissions_image_source_and_promotion_order_remain_strict(self):
        workflow = PUBLICATION_WORKFLOW.read_text(encoding="utf-8")
        self.assertIn("permissions: {}", workflow)
        self.assertEqual(workflow.count("contents: write"), 1)
        self.assertIn("needs: finalize", workflow)
        self.assertIn("needs: [finalize, build]", workflow)
        self.assertIn("types: [closed]", workflow)
        self.assertIn("cancel-in-progress: false", workflow)
        for text in (workflow, VALIDATION_WORKFLOW.read_text()):
            for forbidden in ("workflow_dispatch:", "pull_request_target:", "deployment:"):
                self.assertNotIn(forbidden, text)
        self.assertEqual(workflow.count("packages: write"), 2)
        self.assertEqual(
            workflow.count("ref: ${{ github.event.pull_request.merge_commit_sha }}"),
            1,
        )
        assert_order(
            self,
            workflow,
            "- name: Verify tag remains newest",
            "- name: Verify both versioned manifests",
            "- name: Repoint latest aliases without rebuilding",
        )
        manifests = step_scripts(workflow, "Verify both versioned manifests")[0]
        self.assertIn('inspect "$IMAGE_BASE-api:$RELEASE_VERSION"', manifests)
        self.assertIn('inspect "$IMAGE_BASE-web:$RELEASE_VERSION"', manifests)

    def test_publication_cli_is_bound_to_event_shas(self):
        workflow = PUBLICATION_WORKFLOW.read_text()
        step = workflow.split("      - name: Revalidate and publish GitHub release\n", 1)[1].split("\n  build:", 1)[0]
        for variable, field in (("BASE_SHA", "base.sha"), ("HEAD_SHA", "head.sha"),
                                ("MERGE_SHA", "merge_commit_sha")):
            self.assertIn(f"{variable}: ${{{{ github.event.pull_request.{field} }}}}", step)
            self.assertIn(f'--{variable.lower().replace("_", "-")} "${variable}"', step)

    def test_release_test_ci_has_read_only_unfiltered_root_suite(self):
        workflow = (ROOT / ".github/workflows/release-tests.yml").read_text()
        self.assertIn("  push:\n    branches: [dev, main]", workflow)
        self.assertIn("  pull_request:\n    branches: [dev, main]", workflow)
        self.assertIn("permissions:\n  contents: read", workflow)
        self.assertEqual(workflow.count("permissions:"), 1)
        self.assertIn("name: Release automation / tests", workflow)
        self.assertIn("timeout-minutes: 5", workflow)
        self.assertIn("persist-credentials: false", workflow)
        self.assertIn('python-version: "3.13"', workflow)
        self.assertIn("working-directory: .\n", workflow)
        self.assertIn("run: python -m unittest discover -s scripts/tests", workflow)
        for forbidden in ("write", "secrets.", "workflow_dispatch:", "pull_request_target:",
                          "paths:", "paths-ignore:", "types:"):
            self.assertNotIn(forbidden, workflow)

    def test_web_build_uses_only_validated_finalize_version_as_build_arg(self):
        workflow = PUBLICATION_WORKFLOW.read_text(encoding="utf-8")
        build = workflow.split("  build:\n", 1)[1].split("\n  promote:\n", 1)[0]
        api_matrix = build.split("          - suffix: api\n", 1)[1].split(
            "          - suffix: web\n", 1
        )[0]
        web_matrix = build.split("          - suffix: web\n", 1)[1].split(
            "    steps:\n", 1
        )[0]

        self.assertIn("dockerfile: deploy/Dockerfile.api", api_matrix)
        self.assertIn('build-args: ""', api_matrix)
        self.assertNotIn("RELEASE_VERSION", api_matrix)
        self.assertIn("dockerfile: deploy/Dockerfile.web", web_matrix)
        self.assertIn(
            "build-args: RELEASE_VERSION=${{ needs.finalize.outputs.version }}",
            web_matrix,
        )
        self.assertEqual(build.count("build-args: ${{ matrix.build-args }}"), 1)
        self.assertNotIn("github.event.pull_request.title", build)

    def test_web_build_fails_closed_without_strict_finalize_version(self):
        workflow = PUBLICATION_WORKFLOW.read_text(encoding="utf-8")
        build = workflow.split("  build:\n", 1)[1].split("\n  promote:\n", 1)[0]
        guard_step = build.split("      - name: Require web release version\n", 1)[
            1
        ].split("\n      - name:", 1)[0]
        guard_scripts = step_scripts(workflow, "Require web release version")

        self.assertEqual(len(guard_scripts), 1)
        self.assertIn("if: matrix.suffix == 'web'", guard_step)
        self.assertIn(
            "RELEASE_VERSION: ${{ needs.finalize.outputs.version }}", guard_step
        )
        self.assertIn("set -euo pipefail", guard_scripts[0])
        self.assertIn(
            '[[ "$RELEASE_VERSION" =~ ^(0|[1-9][0-9]*)\\.'
            '(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)$ ]]',
            guard_scripts[0],
        )
        assert_order(
            self,
            build,
            "- name: Require web release version",
            "- name: Build and push versioned image",
        )


class SelectorExecutionTests(unittest.TestCase):
    """Run actual workflow Bash with no possible access to real git/gh credentials."""

    def run_script(self, script, *, privileged=True, tool="regular", selected_tool="regular", changes=None,
                   pr_changes=None, main_changes=None, api_failure="", raw_pr=None,
                   raw_main=None):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            bin_path = root / "bin"
            bin_path.mkdir()
            for name in ("bash", "jq"):
                executable = shutil.which(name)
                self.assertIsNotNone(executable, f"required shell test runtime missing: {name}")
                (bin_path / name).symlink_to(executable)
            # PATH contains only these stubs plus real Bash/jq. Never inherit HOME,
            # tokens, git config, GH_HOST, BASH_ENV, or credential helpers.
            stubs = {
                "git": '''#!/bin/bash
set -euo pipefail
printf 'git %s\\n' "$*" >> "$CALLS"
[[ "$1" == -C && "$3" == rev-parse && "$4" == HEAD && "$#" == 4 ]]
case "$2" in
  base-automation) printf '%s\\n' "$CHECKOUT_BASE" ;;
  release-automation) printf '%s\\n' "$CHECKOUT_SELECTED" ;;
  *) exit 91 ;;
esac
''',
                "gh": '''#!/bin/bash
set -euo pipefail
printf 'gh %s\\n' "$*" >> "$CALLS"
[[ "$#" == 2 && "$1" == api ]]
[[ "$2" != "$API_FAILURE" ]] || exit 92
case "$2" in
  "repos/$EVENT_REPOSITORY/pulls/12") printf '%s\\n' "$PR_JSON" ;;
  "repos/$EVENT_REPOSITORY/branches/main") printf '%s\\n' "$MAIN_JSON" ;;
  *) exit 93 ;;
esac
''',
                "python": f'''#!{sys.executable}
import importlib.util
import json
import os
import sys
from unittest import mock

def record(value):
    with open(os.environ["CALLS"], "a") as calls:
        calls.write(value + "\\n")

record("python " + json.dumps(sys.argv[1:]))
spec = importlib.util.spec_from_file_location("selected_release", sys.argv[1])
module = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = module
spec.loader.exec_module(module)

def publish(client, number, **shas):
    record("publish " + json.dumps({{"number": number, **shas}}, sort_keys=True))
    return "1.2.3"

with mock.patch.object(module, "GitHubClient", create=True), mock.patch.object(
    module, "publish", side_effect=publish, create=True
), mock.patch.object(module, "urllib_transport", create=True,
                     side_effect=AssertionError("network called")):
    sys.exit(module.main(sys.argv[2:]))
''',
            }
            for name, content in stubs.items():
                path = bin_path / name
                path.write_text(content)
                path.chmod(0o700)
            for checkout, shape in (("base-automation", tool), ("release-automation", selected_tool)):
                path = root / checkout / "scripts/release_automation.py"
                path.parent.mkdir(parents=True)
                if shape == "regular":
                    path.write_text("raise AssertionError('tooling must not execute')\n")
                elif shape == "legacy":
                    path.write_text(LEGACY_TOOL)
                elif shape == "current":
                    path.write_text((ROOT / "scripts/release_automation.py").read_text())
                elif shape == "directory":
                    path.mkdir()
                elif shape == "symlink":
                    path.symlink_to(root / "missing")
                else:
                    self.assertEqual(shape, "absent")
            pr = {
                "number": 12, "state": "closed", "merged": True,
                "base": {"ref": "main", "sha": BASE_SHA, "repo": {"full_name": REPOSITORY}},
                "head": {"ref": "dev", "sha": HEAD_SHA,
                         "repo": {"full_name": REPOSITORY, "fork": False}},
                "merge_commit_sha": MERGE_SHA,
            }
            main = {"name": "main", "protected": True, "commit": {"sha": MERGE_SHA}}
            for target, updates in ((pr, pr_changes), (main, main_changes)):
                for key, value in (updates or {}).items():
                    cursor = target
                    parts = key.split(".")
                    for part in parts[:-1]:
                        cursor = cursor[part]
                    cursor[parts[-1]] = value
            env = {
                "PATH": str(bin_path), "HOME": directory, "LC_ALL": "C",
                "EVENT_NAME": "pull_request", "EVENT_ACTION": "closed" if privileged else "opened",
                "EVENT_REPOSITORY": REPOSITORY, "PR_NUMBER": "12",
                "PR_STATE": "closed", "PR_MERGED": "true",
                "EVENT_STATE": "open", "EVENT_MERGED": "false",
                "BASE_REPOSITORY": REPOSITORY, "HEAD_REPOSITORY": REPOSITORY,
                "BASE_REF": "main", "HEAD_REF": "dev", "HEAD_FORK": "false",
                "BASE_SHA": BASE_SHA, "HEAD_SHA": HEAD_SHA, "MERGE_SHA": MERGE_SHA,
                "WORKFLOW_SHA": MERGE_SHA,
                "CHECKOUT_BASE": BASE_SHA, "CHECKOUT_SELECTED": MERGE_SHA if privileged else BASE_SHA,
                "AUTOMATION_REF": MERGE_SHA if privileged else BASE_SHA,
                "AUTOMATION_SOURCE": "merge" if privileged else "base",
                "RELEASE_REPOSITORY": REPOSITORY, "RELEASE_PULL_REQUEST": "12",
                "PYTHONDONTWRITEBYTECODE": "1",
                "GITHUB_OUTPUT": str(root / "output"), "CALLS": str(root / "calls"),
                "PR_JSON": json.dumps(pr) if raw_pr is None else raw_pr,
                "MAIN_JSON": json.dumps(main) if raw_main is None else raw_main,
                "API_FAILURE": api_failure,
            }
            env.update(changes or {})
            env = {key: value for key, value in env.items() if value is not None}
            result = subprocess.run([str(bin_path / "bash"), "--noprofile", "--norc", "-c", script],
                                    cwd=root, env=env, text=True, capture_output=True, timeout=5)
            output = root / "output"
            calls = root / "calls"
            return result, output.read_text() if output.exists() else "", calls.read_text() if calls.exists() else ""

    def selectors(self):
        for workflow, privileged in ((VALIDATION_WORKFLOW, False), (PUBLICATION_WORKFLOW, True)):
            for script in step_scripts(workflow.read_text(), "Select release automation source"):
                yield script, privileged

    def assert_rejected(self, script, **kwargs):
        result, output, calls = self.run_script(script, **kwargs)
        self.assertNotEqual(result.returncode, 0, result.stderr)
        self.assertEqual(output, "", calls)
        self.assertNotIn("python ", calls)
        return calls

    def test_valid_sources(self):
        for script, privileged in self.selectors():
            tools = ("regular", "legacy", "absent", "directory", "symlink") if privileged else ("regular", "absent")
            for tool in tools:
                with self.subTest(privileged=privileged, tool=tool):
                    result, output, calls = self.run_script(script, privileged=privileged, tool=tool)
                    self.assertEqual(result.returncode, 0, result.stderr)
                    source, sha = ("merge", MERGE_SHA) if privileged else (
                        ("base", BASE_SHA) if tool == "regular" else ("head", HEAD_SHA))
                    self.assertEqual(output, f"source={source}\nref={sha}\n")
                    self.assertEqual(calls.count("gh api"), 2 if privileged else 0)

    def test_event_mismatches_reject_present_and_absent_base(self):
        common = {
            "EVENT_NAME": "workflow_dispatch", "EVENT_ACTION": "deleted",
            "EVENT_REPOSITORY": "invalid", "BASE_REPOSITORY": "other/repo",
            "HEAD_REPOSITORY": "other/repo", "BASE_REF": "dev", "HEAD_REF": "main",
            "HEAD_FORK": "true", "BASE_SHA": "short", "HEAD_SHA": "short",
            "CHECKOUT_BASE": HEAD_SHA,
        }
        for script, privileged in self.selectors():
            bad = dict(common)
            bad.update({"PR_NUMBER": "true", "PR_STATE": "open", "PR_MERGED": "false",
                        "MERGE_SHA": "short"} if privileged else
                       {"EVENT_STATE": "closed", "EVENT_MERGED": "true"})
            for tool in ("regular", "absent"):
                for key, value in bad.items():
                    for invalid in (value, ""):
                        with self.subTest(privileged=privileged, tool=tool, field=key, value=invalid):
                            calls = self.assert_rejected(script, privileged=privileged, tool=tool,
                                                         changes={key: invalid})
                            self.assertNotIn("gh api", calls)

    def test_workflow_revision_must_match_merge_before_api_reads(self):
        for script, privileged in self.selectors():
            if not privileged:
                continue
            for tool in ("regular", "legacy", "absent", "directory", "symlink"):
                for value in (None, "", "short", "g" * 40, MERGE_SHA.upper(), BASE_SHA, HEAD_SHA):
                    with self.subTest(tool=tool, workflow_sha=value):
                        calls = self.assert_rejected(script, tool=tool, changes={"WORKFLOW_SHA": value})
                        self.assertNotIn("gh api", calls)

    def test_fresh_api_identity_and_main_must_match_with_present_or_absent_base(self):
        bad_pr = {
            "number": [13, True, "12", None], "state": ["open", None], "merged": [False, "true", 1, None],
            "base.ref": ["dev", None], "head.ref": ["main", None],
            "base.repo.full_name": ["other/repo", None], "head.repo.full_name": ["other/repo", None],
            "head.repo.fork": [True, "false", 0, None], "base.sha": [HEAD_SHA, None],
            "head.sha": [BASE_SHA, None], "merge_commit_sha": [HEAD_SHA, None],
        }
        bad_main = {"name": ["dev", None], "protected": [False, "true", 1, None],
                    "commit.sha": [HEAD_SHA, None]}
        for script, privileged in self.selectors():
            if not privileged:
                continue
            for tool in ("regular", "absent"):
                for parameter, invalid in (("pr_changes", bad_pr), ("main_changes", bad_main)):
                    for key, values in invalid.items():
                        for value in values:
                            with self.subTest(tool=tool, parameter=parameter, field=key, value=value):
                                self.assert_rejected(script, tool=tool, **{parameter: {key: value}})
                for parameter in ("raw_pr", "raw_main"):
                    for value in ("not-json", "null", "[]", "{}"):
                        with self.subTest(tool=tool, parameter=parameter, value=value):
                            self.assert_rejected(script, tool=tool, **{parameter: value})
                for endpoint in ("pulls/12", "branches/main"):
                    self.assert_rejected(script, tool=tool, api_failure=f"repos/{REPOSITORY}/{endpoint}")

    def test_validation_unsafe_present_paths_never_fall_back(self):
        for script, privileged in self.selectors():
            if privileged:
                continue
            for tool in ("directory", "symlink"):
                with self.subTest(privileged=privileged, tool=tool):
                    self.assert_rejected(script, privileged=privileged, tool=tool)

    def test_selected_checkout_and_source_are_verified_before_execution(self):
        for workflow in (VALIDATION_WORKFLOW, PUBLICATION_WORKFLOW):
            privileged = workflow == PUBLICATION_WORKFLOW
            for script in step_scripts(workflow.read_text(), "Verify selected release automation"):
                result, _, _ = self.run_script(script, privileged=privileged)
                self.assertEqual(result.returncode, 0, result.stderr)
                source, sha = ("head", HEAD_SHA) if workflow == VALIDATION_WORKFLOW else ("merge", MERGE_SHA)
                result, _, _ = self.run_script(script, privileged=privileged, changes={"AUTOMATION_SOURCE": source,
                                                               "AUTOMATION_REF": sha,
                                                               "CHECKOUT_SELECTED": sha})
                self.assertEqual(result.returncode, 0, result.stderr)
                bad = [{"CHECKOUT_SELECTED": HEAD_SHA}, {"AUTOMATION_REF": "main"},
                       {"AUTOMATION_REF": ""}, {"AUTOMATION_SOURCE": "unknown"},
                       {"AUTOMATION_SOURCE": ""}]
                if privileged:
                    bad.extend([
                        {"AUTOMATION_SOURCE": "base", "AUTOMATION_REF": BASE_SHA,
                         "CHECKOUT_SELECTED": BASE_SHA},
                        {"AUTOMATION_SOURCE": "head", "AUTOMATION_REF": HEAD_SHA,
                         "CHECKOUT_SELECTED": HEAD_SHA},
                        {"AUTOMATION_SOURCE": "merge", "AUTOMATION_REF": BASE_SHA,
                         "CHECKOUT_SELECTED": BASE_SHA},
                        {"AUTOMATION_SOURCE": "merge", "AUTOMATION_REF": "main",
                         "CHECKOUT_SELECTED": "main"},
                    ])
                for changes in bad:
                    self.assert_rejected(script, privileged=privileged, changes=changes)
                for tool in ("absent", "symlink", "directory"):
                    self.assert_rejected(script, privileged=privileged, selected_tool=tool)

    def test_legacy_base_cannot_override_merge_publisher_or_its_sha_arguments(self):
        workflow = PUBLICATION_WORKFLOW.read_text()
        selectors = step_scripts(workflow, "Select release automation source")
        verifiers = step_scripts(workflow, "Verify selected release automation")
        invocation = step_scripts(workflow, "Revalidate and publish GitHub release")[0]

        # The legacy base really lacks the SHA options used by the workflow.
        result, output, calls = self.run_script(
            "set -euo pipefail\n" + invocation.replace("release-automation/", "base-automation/"),
            tool="legacy",
        )
        self.assertEqual(result.returncode, 2, result.stderr)
        self.assertIn("unrecognized arguments: --base-sha", result.stderr)
        self.assertEqual(output, "")
        self.assertNotIn("publish ", calls)

        for selector, verifier in zip(selectors, verifiers, strict=True):
            # Simulate checkout of the selector's exact output, not a fixed fixture SHA.
            script = "\n".join((selector, '''
source "$GITHUB_OUTPUT"
AUTOMATION_SOURCE="$source"
AUTOMATION_REF="$ref"
CHECKOUT_SELECTED="$ref"
export CHECKOUT_SELECTED
''', verifier, invocation))
            result, output, calls = self.run_script(script, tool="legacy", selected_tool="current")
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(output, f"source=merge\nref={MERGE_SHA}\nversion=1.2.3\n")
            python_calls = [json.loads(line.removeprefix("python ")) for line in calls.splitlines()
                            if line.startswith("python ")]
            self.assertEqual(python_calls, [[
                "release-automation/scripts/release_automation.py", "publish",
                "--repository", REPOSITORY, "--pull-request-number", "12",
                "--base-sha", BASE_SHA, "--head-sha", HEAD_SHA, "--merge-sha", MERGE_SHA,
            ]])
            published = [json.loads(line.removeprefix("publish ")) for line in calls.splitlines()
                         if line.startswith("publish ")]
            self.assertEqual(published, [{"number": 12, "base_sha": BASE_SHA,
                                          "head_sha": HEAD_SHA, "merge_sha": MERGE_SHA}])


if __name__ == "__main__":
    unittest.main()
