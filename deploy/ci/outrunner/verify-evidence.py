#!/usr/bin/python3
"""Verify private integration artifacts; publish only aggregate numeric counts."""
import hashlib
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]


def main():
    reports = []
    for path in (ROOT / 'tmp/validation').glob('*/report.json'):
        report = json.loads(path.read_text())
        if report['kind'] == 'validation' and report['selection']['checks'] == ['go-integration']:
            reports.append(report)
    if len(reports) != 1:
        raise RuntimeError('Expected exactly one integration validation report')
    report = reports[0]
    if report['status'] != 'passed' or report['runner_exit_code'] != 0:
        raise RuntimeError('Integration validation did not pass')
    check = next(c for c in report['checks'] if c['id'] == 'go-integration')
    if not check['selected'] or check['status'] != 'passed' or check['returncode'] != 0:
        raise RuntimeError('Integration child command did not pass')
    if report['database_lifecycle']['cleanup']['status'] != 'passed':
        raise RuntimeError('Test database disposal was not verified')
    for artifact in report['artifacts']:
        path = (ROOT / artifact['path']).resolve()
        if not path.is_relative_to(ROOT / 'tmp/validation'):
            raise RuntimeError('Artifact escaped the private validation directory')
        if not artifact['exists'] or not artifact['complete'] or not path.is_file():
            raise RuntimeError('Validation artifact is incomplete')
        with path.open('rb') as stream:
            digest = hashlib.file_digest(stream, 'sha256').hexdigest()
        if path.stat().st_size != artifact['size_bytes'] or digest != artifact['sha256']:
            raise RuntimeError('Validation artifact changed')
    counts = check['test_counts']
    if counts['top_level']['total'] == 0 or counts['top_level']['passed'] == 0:
        raise RuntimeError('No meaningful integration test coverage')
    summary = {'status': 'passed', 'duration_seconds': check['duration_ms'] / 1000}
    for category in ('top_level', 'subtests', 'packages'):
        summary[category] = {key: int(counts[category][key])
                             for key in ('passed', 'failed', 'skipped', 'total')}
    print(json.dumps(summary, sort_keys=True))


if __name__ == '__main__':
    main()
