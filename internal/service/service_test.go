package service

import (
	"testing"
	"time"

	"github.com/qilian/patrol-dispatch/internal/domain"
	"github.com/qilian/patrol-dispatch/internal/store"
)

func newTestService(t *testing.T) (*Service, *store.Store, *store.EquipmentStore) {
	t.Helper()
	s := store.New()
	es := store.NewEquipmentStore()
	// Seed a team and route.
	s.SaveTeam(&domain.PatrolTeam{ID: "team-1", Name: "一分队"})
	s.SaveRoute(&domain.PatrolRoute{
		ID: "route-1", Name: "巡护线A", Boundary: "hualong",
		Waypoints: []domain.Waypoint{{Lat: 37.66, Lng: 102.61}},
	})
	// Seed equipment.
	for _, d := range []*domain.Equipment{
		{ID: "sp-1", Type: domain.EquipmentSatellitePhone, Category: domain.EquipmentCategoryEmergency, Status: domain.EquipmentStatusInStock},
		{ID: "sp-2", Type: domain.EquipmentSatellitePhone, Category: domain.EquipmentCategoryEmergency, Status: domain.EquipmentStatusInStock},
		{ID: "ic-1", Type: domain.EquipmentInfraredCamera, Category: domain.EquipmentCategoryEmergency, Status: domain.EquipmentStatusInStock},
		{ID: "gps-1", Type: domain.EquipmentGPS, Category: domain.EquipmentCategoryRegular, Status: domain.EquipmentStatusInStock},
		{ID: "gps-2", Type: domain.EquipmentGPS, Category: domain.EquipmentCategoryRegular, Status: domain.EquipmentStatusInStock},
		{ID: "bin-1", Type: domain.EquipmentBinoculars, Category: domain.EquipmentCategoryRegular, Status: domain.EquipmentStatusInStock},
	} {
		es.AddDevice(d)
	}
	svc := New(DefaultConfig(), s, es)
	return svc, s, es
}

func TestSubmitPlanValidation(t *testing.T) {
	svc, _, _ := newTestService(t)

	// Missing date.
	err := svc.SubmitPlan(&domain.PatrolPlan{TeamID: "team-1", RouteID: "route-1"})
	if err == nil {
		t.Fatal("missing date should fail")
	}

	// Non-existent team.
	err = svc.SubmitPlan(&domain.PatrolPlan{TeamID: "nope", RouteID: "route-1", Date: "2026-08-16"})
	if err == nil {
		t.Fatal("non-existent team should fail")
	}

	// Non-existent route.
	err = svc.SubmitPlan(&domain.PatrolPlan{TeamID: "team-1", RouteID: "nope", Date: "2026-08-16"})
	if err == nil {
		t.Fatal("non-existent route should fail")
	}

	// Valid submission.
	plan := &domain.PatrolPlan{
		TeamID: "team-1", RouteID: "route-1", Date: "2026-08-16",
		EquipmentNeeds: []domain.EquipmentNeed{
			{Type: domain.EquipmentSatellitePhone, Quantity: 1, Category: domain.EquipmentCategoryEmergency},
		},
	}
	if err := svc.SubmitPlan(plan); err != nil {
		t.Fatalf("valid submit failed: %v", err)
	}
	if plan.ID == "" {
		t.Error("plan ID should be assigned")
	}
	if plan.Status != domain.PlanStatusSubmitted {
		t.Errorf("status = %s, want submitted", plan.Status)
	}
}

func TestFullPlanLifecycle(t *testing.T) {
	svc, s, es := newTestService(t)

	// Submit → approve → dispatch → (approve req) → start → complete.
	plan := &domain.PatrolPlan{
		TeamID: "team-1", RouteID: "route-1", Date: "2026-08-16",
		EquipmentNeeds: []domain.EquipmentNeed{
			{Type: domain.EquipmentGPS, Quantity: 1, Category: domain.EquipmentCategoryRegular},
		},
	}
	if err := svc.SubmitPlan(plan); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if err := svc.ApprovePlan(plan.ID); err != nil {
		t.Fatalf("approve: %v", err)
	}
	result, err := svc.GenerateDispatch(plan.ID)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if result.Dispatch == nil || result.Requisition == nil {
		t.Fatal("dispatch and requisition should be generated")
	}

	// Equipment should be reserved.
	if es.AvailableStock(domain.EquipmentGPS) != 1 {
		t.Errorf("GPS stock after reserve = %d, want 1", es.AvailableStock(domain.EquipmentGPS))
	}

	// Approve requisition (station chief).
	if err := svc.ApproveRequisition(result.Requisition.ID, "chief-001"); err != nil {
		t.Fatalf("approve requisition: %v", err)
	}
	req, _ := s.GetRequisition(result.Requisition.ID)
	if req.Status != domain.RequisitionStatusIssued {
		t.Errorf("requisition status = %s, want issued", req.Status)
	}

	// Start and complete patrol.
	if err := svc.StartPatrol(plan.ID); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := svc.CompletePatrol(plan.ID); err != nil {
		t.Fatalf("complete: %v", err)
	}

	// Equipment should be returned to stock.
	if es.AvailableStock(domain.EquipmentGPS) != 2 {
		t.Errorf("GPS stock after return = %d, want 2", es.AvailableStock(domain.EquipmentGPS))
	}

	// Plan should be completed.
	p, _ := s.GetPlan(plan.ID)
	if p.Status != domain.PlanStatusCompleted {
		t.Errorf("plan status = %s, want completed", p.Status)
	}
}

