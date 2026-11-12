"""Explicit full-upgrade lane for the reviewed additive personal quota feature.

The frontend-only entry point continues rejecting backend/schema changes.
"""
import argparse
import copy
import hashlib
import json
from pathlib import Path
import re

from delivery_core import DeliveryError, digest, utcnow
from release_server import DockerRuntime, assert_preserved, deploy, stage_release


class PersonalQuotaUpgrade:
    def __init__(self, contract):
        migrations = contract.get('migrations', {})
        if contract.get('name') != 'personal-quota-reset-v1' or set(migrations) != {'260_personal_quota_resets.sql'}:
            raise DeliveryError('Only the reviewed personal-reset additive upgrade is supported')
        if not re.fullmatch('[0-9a-f]{64}', migrations['260_personal_quota_resets.sql']):
            raise DeliveryError('Fixed migration SHA-256 required')
        database = contract.get('database_migrations', migrations)
        if set(database) != set(migrations) or not all(re.fullmatch('[0-9a-f]{64}', v) for v in database.values()):
            raise DeliveryError('Fixed database migration checksums required')
        self.contract = contract

    def validate(self, manifest):
        if manifest.get('backend_unchanged') is not False or manifest.get('upgrade_contract') != self.contract:
            raise DeliveryError('Explicit full-upgrade manifest required')

    def assert_preserved(self, before, after):
        expected = self.contract.get('database_migrations', self.contract['migrations'])
        prior = {m['filename'] for m in before['migrations']}
        added = [m for m in after['migrations'] if m['filename'] not in prior]
        if {m['filename']: m['checksum'] for m in added} != expected:
            raise DeliveryError('Unexpected added migration or checksum')
        adjusted = copy.deepcopy(after)
        adjusted['migrations'] = [m for m in after['migrations'] if m['filename'] in prior]
        assert_preserved(before, adjusted)

    def can_rollback(self, runtime):
        # Rolling back a program must never silently revoke a newly issued grant.
        return not runtime.snapshot(utcnow())['immutable'].get('personal_resets')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest='command', required=True)
    package = commands.add_parser('package')
    package.add_argument('--output', type=Path, required=True)
    package.add_argument('--destination', type=Path, required=True)
    package.add_argument('--version', required=True)
    package.add_argument('--baseline-commit', required=True)
    package.add_argument('--baseline-sha256', required=True)
    package.add_argument('--baseline-binary', type=Path, required=True)
    package.add_argument('--full-upload', action='store_true', help='explicitly use gzip instead of computing a delta')
    server = commands.add_parser('deploy')
    server.add_argument('--root', type=Path, default=Path('/opt/sub2api'))
    server.add_argument('--release', type=Path, required=True)
    server.add_argument('--stage', action='store_true')
    server.add_argument('--apply', action='store_true')
    args = parser.parse_args()
    if args.command == 'package':
        from release_package import package_release
        root = Path(__file__).resolve().parents[1]
        contract = {'name': 'personal-quota-reset-v1', 'migrations': {
            '260_personal_quota_resets.sql': digest(root / 'backend/migrations/260_personal_quota_resets.sql')}}
        # The Go migration runner hashes strings.TrimSpace(content), while
        # payload integrity uses the complete file bytes, including final LF.
        contract['database_migrations'] = {'260_personal_quota_resets.sql': hashlib.sha256(
            (root / 'backend/migrations/260_personal_quota_resets.sql').read_bytes().strip()).hexdigest()}
        result = package_release(root, args.output, args.destination, args.output / 'sub2api-linux-amd64',
                                 args.version, args.baseline_sha256, args.baseline_commit,
                                 baseline_binary=args.baseline_binary, full_upload=args.full_upload, upgrade_contract=contract)
    else:
        manifest = json.loads((args.release / 'manifest.json').read_text())
        contract = PersonalQuotaUpgrade(manifest.get('upgrade_contract', {}))
        contract.validate(manifest)
        if args.stage:
            stage_release(args.root, args.release)
            return
        result = deploy(args.root, args.release, DockerRuntime(args.root, args.release),
                        apply=args.apply, upgrade=contract)
    print(json.dumps(result, indent=2))


if __name__ == '__main__':
    main()
