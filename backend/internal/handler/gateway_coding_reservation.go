package handler

import (
	"context"
	"log/slog"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// Coding bills must reach durable storage before returning; a droppable log
// worker is never the owner of the only copy of a completed result.
func runCodingUsageTask(parent context.Context, task service.UsageRecordTask) {
	if task == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 10*time.Second)
	defer cancel()
	defer func() {
		if r := recover(); r != nil {
			slog.Error("coding_billing_handoff_panic", "panic", r)
		}
	}()
	task(usageRecordContext(parent, ctx))
}
