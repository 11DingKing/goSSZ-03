package store

import (
	"sync"
	"testing"

	"github.com/qilian/patrol-dispatch/internal/domain"
)

func seedEquipment(t *testing.T) *EquipmentStore {
	t.Helper()
	es := NewEquipmentStore()
	devices := []*domain.Equipment{
		{ID: "sp-1", Type: domain.EquipmentSatellitePhone, Category: domain.EquipmentCategoryEmergency, Status: domain.EquipmentStatusInStock},
		{ID: "sp-2", Type: domain.EquipmentSatellitePhone, Category: domain.EquipmentCategoryEmergency, Status: domain.EquipmentStatusInStock},
		{ID: "ic-1", Type: domain.EquipmentInfraredCamera, Category: domain.EquipmentCategoryEmergency, Status: domain.EquipmentStatusInStock},
		{ID: "ic-2", Type: domain.EquipmentInfraredCamera, Category: domain.EquipmentCategoryEmergency, Status: domain.EquipmentStatusInStock},
		{ID: "gps-1", Type: domain.EquipmentGPS, Category: domain.EquipmentCategoryRegular, Status: domain.EquipmentStatusInStock},
		{ID: "gps-2", Type: domain.EquipmentGPS, Category: domain.EquipmentCategoryRegular, Status: domain.EquipmentStatusInStock},
	}
	for _, d := range devices {
		es.AddDevice(d)
	}
	return es
}

func TestReserveForTeamSuccess(t *testing.T) {
	es := seedEquipment(t)
	needs := []domain.EquipmentNeed{
		{Type: domain.EquipmentSatellitePhone, Quantity: 1, Category: domain.EquipmentCategoryEmergency},
		{Type: domain.EquipmentGPS, Quantity: 1, Category: domain.EquipmentCategoryRegular},
	}
	assigned, err := es.ReserveForTeam("team-A", "2026-08-16", needs)
	if err != nil {
		t.Fatalf("reserve failed: %v", err)
	}
	if len(assigned) != 2 {
		t.Fatalf("expected 2 assigned, got %d", len(assigned))
	}
	// Verify occupancy lock for emergency sat phone.
	holder, ok := es.IsEmergencyOccupied(domain.EquipmentSatellitePhone, "2026-08-16")
	if !ok || holder != "team-A" {
		t.Errorf("occupancy holder = %s, want team-A", holder)
	}
}

func TestEmergencyOccupancyConflict(t *testing.T) {
	es := seedEquipment(t)
	needs := []domain.EquipmentNeed{
		{Type: domain.EquipmentSatellitePhone, Quantity: 1, Category: domain.EquipmentCategoryEmergency},
	}
	// Team A reserves a satellite phone.
	_, err := es.ReserveForTeam("team-A", "2026-08-16", needs)
	if err != nil {
		t.Fatalf("team A reserve failed: %v", err)
	}
	// Team B tries to reserve a satellite phone on the same day.
	_, err = es.ReserveForTeam("team-B", "2026-08-16", needs)
	if err == nil {
		t.Fatal("team B should not be able to reserve same emergency type on same day")
	}
	conflict, ok := err.(*EquipmentConflictError)
	if !ok {
		t.Fatalf("expected EquipmentConflictError, got %T", err)
	}
	if conflict.HeldBy != "team-A" {
		t.Errorf("held_by = %s, want team-A", conflict.HeldBy)
	}
	if conflict.Type != domain.EquipmentSatellitePhone {
		t.Errorf("type = %s, want satellite_phone", conflict.Type)
	}
}

func TestEmergencyOccupancyReleasedOnReturn(t *testing.T) {
	es := seedEquipment(t)
	needs := []domain.EquipmentNeed{
		{Type: domain.EquipmentInfraredCamera, Quantity: 1, Category: domain.EquipmentCategoryEmergency},
	}
	assigned, _ := es.ReserveForTeam("team-A", "2026-08-16", needs)

	// Issue and return.
	if err := es.IssueForTeam(assigned); err != nil {
		t.Fatalf("issue: %v", err)
	}
	if err := es.ReturnEquipment(assigned, "2026-08-16"); err != nil {
		t.Fatalf("return: %v", err)
	}

	// Now team B should be able to reserve.
	_, err := es.ReserveForTeam("team-B", "2026-08-16", needs)
	if err != nil {
		t.Fatalf("team B should reserve after return: %v", err)
	}
}

func TestReserveRollbackOnInsufficientStock(t *testing.T) {
	es := seedEquipment(t)
	needs := []domain.EquipmentNeed{
		{Type: domain.EquipmentGPS, Quantity: 1, Category: domain.EquipmentCategoryRegular},
		{Type: domain.EquipmentSatellitePhone, Quantity: 1, Category: domain.EquipmentCategoryEmergency},
		// Request more GPS than available (only 2 in stock, request 5).
		{Type: domain.EquipmentGPS, Quantity: 5, Category: domain.EquipmentCategoryRegular},
	}
	_, err := es.ReserveForTeam("team-A", "2026-08-16", needs)
	if err == nil {
		t.Fatal("should fail with insufficient stock")
	}
	// Verify rollback: the first GPS and sat phone should be back in stock,
	// and occupancy should be released.
	if es.AvailableStock(domain.EquipmentSatellitePhone) != 2 {
		t.Errorf("sat phones should be back in stock after rollback, got %d", es.AvailableStock(domain.EquipmentSatellitePhone))
	}
	if es.AvailableStock(domain.EquipmentGPS) != 2 {
		t.Errorf("GPS should be back in stock after rollback, got %d", es.AvailableStock(domain.EquipmentGPS))
	}
	_, occupied := es.IsEmergencyOccupied(domain.EquipmentSatellitePhone, "2026-08-16")
	if occupied {
		t.Error("occupancy should be released after rollback")
	}
}

func TestConcurrentReserveSameEmergency(t *testing.T) {
	es := seedEquipment(t)
	needs := []domain.EquipmentNeed{
		{Type: domain.EquipmentSatellitePhone, Quantity: 1, Category: domain.EquipmentCategoryEmergency},
	}
	var wg sync.WaitGroup
	successes := make(chan string, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			teamID := "team-" + string(rune('A'+n))
			_, err := es.ReserveForTeam(teamID, "2026-08-16", needs)
			if err == nil {
				successes <- teamID
			}
		}(i)
	}
	wg.Wait()
	close(successes)
	count := 0
	for range successes {
		count++
	}
	if count != 1 {
		t.Fatalf("exactly one team should succeed, got %d", count)
	}
}

func TestIssueForTeamWrongStatus(t *testing.T) {
	es := seedEquipment(t)
	needs := []domain.EquipmentNeed{
		{Type: domain.EquipmentGPS, Quantity: 1, Category: domain.EquipmentCategoryRegular},
	}
	assigned, _ := es.ReserveForTeam("team-A", "2026-08-16", needs)

	// Try to return without issuing first → should fail.
	err := es.ReturnEquipment(assigned, "2026-08-16")
	if err == nil {
		t.Fatal("return before issue should fail")
	}
}
