package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/lib/pq"
)

const resolvedPlatformExpr = `CASE WHEN g.platform IS NULL OR g.platform = 'composite' THEN a.platform ELSE g.platform END`

// WeeklyExportWeeks 是管理端周选择的响应。
type WeeklyExportWeeks struct {
	Timezone           string             `json:"timezone"`
	RetentionDays      int                `json:"retention_days"`
	RetentionUnlimited bool               `json:"retention_unlimited"`
	UsageCutoff        *time.Time         `json:"usage_cutoff"`
	Weeks              []WeeklyExportWeek `json:"weeks"`
}

// WeeklyExportWeek 是一个网关周。
type WeeklyExportWeek struct {
	Start   time.Time `json:"start"`
	End     time.Time `json:"end"`
	Current bool      `json:"current"`
}

// WeeklyUsageExportService 组装管理员周报。导出不请求上游。
type WeeklyUsageExportService struct {
	db   *sql.DB
	agg  *DashboardAggregationService
	mu   sync.Mutex
	busy bool
}

// NewWeeklyUsageExportService 创建周报导出服务。
func NewWeeklyUsageExportService(db *sql.DB, agg *DashboardAggregationService) *WeeklyUsageExportService {
	return &WeeklyUsageExportService{db: db, agg: agg}
}

// Weeks 返回仍在保留期内的网关周，不写快照。
func (s *WeeklyUsageExportService) Weeks(ctx context.Context) (*WeeklyExportWeeks, error) {
	days, err := s.agg.RequestRetentionDays(ctx)
	if err != nil {
		return nil, infraerrors.ServiceUnavailable("WEEKLY_EXPORT_RETENTION_UNKNOWN", "无法读取用量保留天数")
	}
	now := timezone.Now()
	out := &WeeklyExportWeeks{
		Timezone:           timezone.Name(),
		RetentionDays:      days,
		RetentionUnlimited: days == 0,
		Weeks:              listGatewayWeeks(now, days),
	}
	if days > 0 {
		cutoff := now.UTC().AddDate(0, 0, -days)
		out.UsageCutoff = &cutoff
	}
	return out, nil
}

// Export 生成一个网关周的 zip。当前周会写入官方额度快照。
func (s *WeeklyUsageExportService) Export(ctx context.Context, weekStartRaw string, userID int64) ([]byte, string, error) {
	if !s.tryLock() {
		return nil, "", infraerrors.TooManyRequests("WEEKLY_EXPORT_BUSY", "已有周报导出在进行")
	}
	defer s.unlock()

	days, err := s.agg.RequestRetentionDays(ctx)
	if err != nil {
		return nil, "", infraerrors.ServiceUnavailable("WEEKLY_EXPORT_RETENTION_UNKNOWN", "无法读取用量保留天数")
	}
	now := timezone.Now()
	start, err := parseExportWeek(weekStartRaw, now, days)
	if err != nil {
		return nil, "", err
	}
	end := start.AddDate(0, 0, 7)
	current := start.Equal(timezone.StartOfWeek(now))
	upper := end
	if current && now.Before(end) {
		upper = now
	}
	if s.db == nil {
		return nil, "", infraerrors.ServiceUnavailable("WEEKLY_EXPORT_UNAVAILABLE", "周报导出尚未就绪")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, "SET LOCAL statement_timeout = '15s'"); err != nil {
		return nil, "", mapExportQueryErr(err)
	}

	userRows, accountInputs, snaps, err := s.collect(ctx, tx, start, end, upper, now, current, userID, usageSliceFilter{})
	if err != nil {
		return nil, "", mapExportQueryErr(err)
	}
	if current {
		if err := insertWeeklySnapshots(ctx, tx, snaps); err != nil {
			return nil, "", mapExportQueryErr(err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, "", mapExportQueryErr(err)
	}
	body, err := writeWeeklyZip(buildUserCSV(userRows), buildAccountCSV(accountInputs))
	if err != nil {
		return nil, "", err
	}
	return body, weeklyZipName(start, end), nil
}

// ExportFiltered 按任意日期、分组和账号汇总用量。不写快照，也不计算官方 80%。
func (s *WeeklyUsageExportService) ExportFiltered(ctx context.Context, startDate, endDate, groupRaw, accountRaw string) ([]byte, string, error) {
	if !s.tryLock() {
		return nil, "", infraerrors.TooManyRequests("FILTER_EXPORT_BUSY", "已有用量汇总导出在进行")
	}
	defer s.unlock()

	days, err := s.agg.RequestRetentionDays(ctx)
	if err != nil {
		return nil, "", infraerrors.ServiceUnavailable("WEEKLY_EXPORT_RETENTION_UNKNOWN", "无法读取用量保留天数")
	}
	now := timezone.Now()
	start, endExclusive, err := parseFilterDates(startDate, endDate, now, days)
	if err != nil {
		return nil, "", err
	}
	groupID, err := optionalExportID(groupRaw, "FILTER_GROUP_INVALID", "分组编号无效")
	if err != nil {
		return nil, "", err
	}
	accountID, err := optionalExportID(accountRaw, "FILTER_ACCOUNT_INVALID", "账号编号无效")
	if err != nil {
		return nil, "", err
	}
	if s.db == nil {
		return nil, "", infraerrors.ServiceUnavailable("WEEKLY_EXPORT_UNAVAILABLE", "周报导出尚未就绪")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, "SET LOCAL statement_timeout = '15s'"); err != nil {
		return nil, "", mapExportQueryErr(err)
	}
	groupLabel, accountLabel, filter, err := lookupFilterLabels(ctx, tx, groupID, accountID)
	if err != nil {
		return nil, "", mapExportQueryErr(err)
	}
	users, err := loadUserAggs(ctx, tx, start, endExclusive, filter)
	if err != nil {
		return nil, "", mapExportQueryErr(err)
	}
	if err := attachUserDetails(ctx, tx, users); err != nil {
		return nil, "", mapExportQueryErr(err)
	}
	if err := tx.Commit(); err != nil {
		return nil, "", mapExportQueryErr(err)
	}
	body, err := writeCSV(buildFilterCSV(users, FilterExportMeta{
		Start: start, End: endExclusive, Group: groupLabel, Account: accountLabel,
	}))
	if err != nil {
		return nil, "", err
	}
	return body, filterCSVName(start, endExclusive, groupID, accountID), nil
}

func optionalExportID(raw, reason, message string) (*int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return nil, infraerrors.BadRequest(reason, message)
	}
	return &id, nil
}

func parseFilterDates(startRaw, endRaw string, now time.Time, retentionDays int) (time.Time, time.Time, error) {
	startRaw = strings.TrimSpace(startRaw)
	endRaw = strings.TrimSpace(endRaw)
	if startRaw == "" || endRaw == "" {
		return time.Time{}, time.Time{}, infraerrors.BadRequest("FILTER_RANGE_REQUIRED", "请选择开始和结束日期")
	}
	start, err := timezone.ParseInUserLocation("2006-01-02", startRaw, "")
	if err != nil {
		return time.Time{}, time.Time{}, infraerrors.BadRequest("FILTER_RANGE_REQUIRED", "开始日期格式应为 YYYY-MM-DD")
	}
	endDay, err := timezone.ParseInUserLocation("2006-01-02", endRaw, "")
	if err != nil {
		return time.Time{}, time.Time{}, infraerrors.BadRequest("FILTER_RANGE_REQUIRED", "结束日期格式应为 YYYY-MM-DD")
	}
	endExclusive := endDay.AddDate(0, 0, 1)
	if !start.Before(endExclusive) {
		return time.Time{}, time.Time{}, infraerrors.BadRequest("FILTER_RANGE_ORDER", "开始日期不能晚于结束日期")
	}
	if start.After(now) {
		return time.Time{}, time.Time{}, infraerrors.BadRequest("FILTER_RANGE_IN_FUTURE", "开始日期还没到")
	}
	if start.AddDate(0, 0, 366).Before(endExclusive) {
		return time.Time{}, time.Time{}, infraerrors.BadRequest("FILTER_RANGE_TOO_LONG", "一次最多导出 366 天")
	}
	if retentionDays > 0 {
		cutoff := now.UTC().AddDate(0, 0, -retentionDays)
		if start.Before(cutoff) {
			return time.Time{}, time.Time{}, infraerrors.BadRequest("FILTER_OUTSIDE_RETENTION", "开始日期早于用量保留截止，导出的会是残缺合计")
		}
	}
	return start, endExclusive, nil
}

func lookupFilterLabels(ctx context.Context, tx *sql.Tx, groupID, accountID *int64) (string, string, usageSliceFilter, error) {
	filter := usageSliceFilter{GroupID: groupID}
	groupLabel := "全部"
	accountLabel := "全部"
	if groupID != nil {
		var name string
		err := tx.QueryRowContext(ctx, `SELECT name FROM groups WHERE id = $1`, *groupID).Scan(&name)
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", filter, infraerrors.BadRequest("FILTER_GROUP_NOT_FOUND", "分组不存在")
		}
		if err != nil {
			return "", "", filter, err
		}
		groupLabel = name
	}
	if accountID != nil {
		var name string
		var parent sql.NullInt64
		err := tx.QueryRowContext(ctx, `SELECT name, parent_account_id FROM accounts WHERE id = $1 AND deleted_at IS NULL`, *accountID).Scan(&name, &parent)
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", filter, infraerrors.BadRequest("FILTER_ACCOUNT_NOT_FOUND", "账号不存在")
		}
		if err != nil {
			return "", "", filter, err
		}
		accountLabel = name
		id := *accountID
		filter.AccountID = &id
		filter.AccountFamily = !parent.Valid
	}
	return groupLabel, accountLabel, filter, nil
}

