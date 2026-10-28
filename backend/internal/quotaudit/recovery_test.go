package quotaudit

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func recoveryFixture() Snapshot {
	base := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	end := base.Add(7 * 24 * time.Hour)
	return Snapshot{AccountID: 10, From: base, To: base.Add(time.Hour),
		Observations: []Observation{
			{ID: 1, AccountID: 10, Purpose: "openai", At: base, End: end, Used: "69", Processed: true},
			{ID: 2, AccountID: 10, Purpose: "openai", At: base.Add(time.Minute), End: end.Add(90 * time.Second), Used: "68", Processed: true},
			{ID: 3, AccountID: 10, Purpose: "openai", At: base.Add(2 * time.Minute), End: end, Used: "69", Processed: true},
		}, Requests: []Request{
			{ID: 20, AccountID: 10, UserID: 1, Purpose: "openai", StartedAt: base, CompletedAt: base.Add(80 * time.Second), End: &end, Weight: "1", Points: "0.25000000", State: "settled", UpdatedAt: base.Add(3 * time.Minute)},
			{ID: 21, AccountID: 10, UserID: 2, Purpose: "openai", StartedAt: base, CompletedAt: base.Add(90 * time.Second), End: &end, Weight: "3", Points: "0.75000000", State: "settled", UpdatedAt: base.Add(3 * time.Minute)},
		}}
}

func TestPlanRecoveryCreditsOnlyProvenDuplicateAndIsStable(t *testing.T) {
	s := recoveryFixture()
	p, err := Build(s)
	require.NoError(t, err)
	require.Len(t, p.Episodes, 1)
	require.Empty(t, p.Review)
	require.Equal(t, "1.00000000", p.Episodes[0].Credit)
	require.Equal(t, "-0.25000000", p.Episodes[0].Entries[0].Delta)
	require.Equal(t, "-0.75000000", p.Episodes[0].Entries[1].Delta)
	s.Observations[0], s.Observations[2] = s.Observations[2], s.Observations[0]
	s.Requests[0], s.Requests[1] = s.Requests[1], s.Requests[0]
	reordered, err := Build(s)
	require.NoError(t, err)
	require.Equal(t, p, reordered)
	changed := p
	changed.Episodes = append([]Episode(nil), p.Episodes...)
	changed.Episodes[0].Credit = "2.00000000"
	require.NotEqual(t, Digest(p), Digest(changed), "review digest must cover the actual credit")
}

func TestPlanRecoveryRequiresUnambiguousEvidence(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Snapshot)
	}{
		{"natural window change", func(s *Snapshot) {
			s.Observations[1].End = s.Observations[1].End.Add(24 * time.Hour)
			s.Observations[2].End = s.Observations[1].End
		}},
		{"local reset", func(s *Snapshot) { s.Observations[1].Source = "codex_local_reset" }},
		{"incomplete billing", func(s *Snapshot) { s.Requests = s.Requests[:1] }},
		{"different settlement batch", func(s *Snapshot) { s.Requests[1].UpdatedAt = s.Requests[1].UpdatedAt.Add(time.Second) }},
		{"unsettled observation", func(s *Snapshot) { s.Observations[2].Processed = false }},
		{"partial true increment", func(s *Snapshot) { s.Observations[2].Used = "70" }},
		{"confirmed reset", func(s *Snapshot) { s.Resets = []Reset{{EffectiveAt: s.Observations[1].At, Status: "confirmed"}} }},
		{"conflicting same-second reading", func(s *Snapshot) {
			o := s.Observations[1]
			o.ID = 4
			o.Used = "70"
			s.Observations = append(s.Observations, o)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := recoveryFixture()
			tc.change(&s)
			p, err := Build(s)
			require.NoError(t, err)
			require.Empty(t, p.Episodes)
		})
	}
}
