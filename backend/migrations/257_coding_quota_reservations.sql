-- Runtime coordination is durable and separate from immutable historical charges.
-- No production events, anchors, refunds or initialization are embedded here.
CREATE TABLE coding_quota_reservations (
 id TEXT PRIMARY KEY,
 user_id BIGINT NOT NULL REFERENCES users(id),
 api_key_id BIGINT NOT NULL,
 account_id BIGINT NOT NULL,
 purpose TEXT NOT NULL CHECK(purpose IN ('openai','grok')),
 model TEXT NOT NULL,
 mode TEXT NOT NULL CHECK(mode IN ('shadow','enforce')),
 state TEXT NOT NULL DEFAULT 'reserved' CHECK(state IN ('reserved','sent','handoff','captured','released','unresolved')),
 admission_mode TEXT NOT NULL CHECK(admission_mode IN ('normal','single','exhausted')),
 reason TEXT NOT NULL,
 estimated_points NUMERIC(24,8) CHECK(estimated_points>=0),
 rate NUMERIC(28,12),
 sample_count INTEGER NOT NULL DEFAULT 0,
 reliable BOOLEAN NOT NULL,
 would_allow BOOLEAN NOT NULL,
 estimator_version TEXT NOT NULL DEFAULT 'window-cost-v1',
 started_at TIMESTAMPTZ NOT NULL,
 observed_at TIMESTAMPTZ,
 window_end TIMESTAMPTZ,
 heartbeat_at TIMESTAMPTZ NOT NULL,
 expires_at TIMESTAMPTZ NOT NULL,
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX coding_quota_reservations_personal ON coding_quota_reservations(user_id,purpose,started_at)
 INCLUDE(state,estimated_points,expires_at);
CREATE INDEX coding_quota_reservations_expiring ON coding_quota_reservations(expires_at,id) WHERE state IN ('reserved','sent');
CREATE INDEX coding_quota_reservations_page ON coding_quota_reservations(started_at DESC,id DESC);
CREATE TABLE coding_quota_reservation_events (
 id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 reservation_id TEXT NOT NULL REFERENCES coding_quota_reservations(id),
 state TEXT NOT NULL,
 reason TEXT NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX coding_quota_reservation_events_parent ON coding_quota_reservation_events(reservation_id,id);
CREATE TABLE coding_quota_billing_tasks (
 id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 reservation_id TEXT NOT NULL REFERENCES coding_quota_reservations(id),
 request_id TEXT NOT NULL,
 api_key_id BIGINT NOT NULL,
 fingerprint TEXT NOT NULL,
 command JSONB NOT NULL,
 state TEXT NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','applied','failed')),
 attempts INTEGER NOT NULL DEFAULT 0,
 next_retry_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 last_error TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 applied_at TIMESTAMPTZ,
 UNIQUE(request_id,api_key_id)
);
CREATE INDEX coding_quota_billing_tasks_queue ON coding_quota_billing_tasks(next_retry_at,id) WHERE state='pending';
CREATE INDEX coding_quota_billing_tasks_reservation ON coding_quota_billing_tasks(reservation_id,id);
