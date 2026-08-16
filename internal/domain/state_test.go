package domain

import (
	"testing"
	"time"
)

func TestPlanStatusTransition(t *testing.T) {
	tests := []struct {
		name    string
		from    PlanStatus
		to      PlanStatus
		wantErr bool
	}{
		{"draft to submitted", PlanStatusDraft, PlanStatusSubmitted, false},
		{"submitted to approved", PlanStatusSubmitted, PlanStatusApproved, false},
		{"approved to dispatched", PlanStatusApproved, PlanStatusDispatched, false},
		{"dispatched to in_progress", PlanStatusDispatched, PlanStatusInProgress, false},
		{"in_progress to completed", PlanStatusInProgress, PlanStatusCompleted, false},
		{"draft to completed (invalid)", PlanStatusDraft, PlanStatusCompleted, true},
		{"completed to anything (invalid)", PlanStatusCompleted, PlanStatusSubmitted, true},
		{"draft to cancelled", PlanStatusDraft, PlanStatusCancelled, false},
		{"cancelled to draft (invalid)", PlanStatusCancelled, PlanStatusDraft, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.from.Transition(tt.to)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Transition(%s->%s): err=%v, wantErr=%v", tt.from, tt.to, err, tt.wantErr)
			}
			if !tt.wantErr && !tt.from.CanTransitionTo(tt.to) {
				t.Fatalf("CanTransitionTo(%s->%s) returned false for a valid edge", tt.from, tt.to)
			}
		})
	}
}

func TestPlanStatusTerminal(t *testing.T) {
	if !PlanStatusCompleted.IsTerminal() {
		t.Error("completed should be terminal")
	}
	if !PlanStatusCancelled.IsTerminal() {
		t.Error("cancelled should be terminal")
	}
	if PlanStatusInProgress.IsTerminal() {
		t.Error("in_progress should not be terminal")
	}
}

func TestRequisitionStatusTransition(t *testing.T) {
	tests := []struct {
		name    string
		from    RequisitionStatus
		to      RequisitionStatus
		wantErr bool
	}{
		{"requested to pending", RequisitionStatusRequested, RequisitionStatusPendingApproval, false},
		{"pending to approved", RequisitionStatusPendingApproval, RequisitionStatusApproved, false},
		{"pending to rejected", RequisitionStatusPendingApproval, RequisitionStatusRejected, false},
		{"approved to issued", RequisitionStatusApproved, RequisitionStatusIssued, false},
		{"issued to returned", RequisitionStatusIssued, RequisitionStatusReturned, false},
		{"requested to approved (invalid)", RequisitionStatusRequested, RequisitionStatusApproved, true},
		{"rejected to approved (invalid)", RequisitionStatusRejected, RequisitionStatusApproved, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.from.Transition(tt.to)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Transition(%s->%s): err=%v, wantErr=%v", tt.from, tt.to, err, tt.wantErr)
			}
		})
	}
}

func TestEquipmentStatusTransition(t *testing.T) {
	tests := []struct {
		name    string
		from    EquipmentStatus
		to      EquipmentStatus
		wantErr bool
	}{
		{"in_stock to reserved", EquipmentStatusInStock, EquipmentStatusReserved, false},
		{"reserved to issued", EquipmentStatusReserved, EquipmentStatusIssued, false},
		{"reserved to in_stock (release)", EquipmentStatusReserved, EquipmentStatusInStock, false},
		{"issued to returned", EquipmentStatusIssued, EquipmentStatusReturned, false},
		{"returned to in_stock", EquipmentStatusReturned, EquipmentStatusInStock, false},
		{"in_stock to issued (invalid)", EquipmentStatusInStock, EquipmentStatusIssued, true},
		{"issued to reserved (invalid)", EquipmentStatusIssued, EquipmentStatusReserved, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.from.Transition(tt.to)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Transition(%s->%s): err=%v, wantErr=%v", tt.from, tt.to, err, tt.wantErr)
			}
		})
	}
}

func TestRouteLevelForSeverity(t *testing.T) {
	tests := []struct {
		severity EventSeverity
		want     RoutingLevel
	}{
		{SeverityLow, RoutingStation},
		{SeverityMedium, RoutingStation},
		{SeverityHigh, RoutingDepartment},
		{SeverityCritical, RoutingDepartment},
	}
	for _, tt := range tests {
		got := RouteLevelForSeverity(tt.severity)
		if got != tt.want {
			t.Errorf("RouteLevelForSeverity(%s) = %s, want %s", tt.severity, got, tt.want)
		}
	}
}

func TestEventStatusTransition(t *testing.T) {
	_, err := EventStatusPending.Transition(EventStatusRouted)
	if err != nil {
		t.Fatalf("pending->routed should succeed: %v", err)
	}
	_, err = EventStatusRouted.Transition(EventStatusResolved)
	if err != nil {
		t.Fatalf("routed->resolved should succeed: %v", err)
	}
	_, err = EventStatusPending.Transition(EventStatusResolved)
	if err == nil {
		t.Fatal("pending->resolved should fail (must route first)")
	}
}

func TestDedupKeyDeterministic(t *testing.T) {
	ts := time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)
	k1 := DedupKey("DEV-001", "officer-001", ts)
	k2 := DedupKey("DEV-001", "officer-001", ts)
	k3 := DedupKey("DEV-002", "officer-001", ts)
	if k1 != k2 {
		t.Error("same inputs must produce same key")
	}
	if k1 == k3 {
		t.Error("different device must produce different key")
	}
}