func filterCSVName(start, endExclusive time.Time, groupID, accountID *int64) string {
	inclusiveEnd := endExclusive.AddDate(0, 0, -1)
	name := fmt.Sprintf("usage-filter_%s-%s", start.Format("20060102"), inclusiveEnd.Format("20060102"))
	if groupID != nil {
		name += fmt.Sprintf("_g%d", *groupID)
	}
	if accountID != nil {
		name += fmt.Sprintf("_a%d", *accountID)
	}
	return name + ".csv"
}

func (s *WeeklyUsageExportService) tryLock() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.busy {
		return false
	}
	s.busy = true
	return true
}

func (s *WeeklyUsageExportService) unlock() {
	s.mu.Lock()
	s.busy = false
	s.mu.Unlock()
}

func listGatewayWeeks(now time.Time, retentionDays int) []WeeklyExportWeek {
	var cutoff time.Time
	if retentionDays > 0 {
		cutoff = now.UTC().AddDate(0, 0, -retentionDays)
	}
	start := timezone.StartOfWeek(now)
	weeks := make([]WeeklyExportWeek, 0, 16)
	for i := 0; i < 53; i++ {
		if retentionDays > 0 && start.Before(cutoff) {
			break
		}
		weeks = append(weeks, WeeklyExportWeek{Start: start, End: start.AddDate(0, 0, 7), Current: i == 0})
		start = start.AddDate(0, 0, -7)
	}
	return weeks
}

func parseExportWeek(raw string, now time.Time, retentionDays int) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, infraerrors.BadRequest("WEEK_START_REQUIRED", "缺少或无法解析 week_start")
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, infraerrors.BadRequest("WEEK_START_REQUIRED", "缺少或无法解析 week_start")
	}
	start := timezone.StartOfWeek(parsed)
	if !start.Equal(parsed) {
		return time.Time{}, infraerrors.BadRequest("WEEK_NOT_ALIGNED", "网关周必须从周一开始")
	}
	current := timezone.StartOfWeek(now)
	if start.After(current) {
		return time.Time{}, infraerrors.BadRequest("WEEK_IN_FUTURE", "不能导出还没开始的网关周")
	}
	if retentionDays > 0 {
		cutoff := now.UTC().AddDate(0, 0, -retentionDays)
		if start.Before(cutoff) {
			return time.Time{}, infraerrors.BadRequest("WEEK_OUTSIDE_RETENTION", "这一周早于用量保留截止，导出的会是残缺合计")
		}
	}
	return start, nil
}

