package sync

import (
	"testing"
	"time"

	"github.com/qilian/patrol-dispatch/internal/domain"
	"github.com/qilian/patrol-dispatch/internal/service"
	"github.com/qilian/patrol-dispatch/internal/store"
)

func newSyncTestEnv(t *testing.T) (*Manager, *service.Service, *store.Store) {
	t.Helper()
	s := store.New()
	es := store.NewEquipmentStore()
	s.SaveTeam(&domain.PatrolTeam{ID: "team-1", Name: "一分队"})
	s.SaveRoute(&domain.PatrolRoute{ID: "route-1", Name: "巡护线", Boundary: "hualong",
		Waypoints: []domain.Waypoint{{Lat: 37.66, Lng: 102.61}}})
	for _, d := range []*domain.Equipment{
		{ID: "gps-1", Type: domain.EquipmentGPS, Category: domain.EquipmentCategoryRegular, Status: domain.EquipmentStatusInStock},
	} {
		es.AddDevice(d)
	}
	svc := service.New(service.DefaultConfig(), s, es)

	// Create and approve a plan so field records can reference it.
	plan := &domain.PatrolPlan{
		TeamID: "team-1", RouteID: "route-1", Date: "2026-08-16",
	}
	_ = svc.SubmitPlan(plan)
	_ = svc.ApprovePlan(plan.ID)
	_ = svc.StartPatrol(plan.ID)

	sm := NewManager(svc)
	return sm, svc, s
}

func TestSyncBatchDedup(t *testing.T) {
	sm, svc, s := newSyncTestEnv(t)
	plan, _ := s.GetPlan(s.ListPlans("")[0].ID)
	ts := time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)

	// Pre-seed one check-in directly via the service.
	svc.RecordCheckIn(&domain.CheckIn{
		OfficerID: "off-1", DeviceID: "DEV-1", PlanID: plan.ID,
		Timestamp: ts, Location: domain.Waypoint{Lat: 37.66, Lng: 102.61},
	})

	// Sync batch with a duplicate check-in + a new one.
	batch := SyncBatch{
		CheckIns: []domain.CheckIn{
			{ // duplicate of the pre-seeded one
				OfficerID: "off-1", DeviceID: "DEV-1", PlanID: plan.ID,
				Timestamp: ts, Location: domain.Waypoint{Lat: 37.66, Lng: 102.61},
			},
			{ // new record
				OfficerID: "off-1", DeviceID: "DEV-1", PlanID: plan.ID,
				Timestamp: ts.Add(15 * time.Minute), Location: domain.Waypoint{Lat: 37.67, Lng: 102.62},
			},
		},
	}
	stats := sm.ProcessBatch(batch)
	if stats.CheckInsAccepted != 1 {
		t.Errorf("accepted = %d, want 1", stats.CheckInsAccepted)
	}
	if stats.CheckInsDuplicate != 1 {
		t.Errorf("duplicate = %d, want 1", stats.CheckInsDuplicate)
	}
}

func TestSyncBatchMergeEscalation(t *testing.T) {
	sm, _, s := newSyncTestEnv(t)
	plan, _ := s.GetPlan(s.ListPlans("")[0].ID)
	ts := time.Date(2026, 8, 16, 11, 0, 0, 0, time.UTC)

	batch := SyncBatch{
		Disturbance: []domain.DisturbanceEvent{
			{
				OfficerID: "off-1", DeviceID: "DEV-1", PlanID: plan.ID,
				Timestamp: ts, Type: domain.DisturbancePoaching, Severity: domain.SeverityMedium,
			},
		},
	}
	stats1 := sm.ProcessBatch(batch)
	if stats1.DisturbanceAccepted != 1 {
		t.Fatalf("first batch accepted = %d, want 1", stats1.DisturbanceAccepted)
	}

	// Second batch: same dedup key but escalated to critical.
	batch2 := SyncBatch{
		Disturbance: []domain.DisturbanceEvent{
			{
				OfficerID: "off-1", DeviceID: "DEV-1", PlanID: plan.ID,
				Timestamp: ts, Type: domain.DisturbancePoaching, Severity: domain.SeverityCritical,
			},
		},
	}
	stats2 := sm.ProcessBatch(batch2)
	if stats2.DisturbanceMerged != 1 {
		t.Fatalf("second batch merged = %d, want 1", stats2.DisturbanceMerged)
	}

	// Verify the stored event has critical severity.
	events := s.ListEventsByPlan(plan.ID)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Severity != domain.SeverityCritical {
		t.Errorf("severity = %s, want critical after merge", events[0].Severity)
	}
	if events[0].Level != domain.RoutingDepartment {
		t.Errorf("level = %s, want department for critical", events[0].Level)
	}
}

func TestSyncBatchValidation(t *testing.T) {
	batch := SyncBatch{
		CheckIns: []domain.CheckIn{
			{OfficerID: "", DeviceID: "DEV-1", Timestamp: time.Now()},
		},
	}
	if err := batch.Validate(); err == nil {
		t.Fatal("batch with empty officer_id should fail validation")
	}

	batch2 := SyncBatch{
		Disturbance: []domain.DisturbanceEvent{
			{OfficerID: "off-1", DeviceID: "DEV-1", Timestamp: time.Time{}},
		},
	}
	if err := batch2.Validate(); err == nil {
		t.Fatal("batch with zero timestamp should fail validation")
	}
}

func TestSyncBatchMixedRecords(t *testing.T) {
	sm, _, s := newSyncTestEnv(t)
	plan, _ := s.GetPlan(s.ListPlans("")[0].ID)
	ts := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)

	batch := SyncBatch{
		CheckIns: []domain.CheckIn{
			{OfficerID: "off-1", DeviceID: "DEV-1", PlanID: plan.ID, Timestamp: ts, Location: domain.Waypoint{Lat: 37.66, Lng: 102.61}},
		},
		Wildlife: []domain.WildlifeRecord{
			{OfficerID: "off-1", DeviceID: "DEV-1", PlanID: plan.ID, Timestamp: ts, Species: "雪豹", Location: domain.Waypoint{Lat: 37.66, Lng: 102.61}, ImageRef: "img-001"},
		},
		Disturbance: []domain.DisturbanceEvent{
			{OfficerID: "off-1", DeviceID: "DEV-1", PlanID: plan.ID, Timestamp: ts, Type: domain.DisturbanceLogging, Severity: domain.SeverityMedium},
		},
	}
	stats := sm.ProcessBatch(batch)
	if stats.TotalAccepted() != 3 {
		t.Errorf("total accepted = %d, want 3", stats.TotalAccepted())
	}
	if len(stats.Errors) != 0 {
		t.Errorf("unexpected errors: %v", stats.Errors)
	}
}

func TestMergeConflictTimestampLogic(t *testing.T) {
	earlier := time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)
	later := earlier.Add(5 * time.Minute)

	if !MergeConflict(earlier, later) {
		t.Error("incoming should win when timestamp is later")
	}
	if MergeConflict(later, earlier) {
		t.Error("existing should win when incoming timestamp is earlier")
	}
	if MergeConflict(earlier, earlier) {
		t.Error("equal timestamps should not trigger merge (first-writer-wins)")
	}
}
