package service

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
)

const (
	officialComparePercent = 80

	userQuotaNote    = "平台周限额按用户×平台，不是按分组。同一平台多个分组的限额、已用和百分比相同，不要相加。学生卡只在当前网关周有数。"
	accountReadNote  = "官方百分比是导出时已保存的快照，四位小数，不是学生卡的一位小数，也不是本次向上游查询。网关周与官方重置不是同一套钟。目录价USD是内部价，不是人民币，也不是发票。目录价USD每官方1%用上次导出到本次的目录价。按80%折算的预期只在当前网关周、且上次导出也在本周、且该平台只有一个可比母账号时填写，限额是（学生周限额合计减余额阻断限额）。实扣减预期对比的是折算用实扣，不是本周实扣合计。影子账号并进母账号，不单独成行。接近100%且周还没结束才考虑加账号。多个母账号几乎不动才考虑减账号。学生卡100%而官方百分比仍低，是限额或倍率紧，不是缺账号。"
	filterExportNote = "自定义时间段不对比官方周额度。学生周限额和已用百分比不按本时间段重算。目录价 USD 和实扣 USD 是网关内部美元。筛选结束是不含的边界，结束日当天包含在内。选择母账号时，影子账号的用量算在一起。"
)

var userCSVHeader = []string{
	"邮箱", "分组", "平台", "请求数", "输入token", "输出token", "缓存读取token", "缓存创建token",
	"目录价USD", "实扣USD", "请求倍率", "分组当前倍率", "当前余额USD", "余额状态",
	"平台周限额USD", "平台周已用USD", "学生卡已用百分比", "配额状态", "计入限额合计",
	"主要模型", "思考档", "Fast请求数", "service_tier明细", "不可比项", "配额说明",
}

var filterCSVHeader = []string{
	"筛选开始", "筛选结束", "筛选分组", "筛选账号",
	"邮箱", "分组", "平台", "请求数", "输入token", "输出token", "缓存读取token", "缓存创建token",
	"目录价USD", "实扣USD", "请求倍率", "分组当前倍率", "当前余额USD", "余额状态",
	"主要模型", "思考档", "Fast请求数", "service_tier明细", "不可比项", "说明",
}

var accountCSVHeader = []string{
	"账号名称", "平台", "计划", "账号状态", "所属分组", "请求数", "服务用户数",
	"目录价USD", "实扣USD", "网关周开始", "网关周结束", "网关时区",
	"官方已用百分比", "官方额度重置时间", "官方快照时间", "官方百分比来源",
	"距上次导出官方变化", "变化是否可比", "不可比原因",
	"折算用目录价USD", "折算用实扣USD", "目录价USD每官方1%",
	"本平台学生周限额合计USD", "其中余额阻断的限额USD", "本平台本周实扣合计USD",
	"按80%折算的预期实扣USD", "实扣减预期USD", "读表说明",
}

// WeeklyUserInput 是一行用户×分组×解析平台在组装 CSV 之前的数据。
type WeeklyUserInput struct {
	UserID       int64
	Email        string
	Balance      float64
	UserDeleted  bool
	GroupID      *int64
	GroupName    string
	GroupMissing bool
	Synthetic    bool
	Platform     string
	Requests     int64
	InputTokens  int64
	OutputTokens int64
	CacheRead    int64
	CacheCreate  int64
	ImageCount   int64
	ImageInput   int64
	ImageOutput  int64
	VideoCount   int64
	VideoSeconds int64
	CatalogCost  float64
	ActualCost   float64
	Rates        []WeeklyCount
	GroupRate    *float64
	Models       []WeeklyModelCount
	Reasoning    []WeeklyReasonCount
	Tiers        []WeeklyCount
	FastCount    int64
	CurrentWeek  bool
	HasQuota     bool
	WeeklyLimit  *float64
	WeeklyUsage  float64
}

// WeeklyCount 是倍率或 service_tier 的次数。
type WeeklyCount struct {
	Label string
	Count int64
}

// WeeklyModelCount 是一个模型的请求数和目录价。
type WeeklyModelCount struct {
	Name    string
	Count   int64
	Catalog float64
}

// WeeklyReasonCount 是一对思考档的次数。
type WeeklyReasonCount struct {
	Requested string
	Upstream  string
	Count     int64
}

