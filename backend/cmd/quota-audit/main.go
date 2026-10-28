// quota-audit exports a read-only preview by default. Applying a reviewed plan
// requires its exact digest, operator identity and reason; no startup hook uses it.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/quotaudit"
	"github.com/Wei-Shaw/sub2api/internal/repository"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	account := flag.Int64("account", 0, "one official parent account to audit")
	fromText := flag.String("from", "", "inclusive RFC3339 observation boundary")
	toText := flag.String("to", "", "inclusive RFC3339 boundary, at least 30s in the past")
	snapshotPath := flag.String("snapshot", "", "optional offline snapshot JSON; never connects to a database")
	planPath := flag.String("plan", "", "previously reviewed plan JSON, required for apply")
	apply := flag.Bool("apply", false, "append validated corrections; default is read-only")
	digest := flag.String("digest", "", "exact SHA256 digest of reviewed plan")
	actor := flag.String("actor", "", "real operator identity, not an invented administrator ID")
	note := flag.String("reason", "", "reason and evidence provenance")
	out := flag.String("out", "", "write result JSON to this file with mode 0600")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	var db *sql.DB
	openDB := func() error {
		dsn := os.Getenv("SUB2API_QUOTA_AUDIT_DSN")
		if dsn == "" {
			return fmt.Errorf("SUB2API_QUOTA_AUDIT_DSN is required for database access")
		}
		var err error
		db, err = sql.Open("postgres", dsn)
		if err != nil {
			return err
		}
		db.SetMaxOpenConns(2)
		return db.PingContext(ctx)
	}
	defer func() {
		if db != nil {
			_ = db.Close()
		}
	}()
	if *apply {
		if *planPath == "" || *snapshotPath != "" {
			return fmt.Errorf("apply requires -plan and cannot use an offline snapshot")
		}
		var plan quotaudit.Plan
		if err := readJSON(*planPath, &plan); err != nil {
			return err
		}
		if *digest == "" || *digest != quotaudit.Digest(plan) {
			return fmt.Errorf("reviewed plan digest is missing or does not match")
		}
		if err := openDB(); err != nil {
			return err
		}
		n, err := repository.NewUsageMonitorRepository(db).ApplyCodingQuotaAudit(ctx, plan, *digest, *actor, *note)
		if err != nil {
			return err
		}
		return output(*out, map[string]any{"inserted_corrections": n, "digest": *digest, "account_id": plan.AccountID})
	}
	from, err := time.Parse(time.RFC3339, *fromText)
	if err != nil {
		return fmt.Errorf("invalid -from: %w", err)
	}
	to, err := time.Parse(time.RFC3339, *toText)
	if err != nil {
		return fmt.Errorf("invalid -to: %w", err)
	}
	if to.After(time.Now().Add(-30 * time.Second)) {
		return fmt.Errorf("-to must be at least 30 seconds in the past")
	}
	s := quotaudit.Snapshot{AccountID: *account, From: from, To: to}
	if _, err = quotaudit.Build(s); err != nil {
		return err
	}
	if *snapshotPath != "" {
		if err = readJSON(*snapshotPath, &s); err != nil {
			return err
		}
		s.AccountID, s.From, s.To = *account, from, to
	} else {
		if err = openDB(); err != nil {
			return err
		}
		s, err = repository.NewUsageMonitorRepository(db).CodingQuotaAuditSnapshot(ctx, *account, from, to)
		if err != nil {
			return err
		}
	}
	plan, err := quotaudit.Build(s)
	if err != nil {
		return err
	}
	if err = output(*out, plan); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Read-only preview: %d proven episodes, %d findings for review, digest %s\n", len(plan.Episodes), len(plan.Review), quotaudit.Digest(plan))
	return nil
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}
func output(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if path != "" {
		return os.WriteFile(path, b, 0600)
	}
	_, err = os.Stdout.Write(b)
	return err
}
