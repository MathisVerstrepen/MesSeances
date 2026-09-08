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
BASE_SHA = "e" * 40
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
        "base": {"ref": base, "sha": BASE_SHA, "repo": {"full_name": base_repository}},
        "head": {
            "ref": head,
            "sha": head_sha,
            "repo": {"full_name": head_repository, "fork": False},
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

    def test_existing_candidate_only_allowed_for_immutable_validation(self):
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

    def publish(self, **overrides):
        arguments = {"base_sha": BASE_SHA, "head_sha": HEAD_SHA, "merge_sha": SHA}
        arguments.update(overrides)
        return release.publish(self.client, 12, **arguments)

    def queue_bound(self, *, pr=None, main=None):
        self.transport.add("GET", "/pulls/12", pull() if pr is None else pr)
        self.transport.add("GET", "/branches/main", main if main is not None else
                           {"name": "main", "protected": True, "commit": {"sha": SHA}})

    def queue_validation(self, *, pr=None, tags=None):
        self.queue_bound(pr=pr)
        self.transport.add("GET", f"/commits/{SHA}", {"sha": SHA})
        self.transport.add("GET", changelog_request(SHA), changelog(ref=SHA))
        self.transport.add("GET", tree_request(SHA), tree())
        self.transport.add(
            "GET", "/tags?per_page=100", tags if tags is not None else []
        )

    def tag(self, **overrides):
        return {"ref": f"refs/tags/{VERSION}", "object": {"type": "commit", "sha": SHA}, **overrides}

    def record(self, **overrides):
        return {"id": 8, "tag_name": VERSION, "name": VERSION, "body": BODY,
                "draft": False, "prerelease": False, **overrides}

    def queue_tag(self, value):
        self.transport.add("GET", f"/git/ref/tags/{VERSION}", value, status=404 if value is None else 200)

    def queue_record(self, value):
        self.transport.add("GET", f"/releases/tags/{VERSION}", value, status=404 if value is None else 200)

    def writes(self):
        return [call for call in self.transport.calls if call["method"] != "GET"]

    def assert_stopped(self, writes=0):
        with self.assertRaises(release.ReleaseError) as raised:
            self.publish()
        self.assertEqual(len(self.writes()), writes)
        self.assertFalse(any(call["method"] in ("PATCH", "DELETE") for call in self.transport.calls))
        self.transport.assert_done()
        return str(raised.exception)

    def test_create_and_collision_readbacks_accept_only_exact_state(self):
        for status in (201, 422):
            with self.subTest(status=status):
                self.setUp()
                self.queue_validation()
                self.queue_tag(None)
                self.queue_record(None)
                self.queue_bound()
                self.transport.add("POST", "/git/refs", {"untrusted": "response"}, status=status)
                self.queue_tag(self.tag())
                self.queue_bound()
                self.transport.add("POST", "/releases", {"untrusted": "response"}, status=status)
                self.queue_record(self.record())
                self.transport.add("GET", "/releases/latest", {"id": 8})
                self.assertEqual(self.publish(), VERSION)
                self.assertEqual([call["payload"] for call in self.writes()], [
                    {"ref": f"refs/tags/{VERSION}", "sha": SHA},
                    {"tag_name": VERSION, "name": VERSION, "body": BODY,
                     "draft": False, "prerelease": False, "generate_release_notes": False,
                     "make_latest": "true"}])
                self.transport.assert_done()

    def test_exact_existing_records_are_get_only(self):
        self.queue_validation(tags=[{"name": VERSION}])
        self.queue_tag(self.tag())
        self.queue_record(self.record())
        self.transport.add("GET", "/releases/latest", {"id": 8})
        self.assertEqual(self.publish(), VERSION)
        self.assertEqual(self.writes(), [])
        self.transport.assert_done()

    def test_existing_tag_missing_release_creates_only_release(self):
        self.queue_validation()
        self.queue_tag(self.tag())
        self.queue_record(None)
        self.queue_bound()
        self.transport.add("POST", "/releases", {}, status=201)
        self.queue_record(self.record())
        self.transport.add("GET", "/releases/latest", {"id": 8})
        self.assertEqual(self.publish(), VERSION)
        self.assertEqual([call["path"] for call in self.writes()], ["/releases"])
        self.transport.assert_done()

    def test_invalid_tags_never_write(self):
        invalid = [[], {}, self.tag(ref="refs/tags/other"), self.tag(object=None),
                   self.tag(object={"type": "tag", "sha": SHA}),
                   self.tag(object={"type": "commit", "sha": HEAD_SHA}),
                   self.tag(object={"type": "commit", "sha": True}),
                   self.tag(object={"type": "commit", "sha": "short"})]
        for tag in invalid:
            with self.subTest(tag=tag):
                self.setUp()
                self.queue_validation()
                self.queue_tag(tag)
                self.assert_stopped()

    def test_release_field_mismatches_stop_before_any_write(self):
        invalid = [[], {}, *[self.record(**{key: value}) for key, values in {
            "id": [True, False, 0, -1, "8", 8.0, None], "tag_name": ["other", None],
            "name": ["other", None], "body": [BODY + "\n", None],
            "draft": [True, 0, "false", None], "prerelease": [True, 0, "false", None],
        }.items() for value in values]]
        for record in invalid:
            for tag in (None, self.tag()):
                with self.subTest(record=record, tag=tag):
                    self.setUp()
                    self.queue_validation()
                    self.queue_tag(tag)
                    self.queue_record(record)
                    self.assert_stopped()

    def test_release_without_tag_is_inconsistent(self):
        self.queue_validation()
        self.queue_tag(None)
        self.queue_record(self.record())
        self.assert_stopped()

    def test_latest_must_have_same_positive_integer_id(self):
        for latest in (None, [], {}, {"id": 7}, {"id": True}, {"id": "8"}, {"id": 8.0}, {"id": 0}):
            with self.subTest(latest=latest):
                self.setUp()
                self.queue_validation()
                self.queue_tag(self.tag())
                self.queue_record(self.record())
                self.transport.add("GET", "/releases/latest", latest, status=404 if latest is None else 200)
                self.assert_stopped()

    def test_create_readback_failure_stops_without_retry(self):
        for status in (201, 422):
            for kind in ("tag", "release", "latest"):
                for value in (None, {}, {"wrong": "object"}):
                    with self.subTest(status=status, kind=kind, value=value):
                        self.setUp()
                        self.queue_validation()
                        self.queue_tag(None)
                        self.queue_record(None)
                        self.queue_bound()
                        self.transport.add("POST", "/git/refs", {}, status=status)
                        self.queue_tag(value if kind == "tag" else self.tag())
                        if kind != "tag":
                            self.queue_bound()
                            self.transport.add("POST", "/releases", {}, status=status)
                            self.queue_record(value if kind == "release" else self.record())
                            if kind == "latest":
                                self.transport.add("GET", "/releases/latest", value,
                                                   status=404 if value is None else 200)
                        message = self.assert_stopped(writes=1 if kind == "tag" else 2)
                        self.assertIn("tag create", message)
                        if kind != "tag":
                            self.assertIn("Release create", message)

    def test_uncertain_create_errors_never_retry_or_read_back(self):
        for endpoint in ("/git/refs", "/releases"):
            for status in (403, 500):
                with self.subTest(endpoint=endpoint, status=status):
                    self.setUp()
                    self.queue_validation()
                    self.queue_tag(None if endpoint == "/git/refs" else self.tag())
                    self.queue_record(None)
                    self.queue_bound()
                    self.transport.add("POST", endpoint, {"message": "private-response"}, status=status)
                    message = self.assert_stopped(writes=1)
                    self.assertNotIn("private-response", message)

    def test_expected_shas_are_required_and_malformed_values_fail_before_reads(self):
        with self.assertRaises(TypeError):
            release.publish(self.client, 12)
        for key in ("base_sha", "head_sha", "merge_sha"):
            for value in (None, True, "", "main", "a" * 39, "g" * 40):
                with self.subTest(key=key, value=value), self.assertRaises(release.ReleaseError):
                    self.publish(**{key: value})
        self.assertEqual(self.transport.calls, [])

    def test_requested_number_must_be_positive_nonboolean_integer(self):
        for number in (True, False, 0, -1, "12", None):
            with self.subTest(number=number), self.assertRaises(release.ReleaseError):
                release.publish(self.client, number, base_sha=BASE_SHA, head_sha=HEAD_SHA, merge_sha=SHA)
        self.assertEqual(self.transport.calls, [])

    def test_bound_identity_mismatches_fail_before_writes(self):
        invalid = {"number": [13, True, "12", None], "state": ["open", None],
                   "merged": [False, 1, "true", None], "base.ref": ["dev", None],
                   "head.ref": ["main", None], "base.repo.full_name": ["other/repo", None],
                   "head.repo.full_name": ["other/repo", None], "head.repo.fork": [True, 0, None],
                   "base.sha": [HEAD_SHA, None], "head.sha": [BASE_SHA, None],
                   "merge_commit_sha": [HEAD_SHA, None]}
        for key, values in invalid.items():
            for value in values:
                with self.subTest(key=key, value=value):
                    self.setUp()
                    pr = pull()
                    cursor = pr
                    parts = key.split(".")
                    for part in parts[:-1]:
                        cursor = cursor[part]
                    cursor[parts[-1]] = value
                    self.transport.add("GET", "/pulls/12", pr)
                    self.assert_stopped()

    def test_main_must_be_exact_current_protected_branch(self):
        for main in ([], {}, {"name": "dev", "protected": True, "commit": {"sha": SHA}},
                     *[{"name": "main", "protected": value, "commit": {"sha": SHA}}
                       for value in (False, 1, "true", None)],
                     {"name": "main", "protected": True, "commit": {"sha": HEAD_SHA}}):
            with self.subTest(main=main):
                self.setUp()
                self.queue_bound(main=main)
                self.assert_stopped()

    def test_fresh_pr_and_main_are_checked_at_each_write_boundary(self):
        for after_tag in (False, True):
            for field in ("title", "body", "head", "main"):
                with self.subTest(after_tag=after_tag, field=field):
                    self.setUp()
                    self.queue_validation()
                    self.queue_tag(None)
                    self.queue_record(None)
                    if after_tag:
                        self.queue_bound()
                        self.transport.add("POST", "/git/refs", {}, status=201)
                        self.queue_tag(self.tag())
                    pr = pull()
                    if field == "title":
                        pr["title"] = "Release 0.7.1"
                    elif field == "body":
                        pr["body"] = BODY.replace("licensing", "legal")
                    elif field == "head":
                        pr["head"]["sha"] = BASE_SHA
                    self.transport.add("GET", "/pulls/12", pr)
                    if field == "main":
                        self.transport.add("GET", "/branches/main",
                                           {"name": "main", "protected": True, "commit": {"sha": HEAD_SHA}})
                    message = self.assert_stopped(writes=int(after_tag))
                    if after_tag:
                        self.assertIn("tag create", message)

    def test_api_failures_at_initial_and_write_boundaries_stop(self):
        for stage in ("initial", "first-write", "after-tag"):
            for endpoint in ("/pulls/12", "/branches/main"):
                for status in (404, 403, 500):
                    with self.subTest(stage=stage, endpoint=endpoint, status=status):
                        self.setUp()
                        if stage != "initial":
                            self.queue_validation()
                            self.queue_tag(None)
                            self.queue_record(None)
                        if stage == "after-tag":
                            self.queue_bound()
                            self.transport.add("POST", "/git/refs", {}, status=201)
                            self.queue_tag(self.tag())
                        if endpoint == "/branches/main":
                            self.transport.add("GET", "/pulls/12", pull())
                        self.transport.add("GET", endpoint, {"message": "private-response"}, status=status)
                        message = self.assert_stopped(writes=int(stage == "after-tag"))
                        self.assertNotIn("private-response", message)

    def test_successful_null_optional_objects_are_not_absence(self):
        for endpoint in (f"/git/ref/tags/{VERSION}", f"/releases/tags/{VERSION}"):
            with self.subTest(endpoint=endpoint):
                self.setUp()
                self.queue_validation()
                if "/releases/" in endpoint:
                    self.queue_tag(None)
                self.transport.add("GET", endpoint, None)
                self.assert_stopped()

    def test_collision_with_wrong_commit_or_release_body_is_not_repaired(self):
        for kind in ("tag", "release"):
            with self.subTest(kind=kind):
                self.setUp()
                self.queue_validation()
                self.queue_tag(None if kind == "tag" else self.tag())
                self.queue_record(None)
                self.queue_bound()
                if kind == "tag":
                    self.transport.add("POST", "/git/refs", {}, status=422)
                    self.queue_tag(self.tag(object={"type": "commit", "sha": HEAD_SHA}))
                else:
                    self.transport.add("POST", "/releases", {}, status=422)
                    self.queue_record(self.record(body="wrong"))
                self.assert_stopped(writes=1)

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
                    release.publish(client, 12, base_sha=BASE_SHA, head_sha=HEAD_SHA, merge_sha=SHA)
                transport.assert_done()

    def test_invalid_merge_commit_response_fails_before_tag_read(self):
        self.queue_bound()
        self.transport.add("GET", f"/commits/{SHA}", {"sha": "b" * 40})
        with self.assertRaisesRegex(release.ReleaseError, "merge commit response"):
            self.publish()
        self.transport.assert_done()

    def test_changelog_mismatch_at_merge_commit_stops_before_any_write(self):
        private_file_text = "provider-secret-123"
        self.queue_bound()
        self.transport.add("GET", f"/commits/{SHA}", {"sha": SHA})
        self.transport.add(
            "GET",
            changelog_request(SHA),
            changelog(content=BODY.replace("release automation", private_file_text).encode()),
        )
        with self.assertRaisesRegex(
            release.ReleaseError, "does not exactly match"
        ) as raised:
            self.publish()
        self.assertNotIn(private_file_text, str(raised.exception))
        self.assertNotIn(BODY, str(raised.exception))
        self.assertTrue(all(call["method"] == "GET" for call in self.transport.calls))
        self.assertFalse(any("/git/ref" in call["path"] for call in self.transport.calls))
        self.transport.assert_done()

    def test_newer_tag_rejects_rerun_before_mutation(self):
        self.queue_validation(tags=[{"name": VERSION}, {"name": "0.7.1"}])
        with self.assertRaisesRegex(release.ReleaseError, "not newer"):
            self.publish()
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
    def test_publish_requires_and_passes_all_expected_shas(self):
        args = ["publish", "--repository", REPOSITORY, "--pull-request-number", "12"]
        sha_args = ["--base-sha", BASE_SHA, "--head-sha", HEAD_SHA, "--merge-sha", SHA]
        for index in (0, 2, 4):
            with mock.patch.object(release, "GitHubClient") as client, mock.patch("sys.stderr", io.StringIO()):
                with self.assertRaises(SystemExit) as raised:
                    release.main(args + sha_args[:index] + sha_args[index + 2:])
                self.assertEqual(raised.exception.code, 2)
                client.assert_not_called()
        stdout = io.StringIO()
        with mock.patch.object(release, "GitHubClient") as client, mock.patch.object(
            release, "publish", return_value=VERSION
        ) as publish, mock.patch("sys.stdout", stdout):
            self.assertEqual(release.main(args + sha_args), 0)
            publish.assert_called_once_with(client.return_value, 12, base_sha=BASE_SHA,
                                            head_sha=HEAD_SHA, merge_sha=SHA)
        self.assertEqual(stdout.getvalue(), VERSION + "\n")

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
