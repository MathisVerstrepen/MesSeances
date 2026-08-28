#!/usr/bin/env python3
"""Validate and publish stable MesSeances releases through GitHub's REST API."""

from __future__ import annotations

import argparse
import datetime as dt
import json
import os
import re
import sys
import urllib.error
import urllib.parse
import urllib.request
from dataclasses import dataclass
from typing import Any, Callable, Mapping


VERSION_RE = re.compile(r"^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$")
TITLE_RE = re.compile(
    r"^((?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)) - (\d{4}-\d{2}-\d{2})$"
)
SHA_RE = re.compile(r"^[0-9a-fA-F]{40}$")
API_ROOT = "https://api.github.com"
BODY_SECTIONS = ("Changed", "Added", "Improved", "Fixed")


class ReleaseError(RuntimeError):
    """Safe, operator-facing release automation failure."""


class GitHubError(ReleaseError):
    def __init__(self, method: str, path: str, status: int) -> None:
        super().__init__(f"GitHub {method} {path} failed with status {status}")
        self.status = status


@dataclass(frozen=True, order=True)
class Version:
    major: int
    minor: int
    patch: int

    @classmethod
    def parse(cls, value: str) -> "Version":
        match = VERSION_RE.fullmatch(value)
        if not match:
            raise ReleaseError(f"invalid strict stable version: {value!r}")
        return cls(*(int(part) for part in match.groups()))

    def __str__(self) -> str:
        return f"{self.major}.{self.minor}.{self.patch}"


@dataclass(frozen=True)
class ReleaseMetadata:
    version: Version
    date: dt.date


@dataclass(frozen=True)
class Response:
    status: int
    headers: Mapping[str, str]
    body: bytes


Transport = Callable[[str, str, Mapping[str, str], bytes | None, float], Response]


def urllib_transport(
    method: str, url: str, headers: Mapping[str, str], body: bytes | None, timeout: float
) -> Response:
    request = urllib.request.Request(url, data=body, headers=dict(headers), method=method)
    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            return Response(response.status, dict(response.headers), response.read())
    except urllib.error.HTTPError as exc:
        return Response(exc.code, dict(exc.headers), exc.read())
    except (urllib.error.URLError, TimeoutError) as exc:
        raise ReleaseError("GitHub request failed before receiving a response") from exc


class GitHubClient:
    def __init__(
        self,
        repository: str,
        token: str,
        transport: Transport = urllib_transport,
        timeout: float = 20,
    ) -> None:
        if not re.fullmatch(r"[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+", repository):
            raise ReleaseError("repository must use owner/name format")
        if not token:
            raise ReleaseError("RELEASE_TOKEN is required")
        self.repository = repository
        self.api_url = f"{API_ROOT}/repos/{repository}"
        self.transport = transport
        self.timeout = timeout
        self.headers = {
            "Accept": "application/vnd.github+json",
            "Authorization": f"Bearer {token}",
            "X-GitHub-Api-Version": "2022-11-28",
            "User-Agent": "messeances-release-automation",
        }

    def request(
        self, method: str, path: str, payload: Mapping[str, Any] | None = None
    ) -> tuple[Any, Mapping[str, str]]:
        if path.startswith("https://"):
            url = path
        elif path.startswith("/"):
            url = self.api_url + path
        else:
            raise ReleaseError("GitHub API path must be absolute")
        if not url.startswith(self.api_url + "/"):
            raise ReleaseError("GitHub API URL escaped repository scope")

        body = None
        headers = dict(self.headers)
        if payload is not None:
            body = json.dumps(payload).encode("utf-8")
            headers["Content-Type"] = "application/json"
        response = self.transport(method, url, headers, body, self.timeout)
        if not 200 <= response.status < 300:
            raise GitHubError(method, urllib.parse.urlsplit(url).path, response.status)
        if not response.body:
            return None, response.headers
        try:
            return json.loads(response.body), response.headers
        except (UnicodeDecodeError, json.JSONDecodeError) as exc:
            raise ReleaseError(f"GitHub {method} response was not valid JSON") from exc

    def get_optional(self, path: str) -> Any | None:
        try:
            return self.request("GET", path)[0]
        except GitHubError as exc:
            if exc.status == 404:
                return None
            raise

    def paginate(self, path: str) -> list[Any]:
        items: list[Any] = []
        next_url: str | None = path
        seen: set[str] = set()
        while next_url:
            if next_url in seen:
                raise ReleaseError("GitHub pagination loop detected")
            seen.add(next_url)
            page, headers = self.request("GET", next_url)
            if not isinstance(page, list):
                raise ReleaseError("GitHub paginated response was not a list")
            items.extend(page)
            next_url = _next_link(headers.get("Link") or headers.get("link"))
        return items


