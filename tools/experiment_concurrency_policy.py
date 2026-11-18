"""Opt in only an administrator and an explicitly selected experiment tester."""
import argparse
import json
import os
from pathlib import Path
import re
import subprocess

from delivery_core import DeliveryError, digest, utcnow
from experiment_accounting_upgrade import ExperimentAccountingRuntime


def literal(value):
    return "'" + str(value).replace("'", "''") + "'"


def configure(root, release, expected_sha256, actor_id, email, *, apply=False):
    root, release = Path(root).resolve(), Path(release).resolve()
    if not release.is_relative_to(root / 'releases') or not re.fullmatch(r'[0-9a-f]{64}', expected_sha256):
        raise DeliveryError('Explicit release directory and program identity required')
    if digest(root / 'bin/sub2api') != expected_sha256 or actor_id <= 0 or not email.strip():
        raise DeliveryError('Program or rollout identity changed')
    runtime = ExperimentAccountingRuntime(root, release)
    def query(sql):
        return subprocess.check_output(runtime._command(), input=sql, text=True, timeout=30).strip()
    # Read only until all concrete identities and the existing policy are known.
    users = json.loads(query("SELECT COALESCE(json_agg(t),'[]') FROM (SELECT id,email,role,status FROM users "
                             "WHERE deleted_at IS NULL AND (id=" + str(actor_id) + " OR email=" + literal(email) + '))t;'))
    actor = next((user for user in users if user['id'] == actor_id), None)
    tester = next((user for user in users if user['email'] == email), None)
    if not actor or actor['role'] != 'admin' or actor['status'] != 'active' or not tester or tester['status'] != 'active':
        raise DeliveryError('An active administrator and exact active tester are required')
    if query("SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE filename='273_experiment_async_admission.sql');") != 't':
        raise DeliveryError('Async admission migration must be deployed first')
    prior = json.loads(query("SELECT COALESCE((SELECT json_build_object('value',value,'updated_at',updated_at) "
                             "FROM settings WHERE key='experiment_async_admission_policy'),'null');"))
    policy = dict(version=1, user_ids=sorted({actor_id, tester['id']}), max_active=10,
                  max_outstanding=100, max_pending_age_seconds=900)
    if prior is not None:
        existing = json.loads(prior['value'])
        if existing != policy:
            raise DeliveryError('Existing rollout differs; preserve it and review the intended change explicitly')
    result = dict(checked_at=utcnow(), sha256=expected_sha256, actor_user_id=actor_id,
                  tester_user_id=tester['id'], prior=prior, policy=policy, applied=False)
    if apply and prior is None:
        audit = release / 'experiment-concurrency-rollout.json'
        descriptor = os.open(audit, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(descriptor, 'w', encoding='utf-8') as target:
            json.dump(result, target, ensure_ascii=False, indent=2)
        encoded = json.dumps(policy, sort_keys=True, separators=(',', ':'))
        inserted = query("BEGIN; SELECT pg_advisory_xact_lock(hashtext('experiment_async_admission_policy')); "
                         "INSERT INTO settings(key,value,updated_at) VALUES('experiment_async_admission_policy',"
                         + literal(encoded) + ",clock_timestamp()) ON CONFLICT(key) DO NOTHING RETURNING key; COMMIT;")
        if 'experiment_async_admission_policy' not in inserted:
            raise DeliveryError('Concurrent rollout change detected; original audit retained')
        result['applied'] = True
        audit.write_text(json.dumps(result, ensure_ascii=False, indent=2), encoding='utf-8')
    elif apply:
        result['applied'] = True  # Idempotent, identical policy; no write.
    return result


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', type=Path, default=Path('/opt/sub2api'))
    parser.add_argument('--release', type=Path, required=True)
    parser.add_argument('--expected-sha256', required=True)
    parser.add_argument('--actor-user-id', type=int, required=True)
    parser.add_argument('--tester-email', required=True)
    parser.add_argument('--apply', action='store_true')
    args = parser.parse_args()
    print(json.dumps(configure(args.root, args.release, args.expected_sha256, args.actor_user_id,
                               args.tester_email, apply=args.apply), ensure_ascii=False, indent=2))
