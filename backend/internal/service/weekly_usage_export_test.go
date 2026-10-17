package service

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestWeeklyUserCSVSample(t *testing.T) {
	limit := 100.0
	usage := 25.0
	rate := 0.5
	rows := buildUserCSV([]WeeklyUserInput{
		{
			UserID: 1, Email: "student-a@example.com", Balance: 100, GroupID: int64Ptr(7), GroupName: "openai-class",
			Platform: "openai", Requests: 12, InputTokens: 100000, OutputTokens: 20000, CacheRead: 50000,
			CatalogCost: 40, ActualCost: 20, Rates: []WeeklyCount{{Label: "0.5000", Count: 12}}, GroupRate: &rate,
			Models:      []WeeklyModelCount{{Name: "gpt-5.6-sol", Count: 10, Catalog: 30}, {Name: "gpt-5.6-sol-thinking", Count: 2, Catalog: 10}},
			Reasoning:   []WeeklyReasonCount{{Requested: "high", Upstream: "high", Count: 2}},
			FastCount:   0,
			CurrentWeek: true, HasQuota: true, WeeklyLimit: &limit, WeeklyUsage: usage,
		},
		{
			UserID: 1, Email: "student-a@example.com", Balance: 100, GroupID: int64Ptr(8), GroupName: "openai-spare",
			Platform: "openai", Requests: 1, InputTokens: 1000, OutputTokens: 500,
			CatalogCost: 2, ActualCost: 1, Rates: []WeeklyCount{{Label: "0.5000", Count: 1}}, GroupRate: &rate,
			Models:    []WeeklyModelCount{{Name: "gpt-5.6-sol", Count: 1, Catalog: 2}},
			Tiers:     []WeeklyCount{{Label: "priority", Count: 1}},
			FastCount: 1, CurrentWeek: true, HasQuota: true, WeeklyLimit: &limit, WeeklyUsage: usage,
		},
		{
			UserID: 2, Email: "student-b@example.com", Balance: 0, Synthetic: true, Platform: "openai",
			CurrentWeek: true, HasQuota: true, WeeklyLimit: &limit, WeeklyUsage: 0,
		},
	})
	if len(rows) != 4 {
		t.Fatalf("rows = %d", len(rows))
	}
	class := rows[1]
	if class[16] != "25" || class[18] != "是" || class[20] != "high×2" || class[23] != "缓存；思考" {
		t.Fatalf("class row = %#v", class)
	}
	spare := rows[2]
	if spare[18] != "否" || spare[20] != "无" || spare[21] != "1" || spare[22] != "priority×1" || spare[23] != "Fast" {
		t.Fatalf("spare row = %#v", spare)
	}
	idle := rows[3]
	if idle[1] != "（本周无请求）" || idle[13] != "阻断" || idle[18] != "是" || idle[19] != "" || idle[20] != "无" || idle[21] != "0" || idle[22] != "无" || idle[23] != "无" {
		t.Fatalf("idle row = %#v", idle)
	}
}

