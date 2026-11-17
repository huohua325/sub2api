package service

import (
	"context"
	"sync"
	"testing"
	"time"
)

type experimentWorkerLifecycleStore struct {
	OfficialPointStore
	CodingQuotaLedger
	ExperimentQuotaReconciler
	codingEntered, experimentEntered     chan struct{}
	codingCancelled, experimentCancelled chan struct{}
	codingOnce, experimentOnce           sync.Once
}

func (s *experimentWorkerLifecycleStore) ListOfficialParents(ctx context.Context) ([]UsageMonitorParentRow, error) {
	s.codingOnce.Do(func() { close(s.codingEntered) })
	<-ctx.Done()
	close(s.codingCancelled)
	return nil, ctx.Err()
}

func (s *experimentWorkerLifecycleStore) ListPendingExperimentCharges(ctx context.Context, _ time.Time) ([]PendingExperimentCharge, error) {
	s.experimentOnce.Do(func() { close(s.experimentEntered) })
	<-ctx.Done()
	close(s.experimentCancelled)
	return nil, ctx.Err()
}

type experimentWorkerAccountRepo struct{ AccountRepository }

func TestExperimentWorkerRemainsIndependentAndStopCancelsBothWorkers(t *testing.T) {
	store := &experimentWorkerLifecycleStore{
		codingEntered: make(chan struct{}), experimentEntered: make(chan struct{}),
		codingCancelled: make(chan struct{}), experimentCancelled: make(chan struct{}),
	}
	service := NewOfficialPointService(nil, store, &GrokQuotaService{accountRepo: &experimentWorkerAccountRepo{}})
	service.Start(context.Background())
	defer service.Stop()
	for name, entered := range map[string]<-chan struct{}{"coding": store.codingEntered, "experiment": store.experimentEntered} {
		select {
		case <-entered:
		case <-time.After(2 * time.Second):
			t.Fatalf("%s worker was blocked behind the other billing worker", name)
		}
	}
	done := make(chan struct{})
	go func() { service.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not wait for bounded cancellation of both workers")
	}
	for name, cancelled := range map[string]<-chan struct{}{"coding": store.codingCancelled, "experiment": store.experimentCancelled} {
		select {
		case <-cancelled:
		default:
			t.Fatalf("Stop returned before %s worker acknowledged cancellation", name)
		}
	}
}
