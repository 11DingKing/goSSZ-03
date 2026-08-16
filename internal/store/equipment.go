package store

import (
	"fmt"
	"sync"
	"time"

	"github.com/qilian/patrol-dispatch/internal/domain"
)

// EquipmentConflictError is returned when an emergency device is already
// occupied by another team on the requested date. Callers can surface the
// details to the dispatcher so they can reassign the plan.
type EquipmentConflictError struct {
	Type    domain.EquipmentType
	Date    string
	HeldBy  string
	Message string
}

func (e *EquipmentConflictError) Error() string {
	return fmt.Sprintf("emergency equipment %s on %s is held by team %s: %s",
		e.Type, e.Date, e.HeldBy, e.Message)
}

// EquipmentStore manages the physical device inventory and enforces the
// per-day single-team occupancy rule for emergency gear (satellite phones,
// infrared cameras, etc.).
type EquipmentStore struct {
	mu sync.Mutex

	// devices keyed by equipment ID
	devices map[string]*domain.Equipment

	// daily occupancy for emergency types: key "type|date" -> teamID.
	// This is the concurrency boundary: only one team may hold a given
	// emergency device type on a given calendar day.
	occupancy map[string]string
}

// NewEquipmentStore returns an empty equipment store.
func NewEquipmentStore() *EquipmentStore {
	return &EquipmentStore{
		devices:   make(map[string]*domain.Equipment),
		occupancy: make(map[string]string),
	}
}

// AddDevice registers a physical device in the warehouse.
func (es *EquipmentStore) AddDevice(d *domain.Equipment) {
	es.mu.Lock()
	defer es.mu.Unlock()
	es.devices[d.ID] = d
}

// GetDevice returns a device by ID.
func (es *EquipmentStore) GetDevice(id string) (*domain.Equipment, bool) {
	es.mu.Lock()
	defer es.mu.Unlock()
	d, ok := es.devices[id]
	return d, ok
}

// ListDevices returns all devices, optionally filtered by type.
func (es *EquipmentStore) ListDevices(t domain.EquipmentType) []*domain.Equipment {
	es.mu.Lock()
	defer es.mu.Unlock()
	var result []*domain.Equipment
	for _, d := range es.devices {
		if t == "" || d.Type == t {
			result = append(result, d)
		}
	}
	return result
}

// AvailableStock returns the count of in-stock devices for a type.
func (es *EquipmentStore) AvailableStock(t domain.EquipmentType) int {
	es.mu.Lock()
	defer es.mu.Unlock()
	count := 0
	for _, d := range es.devices {
		if d.Type == t && d.Status == domain.EquipmentStatusInStock {
			count++
		}
	}
	return count
}

// occupancyKey builds the map key for the daily-occupancy lock.
func occupancyKey(t domain.EquipmentType, date string) string {
	return string(t) + "|" + date
}

// ReserveForTeam attempts to reserve a set of equipment items for a team on a
// given date. For emergency items it checks and acquires the daily occupancy
// lock atomically. On any conflict the entire reservation is rolled back and
// an *EquipmentConflictError is returned so the dispatcher can reassign.
func (es *EquipmentStore) ReserveForTeam(teamID, date string, needs []domain.EquipmentNeed) ([]domain.AssignedEquipment, error) {
	es.mu.Lock()
	defer es.mu.Unlock()

	// Each reservation gets its own slice: the result is handed to the caller
	// and stored in a dispatch order, so it must not share storage with any
	// other reservation.
	assigned := make([]domain.AssignedEquipment, 0, len(needs))
	acquiredOccupancy := make(map[string]domain.EquipmentType) // rollback tracker

	for _, need := range needs {
		// Emergency items: enforce single-team-per-day occupancy.
		if need.Category.IsEmergency() {
			ocKey := occupancyKey(need.Type, date)
			if holder, ok := es.occupancy[ocKey]; ok && holder != teamID {
				return nil, &EquipmentConflictError{
					Type:    need.Type,
					Date:    date,
					HeldBy:  holder,
					Message: "emergency device already occupied; reassign or use alternate type",
				}
			}
			// Acquire or confirm occupancy.
			es.occupancy[ocKey] = teamID
			acquiredOccupancy[ocKey] = need.Type
		}

		// Find available in-stock devices of the required type.
		found := 0
		for _, d := range es.devices {
			if d.Type == need.Type && d.Status == domain.EquipmentStatusInStock {
				d.Status = domain.EquipmentStatusReserved
				assigned = append(assigned, domain.AssignedEquipment{
					EquipmentID: d.ID,
					Type:        d.Type,
					Category:    need.Category,
				})
				found++
				if found >= need.Quantity {
					break
				}
			}
		}
		if found < need.Quantity {
			// Rollback: release reservations and occupancy locks.
			es.rollbackReservation(assigned, acquiredOccupancy)
			return nil, fmt.Errorf("insufficient stock for %s: need %d, found %d",
				need.Type, need.Quantity, found)
		}
	}

	return assigned, nil
}