func TestApproveRequisitionWrongApprover(t *testing.T) {
	svc, _, _ := newTestService(t)
	// Create a plan and dispatch manually.
	plan := &domain.PatrolPlan{
		TeamID: "team-1", RouteID: "route-1", Date: "2026-08-16",
		EquipmentNeeds: []domain.EquipmentNeed{
			{Type: domain.EquipmentGPS, Quantity: 1, Category: domain.EquipmentCategoryRegular},
		},
	}
	_ = svc.SubmitPlan(plan)
	_ = svc.ApprovePlan(plan.ID)
	result, _ := svc.GenerateDispatch(plan.ID)

	// Wrong approver.
	err := svc.ApproveRequisition(result.Requisition.ID, "someone-else")
	if err == nil {
		t.Fatal("wrong approver should fail")
	}
	// Correct approver.
	err = svc.ApproveRequisition(result.Requisition.ID, "chief-001")
	if err != nil {
		t.Fatalf("correct approver failed: %v", err)
	}
	// Double-approve should fail (already issued).
	err = svc.ApproveRequisition(result.Requisition.ID, "chief-001")
	if err == nil {
		t.Fatal("re-approving issued requisition should fail")
	}
}

func TestCancelPlanReleasesEquipment(t *testing.T) {
	svc, _, es := newTestService(t)
	plan := &domain.PatrolPlan{
		TeamID: "team-1", RouteID: "route-1", Date: "2026-08-16",
		EquipmentNeeds: []domain.EquipmentNeed{
			{Type: domain.EquipmentSatellitePhone, Quantity: 1, Category: domain.EquipmentCategoryEmergency},
		},
	}
	_ = svc.SubmitPlan(plan)
	_ = svc.ApprovePlan(plan.ID)
	_, _ = svc.GenerateDispatch(plan.ID)

	// Verify occupancy.
	holder, ok := es.IsEmergencyOccupied(domain.EquipmentSatellitePhone, "2026-08-16")
	if !ok || holder != "team-1" {
		t.Fatalf("occupancy should be team-1, got %s", holder)
	}

	// Cancel → should release.
	if err := svc.CancelPlan(plan.ID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	_, ok = es.IsEmergencyOccupied(domain.EquipmentSatellitePhone, "2026-08-16")
	if ok {
		t.Error("occupancy should be released after cancel")
	}
}

func TestRoutePendingEventsBySeverity(t *testing.T) {
	svc, s, _ := newTestService(t)
	// Create a plan so events can reference it.
	plan := &domain.PatrolPlan{
		TeamID: "team-1", RouteID: "route-1", Date: "2026-08-16",
	}
	_ = svc.SubmitPlan(plan)
	_ = svc.ApprovePlan(plan.ID)

	// Low-severity event → station.
	low := &domain.DisturbanceEvent{
		OfficerID: "off-1", DeviceID: "DEV-1", PlanID: plan.ID,
		Timestamp: time.Now(), Type: domain.DisturbanceGrazing, Severity: domain.SeverityLow,
	}
	svc.RecordDisturbance(low)

	// High-severity event → department.
	high := &domain.DisturbanceEvent{
		OfficerID: "off-1", DeviceID: "DEV-1", PlanID: plan.ID,
		Timestamp: time.Now().Add(time.Second), Type: domain.DisturbancePoaching, Severity: domain.SeverityHigh,
	}
	svc.RecordDisturbance(high)

	count, err := svc.RoutePendingEvents()
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if count != 2 {
		t.Fatalf("routed = %d, want 2", count)
	}

	// Verify routing targets.
	events := s.ListEventsByPlan(plan.ID)
	for _, e := range events {
		if e.Status != domain.EventStatusRouted {
			t.Errorf("event %s status = %s, want routed", e.ID, e.Status)
		}
		if e.Level == domain.RoutingDepartment && e.RoutedTo != "forestry-dept-001" {
			t.Errorf("high event routed to %s, want forestry-dept-001", e.RoutedTo)
		}
		if e.Level == domain.RoutingStation && e.RoutedTo != "duty-room-001" {
			t.Errorf("low event routed to %s, want duty-room-001", e.RoutedTo)
		}
	}
}
