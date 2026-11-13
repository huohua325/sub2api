"""Verify release manifest hashes from any host, optionally retrying failed paths only."""
import argparse
from concurrent.futures import ThreadPoolExecutor
import hashlib
import json
from pathlib import Path
import time
import urllib.parse
import urllib.request


def verify(base, manifest, paths=None):
    origin = urllib.parse.urlsplit(base)
    if origin.scheme not in ('http', 'https') or origin.path not in ('', '/') or origin.username or origin.query or origin.fragment:
        raise ValueError('An HTTP(S) origin is required')
    expected = {'/' + p: sha for p, sha in manifest['frontend_files'].items() if p != 'index.html'}
    selected = sorted(expected) if paths is None else [p for p in paths if p in expected]
    selected += ['/', '/health', '/login', '/api/v1/settings/public']
    def fetch(path):
        attempts = []
        for attempt in range(3):
            try:
                request = urllib.request.Request(base.rstrip('/') + path, headers={'User-Agent': 'MILE-release-manifest-check'})
                with urllib.request.urlopen(request, timeout=60) as response:
                    if urllib.parse.urlsplit(response.url).netloc != origin.netloc: raise ValueError('Cross-origin redirect')
                    body = response.read(64 * 1024 * 1024 + 1)
                    if len(body) > 64 * 1024 * 1024: raise ValueError('Response too large')
                    checksum = hashlib.sha256(body).hexdigest()
                    if path in expected and checksum != expected[path]: raise ValueError('Hash mismatch')
                return path, {'ok': True, 'bytes': len(body), 'sha256': checksum, 'attempts': attempts}
            except ValueError as error: return path, {'ok': False, 'error': str(error), 'attempts': attempts}
            except OSError as error:
                attempts.append(type(error).__name__)
                time.sleep(.1)
        return path, {'ok': False, 'attempts': attempts}
    with ThreadPoolExecutor(max_workers=4) as executor: resources = dict(executor.map(fetch, selected))
    return {'base_url': base, 'release_sha256': manifest['sha256'], 'ok': all(v['ok'] for v in resources.values()), 'resources': resources}


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--base-url', required=True)
    parser.add_argument('--manifest', required=True)
    parser.add_argument('--retry-report')
    parser.add_argument('--output', required=True)
    args = parser.parse_args()
    paths = None if not args.retry_report else [p for p, r in json.loads(Path(args.retry_report).read_text())['resources'].items() if not r['ok']]
    result = verify(args.base_url, json.loads(Path(args.manifest).read_text()), paths)
    Path(args.output).write_text(json.dumps(result, indent=2))
    print(json.dumps({'ok': result['ok'], 'checked': len(result['resources']), 'failed': [p for p, r in result['resources'].items() if not r['ok']]}))
    raise SystemExit(0 if result['ok'] else 1)
