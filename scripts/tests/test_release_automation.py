import base64
import io
import json
import os
import sys
import unittest
from pathlib import Path
from unittest import mock


sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

import release_automation as release


REPOSITORY = "MathisVerstrepen/MesSeances"
API = f"https://api.github.com/repos/{REPOSITORY}"
SHA = "a" * 40
HEAD_SHA = "b" * 40
VERSION = "0.7.0"
CHANGELOG_PATH = f"docs/changelogs/{VERSION}.md"
BODY = """## Changed

- Changed licensing details

## Added

- Added release automation

## Improved

- None

## Fixed

- Fixed image publication
"""


def pull(
    *,
    title=f"Release {VERSION}",
    body=BODY,
    state="closed",
    merged=True,
    sha=SHA,
    base="main",
    head="dev",
    base_repository=REPOSITORY,
    head_repository=REPOSITORY,
    head_sha=HEAD_SHA,
):
    return {
        "number": 12,
        "state": state,
        "merged": merged,
        "merged_at": "2026-08-28T15:04:05Z" if merged else None,
        "merge_commit_sha": sha,
        "title": title,
        "body": body,
        "base": {"ref": base, "repo": {"full_name": base_repository}},
        "head": {
            "ref": head,
            "sha": head_sha,
            "repo": {"full_name": head_repository},
        },
    }


def changelog(*, content=BODY.encode(), ref=SHA, **overrides):
    encoded = content if isinstance(content, str) else base64.encodebytes(content).decode("ascii")
    decoded_size = len(BODY.encode()) if isinstance(content, str) else len(content)
    item = {
        "type": "file",
        "encoding": "base64",
        "size": decoded_size,
        "name": f"{VERSION}.md",
        "path": CHANGELOG_PATH,
        "sha": "c" * 40,
        "content": encoded,
        "url": f"{API}/contents/{CHANGELOG_PATH}?ref={ref}",
    }
    item.update(overrides)
    return item


def changelog_request(ref):
    return f"/contents/{CHANGELOG_PATH}?ref={ref}"


def tree(*, mode="100644", sha="c" * 40, size=len(BODY.encode()), **overrides):
    response = {
        "sha": "d" * 40,
        "truncated": False,
        "tree": [
            {
                "path": CHANGELOG_PATH,
                "mode": mode,
                "type": "blob",
                "sha": sha,
                "size": size,
            }
        ],
    }
    response.update(overrides)
    return response


def tree_request(ref):
    return f"/git/trees/{ref}?recursive=1"


class FakeTransport:
    def __init__(self):
        self.responses = []
        self.calls = []

    def add(self, method, path, data=None, status=200, headers=None):
        body = b"" if data is None else json.dumps(data).encode()
        self.responses.append((method, path, release.Response(status, headers or {}, body)))

    def __call__(self, method, url, headers, body, timeout):
        self.calls.append(
            {
                "method": method,
                "path": url.removeprefix(API),
                "headers": headers,
                "payload": json.loads(body) if body else None,
                "timeout": timeout,
            }
        )
        if not self.responses:
            raise AssertionError(f"unexpected request: {method} {url}")
        expected_method, expected_path, response = self.responses.pop(0)
        if method != expected_method or url != API + expected_path:
            raise AssertionError(
                f"expected {expected_method} {expected_path}, "
                f"got {method} {url.removeprefix(API)}"
            )
        return response

    def assert_done(self):
        if self.responses:
            raise AssertionError(f"unused responses: {self.responses}")


