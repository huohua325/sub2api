-- Separate non-resetting wallets for the three lab uses.
-- openai = Codex subscription group, grok = Grok Build, experiment = every other API group.

CREATE TABLE IF NOT EXISTS user_purpose_allowances (
    user_id BIGINT NOT NULL REFERENCES users (id),
    purpose TEXT NOT NULL,
    limit_amount NUMERIC(20, 8) NOT NULL,
    used_amount NUMERIC(20, 8) NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, purpose),
    CONSTRAINT user_purpose_allowances_purpose_check CHECK (purpose IN ('openai', 'grok', 'experiment')),
    CONSTRAINT user_purpose_allowances_limit_check CHECK (limit_amount >= 0),
    CONSTRAINT user_purpose_allowances_used_check CHECK (used_amount >= 0)
);

INSERT INTO user_purpose_allowances (user_id, purpose, limit_amount, used_amount)
SELECT id, purpose, limit_amount, 0
FROM users
CROSS JOIN (
    VALUES
        ('openai', 100::numeric),
        ('grok', 100::numeric),
        ('experiment', 500::numeric)
) AS defaults(purpose, limit_amount)
WHERE deleted_at IS NULL
ON CONFLICT (user_id, purpose) DO NOTHING;