// WeeklyAccountInput 是一个母账号在组装 CSV 之前的数据。
type WeeklyAccountInput struct {
	ID               int64
	Name             string
	Platform         string
	Plan             string
	Status           string
	Groups           []string
	Requests         int64
	DistinctUsers    int64
	CatalogCost      float64
	ActualCost       float64
	WeekStart        time.Time
	WeekEnd          time.Time
	Timezone         string
	CurrentWeek      bool
	NoWeekSnapshot   bool
	OfficialPercent  *float64
	OfficialReset    *time.Time
	OfficialObserved *time.Time
	OfficialSource   string
	HasPrev          bool
	PrevPercent      *float64
	PrevReset        *time.Time
	PrevExportedAt   time.Time
	SinceCatalog     *float64
	SinceActual      *float64
	LimitSum         *float64
	BlockedLimit     *float64
	WeekActualSum    *float64
}

func buildUserCSV(rows []WeeklyUserInput) [][]string {
	sorted := append([]WeeklyUserInput(nil), rows...)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if a.Email != b.Email {
			return a.Email < b.Email
		}
		if a.Platform != b.Platform {
			return a.Platform < b.Platform
		}
		if a.Synthetic != b.Synthetic {
			return !a.Synthetic && b.Synthetic
		}
		if a.GroupName != b.GroupName {
			return a.GroupName < b.GroupName
		}
		return groupIDLess(a.GroupID, b.GroupID)
	})
	include := map[string]int{}
	for i, row := range sorted {
		if !row.CurrentWeek || row.UserDeleted || row.WeeklyLimit == nil || *row.WeeklyLimit <= 0 {
			continue
		}
		key := fmt.Sprintf("%d\x00%s", row.UserID, row.Platform)
		if _, ok := include[key]; !ok {
			include[key] = i
		}
	}
	out := make([][]string, 0, len(sorted)+1)
	out = append(out, userCSVHeader)
	for i, row := range sorted {
		idx, ok := include[fmt.Sprintf("%d\x00%s", row.UserID, row.Platform)]
		out = append(out, userCSVRecord(row, ok && idx == i))
	}
	return out
}

func groupIDLess(a, b *int64) bool {
	if a == nil && b == nil {
		return false
	}
	if a == nil {
		return false
	}
	if b == nil {
		return true
	}
	return *a < *b
}

// FilterExportMeta 写在筛选 CSV 的每一行，避免排序后看不出本次条件。
type FilterExportMeta struct {
	Start   time.Time
	End     time.Time
	Group   string
	Account string
}

func buildFilterCSV(rows []WeeklyUserInput, meta FilterExportMeta) [][]string {
	sorted := append([]WeeklyUserInput(nil), rows...)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if a.Email != b.Email {
			return a.Email < b.Email
		}
		if a.Platform != b.Platform {
			return a.Platform < b.Platform
		}
		if a.GroupName != b.GroupName {
			return a.GroupName < b.GroupName
		}
		return groupIDLess(a.GroupID, b.GroupID)
	})
	out := make([][]string, 0, len(sorted)+1)
	out = append(out, filterCSVHeader)
	for _, row := range sorted {
		out = append(out, filterCSVRecord(row, meta))
	}
	return out
}

func filterCSVRecord(row WeeklyUserInput, meta FilterExportMeta) []string {
	reasoning := formatReasoning(row.Reasoning)
	if reasoning == "" {
		reasoning = "无"
	}
	tiers := formatCounts(row.Tiers)
	if tiers == "" {
		tiers = "无"
	}
	balanceStatus := "可用"
	if row.Balance <= 0 {
		balanceStatus = "阻断"
	}
	return []string{
		formatFilterTime(meta.Start),
		formatFilterTime(meta.End),
		meta.Group,
		meta.Account,
		row.Email,
		displayGroup(row),
		row.Platform,
		strconv.FormatInt(row.Requests, 10),
		strconv.FormatInt(row.InputTokens, 10),
		strconv.FormatInt(row.OutputTokens, 10),
		strconv.FormatInt(row.CacheRead, 10),
		strconv.FormatInt(row.CacheCreate, 10),
		formatMoney(row.CatalogCost),
		formatMoney(row.ActualCost),
		formatRates(row.Rates),
		formatOptionalRate(row.GroupRate),
		formatMoney(row.Balance),
		balanceStatus,
		formatModels(row.Models),
		reasoning,
		strconv.FormatInt(row.FastCount, 10),
		tiers,
		incomparableItems(row, reasoning),
		filterExportNote,
	}
}

func formatFilterTime(value time.Time) string {
	return value.In(timezone.Location()).Format("2006-01-02 15:04")
}

func displayGroup(row WeeklyUserInput) string {
	switch {
	case row.Synthetic:
		return "（本周无请求）"
	case row.GroupID == nil:
		return "（未分组）"
	case row.GroupMissing:
		return fmt.Sprintf("（分组已不存在#%d）", *row.GroupID)
	default:
		return row.GroupName
	}
}