class VersionTests(unittest.TestCase):
    def test_strict_version_parsing_and_rendering(self):
        parsed = release.Version.parse("12.34.56")
        self.assertEqual((parsed.major, parsed.minor, parsed.patch), (12, 34, 56))
        self.assertEqual(str(parsed), "12.34.56")

    def test_invalid_versions_are_rejected(self):
        invalid = (
            "v1.2.3-beta.1",
            "1.2.3-beta",
            "1.2.3-beta.01",
            "1.2.3-rc.1",
            "1.2.3+build.1",
            "01.2.3",
            "1.02.3",
            "1.2.03",
            "1.2.3-BETA.1",
            "1.2.3 ",
        )
        for value in invalid:
            with self.subTest(value=value), self.assertRaises(release.ReleaseError):
                release.Version.parse(value)

    def test_numeric_semver_ordering(self):
        versions = [
            release.Version.parse("1.9.9"),
            release.Version.parse("1.10.0"),
            release.Version.parse("1.10.10"),
        ]
        self.assertEqual(str(max(versions)), "1.10.10")
        self.assertGreater(
            release.Version.parse("1.10.0"),
            release.Version.parse("1.9.9"),
        )

    def test_empty_strict_tag_history_bootstraps(self):
        release.validate_version_progression(
            [
                {"name": "v9.0.0"},
                {"name": "1.0.0-beta.1"},
                {"name": "1.0.0+build.1"},
                {"name": "release-2025"},
            ],
            release.Version.parse("0.1.0"),
            allow_existing=False,
        )

    def test_progression_must_be_numerically_newer(self):
        tags = [{"name": "1.9.9"}, {"name": "1.8.100"}]
        release.validate_version_progression(
            tags, release.Version.parse("1.10.0"), allow_existing=False
        )
        with self.assertRaisesRegex(release.ReleaseError, "not newer"):
            release.validate_version_progression(
                tags, release.Version.parse("1.9.8"), allow_existing=False
            )

    def test_existing_candidate_only_allowed_for_reconciliation(self):
        tags = [{"name": VERSION}]
        with self.assertRaisesRegex(release.ReleaseError, "already has a tag"):
            release.validate_version_progression(
                tags, release.Version.parse(VERSION), allow_existing=False
            )
        release.validate_version_progression(
            tags, release.Version.parse(VERSION), allow_existing=True
        )


class MetadataTests(unittest.TestCase):
    def test_exact_title_is_accepted(self):
        metadata = release.parse_release_title("Release 0.7.0")
        self.assertEqual(str(metadata.version), VERSION)

    def test_title_format_and_version_are_strict(self):
        invalid = (
            "0.7.0 - 2026-08-28",
            "0.7.0",
            "release 0.7.0",
            "RELEASE 0.7.0",
            "Release0.7.0",
            "Release  0.7.0",
            "Release\t0.7.0",
            " Release 0.7.0",
            "Release v0.7.0",
            "Release 0.7.0-beta.1",
            "Release 0.7.0+build.1",
            "Release 01.7.0",
            "Release 0.07.0",
            "Release 0.7.00",
            "Release 0.7",
            "Release 0.7.0.1",
            "Release 0..7.0",
            "Release 0.7.0 ",
        )
        for title in invalid:
            with self.subTest(title=title), self.assertRaises(release.ReleaseError):
                release.parse_release_title(title)

    def test_body_validation_preserves_exact_text(self):
        self.assertIs(release.validate_release_body(BODY), BODY)

    def test_body_requires_ordered_sections_and_bullets(self):
        invalid = (
            "",
            BODY[:-1],
            BODY + "\n",
            BODY.replace("## Changed", "Changed"),
            BODY.replace("## Added", "## New"),
            BODY.replace("- Changed licensing details", "Changed licensing details"),
            BODY.replace("- Added release automation", ""),
            BODY.replace("- None", "- None\n- Also improved caching"),
            "Intro\n\n" + BODY,
            BODY + "\n## Security\n\n- None\n",
        )
        for body in invalid:
            with self.subTest(body=body), self.assertRaises(release.ReleaseError):
                release.validate_release_body(body)


class ClientTests(unittest.TestCase):
    def setUp(self):
        self.transport = FakeTransport()
        self.client = release.GitHubClient(REPOSITORY, "secret", self.transport)

    def test_pagination_stays_inside_repository_scope(self):
        next_url = API + "/tags?per_page=100&page=2"
        self.transport.add(
            "GET",
            "/tags?per_page=100",
            [{"name": "0.6.0"}],
            headers={"Link": f'<{next_url}>; rel="next"'},
        )
        self.transport.add("GET", "/tags?per_page=100&page=2", [{"name": VERSION}])
        self.assertEqual(len(self.client.paginate("/tags?per_page=100")), 2)
        self.transport.assert_done()

        self.transport.add(
            "GET",
            "/tags",
            [],
            headers={"Link": '<https://api.github.com/repos/other/repo/tags>; rel="next"'},
        )
        with self.assertRaisesRegex(release.ReleaseError, "escaped repository scope"):
            self.client.paginate("/tags")

    def test_api_errors_do_not_expose_token_or_response_body(self):
        self.transport.add(
            "POST", "/releases", {"message": "secret provider response"}, status=403
        )
        with self.assertRaises(release.GitHubError) as raised:
            self.client.request("POST", "/releases", {"body": "sensitive body"})
        message = str(raised.exception)
        self.assertEqual(raised.exception.status, 403)
        self.assertNotIn("secret", message)
        self.assertNotIn("sensitive", message)
        self.assertEqual(message, "GitHub POST /repos/MathisVerstrepen/MesSeances/releases failed with status 403")

    def test_invalid_json_response_fails_safely(self):
        self.transport.responses.append(
            ("GET", "/tags", release.Response(200, {}, b"not-json"))
        )
        with self.assertRaisesRegex(release.ReleaseError, "not valid JSON"):
            self.client.request("GET", "/tags")


