package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/shopspring/decimal"
)

const experimentReconcileMaxBytes = 64 << 20

var experimentReconcileHash = regexp.MustCompile(`^[0-9a-f]{64}$`)
var experimentReconcileCommit = regexp.MustCompile(`^[0-9a-f]{40}$`)

type experimentReconcileEntry struct {
	ID                int64     `json:"id"`
	RequestID         string    `json:"request_id"`
	APIKeyID          int64     `json:"api_key_id"`
	UserID            int64     `json:"user_id"`
	AccountID         int64     `json:"account_id"`
	UpstreamRequestID string    `json:"upstream_request_id"`
	StartedAt         time.Time `json:"started_at"`
	Quota             int64     `json:"quota"`
	AmountYuan        string    `json:"amount_yuan"`
}

type experimentReconcilePlan struct {
	Schema         int                        `json:"schema"`
	Provider       string                     `json:"provider"`
	QuotaPerYuan   int64                      `json:"quota_per_yuan"`
	EvidenceSHA256 string                     `json:"evidence_sha256"`
	Entries        []experimentReconcileEntry `json:"entries"`
}

type experimentReconcileInput struct {
	Schema         int                     `json:"schema"`
	EvidenceBase64 string                  `json:"evidence_base64"`
	Plan           experimentReconcilePlan `json:"plan"`
}

type experimentSupplierLog struct {
	AccountID         int64  `json:"account_id"`
	Type              int    `json:"type"`
	RequestID         string `json:"request_id"`
	UpstreamRequestID string `json:"upstream_request_id"`
	Quota             int64  `json:"quota"`
}

type experimentProofKey struct {
	Account int64
	Request string
}

func parseExperimentReconcile(reader io.Reader) (*experimentReconcileInput, error) {
	raw, err := io.ReadAll(io.LimitReader(reader, experimentReconcileMaxBytes+1))
	if err != nil || len(raw) > experimentReconcileMaxBytes {
		return nil, errors.New("cannot read bounded experiment evidence input")
	}
	var input experimentReconcileInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return nil, errors.New("invalid experiment reconciliation input JSON")
	}
	plan := &input.Plan
	if input.Schema != 1 || plan.Schema != 1 || plan.Provider != "globalai.vip" || plan.QuotaPerYuan != 500000 || !experimentReconcileHash.MatchString(plan.EvidenceSHA256) {
		return nil, errors.New("unapproved experiment evidence schema, provider, unit, or digest")
	}
	evidence, err := base64.StdEncoding.DecodeString(input.EvidenceBase64)
	if err != nil {
		return nil, errors.New("invalid base64 supplier evidence")
	}
	digest := sha256.Sum256(evidence)
	if hex.EncodeToString(digest[:]) != plan.EvidenceSHA256 {
		return nil, errors.New("supplier evidence digest does not match the reviewed plan")
	}
	proof := make(map[experimentProofKey]int64)
	add := func(row experimentSupplierLog, account int64) error {
		if row.Type != 2 {
			return nil
		}
		request := row.RequestID
		if request == "" {
			request = row.UpstreamRequestID
		}
		if account <= 0 || request == "" || len(request) > 256 || row.Quota < 0 {
			return errors.New("invalid supplier consume record")
		}
		key := experimentProofKey{account, request}
		if old, ok := proof[key]; ok && old != row.Quota {
			return errors.New("conflicting supplier consume records")
		}
		proof[key] = row.Quota
		return nil
	}
	if len(evidence) > 0 && evidence[0] == '[' {
		// Retained November audit captures predate the normalized envelope.
		var endpoints []struct {
			AccountID int64                   `json:"account_id"`
			Path      string                  `json:"path"`
			Status    int                     `json:"status"`
			Logs      []experimentSupplierLog `json:"logs"`
		}
		if err := json.Unmarshal(evidence, &endpoints); err != nil {
			return nil, errors.New("invalid retained supplier audit capture")
		}
		for _, endpoint := range endpoints {
			if endpoint.Path != "/api/log/token" || endpoint.Status != 200 {
				continue
			}
			for _, row := range endpoint.Logs {
				if err := add(row, endpoint.AccountID); err != nil {
					return nil, err
				}
			}
		}
	} else {
		var envelope struct {
			Schema       int                     `json:"schema"`
			Provider     string                  `json:"provider"`
			QuotaPerYuan int64                   `json:"quota_per_yuan"`
			Logs         []experimentSupplierLog `json:"logs"`
		}
		if err := json.Unmarshal(evidence, &envelope); err != nil || envelope.Schema != 1 || envelope.Provider != plan.Provider || envelope.QuotaPerYuan != plan.QuotaPerYuan {
			return nil, errors.New("invalid normalized supplier evidence")
		}
		for _, row := range envelope.Logs {
			if err := add(row, row.AccountID); err != nil {
				return nil, err
			}
		}
	}
	seenIDs := make(map[int64]bool)
	seenProof := make(map[experimentProofKey]bool)
	for _, entry := range plan.Entries {
		key := experimentProofKey{entry.AccountID, entry.UpstreamRequestID}
		quota, ok := proof[key]
		if !ok || quota != entry.Quota || entry.ID <= 0 || entry.UserID <= 0 || entry.APIKeyID <= 0 || entry.RequestID == "" || entry.StartedAt.IsZero() || seenIDs[entry.ID] || seenProof[key] {
			return nil, errors.New("reviewed plan has an invalid, duplicate, or unproven request identity")
		}
		price, err := decimal.NewFromString(entry.AmountYuan)
		want := decimal.NewFromInt(quota).Div(decimal.NewFromInt(500000))
		if err != nil || !price.Equal(want) {
			return nil, errors.New("reviewed amount is not exactly supplier quota / 500000")
		}
		seenIDs[entry.ID], seenProof[key] = true, true
	}
	return &input, nil
}