func mapExportQueryErr(err error) error {
	if err == nil {
		return nil
	}
	var pqErr *pq.Error
	if errors.As(err, &pqErr) && string(pqErr.Code) == "57014" {
		return infraerrors.ServiceUnavailable("WEEKLY_EXPORT_TIMEOUT", "周用量汇总查询超时")
	}
	return err
}

type userAggRow struct {
	WeeklyUserInput
}

func (s *WeeklyUsageExportService) collect(ctx context.Context, tx *sql.Tx, start, end, upper, now time.Time, current bool, actorID int64, filter usageSliceFilter) ([]WeeklyUserInput, []WeeklyAccountInput, []weeklySnapshot, error) {
	users, err := loadUserAggs(ctx, tx, start, upper, filter)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := attachUserDetails(ctx, tx, users); err != nil {
		return nil, nil, nil, err
	}
	quotas, err := loadQuotas(ctx, tx, userIDs(users))
	if err != nil {
		return nil, nil, nil, err
	}
	applyQuotas(users, quotas, now, current, quotaWeekModeTx(ctx, tx))
	synthetics, err := loadSyntheticUsers(ctx, tx, start, upper, now, current)
	if err != nil {
		return nil, nil, nil, err
	}
	users = append(users, synthetics...)

	parents, err := loadParentAccounts(ctx, tx)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := attachAccountGroups(ctx, tx, parents); err != nil {
		return nil, nil, nil, err
	}
	usageByParent, err := loadParentUsage(ctx, tx, start, upper, parentIDs(parents))
	if err != nil {
		return nil, nil, nil, err
	}
	platformSpend, err := loadPlatformSpend(ctx, tx, start, upper)
	if err != nil {
		return nil, nil, nil, err
	}
	limits, blocked, err := quotaTotalsNeedsUsers(ctx, tx, now)
	if err != nil {
		return nil, nil, nil, err
	}
	prev, err := loadPreviousSnapshots(ctx, tx, parentIDs(parents), now)
	if err != nil {
		return nil, nil, nil, err
	}
	var weekSnaps map[int64]weeklySnapshot
	if !current {
		weekSnaps, err = loadWeekSnapshots(ctx, tx, parentIDs(parents), start)
		if err != nil {
			return nil, nil, nil, err
		}
		prev, err = loadSnapshotsBefore(ctx, tx, weekSnaps)
		if err != nil {
			return nil, nil, nil, err
		}
	}
	since := map[int64]costPair{}
	if current {
		since, err = loadSincePrev(ctx, tx, prev, now)
		if err != nil {
			return nil, nil, nil, err
		}
	}

	accounts := make([]WeeklyAccountInput, 0, len(parents))
	snaps := make([]weeklySnapshot, 0, len(parents))
	for _, parent := range parents {
		item := WeeklyAccountInput{
			ID: parent.IDInt, Name: parent.Name, Platform: parent.Platform, Plan: parent.Plan, Status: parent.Status,
			Groups: parent.Groups, WeekStart: start, WeekEnd: end, Timezone: timezone.Name(), CurrentWeek: current,
		}
		if usage, ok := usageByParent[parent.IDInt]; ok {
			item.Requests = usage.Requests
			item.DistinctUsers = usage.Users
			item.CatalogCost = usage.Catalog
			item.ActualCost = usage.Actual
		}
		if spend, ok := platformSpend[parent.Platform]; ok {
			item.WeekActualSum = exportFloat(spend)
		} else if parent.Platform == "openai" || parent.Platform == "grok" {
			item.WeekActualSum = exportFloat(0)
		}
		if current {
			if total, ok := limits[parent.Platform]; ok {
				item.LimitSum = exportFloat(total)
			} else {
				item.LimitSum = exportFloat(0)
			}
			if total, ok := blocked[parent.Platform]; ok {
				item.BlockedLimit = exportFloat(total)
			} else {
				item.BlockedLimit = exportFloat(0)
			}
		}
		var snap weeklySnapshot
		if current {
			percent, resetAt, observed, source, plan := officialFromExtra(parent.Platform, parent.Extra, parent.Plan, now)
			item.OfficialPercent = percent
			item.OfficialReset = resetAt
			item.OfficialObserved = observed
			item.OfficialSource = source
			if plan != "" && parent.Platform == "grok" {
				item.Plan = plan
			}
			if previous, ok := prev[parent.IDInt]; ok {
				item.HasPrev = true
				item.PrevPercent = previous.OfficialUsedPercent
				item.PrevReset = previous.OfficialResetAt
				item.PrevExportedAt = previous.ExportedAt
				pair := since[parent.IDInt]
				item.SinceCatalog = exportFloat(pair.Catalog)
				item.SinceActual = exportFloat(pair.Actual)
			}
			snap = weeklySnapshot{
				AccountID: parent.IDInt, GatewayWeekStart: start, ExportedAt: now, ExportedByUserID: actorID,
				Platform: parent.Platform, OfficialUsedPercent: item.OfficialPercent, OfficialResetAt: item.OfficialReset,
				OfficialObservedAt: item.OfficialObserved, OfficialSource: item.OfficialSource,
				CatalogCostSincePrev: item.SinceCatalog, ActualCostSincePrev: item.SinceActual,
			}
		} else if stored, ok := weekSnaps[parent.IDInt]; ok {
			item.OfficialPercent = stored.OfficialUsedPercent
			item.OfficialReset = stored.OfficialResetAt
			item.OfficialObserved = stored.OfficialObservedAt
			item.OfficialSource = stored.OfficialSource
			item.SinceCatalog = stored.CatalogCostSincePrev
			item.SinceActual = stored.ActualCostSincePrev
			item.HasPrev = stored.CatalogCostSincePrev != nil || stored.ActualCostSincePrev != nil
			if previous, ok := prev[parent.IDInt]; ok {
				item.HasPrev = true
				item.PrevPercent = previous.OfficialUsedPercent
				item.PrevReset = previous.OfficialResetAt
				item.PrevExportedAt = previous.ExportedAt
			}
		} else {
			item.NoWeekSnapshot = true
		}
		if current {
			meta := accountMeta(item)
			snap.Comparable = meta.comparable
			if item.OfficialSource == "" {
				snap.OfficialSource = "none"
				item.OfficialSource = "none"
			}
			snaps = append(snaps, snap)
		}
		accounts = append(accounts, item)
	}
	return users, accounts, snaps, nil
}

