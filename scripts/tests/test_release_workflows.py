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


if __name__ == "__main__":
    unittest.main()