func validateExperimentReconcileRows(ctx context.Context, db *sql.DB, plan *experimentReconcilePlan) (int, error) {
	var schemaReady bool
	if err := db.QueryRowContext(ctx, `SELECT to_regclass('experiment_quota_settlement_events') IS NOT NULL`).Scan(&schemaReady); err != nil || !schemaReady {
		return 0, errors.New("experiment accounting audit migration is required before reconciliation")
	}
	already := 0
	for _, entry := range plan.Entries {
		var request, upstream, state string
		var user, key, account int64
		var started time.Time
		var amount sql.NullString
		err := db.QueryRowContext(ctx, `SELECT request_id,api_key_id,user_id,account_id,upstream_request_id,started_at,state,amount::text FROM experiment_quota_charges WHERE id=$1`, entry.ID).
			Scan(&request, &key, &user, &account, &upstream, &started, &state, &amount)
		if err != nil || request != entry.RequestID || key != entry.APIKeyID || user != entry.UserID || account != entry.AccountID || upstream != entry.UpstreamRequestID || !started.Equal(entry.StartedAt) {
			return 0, fmt.Errorf("charge %d no longer matches the reviewed request identity", entry.ID)
		}
		if state == "settled" {
			price, priceErr := decimal.NewFromString(amount.String)
			if priceErr != nil || !price.Equal(decimal.NewFromInt(entry.Quota).Div(decimal.NewFromInt(500000))) {
				return 0, fmt.Errorf("charge %d conflicts with prior settlement", entry.ID)
			}
			already++
		} else if state != "pending" || amount.Valid {
			return 0, fmt.Errorf("charge %d is not an unpriced pending request", entry.ID)
		}
	}
	return already, nil
}

func runExperimentReconcile(args []string) int {
	return experimentReconcileCommand(args, os.Stdin, os.Stdout, os.Stderr)
}

func experimentReconcileCommand(args []string, input io.Reader, output, errorOutput io.Writer) int {
	flags := flag.NewFlagSet("reconcile-experiment", flag.ContinueOnError)
	flags.SetOutput(errorOutput)
	apply := flags.Bool("apply", false, "settle proven pending charges; default is read-only")
	backup := flags.String("backup-sha256", "", "verified production database archive SHA256, required for apply")
	expectedCommit := flags.String("expected-source-commit", "", "exact reviewed running program source SHA, required for apply")
	if flags.Parse(args) != nil || flags.NArg() != 0 {
		return 2
	}
	if *apply && (!experimentReconcileHash.MatchString(*backup) || !experimentReconcileCommit.MatchString(*expectedCommit) || *expectedCommit != Commit) {
		fmt.Fprintln(errorOutput, "apply requires a verified backup digest and this exact program source commit")
		return 2
	}
	if *expectedCommit != "" && *expectedCommit != Commit {
		fmt.Fprintln(errorOutput, "reconciliation program source does not match the reviewed source commit")
		return 2
	}
	payload, err := parseExperimentReconcile(input)
	if err != nil {
		fmt.Fprintln(errorOutput, err)
		return 2
	}
	dsn := strings.TrimSpace(os.Getenv("SUB2API_RECONCILE_DSN"))
	if dsn == "" {
		fmt.Fprintln(errorOutput, "SUB2API_RECONCILE_DSN is required; credentials must not be command arguments")
		return 2
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		fmt.Fprintln(errorOutput, "cannot open the reconciliation database")
		return 1
	}
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(2)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	already, err := validateExperimentReconcileRows(ctx, db, &payload.Plan)
	if err != nil {
		fmt.Fprintln(errorOutput, err)
		return 1
	}
	result := map[string]any{"status": "planned", "entries": len(payload.Plan.Entries), "already_settled": already, "evidence_sha256": payload.Plan.EvidenceSHA256, "source_commit": Commit}
	if *apply {
		store := repository.NewUsageMonitorRepository(db)
		processed := 0
		for _, entry := range payload.Plan.Entries {
			price := decimal.NewFromInt(entry.Quota).Div(decimal.NewFromInt(500000))
			if err := store.CompleteExperimentChargeWithEvidence(ctx, entry.ID, price.InexactFloat64(), service.ExperimentChargeEvidence{
				Source: "operator_globalai_history", SHA256: payload.Plan.EvidenceSHA256, AccountID: entry.AccountID, UpstreamRequestID: entry.UpstreamRequestID,
			}); err != nil {
				// Each charge is atomic and repeatable. Persist explicit partial progress.
				_ = json.NewEncoder(output).Encode(map[string]any{"status": "partial", "processed": processed, "failed_charge_id": entry.ID, "evidence_sha256": payload.Plan.EvidenceSHA256})
				fmt.Fprintln(errorOutput, "settlement failed; retain this evidence and safely rerun the same plan")
				return 1
			}
			processed++
		}
		result["status"], result["processed"], result["backup_sha256"] = "settled", processed, *backup
	}
	if err := json.NewEncoder(output).Encode(result); err != nil {
		fmt.Fprintln(errorOutput, "cannot write reconciliation result")
		return 1
	}
	return 0
}