def _next_link(header: str | None) -> str | None:
    if not header:
        return None
    for part in header.split(","):
        match = re.fullmatch(r'\s*<([^>]+)>;\s*rel="([^"]+)"\s*', part)
        if not match:
            raise ReleaseError("malformed GitHub Link pagination header")
        if match.group(2) == "next":
            return match.group(1)
    return None


def parse_release_title(title: Any, expected_date: dt.date) -> ReleaseMetadata:
    if not isinstance(title, str):
        raise ReleaseError("release PR title is missing")
    match = TITLE_RE.fullmatch(title)
    if not match:
        raise ReleaseError(
            "release PR title must be exactly '<X.Y.Z> - <YYYY-MM-DD>'"
        )
    try:
        release_date = dt.date.fromisoformat(match.group(2))
    except ValueError as exc:
        raise ReleaseError("release PR title contains an invalid calendar date") from exc
    if release_date != expected_date:
        raise ReleaseError(
            f"release PR date must be {expected_date.isoformat()}, got {release_date.isoformat()}"
        )
    return ReleaseMetadata(Version.parse(match.group(1)), release_date)


def validate_release_body(body: Any) -> str:
    if not isinstance(body, str) or not body:
        raise ReleaseError("release PR body is missing")
    if "\r" in body:
        raise ReleaseError("release PR body must use LF line endings")

    lines = body.splitlines()
    while lines and lines[-1] == "":
        lines.pop()
    index = 0
    for section in BODY_SECTIONS:
        heading = f"## {section}"
        if index >= len(lines) or lines[index] != heading:
            raise ReleaseError(
                "release PR body must contain only '## Changed', '## Added', "
                "'## Improved', and '## Fixed' in that order"
            )
        index += 1
        entries: list[str] = []
        while index < len(lines) and not lines[index].startswith("## "):
            line = lines[index]
            if line:
                if not line.startswith("- ") or not line[2:].strip():
                    raise ReleaseError(f"section {heading!r} must contain Markdown '- ' bullets")
                entries.append(line[2:].strip())
            index += 1
        if not entries:
            raise ReleaseError(f"section {heading!r} must contain at least one bullet")
        if "None" in entries and len(entries) != 1:
            raise ReleaseError(f"section {heading!r} cannot combine '- None' with other bullets")
    if index != len(lines):
        raise ReleaseError("release PR body contains content outside required sections")
    return body


def _strict_versions(tags: list[Any], excluded: Version | None = None) -> list[Version]:
    versions: list[Version] = []
    for tag in tags:
        if not isinstance(tag, dict) or not isinstance(tag.get("name"), str):
            raise ReleaseError("GitHub tag response contained an invalid item")
        if VERSION_RE.fullmatch(tag["name"]):
            version = Version.parse(tag["name"])
            if version != excluded:
                versions.append(version)
    return versions


def validate_version_progression(
    tags: list[Any], candidate: Version, *, allow_existing: bool
) -> None:
    names = [tag.get("name") for tag in tags if isinstance(tag, dict)]
    if str(candidate) in names and not allow_existing:
        raise ReleaseError(f"release {candidate} already has a tag")
    prior_versions = _strict_versions(tags, excluded=candidate if allow_existing else None)
    if prior_versions and candidate <= max(prior_versions):
        raise ReleaseError(
            f"release {candidate} is not newer than latest prior strict stable tag "
            f"{max(prior_versions)}"
        )