func userCSVRecord(row WeeklyUserInput, countLimit bool) []string {
	group := displayGroup(row)
	balanceStatus := "可用"
	if row.Balance <= 0 {
		balanceStatus = "阻断"
	}
	limitCell, usageCell, percentCell, quotaStatus := quotaCells(row)
	flag := "否"
	if countLimit {
		flag = "是"
	}
	reasoning := formatReasoning(row.Reasoning)
	if row.Requests == 0 && reasoning == "" {
		reasoning = "无"
	}
	if reasoning == "" {
		reasoning = "无"
	}
	tiers := formatCounts(row.Tiers)
	if tiers == "" {
		tiers = "无"
	}
	return []string{
		row.Email,
		group,
		row.Platform,
		strconv.FormatInt(row.Requests, 10),
		strconv.FormatInt(row.InputTokens, 10),
		strconv.FormatInt(row.OutputTokens, 10),
		strconv.FormatInt(row.CacheRead, 10),
		strconv.FormatInt(row.CacheCreate, 10),
		formatMoney(row.CatalogCost),
		formatMoney(row.ActualCost),
		formatRates(row.Rates),
		formatOptionalRate(row.GroupRate),
		formatMoney(row.Balance),
		balanceStatus,
		limitCell,
		usageCell,
		percentCell,
		quotaStatus,
		flag,
		formatModels(row.Models),
		reasoning,
		strconv.FormatInt(row.FastCount, 10),
		tiers,
		incomparableItems(row, reasoning),
		userQuotaNote,
	}
}

func quotaCells(row WeeklyUserInput) (limit, usage, percent, status string) {
	if !row.CurrentWeek {
		return "", "", "", "历史周无快照"
	}
	if !row.HasQuota {
		return "", "", "0", "未配置"
	}
	if row.WeeklyLimit == nil {
		return "", "", "0", "不限制"
	}
	limit = formatMoney(*row.WeeklyLimit)
	usage = formatMoney(row.WeeklyUsage)
	if *row.WeeklyLimit <= 0 {
		return limit, usage, "100", "已禁用"
	}
	used := row.WeeklyUsage / *row.WeeklyLimit * 100
	if used < 0 {
		used = 0
	}
	if used > 100 {
		used = 100
	}
	status = "正常"
	if used >= 100 {
		status = "已用完"
	}
	return limit, usage, formatCodexPoints(used), status
}

func formatCodexPoints(used float64) string {
	rounded := math.Round(used*10) / 10
	if rounded == math.Trunc(rounded) {
		return strconv.FormatInt(int64(rounded), 10)
	}
	return strconv.FormatFloat(rounded, 'f', 1, 64)
}

func formatReasoning(items []WeeklyReasonCount) string {
	list := append([]WeeklyReasonCount(nil), items...)
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].Count != list[j].Count {
			return list[i].Count > list[j].Count
		}
		if list[i].Requested != list[j].Requested {
			return list[i].Requested < list[j].Requested
		}
		return list[i].Upstream < list[j].Upstream
	})
	parts := make([]string, 0, len(list))
	for _, item := range list {
		if item.Count <= 0 || (item.Requested == "" && item.Upstream == "") {
			continue
		}
		var label string
		switch {
		case item.Requested != "" && item.Requested == item.Upstream:
			label = item.Requested
		case item.Requested != "" && item.Upstream == "":
			label = "请求:" + item.Requested
		case item.Requested == "" && item.Upstream != "":
			label = "上游:" + item.Upstream
		default:
			label = "请求:" + item.Requested + "/上游:" + item.Upstream
		}
		parts = append(parts, fmt.Sprintf("%s×%d", label, item.Count))
	}
	return joinLimited(parts)
}

func formatCounts(items []WeeklyCount) string {
	list := append([]WeeklyCount(nil), items...)
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].Count != list[j].Count {
			return list[i].Count > list[j].Count
		}
		return list[i].Label < list[j].Label
	})
	parts := make([]string, 0, len(list))
	for _, item := range list {
		if item.Count <= 0 || strings.TrimSpace(item.Label) == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s×%d", item.Label, item.Count))
	}
	return joinLimited(parts)
}

func formatModels(items []WeeklyModelCount) string {
	list := append([]WeeklyModelCount(nil), items...)
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].Catalog != list[j].Catalog {
			return list[i].Catalog > list[j].Catalog
		}
		return list[i].Name < list[j].Name
	})
	parts := make([]string, 0, len(list))
	for _, item := range list {
		if item.Name == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s×%d", item.Name, item.Count))
	}
	return joinLimited(parts)
}

