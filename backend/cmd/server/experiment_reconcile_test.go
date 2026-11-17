package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func experimentCLIInput(t *testing.T, started time.Time) experimentReconcileInput {
	t.Helper()
	evidence, err := json.Marshal(map[string]any{"schema": 1, "provider": "globalai.vip", "quota_per_yuan": 500000,
		"coverage": "supplier_export", "logs": []map[string]any{{"account_id": 8, "type": 2, "upstream_request_id": "supplier-1", "quota": 10247}}})
	require.NoError(t, err)
	digest := sha256.Sum256(evidence)
	return experimentReconcileInput{Schema: 1, EvidenceBase64: base64.StdEncoding.EncodeToString(evidence), Plan: experimentReconcilePlan{
		Schema: 1, Provider: "globalai.vip", QuotaPerYuan: 500000, EvidenceSHA256: hex.EncodeToString(digest[:]),
		Entries: []experimentReconcileEntry{{ID: 1, RequestID: "local-1", APIKeyID: 43, UserID: 3, AccountID: 8,
			UpstreamRequestID: "supplier-1", StartedAt: started, Quota: 10247, AmountYuan: "0.020494"}},
	}}
}

func TestExperimentReconcileEvidenceValidation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*experimentReconcileInput)
		valid  bool
	}{
		{"valid", func(*experimentReconcileInput) {}, true},
		{"wrong_digest", func(x *experimentReconcileInput) { x.Plan.EvidenceSHA256 = strings.Repeat("0", 64) }, false},
		{"invalid_base64", func(x *experimentReconcileInput) { x.EvidenceBase64 = "!bad" }, false},
		{"wrong_provider", func(x *experimentReconcileInput) { x.Plan.Provider = "elsewhere" }, false},
		{"wrong_currency_unit", func(x *experimentReconcileInput) { x.Plan.QuotaPerYuan = 1 }, false},
		{"wrong_quota", func(x *experimentReconcileInput) { x.Plan.Entries[0].Quota++ }, false},
		{"wrong_amount", func(x *experimentReconcileInput) { x.Plan.Entries[0].AmountYuan = "0.1" }, false},
		{"wrong_account", func(x *experimentReconcileInput) { x.Plan.Entries[0].AccountID = 13 }, false},
		{"wrong_request", func(x *experimentReconcileInput) { x.Plan.Entries[0].UpstreamRequestID = "other" }, false},
		{"duplicate_charge", func(x *experimentReconcileInput) { x.Plan.Entries = append(x.Plan.Entries, x.Plan.Entries[0]) }, false},
		{"zero_user", func(x *experimentReconcileInput) { x.Plan.Entries[0].UserID = 0 }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := experimentCLIInput(t, time.Now().UTC())
			tc.change(&input)
			raw, err := json.Marshal(input)
			require.NoError(t, err)
			_, err = parseExperimentReconcile(bytes.NewReader(raw))
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestExperimentReconcileLegacyCapture(t *testing.T) {
	input := experimentCLIInput(t, time.Now().UTC())
	evidence := []byte(`[{"account_id":8,"path":"/api/log/token","status":200,"logs":[{"type":2,"request_id":"supplier-1","quota":10247}]}]`)
	digest := sha256.Sum256(evidence)
	input.EvidenceBase64, input.Plan.EvidenceSHA256 = base64.StdEncoding.EncodeToString(evidence), hex.EncodeToString(digest[:])
	raw, err := json.Marshal(input)
	require.NoError(t, err)
	_, err = parseExperimentReconcile(bytes.NewReader(raw))
	require.NoError(t, err)
}

func TestExperimentReconcileDatabaseIdentityPreflight(t *testing.T) {
	for _, tc := range []struct {
		name, upstream, state string
		amount                any
		wantError             bool
	}{
		{"pending", "supplier-1", "pending", nil, false},
		{"already_settled", "supplier-1", "settled", "0.02049400", false},
		{"identity_changed", "other", "pending", nil, true},
		{"settled_conflict", "supplier-1", "settled", "0.03000000", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer func() { _ = db.Close() }()
			started := time.Now().UTC()
			input := experimentCLIInput(t, started)
			mock.ExpectQuery("SELECT to_regclass").WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
			mock.ExpectQuery("SELECT request_id,api_key_id").WithArgs(int64(1)).WillReturnRows(
				sqlmock.NewRows([]string{"request_id", "key", "user", "account", "upstream", "started", "state", "amount"}).
					AddRow("local-1", 43, 3, 8, tc.upstream, started, tc.state, tc.amount))
			_, err = validateExperimentReconcileRows(context.Background(), db, &input.Plan)
			if tc.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestExperimentReconcileDBDryRunAndIdempotentApply(t *testing.T) {
	dsn := os.Getenv("SUB2API_QUOTA_TEST_DSN")
	if dsn == "" {
		t.Skip("SUB2API_QUOTA_TEST_DSN is required for actual isolated PostgreSQL CLI validation")
	}
	admin, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	schema := "experiment_cli_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = admin.Exec(`CREATE SCHEMA ` + schema)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`)
		_ = admin.Close()
	})
	if parsed, parseErr := url.Parse(dsn); parseErr == nil && parsed.Scheme != "" {
		query := parsed.Query()
		query.Set("search_path", schema)
		parsed.RawQuery = query.Encode()
		dsn = parsed.String()
	} else {
		dsn += " search_path=" + schema
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	_, err = db.Exec(`CREATE TABLE settings(key text PRIMARY KEY,value text);
CREATE TABLE user_purpose_allowances(user_id bigint,purpose text,limit_amount double precision,used_amount double precision,period text,window_start timestamptz,updated_at timestamptz DEFAULT now(),PRIMARY KEY(user_id,purpose));
CREATE TABLE experiment_quota_charges(id bigint PRIMARY KEY,request_id text,api_key_id bigint,user_id bigint,account_id bigint,upstream_request_id text,started_at timestamptz,amount numeric(20,8),billing_effects jsonb,state text,updated_at timestamptz DEFAULT now(),attempts integer NOT NULL DEFAULT 0,next_retry_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE usage_logs(id bigint PRIMARY KEY,request_id text,api_key_id bigint,input_cost numeric(20,8),total_cost numeric(20,8),actual_cost numeric(20,8));`)
	require.NoError(t, err)
	migration, err := os.ReadFile(filepath.Join("..", "..", "migrations", "272_experiment_accounting_guard.sql"))
	require.NoError(t, err)
	_, err = db.Exec(string(migration))
	require.NoError(t, err)
	started := service.ShanghaiMonthStart(time.Now()).Add(time.Minute)
	_, err = db.Exec(`INSERT INTO user_purpose_allowances VALUES(3,'experiment',500,10,'month',$1,now())`, started)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO experiment_quota_charges(id,request_id,api_key_id,user_id,account_id,upstream_request_id,started_at,amount,billing_effects,state,updated_at) VALUES(1,'local-1',43,3,8,'supplier-1',$1,NULL,'{}','pending',now())`, started)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO usage_logs VALUES(1,'local-1',43,0,0,0)`)
	require.NoError(t, err)
	t.Setenv("SUB2API_RECONCILE_DSN", dsn)
	oldCommit := Commit
	Commit = strings.Repeat("a", 40)
	t.Cleanup(func() { Commit = oldCommit })
	input := experimentCLIInput(t, started)
	raw, err := json.Marshal(input)
	require.NoError(t, err)
	var output, stderr bytes.Buffer
	require.Equal(t, 0, experimentReconcileCommand(nil, bytes.NewReader(raw), &output, &stderr), stderr.String())
	var used string
	require.NoError(t, db.QueryRow(`SELECT used_amount::text FROM user_purpose_allowances`).Scan(&used))
	require.Equal(t, "10", used)
	for range 2 {
		output.Reset()
		stderr.Reset()
		require.Equal(t, 0, experimentReconcileCommand([]string{"--apply", "--backup-sha256", strings.Repeat("b", 64), "--expected-source-commit", Commit}, bytes.NewReader(raw), &output, &stderr), stderr.String())
	}
	var events int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM experiment_quota_settlement_events`).Scan(&events))
	require.Equal(t, 1, events)
	require.NoError(t, db.QueryRow(`SELECT used_amount::text FROM user_purpose_allowances`).Scan(&used))
	require.Equal(t, "10.020494", used)
	var original string
	require.NoError(t, db.QueryRow(`SELECT original_usage_costs->0->>'actual_cost' FROM experiment_quota_settlement_events`).Scan(&original))
	require.Equal(t, "0.00000000", original)
}
