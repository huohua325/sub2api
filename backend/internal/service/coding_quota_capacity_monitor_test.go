package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCodingCapacityMonitorUsesSameAccountProjectionAsWallet(t *testing.T) {
	store := &summaryReuseStore{codingReadTestStore: codingReadTestStore{rows: []CodingQuotaReading{{UserID: 1, Purpose: PurposeOpenAI, Used: "15.00000000", Pending: "2.00000000", Start: time.Now().Add(-time.Hour), End: time.Now().Add(time.Hour), Accounts: []CodingQuotaAccountReading{
		{AccountID: 10, Used: "10.00000000", Pending: "1.00000000", UnmeteredCount: 1},
		{AccountID: 20, Used: "5.00000000", Pending: "1.00000000", PendingCount: 1},
	}}}}}
	points := NewOfficialPointService(nil, store, nil)
	view := &UsageMonitorView{GeneratedAt: time.Now(), Users: []UsageMonitorUser{{ID: 1, Wallets: []UsageMonitorWallet{{Purpose: PurposeOpenAI}}}}, Parents: []UsageMonitorParent{
		{AccountID: 10, Purpose: PurposeOpenAI, Users: []UsageMonitorParentUser{{UserID: 1, ObservedShare: stringPtr("7.5000"), GatewayActualCost: "12"}}},
		{AccountID: 20, Purpose: PurposeOpenAI, Users: []UsageMonitorParentUser{}},
	}}
	points.ApplyMonitor(context.Background(), view)
	require.Equal(t, 1, store.reads, "account attribution must not add another ledger query")
	require.Equal(t, "17.00000000", *view.Users[0].Wallets[0].OfficialPoints)
	require.Equal(t, 34, *view.Users[0].Wallets[0].StudentPercent)
	require.Equal(t, "11.00000000", *view.Parents[0].Users[0].LedgerPoints)
	require.Equal(t, "partial", view.Parents[0].Users[0].LedgerStatus)
	require.Equal(t, "7.5000", *view.Parents[0].Users[0].ObservedShare, "legacy fee-share semantics remain compatible")
	require.Len(t, view.Parents[1].Users, 1, "a mother's new official window must not hide a charge in the personal period")
	require.Equal(t, "6.00000000", *view.Parents[1].Users[0].LedgerPoints)
	require.Equal(t, "pending", view.Parents[1].Users[0].LedgerStatus)
}