// rollbackReservation releases device reservations and occupancy locks acquired
// during a partially-failed ReserveForTeam call. Caller must hold es.mu.
func (es *EquipmentStore) rollbackReservation(assigned []domain.AssignedEquipment, acquired map[string]domain.EquipmentType) {
	for _, a := range assigned {
		if d, ok := es.devices[a.EquipmentID]; ok {
			d.Status = domain.EquipmentStatusInStock
		}
	}
	for ocKey := range acquired {
		delete(es.occupancy, ocKey)
	}
}

// IssueForTeam transitions reserved devices to issued status and is called
// after the station chief approves the requisition.
func (es *EquipmentStore) IssueForTeam(assigned []domain.AssignedEquipment) error {
	es.mu.Lock()
	defer es.mu.Unlock()
	for _, a := range assigned {
		d, ok := es.devices[a.EquipmentID]
		if !ok {
			return fmt.Errorf("device %q not found during issue", a.EquipmentID)
		}
		if d.Status != domain.EquipmentStatusReserved {
			return fmt.Errorf("device %q is %s, expected reserved", a.EquipmentID, d.Status)
		}
		d.Status = domain.EquipmentStatusIssued
	}
	return nil
}

// ReturnEquipment transitions issued devices back to in-stock and releases
// the daily occupancy lock so the gear is available for the next patrol.
func (es *EquipmentStore) ReturnEquipment(assigned []domain.AssignedEquipment, date string) error {
	es.mu.Lock()
	defer es.mu.Unlock()
	for _, a := range assigned {
		d, ok := es.devices[a.EquipmentID]
		if !ok {
			return fmt.Errorf("device %q not found during return", a.EquipmentID)
		}
		if d.Status != domain.EquipmentStatusIssued {
			return fmt.Errorf("device %q is %s, expected issued", a.EquipmentID, d.Status)
		}
		d.Status = domain.EquipmentStatusReturned
		// Release occupancy for emergency devices.
		if a.Category.IsEmergency() {
			ocKey := occupancyKey(a.Type, date)
			delete(es.occupancy, ocKey)
		}
	}
	// Promote returned -> in_stock on a separate pass so the transition
	// state machine is respected (returned -> in_stock is a valid edge).
	for _, a := range assigned {
		if d, ok := es.devices[a.EquipmentID]; ok {
			d.Status = domain.EquipmentStatusInStock
		}
	}
	return nil
}

// ReleaseReservation cancels a reservation without issuing, returning devices
// to stock and freeing occupancy. Used when a plan is cancelled before issue.
func (es *EquipmentStore) ReleaseReservation(assigned []domain.AssignedEquipment, date string) {
	es.mu.Lock()
	defer es.mu.Unlock()
	for _, a := range assigned {
		if d, ok := es.devices[a.EquipmentID]; ok {
			if d.Status == domain.EquipmentStatusReserved {
				d.Status = domain.EquipmentStatusInStock
			}
		}
		if a.Category.IsEmergency() {
			ocKey := occupancyKey(a.Type, date)
			delete(es.occupancy, ocKey)
		}
	}
}

// IsEmergencyOccupied reports whether an emergency type is held on a date and
// by whom. Used by tests and the dispatcher advisory endpoint.
func (es *EquipmentStore) IsEmergencyOccupied(t domain.EquipmentType, date string) (string, bool) {
	es.mu.Lock()
	defer es.mu.Unlock()
	holder, ok := es.occupancy[occupancyKey(t, date)]
	return holder, ok
}

// DeviceCount returns the total number of registered devices.
func (es *EquipmentStore) DeviceCount() int {
	es.mu.Lock()
	defer es.mu.Unlock()
	return len(es.devices)
}

// now returns the current time; replaced in tests for determinism.
var now = time.Now