def _repository_pull(pull: Any, repository: str, base: str, head: str) -> bool:
    try:
        return (
            isinstance(pull, dict)
            and pull["base"]["ref"] == base
            and pull["head"]["ref"] == head
            and pull["base"]["repo"]["full_name"].lower() == repository.lower()
            and pull["head"]["repo"]["full_name"].lower() == repository.lower()
        )
    except (KeyError, TypeError, AttributeError):
        raise ReleaseError("GitHub pull request response is invalid")


def _validate_pull_identity(
    pull: Any, repository: str, base: str = "main", head: str = "dev"
) -> None:
    if not _repository_pull(pull, repository, base, head):
        raise ReleaseError(
            f"release PR must be same-repository {head!r} into {base!r}"
        )


def _utc_today() -> dt.date:
    return dt.datetime.now(dt.timezone.utc).date()


def _merged_date(value: Any) -> dt.date:
    if not isinstance(value, str):
        raise ReleaseError("release PR merged_at timestamp is missing")
    try:
        return dt.datetime.strptime(value, "%Y-%m-%dT%H:%M:%SZ").date()
    except ValueError as exc:
        raise ReleaseError("release PR merged_at timestamp is invalid") from exc


def validate_pull_request(
    client: GitHubClient,
    pull_request_number: int,
    *,
    expected_date: dt.date | None = None,
) -> str:
    pull, _ = client.request("GET", f"/pulls/{pull_request_number}")
    _validate_pull_identity(pull, client.repository)
    if pull.get("state") != "open" or pull.get("merged") is not False:
        raise ReleaseError("release PR validation requires an open, unmerged PR")
    metadata = parse_release_title(pull.get("title"), expected_date or _utc_today())
    validate_release_body(pull.get("body"))
    tags = client.paginate("/tags?per_page=100")
    validate_version_progression(tags, metadata.version, allow_existing=False)
    return str(metadata.version)


def _validate_merge_commit(client: GitHubClient, merge_sha: str) -> None:
    commit, _ = client.request("GET", f"/commits/{merge_sha}")
    if (
        not isinstance(commit, dict)
        or not isinstance(commit.get("sha"), str)
        or commit["sha"].lower() != merge_sha.lower()
    ):
        raise ReleaseError("GitHub merge commit response is invalid")


def _q(value: str) -> str:
    return urllib.parse.quote(value, safe="")


def _read_tag_target(client: GitHubClient, version: str) -> str | None:
    ref = client.get_optional(f"/git/ref/tags/{_q(version)}")
    if ref is None:
        return None
    try:
        obj = ref["object"]
        seen: set[str] = set()
        while obj["type"] == "tag":
            sha = obj["sha"]
            if not isinstance(sha, str) or sha in seen or len(seen) >= 10:
                raise ReleaseError("annotated tag dereference loop detected")
            seen.add(sha)
            tag, _ = client.request("GET", f"/git/tags/{_q(sha)}")
            obj = tag["object"]
        if obj["type"] != "commit" or not SHA_RE.fullmatch(obj["sha"]):
            raise ReleaseError("tag does not resolve to a commit")
        return obj["sha"].lower()
    except (KeyError, TypeError) as exc:
        raise ReleaseError("GitHub tag response is invalid") from exc


def _ensure_tag(client: GitHubClient, version: str, merge_sha: str) -> None:
    target = _read_tag_target(client, version)
    if target is None:
        try:
            client.request("POST", "/git/refs", {"ref": f"refs/tags/{version}", "sha": merge_sha})
        except GitHubError as exc:
            if exc.status != 422:
                raise
        target = _read_tag_target(client, version)
    if target != merge_sha.lower():
        raise ReleaseError(f"tag {version} does not resolve to release PR merge commit")


def _canonical_release(version: str, body: str) -> dict[str, Any]:
    return {
        "tag_name": version,
        "name": version,
        "body": body,
        "draft": False,
        "prerelease": False,
        "generate_release_notes": False,
        "make_latest": "true",
    }