class PullValidationTests(unittest.TestCase):
    def setUp(self):
        self.transport = FakeTransport()
        self.client = release.GitHubClient(REPOSITORY, "secret", self.transport)

    def test_valid_open_dev_to_main_pr_and_empty_history(self):
        self.transport.add("GET", "/pulls/12", pull(state="open", merged=False, sha=None))
        self.transport.add(
            "GET", changelog_request(HEAD_SHA), changelog(ref=HEAD_SHA)
        )
        self.transport.add("GET", tree_request(HEAD_SHA), tree())
        self.transport.add("GET", "/tags?per_page=100", [])
        self.assertEqual(
            release.validate_pull_request(self.client, 12), VERSION
        )
        self.transport.assert_done()

    def test_fork_or_wrong_branch_is_rejected_before_tag_read(self):
        unsafe = (
            pull(state="open", merged=False, head_repository="attacker/MesSeances"),
            pull(state="open", merged=False, head="release"),
            pull(state="open", merged=False, base="production"),
            pull(state="open", merged=False, base_repository="attacker/MesSeances"),
        )
        for candidate in unsafe:
            with self.subTest(candidate=candidate):
                transport = FakeTransport()
                client = release.GitHubClient(REPOSITORY, "secret", transport)
                transport.add("GET", "/pulls/12", candidate)
                with self.assertRaisesRegex(release.ReleaseError, "same-repository"):
                    release.validate_pull_request(client, 12)
                transport.assert_done()

    def test_stale_version_rejected_without_writes(self):
        self.transport.add("GET", "/pulls/12", pull(state="open", merged=False, sha=None))
        self.transport.add(
            "GET", changelog_request(HEAD_SHA), changelog(ref=HEAD_SHA)
        )
        self.transport.add("GET", tree_request(HEAD_SHA), tree())
        self.transport.add("GET", "/tags?per_page=100", [{"name": "0.8.0"}])
        with self.assertRaisesRegex(release.ReleaseError, "not newer"):
            release.validate_pull_request(self.client, 12)
        self.assertTrue(all(call["method"] == "GET" for call in self.transport.calls))

    def test_malformed_head_sha_is_rejected_before_changelog_read(self):
        self.transport.add(
            "GET", "/pulls/12", pull(state="open", merged=False, sha=None, head_sha="short")
        )
        with self.assertRaisesRegex(release.ReleaseError, "head commit SHA"):
            release.validate_pull_request(self.client, 12)
        self.transport.assert_done()

    def test_missing_changelog_is_rejected_before_tag_read(self):
        self.transport.add("GET", "/pulls/12", pull(state="open", merged=False, sha=None))
        self.transport.add(
            "GET", changelog_request(HEAD_SHA), {"message": "missing secret"}, status=404
        )
        with self.assertRaisesRegex(release.ReleaseError, "changelog is missing") as raised:
            release.validate_pull_request(self.client, 12)
        self.assertNotIn("secret", str(raised.exception))
        self.transport.assert_done()

    def test_invalid_changelog_responses_fail_closed(self):
        invalid = {
            "ambiguous list": [],
            "wrong type": changelog(ref=HEAD_SHA, type="symlink"),
            "submodule": changelog(
                ref=HEAD_SHA, submodule_git_url="https://github.com/other/repo.git"
            ),
            "wrong path": changelog(ref=HEAD_SHA, path="docs/changelogs/other.md"),
            "wrong name": changelog(ref=HEAD_SHA, name="other.md"),
            "wrong ref": changelog(
                ref=HEAD_SHA,
                url=f"{API}/contents/{CHANGELOG_PATH}?ref={'d' * 40}",
            ),
            "wrong response sha": changelog(ref=HEAD_SHA, sha="short"),
            "wrong encoding": changelog(ref=HEAD_SHA, encoding="utf-8"),
            "malformed base64": changelog(ref=HEAD_SHA, content="not base64***"),
            "invalid utf8": changelog(content=b"\xff\n", ref=HEAD_SHA),
            "crlf": changelog(content=BODY.replace("\n", "\r\n").encode(), ref=HEAD_SHA),
            "missing final newline": changelog(content=BODY[:-1].encode(), ref=HEAD_SHA),
            "extra final newline": changelog(content=(BODY + "\n").encode(), ref=HEAD_SHA),
            "content mismatch": changelog(
                content=BODY.replace("Changed licensing", "Changed legal").encode(),
                ref=HEAD_SHA,
            ),
            "oversized": changelog(
                ref=HEAD_SHA, size=release.MAX_CHANGELOG_BYTES + 1
            ),
            "truncated": changelog(ref=HEAD_SHA, size=len(BODY.encode()) + 1),
        }
        for name, item in invalid.items():
            with self.subTest(name=name):
                transport = FakeTransport()
                client = release.GitHubClient(REPOSITORY, "secret", transport)
                transport.add(
                    "GET", "/pulls/12", pull(state="open", merged=False, sha=None)
                )
                transport.add("GET", changelog_request(HEAD_SHA), item)
                with self.assertRaises(release.ReleaseError):
                    release.validate_pull_request(client, 12)
                self.assertTrue(all(call["method"] == "GET" for call in transport.calls))
                transport.assert_done()

    def test_tree_must_prove_one_ordinary_file_at_exact_head(self):
        invalid = {
            "ambiguous response": [],
            "truncated": tree(truncated=True),
            "missing path": tree(tree=[]),
            "duplicate path": tree(tree=tree()["tree"] * 2),
            "symlink mode": tree(mode="120000"),
            "submodule mode": tree(mode="160000"),
            "wrong blob": tree(sha="e" * 40),
            "wrong size": tree(size=len(BODY.encode()) + 1),
        }
        for name, tree_response in invalid.items():
            with self.subTest(name=name):
                transport = FakeTransport()
                client = release.GitHubClient(REPOSITORY, "secret", transport)
                transport.add(
                    "GET", "/pulls/12", pull(state="open", merged=False, sha=None)
                )
                transport.add(
                    "GET", changelog_request(HEAD_SHA), changelog(ref=HEAD_SHA)
                )
                transport.add("GET", tree_request(HEAD_SHA), tree_response)
                with self.assertRaises(release.ReleaseError):
                    release.validate_pull_request(client, 12)
                self.assertTrue(all(call["method"] == "GET" for call in transport.calls))
                transport.assert_done()