type parentAccount struct {
	IDInt    int64
	Name     string
	Platform string
	Plan     string
	Status   string
	Groups   []string
	Extra    map[string]any
}

type parentUsage struct {
	Requests, Users int64
	Catalog, Actual float64
}

type costPair struct {
	Catalog, Actual float64
}

type weeklySnapshot struct {
	AccountID            int64
	GatewayWeekStart     time.Time
	ExportedAt           time.Time
	ExportedByUserID     int64
	Platform             string
	OfficialUsedPercent  *float64
	OfficialResetAt      *time.Time
	OfficialObservedAt   *time.Time
	OfficialSource       string
	CatalogCostSincePrev *float64
	ActualCostSincePrev  *float64
	Comparable           bool
}

type quotaRow struct {
	UserID   int64
	Platform string
	Limit    *float64
	Usage    float64
	Window   *time.Time
	Balance  float64
	Email    string
	Deleted  bool
}

// usageSliceFilter 只作用于筛选导出。零值与原来的网关周汇总相同。
type usageSliceFilter struct {
	GroupID       *int64
	AccountID     *int64
	AccountFamily bool
}

func (f usageSliceFilter) clause(from int) (string, []any) {
	var b strings.Builder
	var args []any
	n := from
	if f.GroupID != nil {
		fmt.Fprintf(&b, " AND ul.group_id = $%d", n)
		args = append(args, *f.GroupID)
		n++
	}
	if f.AccountID != nil {
		if f.AccountFamily {
			fmt.Fprintf(&b, " AND COALESCE(a.parent_account_id, ul.account_id) = $%d", n)
		} else {
			fmt.Fprintf(&b, " AND ul.account_id = $%d", n)
		}
		args = append(args, *f.AccountID)
	}
	return b.String(), args
}

