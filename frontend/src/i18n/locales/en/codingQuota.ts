export default {
  attempts: 'Retry attempts',
  ledger: 'Linked ledger',
  events: 'State audit history',
  "active": "In progress",
  "settled": "Settled effective usage",
  "pending": "Pending estimate",
  "reserved": "In-flight reservation",
  "corrections": "Historical corrections",
  "tasks": "Pending / failed tasks",
  "unresolved": "Unverified interruptions",
  "calibration": "Calibration / samples",
  "rate": "Points per raw weight",
  "observed": "Official observation",
  "trusted": "Trusted",
  "unknown": "Unknown",
  "details": "Request and settlement details",
  "hide": "Hide details",
  "state": "State filter",
  "all": "All states",
  "loadFailed": "Could not load details. Reopen to retry.",
  "loading": "Loading?",
  "empty": "No matching requests",
  "more": "Load more",
  "wouldAllow": "Would allow",
  "wouldReject": "Would reject",
  "modes": {
    "normal": "Normal mode",
    "single": "Single request mode",
    "exhausted": "Confirmed exhausted"
  },
  "reasons": {
    "uncalibrated": "Reliable window calibration or request estimate is missing",
    "within_budget": "Estimated usage fits the available budget",
    "estimated_capacity": "Estimated capacity is low; using single request mode",
    "unresolved_request": "An interruption needs verification; using single request mode",
    "settled_exhausted": "Settled usage reached the limit",
    "lease_expired": "Heartbeat lost; awaiting verification",
    "billing_persisted": "Billing result handed off durably",
    "ledger_recorded": "Recorded in the usage ledger"
  },
  "states": {
    "reserved": "Reserved",
    "sent": "Upstream running",
    "handoff": "Billing handoff",
    "captured": "Recorded",
    "released": "Released",
    "unresolved": "Needs verification"
  },
  "student": {
    "ready": "Ready for requests",
    "processing": "Request in progress. This percentage excludes in-flight reservations.",
    "settling": "Settling usage. The percentage may update shortly.",
    "busy": "Concurrent capacity is busy. Retry after your current request finishes.",
    "verifying": "Usage for some requests is being verified.",
    "unavailable": "Quota data is temporarily unavailable. Please retry later."
  }
}
