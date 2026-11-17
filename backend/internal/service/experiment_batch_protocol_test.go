//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
)

func TestExperimentBatchRejectsSelectedGlobalAIBeforeJobHoldOrProviderSubmit(t *testing.T) {
	svc, repo, queue, provider, _ := newTestBatchImagePublicService(true)
	account := testBatchImageAccount(101, AccountTypeAPIKey)
	account.Credentials["base_url"] = "https://globalai.vip/v1"
	svc.AccountRepo = &publicBatchImageAccountRepo{accounts: []Account{account}}
	groupID := int64(4)
	svc.GroupRepo = &publicBatchImageGroupRepo{groups: map[int64]*Group{groupID: {
		ID: groupID, Platform: PlatformGemini, UsageKind: UsageKindExperiment,
		AllowImageGeneration: true, AllowBatchImageGeneration: true,
	}}}
	_, err := svc.Submit(context.Background(), BatchImageOwner{UserID: 11, APIKeyID: 22, GroupID: &groupID}, validBatchImageSubmitRequest(), "")
	if !errors.Is(err, ErrExperimentProtocolUnsupported) {
		t.Fatalf("selected GlobalAI batch was not rejected: %v", err)
	}
	if len(repo.jobs) != 0 || len(queue.enqueued) != 0 || len(provider.submits) != 0 || len(svc.BillingRepo.(*fakeBatchImageBillingRepo).reserves) != 0 {
		t.Fatal("unsupported batch created a job, hold, queue item or supplier request")
	}
}