class PublishTests(unittest.TestCase):
    def setUp(self):
        self.transport = FakeTransport()
        self.client = release.GitHubClient(REPOSITORY, "secret", self.transport)

    def queue_validation(self, *, pr=None, tags=None):
        self.transport.add("GET", "/pulls/12", pr or pull())
        self.transport.add("GET", f"/commits/{SHA}", {"sha": SHA})
        self.transport.add("GET", changelog_request(SHA), changelog(ref=SHA))
        self.transport.add("GET", tree_request(SHA), tree())
        self.transport.add(
            "GET", "/tags?per_page=100", tags if tags is not None else []
        )

    def test_first_release_creates_lightweight_tag_and_stable_latest_release(self):
        self.queue_validation()
        self.transport.add("GET", f"/git/ref/tags/{VERSION}", {"message": "missing"}, status=404)
        self.transport.add("POST", "/git/refs", {"ref": f"refs/tags/{VERSION}"}, status=201)
        self.transport.add(
            "GET", f"/git/ref/tags/{VERSION}", {"object": {"type": "commit", "sha": SHA}}
        )
        self.transport.add("GET", f"/releases/tags/{VERSION}", {"message": "missing"}, status=404)
        self.transport.add("POST", "/releases", {"id": 8}, status=201)

        self.assertEqual(release.publish(self.client, 12), VERSION)
        writes = [call for call in self.transport.calls if call["method"] == "POST"]
        self.assertEqual(
            writes[0]["payload"], {"ref": f"refs/tags/{VERSION}", "sha": SHA}
        )
        self.assertEqual(
            writes[1]["payload"],
            {
                "tag_name": VERSION,
                "name": VERSION,
                "body": BODY,
                "draft": False,
                "prerelease": False,
                "generate_release_notes": False,
                "make_latest": "true",
            },
        )
        self.transport.assert_done()

    def test_same_target_tag_and_exact_release_are_idempotent(self):
        self.queue_validation(tags=[{"name": VERSION}])
        self.transport.add(
            "GET", f"/git/ref/tags/{VERSION}", {"object": {"type": "commit", "sha": SHA}}
        )
        existing = {"id": 8, **release._canonical_release(VERSION, BODY)}
        self.transport.add("GET", f"/releases/tags/{VERSION}", existing)
        self.transport.add("GET", "/releases/latest", {"id": 8})

        self.assertEqual(release.publish(self.client, 12), VERSION)
        self.assertTrue(all(call["method"] == "GET" for call in self.transport.calls))
        self.transport.assert_done()

    def test_mismatched_existing_tag_is_never_moved_or_deleted(self):
        self.queue_validation(tags=[{"name": VERSION}])
        self.transport.add(
            "GET",
            f"/git/ref/tags/{VERSION}",
            {"object": {"type": "commit", "sha": "b" * 40}},
        )
        with self.assertRaisesRegex(release.ReleaseError, "does not resolve"):
            release.publish(self.client, 12)
        self.assertTrue(all(call["method"] == "GET" for call in self.transport.calls))
        self.assertFalse(any("/releases" in call["path"] for call in self.transport.calls))

    def test_existing_release_is_reconciled_as_stable_and_latest(self):
        self.queue_validation(tags=[{"name": "0.6.9"}, {"name": VERSION}])
        self.transport.add(
            "GET", f"/git/ref/tags/{VERSION}", {"object": {"type": "commit", "sha": SHA}}
        )
        self.transport.add(
            "GET",
            f"/releases/tags/{VERSION}",
            {
                "id": 8,
                "tag_name": VERSION,
                "name": "wrong",
                "body": "wrong",
                "draft": True,
                "prerelease": True,
            },
        )
        self.transport.add("GET", "/releases/latest", {"id": 7})
        self.transport.add("PATCH", "/releases/8", {"id": 8})

        release.publish(self.client, 12)
        self.assertEqual(
            self.transport.calls[-1]["payload"],
            {
                "tag_name": VERSION,
                "name": VERSION,
                "body": BODY,
                "draft": False,
                "prerelease": False,
                "make_latest": "true",
            },
        )

    def test_create_races_are_reread_and_reconciled(self):
        self.queue_validation()
        self.transport.add("GET", f"/git/ref/tags/{VERSION}", {"message": "missing"}, status=404)
        self.transport.add("POST", "/git/refs", {"message": "exists"}, status=422)
        self.transport.add(
            "GET", f"/git/ref/tags/{VERSION}", {"object": {"type": "commit", "sha": SHA}}
        )
        self.transport.add("GET", f"/releases/tags/{VERSION}", {"message": "missing"}, status=404)
        self.transport.add("POST", "/releases", {"message": "exists"}, status=422)
        self.transport.add(
            "GET",
            f"/releases/tags/{VERSION}",
            {
                "id": 8,
                "tag_name": VERSION,
                "name": "old",
                "body": "old",
                "draft": True,
                "prerelease": True,
            },
        )
        self.transport.add("GET", "/releases/latest", {"id": 7})
        self.transport.add("PATCH", "/releases/8", {"id": 8})
        self.assertEqual(release.publish(self.client, 12), VERSION)

    def test_unsafe_or_unmerged_pr_fails_before_mutation(self):
        candidates = (
            pull(head_repository="attacker/MesSeances"),
            pull(state="open", merged=False),
            pull(sha="short"),
            pull(title="0.7.0 - 2026-08-28"),
            pull(body=BODY.replace("## Fixed", "## Repairs")),
        )
        for candidate in candidates:
            with self.subTest(candidate=candidate):
                transport = FakeTransport()
                client = release.GitHubClient(REPOSITORY, "secret", transport)
                transport.add("GET", "/pulls/12", candidate)
                with self.assertRaises(release.ReleaseError):
                    release.publish(client, 12)
                transport.assert_done()

    def test_invalid_merge_commit_response_fails_before_tag_read(self):
        self.transport.add("GET", "/pulls/12", pull())
        self.transport.add("GET", f"/commits/{SHA}", {"sha": "b" * 40})
        with self.assertRaisesRegex(release.ReleaseError, "merge commit response"):
            release.publish(self.client, 12)
        self.transport.assert_done()

    def test_changelog_mismatch_at_merge_commit_stops_before_any_write(self):
        private_file_text = "provider-secret-123"
        self.transport.add("GET", "/pulls/12", pull())
        self.transport.add("GET", f"/commits/{SHA}", {"sha": SHA})
        self.transport.add(
            "GET",
            changelog_request(SHA),
            changelog(content=BODY.replace("release automation", private_file_text).encode()),
        )
        with self.assertRaisesRegex(
            release.ReleaseError, "does not exactly match"
        ) as raised:
            release.publish(self.client, 12)
        self.assertNotIn(private_file_text, str(raised.exception))
        self.assertNotIn(BODY, str(raised.exception))
        self.assertTrue(all(call["method"] == "GET" for call in self.transport.calls))
        self.assertFalse(any("/git/ref" in call["path"] for call in self.transport.calls))
        self.transport.assert_done()

    def test_newer_tag_rejects_rerun_before_mutation(self):
        self.queue_validation(tags=[{"name": VERSION}, {"name": "0.7.1"}])
        with self.assertRaisesRegex(release.ReleaseError, "not newer"):
            release.publish(self.client, 12)
        self.assertTrue(all(call["method"] == "GET" for call in self.transport.calls))


