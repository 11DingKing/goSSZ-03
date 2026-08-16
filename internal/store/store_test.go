package store

import (
	"testing"
	"time"

	"github.com/qilian/patrol-dispatch/internal/domain"
)

func TestStorePlanCRUD(t *testing.T) {
	s := New()
	plan := &domain.PatrolPlan{
		ID:      "plan-1",
		TeamID:  "team-1",
		Date:    "2026-08-16",
		RouteID: "route-1",
		Status:  domain.PlanStatusSubmitted,
	}
	s.SavePlan(plan)

	got, ok := s.GetPlan("plan-1")
	if !ok {
		t.Fatal("plan not found after save")
	}
	if got.TeamID != "team-1" {
		t.Errorf("team = %s, want team-1", got.TeamID)
	}
	if got.CreatedAt.IsZero() {
		t.Error("CreatedAt should be set automatically")
	}
}

func TestStoreUpdatePlanStatus(t *testing.T) {
	s := New()
	plan := &domain.PatrolPlan{
		ID: "plan-1", TeamID: "team-1", Date: "2026-08-16",
		RouteID: "route-1", Status: domain.PlanStatusSubmitted,
	}
	s.SavePlan(plan)

	if err := s.UpdatePlanStatus("plan-1", domain.PlanStatusApproved); err != nil {
		t.Fatalf("approve failed: %v", err)
	}
	got, _ := s.GetPlan("plan-1")
	if got.Status != domain.PlanStatusApproved {
		t.Errorf("status = %s, want approved", got.Status)
	}

	// Invalid transition: approved -> submitted should fail.
	if err := s.UpdatePlanStatus("plan-1", domain.PlanStatusSubmitted); err == nil {
		t.Error("approved->submitted should fail")
	}

	// Non-existent plan.
	if err := s.UpdatePlanStatus("nope", domain.PlanStatusApproved); err == nil {
		t.Error("updating non-existent plan should fail")
	}
}

func TestStoreCheckInDedup(t *testing.T) {
	s := New()
	ts := time.Date(2026, 8, 16, 10, 30, 0, 0, time.UTC)
	c1 := &domain.CheckIn{
		OfficerID: "off-1", DeviceID: "DEV-1", PlanID: "plan-1",
		Timestamp: ts, Location: domain.Waypoint{Lat: 37.66, Lng: 102.61},
	}
	id1, inserted1 := s.SaveCheckIn(c1)
	if !inserted1 {
		t.Error("first check-in should be inserted")
	}

	// Duplicate: same device + officer + timestamp.
	c2 := &domain.CheckIn{
		OfficerID: "off-1", DeviceID: "DEV-1", PlanID: "plan-1",
		Timestamp: ts, Location: domain.Waypoint{Lat: 37.66, Lng: 102.61},
	}
	id2, inserted2 := s.SaveCheckIn(c2)
	if inserted2 {
		t.Error("duplicate check-in should not be inserted")
	}
	if id1 != id2 {
		t.Errorf("duplicate should return same ID: %s vs %s", id1, id2)
	}

	// Different timestamp → new record.
	c3 := &domain.CheckIn{
		OfficerID: "off-1", DeviceID: "DEV-1", PlanID: "plan-1",
		Timestamp: ts.Add(10 * time.Minute), Location: domain.Waypoint{Lat: 37.67, Lng: 102.62},
	}
	_, inserted3 := s.SaveCheckIn(c3)
	if !inserted3 {
		t.Error("check-in with different timestamp should be inserted")
	}

	list := s.ListCheckInsByPlan("plan-1")
	if len(list) != 2 {
		t.Errorf("expected 2 check-ins, got %d", len(list))
	}
}

func TestStoreEventMergeOnEscalation(t *testing.T) {
	s := New()
	ts := time.Date(2026, 8, 16, 11, 0, 0, 0, time.UTC)

	// First report: medium severity.
	e1 := &domain.DisturbanceEvent{
		OfficerID: "off-1", DeviceID: "DEV-1", PlanID: "plan-1",
		Timestamp: ts, Type: domain.DisturbanceGrazing, Severity: domain.SeverityMedium,
	}
	id1, inserted1, merged1 := s.SaveEvent(e1)
	if !inserted1 || merged1 {
		t.Fatal("first event should be inserted, not merged")
	}

	// Duplicate key but higher severity → merge (replace).
	e2 := &domain.DisturbanceEvent{
		OfficerID: "off-1", DeviceID: "DEV-1", PlanID: "plan-1",
		Timestamp: ts, Type: domain.DisturbanceGrazing, Severity: domain.SeverityHigh,
	}
	id2, inserted2, merged2 := s.SaveEvent(e2)
	if inserted2 || !merged2 {
		t.Fatal("escalated duplicate should be merged, not inserted")
	}
	if id1 != id2 {
		t.Error("merged event should keep same ID")
	}

	// Verify the stored event has the higher severity.
	got, _ := s.GetEvent(id1)
	if got.Severity != domain.SeverityHigh {
		t.Errorf("severity = %s, want high after merge", got.Severity)
	}

	// Duplicate with lower severity → no change.
	e3 := &domain.DisturbanceEvent{
		OfficerID: "off-1", DeviceID: "DEV-1", PlanID: "plan-1",
		Timestamp: ts, Type: domain.DisturbanceGrazing, Severity: domain.SeverityLow,
	}
	_, inserted3, merged3 := s.SaveEvent(e3)
	if inserted3 || merged3 {
		t.Fatal("lower-severity duplicate should be skipped, not merged")
	}
	got, _ = s.GetEvent(id1)
	if got.Severity != domain.SeverityHigh {
		t.Errorf("severity should stay high, got %s", got.Severity)
	}
}

func TestStoreDispatchAndRequisitionLookup(t *testing.T) {
	s := New()
	d := &domain.DispatchOrder{
		ID: "disp-1", PlanID: "plan-1", TeamID: "team-1", RouteID: "route-1",
	}
	s.SaveDispatch(d)

	got, ok := s.GetDispatchByPlan("plan-1")
	if !ok || got.ID != "disp-1" {
		t.Fatal("dispatch not found by plan")
	}

	r := &domain.Requisition{
		ID: "req-1", PlanID: "plan-1", TeamID: "team-1",
		Status: domain.RequisitionStatusPendingApproval,
	}
	s.SaveRequisition(r)

	gotReq, ok := s.GetRequisitionByPlan("plan-1")
	if !ok || gotReq.ID != "req-1" {
		t.Fatal("requisition not found by plan")
	}

	// Approve via store.
	if err := s.UpdateRequisitionStatus("req-1", domain.RequisitionStatusApproved, "chief-001"); err != nil {
		t.Fatalf("approve requisition: %v", err)
	}
	gotReq, _ = s.GetRequisition("req-1")
	if gotReq.Status != domain.RequisitionStatusApproved {
		t.Errorf("status = %s, want approved", gotReq.Status)
	}
	if gotReq.ApprovedBy != "chief-001" {
		t.Errorf("approved_by = %s, want chief-001", gotReq.ApprovedBy)
	}
}
