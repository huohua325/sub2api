package main

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func provideCodingPoints(settings *service.SettingService, store *repository.UsageMonitorRepository,
	grok *service.GrokQuotaService, codex *service.OpenAIQuotaService,
	openai *service.OpenAIGatewayService, gateway *service.GatewayService,
	user *handler.UserHandler, monitor *service.UsageMonitorService, keys *service.APIKeyService,
) *service.OfficialPointService {
	points := service.NewOfficialPointService(settings, store, grok)
	points.SetCodexQuota(codex)
	openai.SetCodingPoints(points)
	gateway.SetCodingPoints(points)
	user.SetOfficialPoints(points)
	monitor.SetOfficialPoints(points)
	keys.SetOfficialPoints(points)
	return points
}