func joinLimited(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	more := len(parts) > 8
	if more {
		parts = parts[:8]
	}
	out := strings.Join(parts, "；")
	if more {
		out += "；等"
	}
	return out
}

func formatRates(items []WeeklyCount) string {
	if len(items) == 0 {
		return ""
	}
	list := append([]WeeklyCount(nil), items...)
	sort.SliceStable(list, func(i, j int) bool { return list[i].Label < list[j].Label })
	if len(list) == 1 {
		return list[0].Label
	}
	parts := make([]string, 0, len(list))
	for _, item := range list {
		parts = append(parts, fmt.Sprintf("%s×%d", item.Label, item.Count))
	}
	return "混合:" + strings.Join(parts, "；")
}

func formatOptionalRate(v *float64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatFloat(*v, 'f', 4, 64)
}

func incomparableItems(row WeeklyUserInput, reasoning string) string {
	var parts []string
	if row.CacheRead+row.CacheCreate > 0 {
		parts = append(parts, "缓存")
	}
	if reasoning != "无" {
		parts = append(parts, "思考")
	}
	if row.FastCount > 0 {
		parts = append(parts, "Fast")
	}
	if row.ImageCount+row.ImageInput+row.ImageOutput > 0 {
		parts = append(parts, "图片")
	}
	if row.VideoCount > 0 || row.VideoSeconds > 0 {
		parts = append(parts, "视频")
	}
	if len(parts) == 0 {
		return "无"
	}
	return strings.Join(parts, "；")
}

type accountCSVMeta struct {
	comparable bool
	reason     string
	delta      string
	perPercent string
	expected   string
	diff       string
}

func buildAccountCSV(rows []WeeklyAccountInput) [][]string {
	sorted := append([]WeeklyAccountInput(nil), rows...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Platform != sorted[j].Platform {
			return sorted[i].Platform < sorted[j].Platform
		}
		if sorted[i].Name != sorted[j].Name {
			return sorted[i].Name < sorted[j].Name
		}
		return sorted[i].ID < sorted[j].ID
	})
	metas := make([]accountCSVMeta, len(sorted))
	comparableCount := map[string]int{}
	for i, row := range sorted {
		metas[i] = accountMeta(row)
		if metas[i].comparable {
			comparableCount[row.Platform]++
		}
	}
	for i, row := range sorted {
		if !metas[i].comparable || comparableCount[row.Platform] != 1 {
			continue
		}
		if !row.CurrentWeek || !row.HasPrev || row.PrevExportedAt.Before(row.WeekStart) {
			continue
		}
		if row.LimitSum == nil || row.BlockedLimit == nil || row.SinceActual == nil {
			continue
		}
		delta, err := strconv.ParseFloat(metas[i].delta, 64)
		if err != nil {
			continue
		}
		expected := (delta / officialComparePercent) * (*row.LimitSum - *row.BlockedLimit)
		metas[i].expected = formatMoney(expected)
		metas[i].diff = formatMoney(*row.SinceActual - expected)
	}
	out := make([][]string, 0, len(sorted)+1)
	out = append(out, accountCSVHeader)
	for i, row := range sorted {
		out = append(out, accountCSVRecord(row, metas[i]))
	}
	return out
}

func accountMeta(row WeeklyAccountInput) accountCSVMeta {
	meta := accountCSVMeta{}
	if row.NoWeekSnapshot {
		meta.reason = "过去周没有当时的导出快照"
		return meta
	}
	if !row.HasPrev {
		meta.reason = "无上次导出"
		return meta
	}
	delta, comparable, reason := officialDelta(row)
	meta.delta = delta
	meta.comparable = comparable
	meta.reason = reason
	if !comparable {
		return meta
	}
	if row.SinceCatalog == nil || *row.SinceCatalog == 0 {
		meta.comparable = false
		meta.reason = "官方百分比上升但网关目录价为0，可能有网关外用量"
		meta.perPercent = ""
		return meta
	}
	change, err := strconv.ParseFloat(delta, 64)
	if err != nil || change == 0 {
		meta.comparable = false
		meta.reason = "变化不大于0"
		return meta
	}
	meta.reason = ""
	meta.perPercent = formatMoney(*row.SinceCatalog / change)
	return meta
}

