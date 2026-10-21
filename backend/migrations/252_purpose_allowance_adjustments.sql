CREATE TABLE IF NOT EXISTS purpose_allowance_adjustments (
    id BIGSERIAL PRIMARY KEY,
    actor_user_id BIGINT NOT NULL REFERENCES users (id),
    target_user_id BIGINT NOT NULL REFERENCES users (id),
    purpose TEXT NOT NULL,
    field TEXT NOT NULL,
    origin TEXT NOT NULL,
    old_value TEXT NOT NULL,
    new_value TEXT NOT NULL,
    note TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT purpose_allowance_adjustments_purpose_check
        CHECK (purpose IN ('openai', 'grok', 'experiment')),
    CONSTRAINT purpose_allowance_adjustments_shape_check CHECK (
        (origin = 'align' AND field = 'window_start')
        OR (origin = 'rebuild' AND field = 'used_amount')
        OR (origin = 'typed' AND field IN ('limit_amount', 'used_amount'))
    )
);

CREATE INDEX IF NOT EXISTS idx_purpose_allowance_adjustments_created
    ON purpose_allowance_adjustments (created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_purpose_allowance_adjustments_target_created
    ON purpose_allowance_adjustments (target_user_id, created_at DESC, id DESC);
