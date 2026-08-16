package service

import (
	"testing"

	"github.com/qilian/patrol-dispatch/internal/domain"
)

// TestEachDispatchOrderKeepsItsOwnEquipmentList checks a documented property of
// a dispatch order: the equipment list it was created with describes the gear
// that team must collect, and generating a later dispatch order for a different
// plan must not change it.
func TestEachDispatchOrderKeepsItsOwnEquipmentList(t *testing.T) {
	svc, s, _ := newTestService(t)
	s.SaveTeam(&domain.PatrolTeam{ID: "team-2", Name: "二分队"})

	needs := []domain.EquipmentNeed{
		{Type: domain.EquipmentGPS, Quantity: 1, Category: domain.EquipmentCategoryRegular},
	}

	planA := &domain.PatrolPlan{TeamID: "team-1", RouteID: "route-1", Date: "2026-08-16", EquipmentNeeds: needs}
	if err := svc.SubmitPlan(planA); err != nil {
		t.Fatalf("submit plan A: %v", err)
	}
	if err := svc.ApprovePlan(planA.ID); err != nil {
		t.Fatalf("approve plan A: %v", err)
	}
	resultA, err := svc.GenerateDispatch(planA.ID)
	if err != nil {
		t.Fatalf("dispatch plan A: %v", err)
	}
	if len(resultA.Dispatch.Equipment) != 1 {
		t.Fatalf("dispatch A equipment count = %d, want 1", len(resultA.Dispatch.Equipment))
	}
	deviceForA := resultA.Dispatch.Equipment[0].EquipmentID

	planB := &domain.PatrolPlan{TeamID: "team-2", RouteID: "route-1", Date: "2026-08-16", EquipmentNeeds: needs}
	if err := svc.SubmitPlan(planB); err != nil {
		t.Fatalf("submit plan B: %v", err)
	}
	if err := svc.ApprovePlan(planB.ID); err != nil {
		t.Fatalf("approve plan B: %v", err)
	}
	resultB, err := svc.GenerateDispatch(planB.ID)
	if err != nil {
		t.Fatalf("dispatch plan B: %v", err)
	}
	if len(resultB.Dispatch.Equipment) != 1 {
		t.Fatalf("dispatch B equipment count = %d, want 1", len(resultB.Dispatch.Equipment))
	}
	deviceForB := resultB.Dispatch.Equipment[0].EquipmentID
	if deviceForB == deviceForA {
		t.Fatalf("both dispatch orders were given device %s; the two teams must get different devices for this check to be meaningful", deviceForA)
	}

	storedA, ok := svc.GetDispatch(resultA.Dispatch.ID)
	if !ok {
		t.Fatalf("dispatch order %s not found after dispatching plan B", resultA.Dispatch.ID)
	}
	if len(storedA.Equipment) != 1 || storedA.Equipment[0].EquipmentID != deviceForA {
		t.Errorf("dispatch order for plan A now lists %v, want a single entry for %s",
			storedA.Equipment, deviceForA)
	}
	if got := resultA.Dispatch.Equipment[0].EquipmentID; got != deviceForA {
		t.Errorf("dispatch result for plan A now reports device %s, want %s", got, deviceForA)
	}
}