func TestWeeklyAccountCSVSample(t *testing.T) {
	reset := time.Date(2026, 10, 20, 15, 4, 5, 0, time.UTC)
	prevAt := time.Date(2026, 10, 14, 0, 0, 0, 0, time.FixedZone("CST", 8*3600))
	weekStart := time.Date(2026, 10, 12, 0, 0, 0, 0, time.FixedZone("CST", 8*3600))
	weekEnd := weekStart.AddDate(0, 0, 7)
	official := 18.0
	prev := 10.0
	sinceCatalog := 40.0
	sinceActual := 20.0
	limit := 200.0
	blocked := 100.0
	weekActual := 40.0
	rows := buildAccountCSV([]WeeklyAccountInput{
		{
			ID: 5, Name: "chatgpt pro 100", Platform: "openai", Plan: "pro", Status: "active",
			Groups: []string{"openai-class"}, Requests: 40, DistinctUsers: 12,
			CatalogCost: 80, ActualCost: 40, WeekStart: weekStart, WeekEnd: weekEnd, Timezone: "Asia/Shanghai",
			CurrentWeek: true, OfficialPercent: &official, OfficialReset: &reset, OfficialSource: "codex_7d",
			HasPrev: true, PrevPercent: &prev, PrevReset: &reset, PrevExportedAt: prevAt,
			SinceCatalog: &sinceCatalog, SinceActual: &sinceActual,
			LimitSum: &limit, BlockedLimit: &blocked, WeekActualSum: &weekActual,
		},
		{
			ID: 1, Name: "grok1", Platform: "grok", Plan: "SuperGrok", Status: "active",
			Groups: []string{"grok-default"}, Requests: 3, DistinctUsers: 2,
			CatalogCost: 1.2, ActualCost: 1.2, WeekStart: weekStart, WeekEnd: weekEnd, Timezone: "Asia/Shanghai",
			CurrentWeek: true, OfficialPercent: floatPtr(37), OfficialSource: "grok_billing",
			LimitSum: floatPtr(150), BlockedLimit: floatPtr(0), WeekActualSum: floatPtr(1.2),
		},
	})
	codex := findCSVRow(rows, "chatgpt pro 100")
	if codex[12] != "18.0000" || codex[16] != "8.0000" || codex[17] != "是" || codex[18] != "" || codex[21] != "5.000000" || codex[25] != "10.000000" || codex[26] != "10.000000" {
		t.Fatalf("codex row delta/expected = %q %q %q %q %q %q %q", codex[12], codex[16], codex[17], codex[18], codex[21], codex[25], codex[26])
	}
	grok := findCSVRow(rows, "grok1")
	if grok[17] != "否" || grok[18] != "无上次导出" || grok[16] != "" || grok[19] != "" || grok[21] != "" || grok[25] != "" || grok[22] != "150.000000" {
		t.Fatalf("grok first export = %#v", grok)
	}
}

func TestTwoComparableParentsLeaveExpectedEmpty(t *testing.T) {
	reset := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	prevAt := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	weekStart := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	base := WeeklyAccountInput{
		Platform: "openai", Status: "active", WeekStart: weekStart, WeekEnd: weekStart.AddDate(0, 0, 7),
		Timezone: "UTC", CurrentWeek: true, OfficialReset: &reset, OfficialSource: "codex_7d",
		HasPrev: true, PrevReset: &reset, PrevExportedAt: prevAt,
		LimitSum: floatPtr(100), BlockedLimit: floatPtr(0),
	}
	a := base
	a.ID, a.Name = 1, "pro-a"
	a.OfficialPercent = floatPtr(18)
	a.PrevPercent = floatPtr(10)
	a.SinceCatalog = floatPtr(40)
	a.SinceActual = floatPtr(20)
	b := base
	b.ID, b.Name = 2, "pro-b"
	b.OfficialPercent = floatPtr(14)
	b.PrevPercent = floatPtr(10)
	b.SinceCatalog = floatPtr(16)
	b.SinceActual = floatPtr(8)
	rows := buildAccountCSV([]WeeklyAccountInput{b, a})
	if rows[1][0] != "pro-a" || rows[1][17] != "是" || rows[1][18] != "" || rows[1][21] != "5.000000" || rows[1][25] != "" || rows[1][26] != "" {
		t.Fatalf("pro-a = %q %q %q %q %q %q", rows[1][0], rows[1][17], rows[1][18], rows[1][21], rows[1][25], rows[1][26])
	}
	if rows[2][0] != "pro-b" || rows[2][17] != "是" || rows[2][18] != "" || rows[2][21] != "4.000000" || rows[2][25] != "" || rows[2][26] != "" {
		t.Fatalf("pro-b = %q %q %q %q %q %q", rows[2][0], rows[2][17], rows[2][18], rows[2][21], rows[2][25], rows[2][26])
	}
}

func TestWeeklyMigrationSnapshotAmountsNullable(t *testing.T) {
	body, err := os.ReadFile("../../migrations/246_admin_weekly_usage_export_snapshots.sql")
	if err != nil {
		t.Fatal(err)
	}
	sqlText := string(body)
	if !strings.Contains(sqlText, "IF NOT EXISTS") {
		t.Fatal("migration must be repeatable")
	}
	if strings.Contains(strings.ToUpper(sqlText), "DROP ") {
		t.Fatal("migration must not drop")
	}
	if strings.Contains(sqlText, "catalog_cost_since_prev NUMERIC(20,10) NOT NULL") || strings.Contains(sqlText, "DEFAULT 0") && strings.Contains(sqlText, "catalog_cost_since_prev") {
		t.Fatal("since-prev amounts must stay nullable")
	}
}

