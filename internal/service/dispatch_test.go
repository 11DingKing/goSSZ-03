package service

import (
	"testing"

	"github.com/qilian/patrol-dispatch/internal/domain"
)

func TestGenerateDispatchSuccess(t *testing.T) {
	svc, s, _ := newTestService(t)
	plan := &domain.PatrolPlan{
		TeamID: "team-1", RouteID: "route-1", Date: "2026-08-16",
		EquipmentNeeds: []domain.EquipmentNeed{
			{Type: domain.EquipmentSatellitePhone, Quantity: 1, Category: domain.EquipmentCategoryEmergency},
			{Type: domain.EquipmentGPS, Quantity: 1, Category: domain.EquipmentCategoryRegular},
		},
	}
	_ = svc.SubmitPlan(plan)
	_ = svc.ApprovePlan(plan.ID)

	result, err := svc.GenerateDispatch(plan.ID)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if len(result.Dispatch.Equipment) != 2 {
		t.Errorf("expected 2 assigned, got %d", len(result.Dispatch.Equipment))
	}
	if result.Requisition.Status != domain.RequisitionStatusPendingApproval {
		t.Errorf("requisition status = %s, want pending_approval", result.Requisition.Status)
	}

	// Plan should be dispatched.
	p, _ := s.GetPlan(plan.ID)
	if p.Status != domain.PlanStatusDispatched {
		t.Errorf("plan status = %s, want dispatched", p.Status)
	}
}

func TestGenerateDispatchWrongState(t *testing.T) {
	svc, _, _ := newTestService(t)
	plan := &domain.PatrolPlan{
		TeamID: "team-1", RouteID: "route-1", Date: "2026-08-16",
	}
	_ = svc.SubmitPlan(plan)
	// Skip approve → plan is still submitted.
	_, err := svc.GenerateDispatch(plan.ID)
	if err == nil {
		t.Fatal("dispatch on non-approved plan should fail")
	}
}

func TestGenerateDispatchBoundaryViolation(t *testing.T) {
	svc, s, _ := newTestService(t)
	// Add a route outside Hualong jurisdiction.
	s.SaveRoute(&domain.PatrolRoute{
		ID: "route-2", Name: "外部路线", Boundary: "other-station",
	})
	plan := &domain.PatrolPlan{
		TeamID: "team-1", RouteID: "route-2", Date: "2026-08-16",
		EquipmentNeeds: []domain.EquipmentNeed{
			{Type: domain.EquipmentGPS, Quantity: 1, Category: domain.EquipmentCategoryRegular},
		},
	}
	_ = svc.SubmitPlan(plan)
	_ = svc.ApprovePlan(plan.ID)

	_, err := svc.GenerateDispatch(plan.ID)
	if err == nil {
		t.Fatal("dispatch with out-of-boundary route should fail")
	}
}

func TestGenerateDispatchEquipmentConflict(t *testing.T) {
	svc, s, _ := newTestService(t)

	// Team 1 reserves the only satellite phone type on 2026-08-16.
	plan1 := &domain.PatrolPlan{
		TeamID: "team-1", RouteID: "route-1", Date: "2026-08-16",
		EquipmentNeeds: []domain.EquipmentNeed{
			{Type: domain.EquipmentSatellitePhone, Quantity: 1, Category: domain.EquipmentCategoryEmergency},
		},
	}
	_ = svc.SubmitPlan(plan1)
	_ = svc.ApprovePlan(plan1.ID)
	if _, err := svc.GenerateDispatch(plan1.ID); err != nil {
		t.Fatalf("team-1 dispatch: %v", err)
	}

	// Team 2 (need a second team) tries the same emergency type on same day.
	s.SaveTeam(&domain.PatrolTeam{ID: "team-2", Name: "二分队"})
	plan2 := &domain.PatrolPlan{
		TeamID: "team-2", RouteID: "route-1", Date: "2026-08-16",
		EquipmentNeeds: []domain.EquipmentNeed{
			{Type: domain.EquipmentSatellitePhone, Quantity: 1, Category: domain.EquipmentCategoryEmergency},
		},
	}
	_ = svc.SubmitPlan(plan2)
	_ = svc.ApprovePlan(plan2.ID)
	_, err := svc.GenerateDispatch(plan2.ID)
	if err == nil {
		t.Fatal("team-2 should fail to reserve same emergency type on same day")
	}

	// Plan 2 should remain approved (not dispatched).
	p2, _ := s.GetPlan(plan2.ID)
	if p2.Status != domain.PlanStatusApproved {
		t.Errorf("plan-2 status = %s, want approved (rollback)", p2.Status)
	}
}

func TestGenerateDispatchInsufficientStock(t *testing.T) {
	svc, _, _ := newTestService(t)
	plan := &domain.PatrolPlan{
		TeamID: "team-1", RouteID: "route-1", Date: "2026-08-16",
		EquipmentNeeds: []domain.EquipmentNeed{
			{Type: domain.EquipmentGPS, Quantity: 10, Category: domain.EquipmentCategoryRegular},
		},
	}
	_ = svc.SubmitPlan(plan)
	_ = svc.ApprovePlan(plan.ID)

	_, err := svc.GenerateDispatch(plan.ID)
	if err == nil {
		t.Fatal("dispatch with insufficient stock should fail")
	}
}
