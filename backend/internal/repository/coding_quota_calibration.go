package repository

// This is observation freshness, NOT the settlement cursor. Flat processed
// readings at the current high-water mark are trustworthy without moving the
// interval boundary used by settleCodingIncrement. Regressions, queued reads,
// other windows and implausibly future readings never refresh admission.
const codingTrustedObservationSQL = `GREATEST(m.observed_at, (
 SELECT MAX(o.observed_at) FROM coding_quota_observations o
 WHERE o.account_id=m.account_id AND o.purpose=m.purpose AND o.processed
 AND o.used=m.used AND o.observed_at>=m.observed_at
 AND o.observed_at<=$3::timestamptz+INTERVAL '1 minute'
 AND o.window_end BETWEEN m.window_end-INTERVAL '5 minutes' AND m.window_end+INTERVAL '5 minutes'
))`
