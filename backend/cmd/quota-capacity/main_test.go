package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPreviewNeverNeedsDatabaseAndApplyNeedsExactDigest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "event.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"id":"owner:test","factor":"2"}`), 0600))
	t.Setenv("SUB2API_QUOTA_AUDIT_DSN", "")
	var out bytes.Buffer
	require.NoError(t, run([]string{"-event", path}, &out))
	var preview struct {
		Digest  string `json:"digest"`
		Applied bool   `json:"applied"`
	}
	require.NoError(t, json.Unmarshal(out.Bytes(), &preview))
	require.Len(t, preview.Digest, 64)
	require.False(t, preview.Applied)
	require.ErrorContains(t, run([]string{"-event", path, "-apply"}, &out), "exact reviewed preview digest")
	require.ErrorContains(t, run([]string{"-event", path, "-apply", "-digest", preview.Digest}, &out), "DSN is required")
	require.Error(t, run([]string{"-event", path, "unexpected"}, &out))
}
