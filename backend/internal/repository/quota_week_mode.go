package repository

import (
	"context"
	"sync"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

const platformQuotaWeekModeCacheTTL = 5 * time.Second

type platformQuotaWeekModeCache struct {
	mode      string
	expiresAt time.Time
}

var (
	platformQuotaWeekModeMu sync.Mutex
	platformQuotaWeekMode   platformQuotaWeekModeCache
)

// QuotaWeekMode 读取平台周限额的刷新方式。registration 从注册时刻起每 7 天，monday 是每周一 0 点。
func (r *userPlatformQuotaRepository) QuotaWeekMode(ctx context.Context) string {
	if r == nil || r.client == nil {
		return service.PlatformQuotaWeekModeRegistration
	}
	now := time.Now()
	platformQuotaWeekModeMu.Lock()
	if now.Before(platformQuotaWeekMode.expiresAt) && platformQuotaWeekMode.mode != "" {
		mode := platformQuotaWeekMode.mode
		platformQuotaWeekModeMu.Unlock()
		return mode
	}
	platformQuotaWeekModeMu.Unlock()

	mode := service.PlatformQuotaWeekModeRegistration
	rows, err := r.client.QueryContext(ctx, `SELECT value FROM settings WHERE key = $1`, service.SettingKeyPlatformQuotaWeekMode)
	if err == nil {
		defer rows.Close()
		if rows.Next() {
			var raw string
			if scanErr := rows.Scan(&raw); scanErr == nil {
				mode = service.NormalizePlatformQuotaWeekMode(raw)
			}
		}
	}
	platformQuotaWeekModeMu.Lock()
	platformQuotaWeekMode = platformQuotaWeekModeCache{mode: mode, expiresAt: now.Add(platformQuotaWeekModeCacheTTL)}
	platformQuotaWeekModeMu.Unlock()
	return mode
}

func (r *userPlatformQuotaRepository) currentWeeklyWindowStart(ctx context.Context, txClient *dbent.Client, userID int64, stored *time.Time, now time.Time) time.Time {
	if r.QuotaWeekMode(ctx) == service.PlatformQuotaWeekModeMonday {
		return timezone.StartOfWeek(now)
	}
	// 已有起点就接着它每 7 天滚动。管理员重置会把起点改成点击时刻。
	if stored != nil && !stored.IsZero() {
		return timezone.WeeklyPeriodStart(*stored, now)
	}
	anchor := now
	if owner, err := txClient.User.Get(ctx, userID); err == nil && !owner.CreatedAt.IsZero() {
		anchor = owner.CreatedAt
	}
	return timezone.WeeklyPeriodStart(anchor, now)
}
