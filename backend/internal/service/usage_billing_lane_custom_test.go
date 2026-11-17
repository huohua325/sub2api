//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUsageBillingLaneCustomEffectsKeepIndividualSettlement(t *testing.T) {
	cases := map[string]func(*UsageBillingCommand){
		"purpose":            func(c *UsageBillingCommand) { c.Purpose = PurposeExperiment },
		"purpose cost":       func(c *UsageBillingCommand) { c.PurposeCost = 1 },
		"coding charge":      func(c *UsageBillingCommand) { c.CodingQuota = &CodingQuotaCharge{} },
		"reservation":        func(c *UsageBillingCommand) { c.CodingReservationID = "reservation" },
		"voice reservations": func(c *UsageBillingCommand) { c.CodingReservationIDs = []string{"turn"} },
		"pending experiment": func(c *UsageBillingCommand) { c.ExperimentPending = true },
		"experiment request": func(c *UsageBillingCommand) { c.ExperimentUpstreamRequestID = "upstream" },
		"experiment effects": func(c *UsageBillingCommand) { c.ExperimentEffects = &ExperimentBillingEffects{} },
	}
	for name, prepare := range cases {
		t.Run(name, func(t *testing.T) {
			repo := newLaneTestRepo()
			combiner := NewUsageBillingLaneCombiner(repo)
			t.Cleanup(combiner.Stop)
			cmd := laneTestCommand(1, name, 1)
			prepare(cmd)
			result, err := combiner.Apply(context.Background(), cmd)
			require.NoError(t, err)
			require.True(t, result.Applied)
			require.Zero(t, combiner.Stats().SingleApplies, "custom effects must bypass the batch lane")
			require.Empty(t, repo.batchCalls)
			require.Equal(t, []string{name}, repo.applyCalls)
		})
	}
}
