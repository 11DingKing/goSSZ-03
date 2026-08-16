package domain

import "fmt"

// ---------------------------------------------------------------------------
// Patrol plan state machine
//
//   draft -> submitted -> approved -> dispatched -> in_progress -> completed
//                         |
//                         +-> cancelled  (terminal)
//   Any non-terminal state may transition to cancelled.
// ---------------------------------------------------------------------------

// PlanStatus enumerates the lifecycle states of a patrol plan.
type PlanStatus string

const (
	PlanStatusDraft      PlanStatus = "draft"
	PlanStatusSubmitted  PlanStatus = "submitted"
	PlanStatusApproved   PlanStatus = "approved"
	PlanStatusDispatched PlanStatus = "dispatched"
	PlanStatusInProgress PlanStatus = "in_progress"
	PlanStatusCompleted  PlanStatus = "completed"
	PlanStatusCancelled  PlanStatus = "cancelled"
)

var planTransitions = map[PlanStatus][]PlanStatus{
	PlanStatusDraft:      {PlanStatusSubmitted, PlanStatusCancelled},
	PlanStatusSubmitted:  {PlanStatusApproved, PlanStatusCancelled},
	PlanStatusApproved:   {PlanStatusDispatched, PlanStatusCancelled},
	PlanStatusDispatched: {PlanStatusInProgress, PlanStatusCancelled},
	PlanStatusInProgress: {PlanStatusCompleted, PlanStatusCancelled},
	PlanStatusCompleted:  {},
	PlanStatusCancelled:  {},
}

// CanTransitionTo reports whether a plan may move from s to target.
func (s PlanStatus) CanTransitionTo(target PlanStatus) bool {
	for _, a := range planTransitions[s] {
		if a == target {
			return true
		}
	}
	return false
}

// Transition validates the move and returns the new status or an error.
func (s PlanStatus) Transition(target PlanStatus) (PlanStatus, error) {
	if !s.CanTransitionTo(target) {
		return s, fmt.Errorf("invalid plan transition: %s -> %s", s, target)
	}
	return target, nil
}

// IsTerminal reports whether no further transitions are possible.
func (s PlanStatus) IsTerminal() bool {
	return len(planTransitions[s]) == 0
}

// ---------------------------------------------------------------------------
// Requisition state machine
//
//   requested -> pending_approval -> approved -> issued -> returned
//                                -> rejected (terminal)
// ---------------------------------------------------------------------------

// RequisitionStatus enumerates the lifecycle states of a requisition slip.
type RequisitionStatus string

const (
	RequisitionStatusRequested       RequisitionStatus = "requested"
	RequisitionStatusPendingApproval RequisitionStatus = "pending_approval"
	RequisitionStatusApproved        RequisitionStatus = "approved"
	RequisitionStatusRejected        RequisitionStatus = "rejected"
	RequisitionStatusIssued          RequisitionStatus = "issued"
	RequisitionStatusReturned        RequisitionStatus = "returned"
)

var requisitionTransitions = map[RequisitionStatus][]RequisitionStatus{
	RequisitionStatusRequested:       {RequisitionStatusPendingApproval},
	RequisitionStatusPendingApproval: {RequisitionStatusApproved, RequisitionStatusRejected},
	RequisitionStatusApproved:        {RequisitionStatusIssued},
	RequisitionStatusRejected:        {},
	RequisitionStatusIssued:          {RequisitionStatusReturned},
	RequisitionStatusReturned:        {},
}

// CanTransitionTo reports whether a requisition may move from s to target.
func (s RequisitionStatus) CanTransitionTo(target RequisitionStatus) bool {
	for _, a := range requisitionTransitions[s] {
		if a == target {
			return true
		}
	}
	return false
}

// Transition validates the move and returns the new status or an error.
func (s RequisitionStatus) Transition(target RequisitionStatus) (RequisitionStatus, error) {
	if !s.CanTransitionTo(target) {
		return s, fmt.Errorf("invalid requisition transition: %s -> %s", s, target)
	}
	return target, nil
}

// ---------------------------------------------------------------------------
// Equipment state machine
//
//   in_stock -> reserved -> issued -> returned -> in_stock
//                       \-> in_stock  (release without issue)
// ---------------------------------------------------------------------------

// EquipmentStatus enumerates the lifecycle states of a device.
type EquipmentStatus string

const (
	EquipmentStatusInStock  EquipmentStatus = "in_stock"
	EquipmentStatusReserved EquipmentStatus = "reserved"
	EquipmentStatusIssued   EquipmentStatus = "issued"
	EquipmentStatusReturned EquipmentStatus = "returned"
)

var equipmentTransitions = map[EquipmentStatus][]EquipmentStatus{
	EquipmentStatusInStock:  {EquipmentStatusReserved},
	EquipmentStatusReserved: {EquipmentStatusIssued, EquipmentStatusInStock},
	EquipmentStatusIssued:   {EquipmentStatusReturned},
	EquipmentStatusReturned: {EquipmentStatusInStock},
}

// CanTransitionTo reports whether equipment may move from s to target.
func (s EquipmentStatus) CanTransitionTo(target EquipmentStatus) bool {
	for _, a := range equipmentTransitions[s] {
		if a == target {
			return true
		}
	}
	return false
}

// Transition validates the move and returns the new status or an error.
func (s EquipmentStatus) Transition(target EquipmentStatus) (EquipmentStatus, error) {
	if !s.CanTransitionTo(target) {
		return s, fmt.Errorf("invalid equipment transition: %s -> %s", s, target)
	}
	return target, nil
}

// ---------------------------------------------------------------------------
// Disturbance-event state machine
//
//   pending -> routed -> resolved
// ---------------------------------------------------------------------------

// CanTransitionTo reports whether an event may move from s to target.
func (s EventStatus) CanTransitionTo(target EventStatus) bool {
	switch s {
	case EventStatusPending:
		return target == EventStatusRouted
	case EventStatusRouted:
		return target == EventStatusResolved
	default:
		return false
	}
}

// Transition validates the move and returns the new status or an error.
func (s EventStatus) Transition(target EventStatus) (EventStatus, error) {
	if !s.CanTransitionTo(target) {
		return s, fmt.Errorf("invalid event transition: %s -> %s", s, target)
	}
	return target, nil
}
