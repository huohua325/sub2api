-- Execution capacity and supplier-billing backlog are separate resources.
-- No request, charge, wallet or audit row is removed or repriced.
ALTER TABLE experiment_quota_attempts ADD COLUMN IF NOT EXISTS completed_at timestamptz;

CREATE INDEX IF NOT EXISTS experiment_quota_attempts_open_user
    ON experiment_quota_attempts(user_id, state)
    WHERE state IN ('reserved','sent','handed_off','unknown');
DROP INDEX IF EXISTS experiment_quota_attempts_one_open_user;

-- No rollout is seeded here. Missing policy retains the strict one-request gate.
-- Explicit opt-in is persisted in settings.experiment_async_admission_policy.
