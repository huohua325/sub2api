-- Recalibrate from the current official window, without scanning old users or
-- historical windows. Build without blocking billing writes on a busy pool.
CREATE INDEX CONCURRENTLY IF NOT EXISTS coding_quota_requests_calibration
    ON coding_quota_requests(account_id, official_window_end)
    INCLUDE(points, weight)
    WHERE state = 'settled';