func TestFilterCSVOmitsOfficialComparison(t *testing.T) {
	rate := 0.5
	start := time.Date(2026, 10, 12, 0, 0, 0, 0, time.FixedZone("CST", 8*3600))
	end := start.AddDate(0, 0, 6)
	rows := buildFilterCSV([]WeeklyUserInput{
		{
			UserID: 1, Email: "student-a@example.com", Balance: 100, GroupID: int64Ptr(7), GroupName: "openai-class",
			Platform: "openai", Requests: 4, InputTokens: 10, OutputTokens: 2, CacheRead: 3,
			CatalogCost: 1.5, ActualCost: 0.75, Rates: []WeeklyCount{{Label: "0.5000", Count: 4}}, GroupRate: &rate,
			Models:    []WeeklyModelCount{{Name: "gpt-5.6-sol", Count: 4, Catalog: 1.5}},
			Reasoning: []WeeklyReasonCount{{Requested: "high", Upstream: "high", Count: 1}},
		},
	}, FilterExportMeta{Start: start, End: end, Group: "openai-class", Account: "全部"})
	if len(rows) != 2 {
		t.Fatalf("rows = %d", len(rows))
	}
	header := strings.Join(rows[0], ",")
	if strings.Contains(header, "学生卡已用百分比") || strings.Contains(header, "官方已用百分比") || strings.Contains(header, "预期实扣") {
		t.Fatalf("header = %s", header)
	}
	row := rows[1]
	if row[0] != formatFilterTime(start) || row[1] != formatFilterTime(end) || row[2] != "openai-class" || row[3] != "全部" {
		t.Fatalf("meta = %#v", row[:4])
	}
	if row[4] != "student-a@example.com" || row[17] != "可用" || row[22] != "缓存；思考" || row[23] != filterExportNote {
		t.Fatalf("row = %#v", row)
	}
}

func TestUsageSliceFilterClause(t *testing.T) {
	groupID := int64(7)
	accountID := int64(5)
	clause, args := (usageSliceFilter{GroupID: &groupID, AccountID: &accountID, AccountFamily: true}).clause(3)
	if clause != " AND ul.group_id = $3 AND COALESCE(a.parent_account_id, ul.account_id) = $4" || len(args) != 2 {
		t.Fatalf("family clause = %q %#v", clause, args)
	}
	clause, args = (usageSliceFilter{AccountID: &accountID}).clause(3)
	if clause != " AND ul.account_id = $3" || len(args) != 1 {
		t.Fatalf("shadow clause = %q %#v", clause, args)
	}
	clause, args = (usageSliceFilter{}).clause(3)
	if clause != "" || args != nil {
		t.Fatalf("empty clause = %q %#v", clause, args)
	}
}

func TestParseFilterDatesHalfOpen(t *testing.T) {
	now := time.Date(2026, 10, 17, 15, 0, 0, 0, time.Local)
	start, end, err := parseFilterDates("2026-10-12", "2026-10-17", now, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !end.Equal(start.AddDate(0, 0, 6)) {
		t.Fatalf("end = %s start = %s", end, start)
	}
	if _, _, err := parseFilterDates("2026-10-17", "2026-10-12", now, 0); err == nil || !strings.Contains(err.Error(), "FILTER_RANGE_ORDER") {
		t.Fatalf("order err = %v", err)
	}
	if _, _, err := parseFilterDates("2026-10-18", "2026-10-18", now, 0); err == nil || !strings.Contains(err.Error(), "FILTER_RANGE_IN_FUTURE") {
		t.Fatalf("future err = %v", err)
	}
	if _, _, err := parseFilterDates("2020-01-01", "2020-01-02", now, 30); err == nil || !strings.Contains(err.Error(), "FILTER_OUTSIDE_RETENTION") {
		t.Fatalf("retention err = %v", err)
	}
}

func findCSVRow(rows [][]string, name string) []string {
	for _, row := range rows {
		if len(row) > 0 && row[0] == name {
			return row
		}
	}
	return nil
}
