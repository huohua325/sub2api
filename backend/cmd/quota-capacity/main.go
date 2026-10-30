// quota-capacity previews explicit capacity evidence by default. No startup path
// invokes it; applying requires the exact preview digest and retained observations.
package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/repository"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("quota-capacity", flag.ContinueOnError)
	path := flags.String("event", "", "operator-reviewed capacity event JSON")
	apply := flags.Bool("apply", false, "persist this confirmed event; default is read-only preview")
	pinned := flags.String("digest", "", "exact SHA256 digest printed by preview")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *path == "" {
		return fmt.Errorf("-event is required; unexpected positional arguments are refused")
	}
	raw, err := os.ReadFile(*path)
	if err != nil {
		return err
	}
	var change repository.CodingCapacityChange
	if err = json.Unmarshal(raw, &change); err != nil {
		return err
	}
	canonical, err := json.Marshal(change)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(canonical)
	digest := hex.EncodeToString(sum[:])
	if *apply {
		if *pinned != digest {
			return fmt.Errorf("apply requires the exact reviewed preview digest")
		}
		dsn := os.Getenv("SUB2API_QUOTA_AUDIT_DSN")
		if dsn == "" {
			return fmt.Errorf("SUB2API_QUOTA_AUDIT_DSN is required for apply")
		}
		db, err := sql.Open("postgres", dsn)
		if err != nil {
			return err
		}
		defer func() { _ = db.Close() }()
		db.SetMaxOpenConns(2)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err = repository.NewUsageMonitorRepository(db).ConfirmCodingCapacityChange(ctx, change); err != nil {
			return err
		}
	}
	return json.NewEncoder(out).Encode(map[string]any{"event": change, "digest": digest, "applied": *apply})
}