func loadUserAggs(ctx context.Context, tx *sql.Tx, start, upper time.Time, filter usageSliceFilter) ([]WeeklyUserInput, error) {
	extraSQL, extraArgs := filter.clause(3)
	query := `
SELECT ul.user_id, ul.group_id, ` + resolvedPlatformExpr + `,
       MAX(g.name), MAX(g.rate_multiplier),
       BOOL_OR(ul.group_id IS NOT NULL AND g.id IS NULL),
       COUNT(*)::bigint,
       COALESCE(SUM(ul.input_tokens), 0)::bigint,
       COALESCE(SUM(ul.output_tokens), 0)::bigint,
       COALESCE(SUM(ul.cache_read_tokens), 0)::bigint,
       COALESCE(SUM(ul.cache_creation_tokens), 0)::bigint,
       COALESCE(SUM(ul.image_count), 0)::bigint,
       COALESCE(SUM(ul.image_input_tokens), 0)::bigint,
       COALESCE(SUM(ul.image_output_tokens), 0)::bigint,
       COALESCE(SUM(ul.video_count), 0)::bigint,
       COALESCE(SUM(ul.video_duration_seconds), 0)::bigint,
       COALESCE(SUM(ul.total_cost), 0)::float8,
       COALESCE(SUM(ul.actual_cost), 0)::float8
FROM usage_logs ul
JOIN accounts a ON a.id = ul.account_id
LEFT JOIN groups g ON g.id = ul.group_id
WHERE ul.created_at >= $1 AND ul.created_at < $2` + extraSQL + `
GROUP BY ul.user_id, ul.group_id, ` + resolvedPlatformExpr
	rows, err := tx.QueryContext(ctx, query, append([]any{start, upper}, extraArgs...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WeeklyUserInput
	index := map[string]int{}
	for rows.Next() {
		var item WeeklyUserInput
		var groupID sql.NullInt64
		var name sql.NullString
		var rate sql.NullFloat64
		var missing bool
		if err := rows.Scan(&item.UserID, &groupID, &item.Platform, &name, &rate, &missing,
			&item.Requests, &item.InputTokens, &item.OutputTokens, &item.CacheRead, &item.CacheCreate,
			&item.ImageCount, &item.ImageInput, &item.ImageOutput, &item.VideoCount, &item.VideoSeconds,
			&item.CatalogCost, &item.ActualCost); err != nil {
			return nil, err
		}
		if groupID.Valid {
			id := groupID.Int64
			item.GroupID = &id
		}
		if name.Valid {
			item.GroupName = name.String
		}
		if rate.Valid {
			item.GroupRate = exportFloat(rate.Float64)
		}
		item.GroupMissing = missing
		index[userKey(item.UserID, item.GroupID, item.Platform)] = len(out)
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := attachDimensions(ctx, tx, start, upper, filter, out, index); err != nil {
		return nil, err
	}
	return out, nil
}

func attachDimensions(ctx context.Context, tx *sql.Tx, start, upper time.Time, filter usageSliceFilter, out []WeeklyUserInput, index map[string]int) error {
	extraSQL, extraArgs := filter.clause(3)
	modelQuery := `
SELECT ul.user_id, ul.group_id, ` + resolvedPlatformExpr + `, ul.model, COUNT(*)::bigint, COALESCE(SUM(ul.total_cost), 0)::float8
FROM usage_logs ul
JOIN accounts a ON a.id = ul.account_id
LEFT JOIN groups g ON g.id = ul.group_id
WHERE ul.created_at >= $1 AND ul.created_at < $2` + extraSQL + `
GROUP BY ul.user_id, ul.group_id, ` + resolvedPlatformExpr + `, ul.model`
	if err := queryKeyed(ctx, tx, modelQuery, start, upper, extraArgs, func(rows *sql.Rows) error {
		var userID int64
		var groupID sql.NullInt64
		var platform, name string
		var count int64
		var catalog float64
		if err := rows.Scan(&userID, &groupID, &platform, &name, &count, &catalog); err != nil {
			return err
		}
		if i, ok := index[userKey(userID, nullGroupID(groupID), platform)]; ok {
			out[i].Models = append(out[i].Models, WeeklyModelCount{Name: name, Count: count, Catalog: catalog})
		}
		return nil
	}); err != nil {
		return err
	}
	rateQuery := `
SELECT ul.user_id, ul.group_id, ` + resolvedPlatformExpr + `, ul.rate_multiplier::float8, COUNT(*)::bigint
FROM usage_logs ul
JOIN accounts a ON a.id = ul.account_id
LEFT JOIN groups g ON g.id = ul.group_id
WHERE ul.created_at >= $1 AND ul.created_at < $2` + extraSQL + `
GROUP BY ul.user_id, ul.group_id, ` + resolvedPlatformExpr + `, ul.rate_multiplier`
	if err := queryKeyed(ctx, tx, rateQuery, start, upper, extraArgs, func(rows *sql.Rows) error {
		var userID int64
		var groupID sql.NullInt64
		var platform string
		var rate float64
		var count int64
		if err := rows.Scan(&userID, &groupID, &platform, &rate, &count); err != nil {
			return err
		}
		if i, ok := index[userKey(userID, nullGroupID(groupID), platform)]; ok {
			out[i].Rates = append(out[i].Rates, WeeklyCount{Label: strconv.FormatFloat(rate, 'f', 4, 64), Count: count})
		}
		return nil
	}); err != nil {
		return err
	}
	reasonQuery := `
SELECT ul.user_id, ul.group_id, ` + resolvedPlatformExpr + `,
       COALESCE(NULLIF(btrim(ul.requested_reasoning_effort), ''), ''),
       COALESCE(NULLIF(btrim(ul.reasoning_effort), ''), ''),
       COUNT(*)::bigint
FROM usage_logs ul
JOIN accounts a ON a.id = ul.account_id
LEFT JOIN groups g ON g.id = ul.group_id
WHERE ul.created_at >= $1 AND ul.created_at < $2` + extraSQL + `
GROUP BY ul.user_id, ul.group_id, ` + resolvedPlatformExpr + `, 4, 5`
	if err := queryKeyed(ctx, tx, reasonQuery, start, upper, extraArgs, func(rows *sql.Rows) error {
		var userID int64
		var groupID sql.NullInt64
		var platform, requested, upstream string
		var count int64
		if err := rows.Scan(&userID, &groupID, &platform, &requested, &upstream, &count); err != nil {
			return err
		}
		if requested == "" && upstream == "" {
			return nil
		}
		if i, ok := index[userKey(userID, nullGroupID(groupID), platform)]; ok {
			out[i].Reasoning = append(out[i].Reasoning, WeeklyReasonCount{Requested: requested, Upstream: upstream, Count: count})
		}
		return nil
	}); err != nil {
		return err
	}
	tierQuery := `
SELECT ul.user_id, ul.group_id, ` + resolvedPlatformExpr + `, lower(btrim(ul.service_tier)), COUNT(*)::bigint
FROM usage_logs ul
JOIN accounts a ON a.id = ul.account_id
LEFT JOIN groups g ON g.id = ul.group_id
WHERE ul.created_at >= $1 AND ul.created_at < $2 AND NULLIF(btrim(ul.service_tier), '') IS NOT NULL` + extraSQL + `
GROUP BY ul.user_id, ul.group_id, ` + resolvedPlatformExpr + `, 4`
	return queryKeyed(ctx, tx, tierQuery, start, upper, extraArgs, func(rows *sql.Rows) error {
		var userID int64
		var groupID sql.NullInt64
		var platform, tier string
		var count int64
		if err := rows.Scan(&userID, &groupID, &platform, &tier, &count); err != nil {
			return err
		}
		i, ok := index[userKey(userID, nullGroupID(groupID), platform)]
		if !ok {
			return nil
		}
		out[i].Tiers = append(out[i].Tiers, WeeklyCount{Label: tier, Count: count})
		if tier == "priority" || tier == "ultrafast" {
			out[i].FastCount += count
		}
		return nil
	})
}

func queryKeyed(ctx context.Context, tx *sql.Tx, query string, start, upper time.Time, extra []any, fn func(*sql.Rows) error) error {
	rows, err := tx.QueryContext(ctx, query, append([]any{start, upper}, extra...)...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := fn(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}

func nullGroupID(groupID sql.NullInt64) *int64 {
	if !groupID.Valid {
		return nil
	}
	id := groupID.Int64
	return &id
}

func userKey(userID int64, groupID *int64, platform string) string {
	gid := "null"
	if groupID != nil {
		gid = strconv.FormatInt(*groupID, 10)
	}
	return fmt.Sprintf("%d|%s|%s", userID, gid, platform)
}

func attachUserDetails(ctx context.Context, tx *sql.Tx, rows []WeeklyUserInput) error {
	ids := userIDs(rows)
	if len(ids) == 0 {
		return nil
	}
	found, err := loadUsersByIDs(ctx, tx, ids)
	if err != nil {
		return err
	}
	for i := range rows {
		if user, ok := found[rows[i].UserID]; ok {
			rows[i].Email = user.Email
			rows[i].Balance = user.Balance
			rows[i].UserDeleted = user.Deleted
		}
	}
	return nil
}

type loadedUser struct {
	Email   string
	Balance float64
	Deleted bool
}

func loadUsersByIDs(ctx context.Context, tx *sql.Tx, ids []int64) (map[int64]loadedUser, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id, email, balance::float8, deleted_at IS NOT NULL FROM users WHERE id = ANY($1)`, pq.Array(ids))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]loadedUser{}
	for rows.Next() {
		var id int64
		var user loadedUser
		if err := rows.Scan(&id, &user.Email, &user.Balance, &user.Deleted); err != nil {
			return nil, err
		}
		out[id] = user
	}
	return out, rows.Err()
}

func userIDs(rows []WeeklyUserInput) []int64 {
	seen := map[int64]struct{}{}
	var ids []int64
	for _, row := range rows {
		if _, ok := seen[row.UserID]; ok {
			continue
		}
		seen[row.UserID] = struct{}{}
		ids = append(ids, row.UserID)
	}
	return ids
}

func loadQuotas(ctx context.Context, tx *sql.Tx, ids []int64) (map[string]quotaRow, error) {
	out := map[string]quotaRow{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := tx.QueryContext(ctx, `
SELECT user_id, platform, weekly_limit_usd::float8, weekly_usage_usd::float8, weekly_window_start
FROM user_platform_quotas
WHERE deleted_at IS NULL AND platform IN ('openai', 'grok') AND user_id = ANY($1)`, pq.Array(ids))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var row quotaRow
		var limit sql.NullFloat64
		var window sql.NullTime
		if err := rows.Scan(&row.UserID, &row.Platform, &limit, &row.Usage, &window); err != nil {
			return nil, err
		}
		if limit.Valid {
			row.Limit = exportFloat(limit.Float64)
		}
		if window.Valid {
			t := window.Time
			row.Window = &t
		}
		out[fmt.Sprintf("%d\x00%s", row.UserID, row.Platform)] = row
	}
	return out, rows.Err()
}

func applyQuotas(rows []WeeklyUserInput, quotas map[string]quotaRow, now time.Time, current bool, mode string) {
	for i := range rows {
		rows[i].CurrentWeek = current
		if !current {
			continue
		}
		quota, ok := quotas[fmt.Sprintf("%d\x00%s", rows[i].UserID, rows[i].Platform)]
		if !ok {
			continue
		}
		view := lazyZeroWeekly(quota, now, mode)
		rows[i].HasQuota = true
		rows[i].WeeklyLimit = view.limit
		rows[i].WeeklyUsage = view.usage
	}
}

type lazyWeeklyView struct {
	limit *float64
	usage float64
}

func quotaWeekModeTx(ctx context.Context, tx *sql.Tx) string {
	if tx == nil {
		return PlatformQuotaWeekModeRegistration
	}
	var raw string
	if err := tx.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = $1`, SettingKeyPlatformQuotaWeekMode).Scan(&raw); err != nil {
		return PlatformQuotaWeekModeRegistration
	}
	return NormalizePlatformQuotaWeekMode(raw)
}

func lazyZeroWeekly(row quotaRow, now time.Time, mode string) lazyWeeklyView {
	rec := UserPlatformQuotaRecord{
		UserID: row.UserID, Platform: row.Platform,
		WeeklyLimitUSD: row.Limit, WeeklyUsageUSD: row.Usage, WeeklyWindowStart: row.Window,
	}
	// 与学生卡片相同：过期周窗口的已用按 0 显示，不写回数据库。
	if weeklyQuotaExpired(rec.WeeklyWindowStart, now, mode) {
		rec.WeeklyUsageUSD = 0
	}
	return lazyWeeklyView{limit: rec.WeeklyLimitUSD, usage: rec.WeeklyUsageUSD}
}

func loadSyntheticUsers(ctx context.Context, tx *sql.Tx, start, upper, now time.Time, current bool) ([]WeeklyUserInput, error) {
	if !current {
		return nil, nil
	}
	rows, err := tx.QueryContext(ctx, `
SELECT u.id, u.email, u.balance::float8, q.platform, q.weekly_limit_usd::float8, q.weekly_usage_usd::float8, q.weekly_window_start
FROM user_platform_quotas q
JOIN users u ON u.id = q.user_id AND u.deleted_at IS NULL
WHERE q.deleted_at IS NULL AND q.platform IN ('openai', 'grok') AND q.weekly_limit_usd IS NOT NULL
  AND NOT EXISTS (
    SELECT 1 FROM usage_logs ul
    JOIN accounts a ON a.id = ul.account_id
    LEFT JOIN groups g ON g.id = ul.group_id
    WHERE ul.user_id = u.id AND ul.created_at >= $1 AND ul.created_at < $2
      AND `+resolvedPlatformExpr+` = q.platform
  )`, start, upper)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	mode := quotaWeekModeTx(ctx, tx)
	var out []WeeklyUserInput
	for rows.Next() {
		var item WeeklyUserInput
		var quota quotaRow
		var limit float64
		var window sql.NullTime
		if err := rows.Scan(&item.UserID, &item.Email, &item.Balance, &item.Platform, &limit, &quota.Usage, &window); err != nil {
			return nil, err
		}
		quota.UserID = item.UserID
		quota.Platform = item.Platform
		quota.Limit = exportFloat(limit)
		if window.Valid {
			t := window.Time
			quota.Window = &t
		}
		view := lazyZeroWeekly(quota, now, mode)
		item.Synthetic = true
		item.CurrentWeek = true
		item.HasQuota = true
		item.WeeklyLimit = view.limit
		item.WeeklyUsage = view.usage
		out = append(out, item)
	}
	return out, rows.Err()
}

func loadParentAccounts(ctx context.Context, tx *sql.Tx) ([]parentAccount, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT id, name, platform, status, COALESCE(credentials->>'plan_type', ''), extra
FROM accounts
WHERE deleted_at IS NULL AND parent_account_id IS NULL AND platform IN ('openai', 'grok')
ORDER BY platform, name, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []parentAccount
	for rows.Next() {
		var item parentAccount
		var extra []byte
		if err := rows.Scan(&item.IDInt, &item.Name, &item.Platform, &item.Status, &item.Plan, &extra); err != nil {
			return nil, err
		}
		if len(extra) > 0 {
			_ = json.Unmarshal(extra, &item.Extra)
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func parentIDs(rows []parentAccount) []int64 {
	ids := make([]int64, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.IDInt)
	}
	return ids
}

func attachAccountGroups(ctx context.Context, tx *sql.Tx, rows []parentAccount) error {
	ids := parentIDs(rows)
	if len(ids) == 0 {
		return nil
	}
	queryRows, err := tx.QueryContext(ctx, `
SELECT ag.account_id, g.name
FROM account_groups ag
JOIN groups g ON g.id = ag.group_id AND g.deleted_at IS NULL
WHERE ag.account_id = ANY($1)
ORDER BY g.name`, pq.Array(ids))
	if err != nil {
		return err
	}
	defer queryRows.Close()
	grouped := map[int64][]string{}
	for queryRows.Next() {
		var id int64
		var name string
		if err := queryRows.Scan(&id, &name); err != nil {
			return err
		}
		grouped[id] = append(grouped[id], name)
	}
	if err := queryRows.Err(); err != nil {
		return err
	}
	for i := range rows {
		names := grouped[rows[i].IDInt]
		sort.Strings(names)
		rows[i].Groups = names
	}
	return nil
}

func loadParentUsage(ctx context.Context, tx *sql.Tx, start, upper time.Time, ids []int64) (map[int64]parentUsage, error) {
	out := map[int64]parentUsage{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := tx.QueryContext(ctx, `
SELECT COALESCE(a.parent_account_id, ul.account_id),
       COUNT(*)::bigint,
       COUNT(DISTINCT ul.user_id)::bigint,
       COALESCE(SUM(ul.total_cost), 0)::float8,
       COALESCE(SUM(ul.actual_cost), 0)::float8
FROM usage_logs ul
JOIN accounts a ON a.id = ul.account_id
WHERE ul.created_at >= $1 AND ul.created_at < $2
  AND COALESCE(a.parent_account_id, ul.account_id) = ANY($3)
GROUP BY 1`, start, upper, pq.Array(ids))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var item parentUsage
		if err := rows.Scan(&id, &item.Requests, &item.Users, &item.Catalog, &item.Actual); err != nil {
			return nil, err
		}
		out[id] = item
	}
	return out, rows.Err()
}

func loadPlatformSpend(ctx context.Context, tx *sql.Tx, start, upper time.Time) (map[string]float64, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT `+resolvedPlatformExpr+`, COALESCE(SUM(ul.actual_cost), 0)::float8
FROM usage_logs ul
JOIN accounts a ON a.id = ul.account_id
LEFT JOIN groups g ON g.id = ul.group_id
WHERE ul.created_at >= $1 AND ul.created_at < $2
GROUP BY 1`, start, upper)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]float64{}
	for rows.Next() {
		var platform string
		var actual float64
		if err := rows.Scan(&platform, &actual); err != nil {
			return nil, err
		}
		if platform == "openai" || platform == "grok" {
			out[platform] = actual
		}
	}
	return out, rows.Err()
}

func loadPreviousSnapshots(ctx context.Context, tx *sql.Tx, ids []int64, before time.Time) (map[int64]weeklySnapshot, error) {
	out := map[int64]weeklySnapshot{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := tx.QueryContext(ctx, `
SELECT DISTINCT ON (account_id) account_id, exported_at, official_used_percent, official_reset_at,
       official_observed_at, official_source, catalog_cost_since_prev::float8, actual_cost_since_prev::float8
FROM admin_weekly_usage_export_snapshots
WHERE account_id = ANY($1) AND exported_at < $2
ORDER BY account_id, exported_at DESC`, pq.Array(ids), before)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		snap, err := scanSnapshot(rows)
		if err != nil {
			return nil, err
		}
		out[snap.AccountID] = snap
	}
	return out, rows.Err()
}

func loadWeekSnapshots(ctx context.Context, tx *sql.Tx, ids []int64, weekStart time.Time) (map[int64]weeklySnapshot, error) {
	out := map[int64]weeklySnapshot{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := tx.QueryContext(ctx, `
SELECT DISTINCT ON (account_id) account_id, exported_at, official_used_percent, official_reset_at,
       official_observed_at, official_source, catalog_cost_since_prev::float8, actual_cost_since_prev::float8
FROM admin_weekly_usage_export_snapshots
WHERE account_id = ANY($1) AND gateway_week_start = $2
ORDER BY account_id, exported_at DESC`, pq.Array(ids), weekStart)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		snap, err := scanSnapshot(rows)
		if err != nil {
			return nil, err
		}
		out[snap.AccountID] = snap
	}
	return out, rows.Err()
}

func loadSnapshotsBefore(ctx context.Context, tx *sql.Tx, week map[int64]weeklySnapshot) (map[int64]weeklySnapshot, error) {
	out := map[int64]weeklySnapshot{}
	if len(week) == 0 {
		return out, nil
	}
	ids := make([]int64, 0, len(week))
	times := make([]time.Time, 0, len(week))
	for id, snap := range week {
		ids = append(ids, id)
		times = append(times, snap.ExportedAt)
	}
	rows, err := tx.QueryContext(ctx, `
SELECT DISTINCT ON (s.account_id) s.account_id, s.exported_at, s.official_used_percent, s.official_reset_at,
       s.official_observed_at, s.official_source, s.catalog_cost_since_prev::float8, s.actual_cost_since_prev::float8
FROM admin_weekly_usage_export_snapshots s
JOIN (
  SELECT * FROM unnest($1::bigint[], $2::timestamptz[])
) AS boundary(account_id, exported_at) ON boundary.account_id = s.account_id
WHERE s.exported_at < boundary.exported_at
ORDER BY s.account_id, s.exported_at DESC`, pq.Array(ids), pq.Array(times))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		snap, err := scanSnapshot(rows)
		if err != nil {
			return nil, err
		}
		out[snap.AccountID] = snap
	}
	return out, rows.Err()
}

type snapshotScanner interface {
	Scan(dest ...any) error
}

func scanSnapshot(rows snapshotScanner) (weeklySnapshot, error) {
	var snap weeklySnapshot
	var percent, catalog, actual sql.NullFloat64
	var reset, observed sql.NullTime
	if err := rows.Scan(&snap.AccountID, &snap.ExportedAt, &percent, &reset, &observed, &snap.OfficialSource, &catalog, &actual); err != nil {
		return snap, err
	}
	if percent.Valid {
		snap.OfficialUsedPercent = exportFloat(percent.Float64)
	}
	if reset.Valid {
		t := reset.Time
		snap.OfficialResetAt = &t
	}
	if observed.Valid {
		t := observed.Time
		snap.OfficialObservedAt = &t
	}
	if catalog.Valid {
		snap.CatalogCostSincePrev = exportFloat(catalog.Float64)
	}
	if actual.Valid {
		snap.ActualCostSincePrev = exportFloat(actual.Float64)
	}
	return snap, nil
}

func loadSincePrev(ctx context.Context, tx *sql.Tx, prev map[int64]weeklySnapshot, exportedAt time.Time) (map[int64]costPair, error) {
	out := map[int64]costPair{}
	if len(prev) == 0 {
		return out, nil
	}
	ids := make([]int64, 0, len(prev))
	times := make([]time.Time, 0, len(prev))
	for id, snap := range prev {
		ids = append(ids, id)
		times = append(times, snap.ExportedAt)
		out[id] = costPair{}
	}
	rows, err := tx.QueryContext(ctx, `
SELECT COALESCE(a.parent_account_id, ul.account_id),
       COALESCE(SUM(ul.total_cost), 0)::float8,
       COALESCE(SUM(ul.actual_cost), 0)::float8
FROM usage_logs ul
JOIN accounts a ON a.id = ul.account_id
JOIN (
  SELECT * FROM unnest($1::bigint[], $2::timestamptz[])
) AS prev(parent_id, prev_at)
  ON COALESCE(a.parent_account_id, ul.account_id) = prev.parent_id
WHERE ul.created_at > prev.prev_at AND ul.created_at <= $3
GROUP BY 1`, pq.Array(ids), pq.Array(times), exportedAt)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var pair costPair
		if err := rows.Scan(&id, &pair.Catalog, &pair.Actual); err != nil {
			return nil, err
		}
		out[id] = pair
	}
	return out, rows.Err()
}

func insertWeeklySnapshots(ctx context.Context, tx *sql.Tx, snaps []weeklySnapshot) error {
	for _, snap := range snaps {
		if snap.OfficialSource == "" {
			snap.OfficialSource = "none"
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO admin_weekly_usage_export_snapshots (
  account_id, gateway_week_start, exported_at, exported_by_user_id, platform,
  official_used_percent, official_reset_at, official_observed_at, official_source,
  catalog_cost_since_prev, actual_cost_since_prev, comparable
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
			snap.AccountID, snap.GatewayWeekStart, snap.ExportedAt, snap.ExportedByUserID, snap.Platform,
			snap.OfficialUsedPercent, snap.OfficialResetAt, snap.OfficialObservedAt, snap.OfficialSource,
			snap.CatalogCostSincePrev, snap.ActualCostSincePrev, snap.Comparable,
		); err != nil {
			return err
		}
	}
	return nil
}

func officialFromExtra(platform string, extra map[string]any, plan string, now time.Time) (*float64, *time.Time, *time.Time, string, string) {
	if extra == nil {
		extra = map[string]any{}
	}
	if platform == "grok" {
		percent, resetAt, observed, source, grokPlan := grokOfficial(extra, now)
		if grokPlan == "" {
			grokPlan = plan
		}
		return percent, resetAt, observed, source, grokPlan
	}
	percent, resetAt, observed, source := codexOfficial(extra, now)
	return percent, resetAt, observed, source, plan
}

func codexOfficial(extra map[string]any, now time.Time) (*float64, *time.Time, *time.Time, string) {
	if _, ok := extra["codex_7d_used_percent"]; !ok {
		return nil, nil, nil, "none"
	}
	progress := buildCodexUsageProgressFromExtra(extra, "7d", now)
	if progress == nil {
		return nil, nil, nil, "none"
	}
	var observed *time.Time
	if raw, ok := extra["codex_usage_updated_at"]; ok {
		if parsed, err := parseTime(fmt.Sprint(raw)); err == nil {
			observed = &parsed
		}
	}
	if progress.ResetsAt != nil && !now.Before(*progress.ResetsAt) {
		return nil, nil, observed, "codex_7d_expired"
	}
	percent := round4(progress.Utilization)
	return &percent, progress.ResetsAt, observed, "codex_7d"
}

func grokOfficial(extra map[string]any, now time.Time) (*float64, *time.Time, *time.Time, string, string) {
	raw, ok := extra["grok_billing_snapshot"].(map[string]any)
	if !ok {
		return nil, nil, nil, "grok_billing_no_weekly_percent", ""
	}
	plan, _ := raw["plan"].(string)
	observed := firstTime(raw, "weekly_updated_at", "updated_at", "fetched_at")
	percent, has := floatFromAny(raw["usage_percent"])
	if !has {
		return nil, nil, observed, "grok_billing_no_weekly_percent", plan
	}
	var resetAt *time.Time
	if text, _ := raw["period_end"].(string); text != "" {
		if parsed, err := parseTime(text); err == nil {
			resetAt = &parsed
			if !now.Before(parsed) {
				return nil, nil, observed, "grok_period_end_expired", plan
			}
		}
	}
	rounded := round4(percent)
	return &rounded, resetAt, observed, "grok_billing", plan
}

func firstTime(raw map[string]any, keys ...string) *time.Time {
	for _, key := range keys {
		text, _ := raw[key].(string)
		if text == "" {
			continue
		}
		if parsed, err := parseTime(text); err == nil {
			return &parsed
		}
	}
	return nil
}

func floatFromAny(value any) (float64, bool) {
	if value == nil {
		return 0, false
	}
	switch typed := value.(type) {
	case float64:
		return typed, true
	case json.Number:
		parsed, err := typed.Float64()
		return parsed, err == nil
	case string:
		parsed, err := strconv.ParseFloat(typed, 64)
		return parsed, err == nil
	default:
		parsed, err := strconv.ParseFloat(fmt.Sprint(value), 64)
		return parsed, err == nil
	}
}

func exportFloat(v float64) *float64 { return &v }

func quotaTotalsNeedsUsers(ctx context.Context, tx *sql.Tx, now time.Time) (map[string]float64, map[string]float64, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT q.platform, q.weekly_limit_usd::float8, q.weekly_usage_usd::float8, q.weekly_window_start, u.balance::float8
FROM user_platform_quotas q
JOIN users u ON u.id = q.user_id AND u.deleted_at IS NULL
WHERE q.deleted_at IS NULL AND q.platform IN ('openai', 'grok') AND q.weekly_limit_usd > 0`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	mode := quotaWeekModeTx(ctx, tx)
	limits := map[string]float64{}
	blocked := map[string]float64{}
	for rows.Next() {
		var row quotaRow
		var limit float64
		var window sql.NullTime
		if err := rows.Scan(&row.Platform, &limit, &row.Usage, &window, &row.Balance); err != nil {
			return nil, nil, err
		}
		row.Limit = &limit
		if window.Valid {
			t := window.Time
			row.Window = &t
		}
		view := lazyZeroWeekly(row, now, mode)
		if view.limit == nil || *view.limit <= 0 {
			continue
		}
		limits[row.Platform] += *view.limit
		if row.Balance <= 0 {
			blocked[row.Platform] += *view.limit
		}
	}
	return limits, blocked, rows.Err()
}
