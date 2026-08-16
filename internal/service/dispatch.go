package service

import (
	"fmt"
	"time"

	"github.com/qilian/patrol-dispatch/internal/domain"
)

// DispatchResult is returned by GenerateDispatch so the caller (HTTP handler)
// can render both the dispatch order and the requisition slip.
type DispatchResult struct {
	Dispatch    *domain.DispatchOrder
	Requisition *domain.Requisition
}

// GenerateDispatch auto-generates a dispatch order and a material requisition
// slip from an approved patrol plan. It:
//  1. Validates the plan is in the approved state.
//  2. Validates the route is within the station's jurisdictional boundary.
//  3. Reserves equipment from the warehouse (enforcing emergency per-day
//     single-team occupancy).
//  4. Creates the requisition slip in the pending-approval state so the
//     station chief can review before issue.
//
// If equipment reservation fails (insufficient stock or emergency conflict)
// the plan stays in the approved state and the caller can adjust or cancel.
func (svc *Service) GenerateDispatch(planID string) (*DispatchResult, error) {
	plan, ok := svc.store.GetPlan(planID)
	if !ok {
		return nil, fmt.Errorf("plan %q not found", planID)
	}
	if plan.Status != domain.PlanStatusApproved {
		return nil, fmt.Errorf("plan %q is %s, must be approved before dispatch", planID, plan.Status)
	}

	route, ok := svc.store.GetRoute(plan.RouteID)
	if !ok {
		return nil, fmt.Errorf("route %q not found", plan.RouteID)
	}

	// Boundary validation: the route must reference the Hualong station boundary.
	if route.Boundary == "" {
		return nil, fmt.Errorf("route %q has no boundary reference", route.ID)
	}
	if route.Boundary != "hualong" {
		return nil, fmt.Errorf("route %q boundary %q is outside Hualong jurisdiction", route.ID, route.Boundary)
	}

	// Reserve equipment with emergency occupancy locking.
	assigned, err := svc.equip.ReserveForTeam(plan.TeamID, plan.Date, plan.EquipmentNeeds)
	if err != nil {
		return nil, fmt.Errorf("equipment reservation failed: %w", err)
	}

	// Transition the plan to dispatched first; if this fails we release the
	// reservation and leave the plan in its current state.
	if err := svc.store.UpdatePlanStatus(planID, domain.PlanStatusDispatched); err != nil {
		svc.equip.ReleaseReservation(assigned, plan.Date)
		return nil, fmt.Errorf("plan status transition failed: %w", err)
	}

	// Build the dispatch order now that the state transition succeeded.
	dispatch := &domain.DispatchOrder{
		ID:        domain.NewID("disp"),
		PlanID:    plan.ID,
		TeamID:    plan.TeamID,
		RouteID:   plan.RouteID,
		Equipment: assigned,
		CreatedAt: time.Now(),
	}
	svc.store.SaveDispatch(dispatch)

	// Build the requisition slip for station-chief approval.
	items := make([]domain.RequisitionItem, len(assigned))
	for i, a := range assigned {
		items[i] = domain.RequisitionItem{
			EquipmentID: a.EquipmentID,
			Type:        a.Type,
			Category:    a.Category,
		}
	}
	req := &domain.Requisition{
		ID:        domain.NewID("req"),
		PlanID:    plan.ID,
		TeamID:    plan.TeamID,
		Items:     items,
		Status:    domain.RequisitionStatusPendingApproval,
		CreatedAt: time.Now(),
	}
	svc.store.SaveRequisition(req)

	return &DispatchResult{Dispatch: dispatch, Requisition: req}, nil
}

// GetDispatch retrieves a dispatch order by its ID.
func (svc *Service) GetDispatch(id string) (*domain.DispatchOrder, bool) {
	return svc.store.GetDispatch(id)
}

// GetRequisition retrieves a requisition slip by its ID.
func (svc *Service) GetRequisition(id string) (*domain.Requisition, bool) {
	return svc.store.GetRequisition(id)
}

// GetDispatchByPlan retrieves the dispatch order associated with a plan.
func (svc *Service) GetDispatchByPlan(planID string) (*domain.DispatchOrder, bool) {
	return svc.store.GetDispatchByPlan(planID)
}

// GetRequisitionByPlan retrieves the requisition associated with a plan.
func (svc *Service) GetRequisitionByPlan(planID string) (*domain.Requisition, bool) {
	return svc.store.GetRequisitionByPlan(planID)
}