class PromotionTests(unittest.TestCase):
    def test_only_current_newest_strict_tag_can_promote(self):
        transport = FakeTransport()
        client = release.GitHubClient(REPOSITORY, "secret", transport)
        transport.add(
            "GET",
            "/tags?per_page=100",
            [
                {"name": VERSION},
                {"name": "v9.0.0"},
                {"name": "1.0.0-beta.1"},
                {"name": "release-2025"},
            ],
        )
        self.assertEqual(release.verify_promotion(client, VERSION), VERSION)
        self.assertTrue(all(call["method"] == "GET" for call in transport.calls))

        transport = FakeTransport()
        client = release.GitHubClient(REPOSITORY, "secret", transport)
        transport.add(
            "GET", "/tags?per_page=100", [{"name": VERSION}, {"name": "0.7.1"}]
        )
        with self.assertRaisesRegex(release.ReleaseError, "stale"):
            release.verify_promotion(client, VERSION)


class CliTests(unittest.TestCase):
    def test_missing_token_exits_nonzero_without_network(self):
        stderr = io.StringIO()
        with mock.patch.dict(os.environ, {}, clear=True), mock.patch.object(
            release, "urllib_transport", side_effect=AssertionError("network called")
        ), mock.patch("sys.stderr", stderr):
            result = release.main(
                [
                    "validate-pr",
                    "--repository",
                    REPOSITORY,
                    "--pull-request-number",
                    "12",
                ]
            )
        self.assertEqual(result, 1)
        self.assertIn("RELEASE_TOKEN is required", stderr.getvalue())

    def test_success_prints_version_and_failure_is_safe(self):
        stdout = io.StringIO()
        client = mock.Mock()
        with mock.patch.object(release, "GitHubClient", return_value=client), mock.patch.object(
            release, "validate_pull_request", return_value=VERSION
        ), mock.patch.dict(os.environ, {"RELEASE_TOKEN": "secret"}, clear=True), mock.patch(
            "sys.stdout", stdout
        ):
            result = release.main(
                [
                    "validate-pr",
                    "--repository",
                    REPOSITORY,
                    "--pull-request-number",
                    "12",
                ]
            )
        self.assertEqual(result, 0)
        self.assertEqual(stdout.getvalue(), VERSION + "\n")

        stderr = io.StringIO()
        with mock.patch.object(release, "GitHubClient", return_value=client), mock.patch.object(
            release, "verify_promotion", side_effect=release.ReleaseError("stale release")
        ), mock.patch.dict(os.environ, {"RELEASE_TOKEN": "secret"}, clear=True), mock.patch(
            "sys.stderr", stderr
        ):
            result = release.main(
                [
                    "verify-promotion",
                    "--repository",
                    REPOSITORY,
                    "--version",
                    VERSION,
                ]
            )
        self.assertEqual(result, 1)
        self.assertEqual(
            stderr.getvalue(), "release automation failed: stale release\n"
        )


if __name__ == "__main__":
    unittest.main()
