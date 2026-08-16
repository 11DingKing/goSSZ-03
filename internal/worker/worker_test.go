package worker

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/qilian/patrol-dispatch/internal/domain"
	"github.com/qilian/patrol-dispatch/internal/service"
	"github.com/qilian/patrol-dispatch/internal/store"
)

func newWorkerTestEnv(t *testing.T) (*Worker, *service.Service, *store.Store) {
	t.Helper()
	s := store.New()
	es := store.NewEquipmentStore()
	s.SaveTeam(&domain.PatrolTeam{ID: "team-1", Name: "一分队"})
	s.SaveRoute(&domain.PatrolRoute{ID: "route-1", Name: "巡护线", Boundary: "hualong"})
	svc := service.New(service.DefaultConfig(), s, es)

	plan := &domain.PatrolPlan{TeamID: "team-1", RouteID: "route-1", Date: "2026-08-16"}
	_ = svc.SubmitPlan(plan)
	_ = svc.ApprovePlan(plan.ID)
	_ = svc.StartPatrol(plan.ID)

	w := New(svc, 50*time.Millisecond)
	return w, svc, s
}

func TestWorkerRoutesPendingEvents(t *testing.T) {
	w, svc, s := newWorkerTestEnv(t)
	plan := s.ListPlans("")[0]

	// Record two pending events.
	svc.RecordDisturbance(&domain.DisturbanceEvent{
		OfficerID: "off-1", DeviceID: "DEV-1", PlanID: plan.ID,
		Timestamp: time.Now(), Type: domain.DisturbanceGrazing, Severity: domain.SeverityLow,
	})
	svc.RecordDisturbance(&domain.DisturbanceEvent{
		OfficerID: "off-1", DeviceID: "DEV-1", PlanID: plan.ID,
		Timestamp: time.Now().Add(time.Second), Type: domain.DisturbancePoaching, Severity: domain.SeverityCritical,
	})

	// Run one tick.
	w.RunOnce()

	if w.TotalRouted() != 2 {
		t.Errorf("total routed = %d, want 2", w.TotalRouted())
	}

	// Verify all events are now routed.
	events := s.ListPendingEvents()
	if len(events) != 0 {
		t.Errorf("pending events after route = %d, want 0", len(events))
	}
}

func TestWorkerStartStop(t *testing.T) {
	w, svc, s := newWorkerTestEnv(t)
	plan := s.ListPlans("")[0]

	// Record an event that will be routed by the background loop.
	svc.RecordDisturbance(&domain.DisturbanceEvent{
		OfficerID: "off-1", DeviceID: "DEV-1", PlanID: plan.ID,
		Timestamp: time.Now(), Type: domain.DisturbanceFire, Severity: domain.SeverityHigh,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.Start(ctx)

	// Wait for the worker to route (tick is 50ms).
	deadline := time.After(2 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("worker did not route event within 2s")
		default:
		}
		if w.TotalRouted() > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	w.Stop()

	// Verify the event was routed to the department (high severity).
	events := s.ListEventsByPlan(plan.ID)
	for _, e := range events {
		if e.Status != domain.EventStatusRouted {
			t.Errorf("event status = %s, want routed", e.Status)
		}
		if e.RoutedTo != "forestry-dept-001" {
			t.Errorf("routed_to = %s, want forestry-dept-001", e.RoutedTo)
		}
	}
}

func TestWorkerRetryQueue(t *testing.T) {
	w, _, _ := newWorkerTestEnv(t)

	// Enqueue several retry items.
	for i := 0; i < 3; i++ {
		w.EnqueueRetry("batch-"+string(rune('A'+i)), []byte("payload"))
	}
	if w.PendingRetryCount() != 3 {
		t.Errorf("pending = %d, want 3", w.PendingRetryCount())
	}

	// RunOnce should keep recent items (within 5-minute cutoff).
	w.RunOnce()
	if w.PendingRetryCount() != 3 {
		t.Errorf("pending after tick = %d, want 3 (recent items kept)", w.PendingRetryCount())
	}
}

func TestWorkerConcurrentRunOnce(t *testing.T) {
	w, svc, s := newWorkerTestEnv(t)
	plan := s.ListPlans("")[0]

	// Record many events.
	for i := 0; i < 20; i++ {
		svc.RecordDisturbance(&domain.DisturbanceEvent{
			OfficerID: "off-1", DeviceID: "DEV-1", PlanID: plan.ID,
			Timestamp: time.Now().Add(time.Duration(i) * time.Second),
			Type:      domain.DisturbanceOther, Severity: domain.SeverityMedium,
		})
	}

	// Run multiple ticks concurrently — should not panic or corrupt state.
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w.RunOnce()
		}()
	}
	wg.Wait()

	// All events should be routed (idempotent — routing pending is a no-op).
	events := s.ListEventsByPlan(plan.ID)
	for _, e := range events {
		if e.Status != domain.EventStatusRouted {
			t.Errorf("event %s status = %s, want routed", e.ID, e.Status)
		}
	}
}