func officialDelta(row WeeklyAccountInput) (delta string, comparable bool, reason string) {
	resetEqual := row.OfficialReset != nil && row.PrevReset != nil && row.OfficialReset.UTC().Truncate(time.Second).Equal(row.PrevReset.UTC().Truncate(time.Second))
	if row.OfficialReset == nil || row.PrevReset == nil || !resetEqual {
		if row.OfficialPercent != nil && row.PrevPercent != nil {
			delta = format4(*row.OfficialPercent - *row.PrevPercent)
		}
		return delta, false, "官方窗口已重置"
	}
	if row.OfficialPercent == nil || row.PrevPercent == nil {
		return "", false, "官方百分比缺失"
	}
	change := round4(*row.OfficialPercent) - round4(*row.PrevPercent)
	delta = format4(change)
	if change <= 0 {
		return delta, false, "变化不大于0"
	}
	return delta, true, ""
}

func accountCSVRecord(row WeeklyAccountInput, meta accountCSVMeta) []string {
	flag := "否"
	if meta.comparable {
		flag = "是"
	}
	reason := meta.reason
	if meta.comparable {
		reason = ""
	}
	sinceCatalog, sinceActual := "", ""
	if !row.NoWeekSnapshot && row.HasPrev {
		if row.SinceCatalog != nil {
			sinceCatalog = formatMoney(*row.SinceCatalog)
		}
		if row.SinceActual != nil {
			sinceActual = formatMoney(*row.SinceActual)
		}
	}
	limit, blocked := "", ""
	if row.CurrentWeek {
		if row.LimitSum != nil {
			limit = formatMoney(*row.LimitSum)
		}
		if row.BlockedLimit != nil {
			blocked = formatMoney(*row.BlockedLimit)
		}
	}
	weekActual := ""
	if row.WeekActualSum != nil {
		weekActual = formatMoney(*row.WeekActualSum)
	}
	return []string{
		row.Name,
		row.Platform,
		row.Plan,
		row.Status,
		strings.Join(row.Groups, "；"),
		strconv.FormatInt(row.Requests, 10),
		strconv.FormatInt(row.DistinctUsers, 10),
		formatMoney(row.CatalogCost),
		formatMoney(row.ActualCost),
		row.WeekStart.Format(time.RFC3339),
		row.WeekEnd.Format(time.RFC3339),
		row.Timezone,
		formatOptional4(row.OfficialPercent),
		formatOptionalTime(row.OfficialReset),
		formatOptionalTime(row.OfficialObserved),
		row.OfficialSource,
		meta.delta,
		flag,
		reason,
		sinceCatalog,
		sinceActual,
		meta.perPercent,
		limit,
		blocked,
		weekActual,
		meta.expected,
		meta.diff,
		accountReadNote,
	}
}

func formatMoney(v float64) string {
	return strconv.FormatFloat(v, 'f', 6, 64)
}

func round4(v float64) float64 {
	return math.Round(v*10000) / 10000
}

func format4(v float64) string {
	return strconv.FormatFloat(round4(v), 'f', 4, 64)
}

func formatOptional4(v *float64) string {
	if v == nil {
		return ""
	}
	return format4(*v)
}

func formatOptionalTime(v *time.Time) string {
	if v == nil || v.IsZero() {
		return ""
	}
	return v.UTC().Format(time.RFC3339)
}

func writeCSV(rows [][]string) ([]byte, error) {
	var buf bytes.Buffer
	buf.Write([]byte{0xEF, 0xBB, 0xBF})
	w := csv.NewWriter(&buf)
	w.UseCRLF = true
	for _, row := range rows {
		cells := make([]string, len(row))
		for i, cell := range row {
			cells[i] = escapeCSVCell(cell)
		}
		if err := w.Write(cells); err != nil {
			return nil, err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func escapeCSVCell(s string) string {
	if s == "" || isDecimal(s) {
		return s
	}
	r, _ := utf8.DecodeRuneInString(s)
	switch r {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + s
	default:
		return s
	}
}

func isDecimal(s string) bool {
	if s == "" {
		return false
	}
	_, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && r != '-' && r != '.' && r != '+' {
			return false
		}
	}
	return true
}

func writeWeeklyZip(userRows, accountRows [][]string) ([]byte, error) {
	userCSV, err := writeCSV(userRows)
	if err != nil {
		return nil, err
	}
	accountCSV, err := writeCSV(accountRows)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	if err := writeZipFile(zw, "用户分组.csv", userCSV); err != nil {
		return nil, err
	}
	if err := writeZipFile(zw, "上游账号.csv", accountCSV); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeZipFile(zw *zip.Writer, name string, body []byte) error {
	w, err := zw.Create(name)
	if err != nil {
		return err
	}
	_, err = w.Write(body)
	return err
}

func weeklyZipName(start, end time.Time) string {
	return fmt.Sprintf("weekly-usage_%s-%s.zip", start.Format("20060102"), end.Format("20060102"))
}