def _ensure_release(client: GitHubClient, version: str, body: str) -> None:
    path = f"/releases/tags/{_q(version)}"
    release = client.get_optional(path)
    canonical = _canonical_release(version, body)
    if release is None:
        try:
            client.request("POST", "/releases", canonical)
            return
        except GitHubError as exc:
            if exc.status != 422:
                raise
        release = client.get_optional(path)
    if not isinstance(release, dict) or not isinstance(release.get("id"), int):
        raise ReleaseError("GitHub release response is invalid")
    desired = {
        key: value
        for key, value in canonical.items()
        if key not in {"generate_release_notes", "make_latest"}
    }
    comparable = {key: release.get(key) for key in desired}
    latest = client.get_optional("/releases/latest")
    if latest is not None and (
        not isinstance(latest, dict) or not isinstance(latest.get("id"), int)
    ):
        raise ReleaseError("GitHub latest release response is invalid")
    needs_latest = latest is None or latest["id"] != release["id"]
    if comparable != desired or needs_latest:
        payload = dict(desired)
        if needs_latest:
            payload["make_latest"] = "true"
        client.request("PATCH", f"/releases/{release['id']}", payload)


def publish(
    client: GitHubClient, pull_request_number: int, base: str = "main", head: str = "dev"
) -> str:
    pull, _ = client.request("GET", f"/pulls/{pull_request_number}")
    _validate_pull_identity(pull, client.repository, base, head)
    if pull.get("merged") is not True or pull.get("state") != "closed":
        raise ReleaseError("release PR is not merged")
    merge_sha = pull.get("merge_commit_sha")
    if not isinstance(merge_sha, str) or not SHA_RE.fullmatch(merge_sha):
        raise ReleaseError("release PR merge commit SHA is invalid")

    metadata = parse_release_title(pull.get("title"), _merged_date(pull.get("merged_at")))
    body = validate_release_body(pull.get("body"))
    _validate_merge_commit(client, merge_sha)
    tags = client.paginate("/tags?per_page=100")
    validate_version_progression(tags, metadata.version, allow_existing=True)
    version = str(metadata.version)
    _ensure_tag(client, version, merge_sha)
    _ensure_release(client, version, body)
    return version


def verify_promotion(client: GitHubClient, version: str) -> str:
    candidate = Version.parse(version)
    tags = client.paginate("/tags?per_page=100")
    versions = _strict_versions(tags)
    if not versions:
        raise ReleaseError("no strict stable tag exists for image promotion")
    newest = max(versions)
    if candidate != newest:
        raise ReleaseError(
            f"promotion candidate {candidate} is stale; newest strict stable tag is {newest}"
        )
    return str(candidate)


def _positive_integer(value: str) -> int:
    parsed = int(value)
    if parsed <= 0:
        raise argparse.ArgumentTypeError("must be positive")
    return parsed


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    subparsers = parser.add_subparsers(dest="command", required=True)

    validate_parser = subparsers.add_parser("validate-pr")
    validate_parser.add_argument("--repository", required=True)
    validate_parser.add_argument(
        "--pull-request-number", required=True, type=_positive_integer
    )

    publish_parser = subparsers.add_parser("publish")
    publish_parser.add_argument("--repository", required=True)
    publish_parser.add_argument(
        "--pull-request-number", required=True, type=_positive_integer
    )

    verify_parser = subparsers.add_parser("verify-promotion")
    verify_parser.add_argument("--repository", required=True)
    verify_parser.add_argument("--version", required=True)

    args = parser.parse_args(argv)
    try:
        client = GitHubClient(args.repository, os.environ.get("RELEASE_TOKEN", ""))
        if args.command == "validate-pr":
            version = validate_pull_request(client, args.pull_request_number)
        elif args.command == "publish":
            version = publish(client, args.pull_request_number)
        else:
            version = verify_promotion(client, args.version)
        print(version)
        return 0
    except ReleaseError as exc:
        print(f"release automation failed: {exc}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
