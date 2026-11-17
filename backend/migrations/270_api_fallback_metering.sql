-- Fixed API fallback charges share personal quota periods but never enter the
-- subscription percentage meter or capacity conversions. Existing rows retain
-- their original subscription meaning; no charge or clock is reinitialized.
ALTER TABLE coding_quota_requests
    ADD COLUMN metering_source TEXT NOT NULL DEFAULT 'subscription'
    CHECK (metering_source IN ('subscription', 'api_fallback'));

ALTER TABLE coding_quota_requests
    ADD CONSTRAINT coding_quota_api_fallback_fixed_charge
    CHECK (metering_source <> 'api_fallback' OR
        (state = 'settled' AND weight = 0 AND estimated_points = 0 AND official_window_end IS NULL));

CREATE TRIGGER coding_quota_api_fallback_immutable
    BEFORE UPDATE OR DELETE ON coding_quota_requests
    FOR EACH ROW WHEN (OLD.metering_source = 'api_fallback')
    EXECUTE FUNCTION coding_quota_audit_append_only();
