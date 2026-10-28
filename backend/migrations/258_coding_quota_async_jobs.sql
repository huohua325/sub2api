-- Durable, owner-scoped pricing evidence for asynchronous media. No request body.
CREATE TABLE coding_quota_async_jobs (
    job_id TEXT NOT NULL,
    user_id BIGINT NOT NULL REFERENCES users(id),
    api_key_id BIGINT NOT NULL,
    reservation_id TEXT NOT NULL REFERENCES coding_quota_reservations(id),
    evidence JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY(job_id,user_id,api_key_id)
);
