package service

import (
	"fmt"
	"time"

	"github.com/qilian/patrol-dispatch/internal/domain"
	"github.com/qilian/patrol-dispatch/internal/store"
)

// Config holds runtime parameters for the application service.
type Config struct {
	StationChiefID string        // who approves requisitions
	DutyRoomID     string        // 保护站值班室 target for low/medium events
	DepartmentID   string        // 上级林草部门 target for high/critical events
	WorkerInterval time.Duration // background worker tick
}

// DefaultConfig returns production defaults.
func DefaultConfig() Config {
	return Config{
		StationChiefID: "chief-001",
		DutyRoomID:     "duty-room-001",
		DepartmentID:   "forestry-dept-001",
		WorkerInterval: 5 * time.Second,
	}
}

// Service is the application orchestration layer. It coordinates the general
// store and equipment store to execute business workflows: plan lifecycle,
// dispatch generation, requisition approval, and field-record intake.
type Service struct {
	cfg   Config
	store *store.Store
	equip *store.EquipmentStore
}

// New creates a Service backed by the given stores.
func New(cfg Config, s *store.Store, es *store.EquipmentStore) *Service {
	return &Service{cfg: cfg, store: s, equip: es}
}

// Store exposes the underlying store for worker and sync packages.
func (svc *Service) Store() *store.Store { return svc.store }

// EquipmentStore exposes the equipment store for worker and sync packages.
func (svc *Service) EquipmentStore() *store.EquipmentStore { return svc.equip }

// Config returns the service configuration.
func (svc *Service) Config() Config { return svc.cfg }

// ---------------------------------------------------------------------------
// Plan lifecycle
// ---------------------------------------------------------------------------

// SubmitPlan registers a patrol plan in the submitted state. The team and
// route must already exist in the store.
func (svc *Service) SubmitPlan(plan *domain.PatrolPlan) error {
	if plan.TeamID == "" || plan.RouteID == "" || plan.Date == "" {
		return fmt.Errorf("team_id, route_id and date are required")
	}
	if _, ok := svc.store.GetTeam(plan.TeamID); !ok {
		return fmt.Errorf("team %q not found", plan.TeamID)
	}
	if _, ok := svc.store.GetRoute(plan.RouteID); !ok {
		return fmt.Errorf("route %q not found", plan.RouteID)
	}
	plan.Status = domain.PlanStatusSubmitted
	plan.ID = domain.NewID("plan")
	svc.store.SavePlan(plan)
	return nil
}

// ApprovePlan moves a plan from submitted to approved.
func (svc *Service) ApprovePlan(planID string) error {
	return svc.store.UpdatePlanStatus(planID, domain.PlanStatusApproved)
}

// CancelPlan moves a plan to cancelled and releases any reserved equipment.
func (svc *Service) CancelPlan(planID string) error {
	p, ok := svc.store.GetPlan(planID)
	if !ok {
		return fmt.Errorf("plan %q not found", planID)
	}
	if p.Status.IsTerminal() {
		return fmt.Errorf("plan %q is already terminal (%s)", planID, p.Status)
	}
	// Release reserved equipment if a dispatch exists.
	if d, ok := svc.store.GetDispatchByPlan(planID); ok {
		svc.equip.ReleaseReservation(d.Equipment, p.Date)
	}
	return svc.store.UpdatePlanStatus(planID, domain.PlanStatusCancelled)
}

// StartPatrol transitions a dispatched plan to in_progress.
func (svc *Service) StartPatrol(planID string) error {
	return svc.store.UpdatePlanStatus(planID, domain.PlanStatusInProgress)
}

