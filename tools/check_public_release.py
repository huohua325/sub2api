"""Verify public release resources with one deadline and transient-only retries."""
import argparse
import copy
from concurrent.futures import ThreadPoolExecutor
from datetime import datetime, timezone
import hashlib
import json
from pathlib import Path
import re
import socket
import ssl
import time
import urllib.error
import urllib.parse
import urllib.request

from delivery_core import DeliveryError, contained, digest, json_hash, utcnow, write_json


def verify(base_url, dist, *, total_timeout=60, attempts=3, resume=None):
    base = urllib.parse.urlsplit(base_url)
    if base.scheme not in ('http', 'https') or not base.hostname or base.username or base.password or base.query or base.fragment or base.path not in ('', '/'):
        raise DeliveryError('Base URL must be an HTTP(S) origin without credentials')
    if total_timeout <= 0 or attempts not in (1, 2, 3):
        raise DeliveryError('Positive deadline and at most three attempts required')
    base_url = base_url.rstrip('/')
    dist = Path(dist).resolve()
    if not (dist / 'index.html').is_file():
        raise DeliveryError('Selected frontend build is missing index.html')
    deadline = time.monotonic() + total_timeout
    expected = {'/' + p.relative_to(dist).as_posix(): digest(p) for p in dist.rglob('*') if p.is_file() and p.name != 'index.html'}
    fingerprint = json_hash({'assets': expected, 'index': digest(dist / 'index.html')})
    result = {'base_url': base_url, 'checked_at': utcnow(), 'dist_sha256': fingerprint, 'ok': False, 'resources': {}}
    previous = {}
    if resume is not None:
        try:
            age = (datetime.now(timezone.utc) - datetime.fromisoformat(resume['checked_at'])).total_seconds()
            valid = resume['base_url'] == base_url and resume['dist_sha256'] == fingerprint and 0 <= age <= 1800
        except (KeyError, TypeError, ValueError):
            valid = False
        if not valid:
            raise DeliveryError('Resume requires the same origin and build, verified within 30 minutes')
        previous = resume['resources']
        for path, checksum in expected.items():
            prior = previous.get(path, {})
            if prior.get('ok') and prior.get('sha256') != checksum:
                raise DeliveryError('Resume success lacks a matching original asset hash: ' + path)
        result['resumed_from'] = {'checked_at': resume['checked_at'], 'sha256': json_hash(resume)}
    # No Authorization headers, cookies, or business API POSTs are ever sent.
    def fetch(path, expected=None):
        evidence = {'attempts': []}
        for attempt in range(attempts):
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                evidence['error'] = 'global deadline exceeded'; break
            entry = {'attempt': attempt + 1}
            evidence['attempts'].append(entry)
            try:
                request = urllib.request.Request(base_url + path, headers={'User-Agent': 'MILE-release-verification'})
                with urllib.request.urlopen(request, timeout=min(10, remaining)) as response:
                    # Never accept a redirect to a different origin.
                    if urllib.parse.urlsplit(response.url).netloc != base.netloc:
                        raise DeliveryError('cross-origin redirect')
                    chunks = []
                    while True:
                        if response.fp is None:
                            break
                        remaining = deadline - time.monotonic()
                        if remaining <= 0:
                            raise TimeoutError('global deadline exceeded')
                        response.fp.raw._sock.settimeout(min(10, remaining))
                        chunk = response.read1(65536)
                        if not chunk:
                            break
                        chunks.append(chunk)
                        if sum(map(len, chunks)) > 64 * 1024 * 1024:
                            raise DeliveryError('resource exceeds verification size budget')
                    body = b''.join(chunks)
                    entry['status'] = response.status
                if expected is not None and hashlib.sha256(body).hexdigest() != expected:
                    raise DeliveryError('hash mismatch')
                evidence.update(ok=True, bytes=len(body), sha256=hashlib.sha256(body).hexdigest(), verified_at=utcnow())
                return evidence, body
            except urllib.error.HTTPError as error:
                entry.update(status=error.code, error='HTTP ' + str(error.code))
                retry = error.code in (408, 429, 500, 502, 503, 504)
                error.close()
            except DeliveryError as error:
                entry['error'] = str(error); retry = False
            except (urllib.error.URLError, TimeoutError, ConnectionError, socket.timeout, ssl.SSLError) as error:
                # Certificate validation errors are persistent configuration errors.
                entry['error'] = type(error).__name__
                reason = getattr(error, 'reason', error)
                retry = not isinstance(reason, ssl.SSLCertVerificationError)
            if not retry:
                break
            time.sleep(min(.1 * (attempt + 1), max(0, deadline - time.monotonic())))
        evidence.update(ok=False, error=evidence.get('error', evidence['attempts'][-1]['error'] if evidence['attempts'] else 'deadline exceeded'))
        return evidence, b''

    homepage, html = fetch('/')
    result['resources']['/'] = homepage
    if not homepage['ok']:
        return result
    # Report service health before large assets can consume the shared deadline.
    for path in ['/health', '/login', '/api/v1/settings/public']:
        result['resources'][path] = fetch(path)[0]
    paths = set(expected)
    paths.update(re.findall(r'(?:src|href)=["\'](/assets/[^"\']+)["\']', html.decode('utf-8', errors='replace')))
    def asset(path):
        try:
            checksum = digest(contained(dist, path.lstrip('/')))
        except (OSError, DeliveryError):
            return path, {'ok': False, 'error': 'public HTML references an asset absent from the selected build', 'attempts': []}
        prior = previous.get(path, {})
        try:
            prior_age = (datetime.now(timezone.utc) - datetime.fromisoformat(prior['verified_at'])).total_seconds()
        except (KeyError, TypeError, ValueError):
            prior_age = float('inf')
        if prior.get('ok') and prior.get('sha256') == checksum and 0 <= prior_age <= 1800:
            reused = copy.deepcopy(prior)
            reused.update(reused=True, reused_from=resume['checked_at'])
            return path, reused
        return path, fetch(path, checksum)[0]
    with ThreadPoolExecutor(max_workers=4) as executor:
        result['resources'].update(executor.map(asset, sorted(paths)))
    result['ok'] = all(row['ok'] for row in result['resources'].values())
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--base-url', required=True)
    parser.add_argument('--dist', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--timeout', type=float, default=60)
    parser.add_argument('--resume', type=Path, help='previous report for this origin/build; only failed assets are fetched again')
    args = parser.parse_args()
    if args.resume and args.resume.resolve() == args.output.resolve():
        raise DeliveryError('Keep the original report; select a new output path')
    prior = json.loads(args.resume.read_text(encoding='utf-8')) if args.resume else None
    result = verify(args.base_url, args.dist, total_timeout=args.timeout, resume=prior)
    write_json(args.output, result)
    print('public verification: ' + ('passed' if result['ok'] else 'failed'))
    return 0 if result['ok'] else 1


if __name__ == '__main__':
    raise SystemExit(main())
