import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
VALIDATION_WORKFLOW = ROOT / ".github/workflows/release-pr.yml"
PUBLICATION_WORKFLOW = ROOT / ".github/workflows/release.yml"


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
            'if [[ -f "$tool_path" && ! -L "$tool_path" ]]',
            "printf 'source=base",
            "exit 0",
            'if [[ -e "$tool_path" || -L "$tool_path" ]]',
            '[[ "$EVENT_NAME" == pull_request ]]',
            '[[ "$BASE_REF" == main ]]',
            '[[ "$HEAD_REF" == dev ]]',
            '[[ "$BASE_REPOSITORY" == "$EVENT_REPOSITORY" ]]',
            '[[ "$HEAD_REPOSITORY" == "$EVENT_REPOSITORY" ]]',
            '[[ "$HEAD_FORK" == false ]]',
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
        assert_order(
            self,
            selector,
            '[[ "$(git -C base-automation rev-parse HEAD)" == "$BASE_SHA" ]]',
            'if [[ -f "$tool_path" && ! -L "$tool_path" ]]',
            "printf 'source=base",
            "exit 0",
            'if [[ -e "$tool_path" || -L "$tool_path" ]]',
            '[[ "$PR_MERGED" == true ]]',
            '[[ "$BASE_REF" == main ]]',
            '[[ "$HEAD_REF" == dev ]]',
            '[[ "$HEAD_FORK" == false ]]',
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


if __name__ == "__main__":
    unittest.main()