// CompletePatrol transitions an in_progress plan to completed and returns
// all assigned equipment to the warehouse.
func (svc *Service) CompletePatrol(planID string) error {
	p, ok := svc.store.GetPlan(planID)
	if !ok {
		return fmt.Errorf("plan %q not found", planID)
	}
	if err := svc.store.UpdatePlanStatus(planID, domain.PlanStatusCompleted); err != nil {
		return err
	}
	// Return issued equipment.
	if d, ok := svc.store.GetDispatchByPlan(planID); ok {
		if err := svc.equip.ReturnEquipment(d.Equipment, p.Date); err != nil {
			return fmt.Errorf("plan completed but equipment return failed: %w", err)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Requisition approval
// ---------------------------------------------------------------------------

// ApproveRequisition lets the station chief approve a pending requisition.
// After approval the equipment is issued to the team.
func (svc *Service) ApproveRequisition(reqID, approverID string) error {
	r, ok := svc.store.GetRequisition(reqID)
	if !ok {
		return fmt.Errorf("requisition %q not found", reqID)
	}
	if approverID != svc.cfg.StationChiefID {
		return fmt.Errorf("only the station chief (%s) may approve requisitions", svc.cfg.StationChiefID)
	}
	if !r.Status.CanTransitionTo(domain.RequisitionStatusApproved) {
		return fmt.Errorf("invalid requisition transition: %s -> %s",
			r.Status, domain.RequisitionStatusApproved)
	}
	// Hand out the physical equipment before committing the approval. The
	// approved state has no edge back to pending_approval, so a failure after
	// the commit could not be undone and would strand the slip.
	items := make([]domain.AssignedEquipment, len(r.Items))
	for i, it := range r.Items {
		items[i] = domain.AssignedEquipment{
			EquipmentID: it.EquipmentID,
			Type:        it.Type,
			Category:    it.Category,
		}
	}
	if err := svc.equip.IssueForTeam(items); err != nil {
		return fmt.Errorf("equipment issue failed: %w", err)
	}
	if err := svc.store.UpdateRequisitionStatus(reqID, domain.RequisitionStatusApproved, approverID); err != nil {
		return err
	}
	return svc.store.UpdateRequisitionStatus(reqID, domain.RequisitionStatusIssued, "")
}

// RejectRequisition lets the station chief reject a pending requisition.
func (svc *Service) RejectRequisition(reqID, approverID string) error {
	if approverID != svc.cfg.StationChiefID {
		return fmt.Errorf("only the station chief may reject requisitions")
	}
	return svc.store.UpdateRequisitionStatus(reqID, domain.RequisitionStatusRejected, approverID)
}

// ---------------------------------------------------------------------------
// Field record intake
// ---------------------------------------------------------------------------

// RecordCheckIn accepts a patrol check-in. Dedup is handled by the store.
func (svc *Service) RecordCheckIn(c *domain.CheckIn) (string, bool, error) {
	if _, ok := svc.store.GetPlan(c.PlanID); !ok {
		return "", false, fmt.Errorf("plan %q not found", c.PlanID)
	}
	if c.Timestamp.IsZero() {
		return "", false, fmt.Errorf("check_in: timestamp is required")
	}
	if c.ID == "" {
		c.ID = domain.NewID("ci")
	}
	id, inserted := svc.store.SaveCheckIn(c)
	return id, inserted, nil
}

// RecordWildlife accepts a wildlife sighting. Dedup is handled by the store.
func (svc *Service) RecordWildlife(w *domain.WildlifeRecord) (string, bool, error) {
	if _, ok := svc.store.GetPlan(w.PlanID); !ok {
		return "", false, fmt.Errorf("plan %q not found", w.PlanID)
	}
	if w.Timestamp.IsZero() {
		return "", false, fmt.Errorf("wildlife: timestamp is required")
	}
	if w.ID == "" {
		w.ID = domain.NewID("wl")
	}
	id, inserted := svc.store.SaveWildlife(w)
	return id, inserted, nil
}

// RecordDisturbance accepts a disturbance event, assigns a routing level
// based on severity, and stores it in the pending state for the background
// worker to route.
func (svc *Service) RecordDisturbance(e *domain.DisturbanceEvent) (string, bool, bool, error) {
	if _, ok := svc.store.GetPlan(e.PlanID); !ok {
		return "", false, false, fmt.Errorf("plan %q not found", e.PlanID)
	}
	if e.Timestamp.IsZero() {
		return "", false, false, fmt.Errorf("disturbance: timestamp is required")
	}
	if e.ID == "" {
		e.ID = domain.NewID("ev")
	}
	e.Level = domain.RouteLevelForSeverity(e.Severity)
	e.Status = domain.EventStatusPending
	id, inserted, merged := svc.store.SaveEvent(e)
	return id, inserted, merged, nil
}

// RoutePendingEvents routes all pending disturbance events to the appropriate
// destination based on severity. This is called by the background worker but
// can also be invoked manually.
func (svc *Service) RoutePendingEvents() (int, error) {
	pending := svc.store.ListPendingEvents()
	routed := 0
	for _, e := range pending {
		target := svc.cfg.DutyRoomID
		if e.Level == domain.RoutingDepartment {
			target = svc.cfg.DepartmentID
		}
		err := svc.store.UpdateEvent(e.ID, func(ev *domain.DisturbanceEvent) error {
			newStatus, terr := ev.Status.Transition(domain.EventStatusRouted)
			if terr != nil {
				return terr
			}
			ev.Status = newStatus
			ev.RoutedTo = target
			ev.RoutedAt = time.Now()
			return nil
		})
		if err != nil {
			return routed, err
		}
		routed++
	}
	return routed, nil
}

// ResolveEvent marks a routed event as resolved.
func (svc *Service) ResolveEvent(eventID string) error {
	return svc.store.UpdateEvent(eventID, func(e *domain.DisturbanceEvent) error {
		newStatus, err := e.Status.Transition(domain.EventStatusResolved)
		if err != nil {
			return err
		}
		e.Status = newStatus
		return nil
	})
}
