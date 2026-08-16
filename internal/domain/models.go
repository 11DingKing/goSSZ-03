package domain

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// NewID generates a prefixed random hex identifier suitable for traceable records.
func NewID(prefix string) string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return prefix + "_" + hex.EncodeToString(b)
}

// ---------------------------------------------------------------------------
// Equipment domain
// ---------------------------------------------------------------------------

// EquipmentCategory distinguishes routine warehouse stock from emergency gear
// that is subject to per-day single-team occupancy.
type EquipmentCategory string

const (
	EquipmentCategoryRegular   EquipmentCategory = "regular"
	EquipmentCategoryEmergency EquipmentCategory = "emergency"
)

// IsEmergency reports whether the category requires daily occupancy locking.
func (c EquipmentCategory) IsEmergency() bool {
	return c == EquipmentCategoryEmergency
}

// EquipmentType identifies a class of field gear.
type EquipmentType string

const (
	EquipmentInfraredCamera EquipmentType = "infrared_camera"
	EquipmentSatellitePhone EquipmentType = "satellite_phone"
	EquipmentGPS            EquipmentType = "gps_tracker"
	EquipmentBinoculars     EquipmentType = "binoculars"
	EquipmentRadio          EquipmentType = "radio"
)

// Equipment is a single physical device held in the protection-station warehouse.
type Equipment struct {
	ID       string            `json:"id"`
	Type     EquipmentType     `json:"type"`
	Category EquipmentCategory `json:"category"`
	Status   EquipmentStatus   `json:"status"`
}

// EquipmentNeed describes what a patrol team requests for a given date.
type EquipmentNeed struct {
	Type     EquipmentType     `json:"type"`
	Quantity int               `json:"quantity"`
	Category EquipmentCategory `json:"category"`
}

// AssignedEquipment links a concrete device to a dispatch order.
type AssignedEquipment struct {
	EquipmentID string            `json:"equipment_id"`
	Type        EquipmentType     `json:"type"`
	Category    EquipmentCategory `json:"category"`
}

// ---------------------------------------------------------------------------
// Patrol domain
// ---------------------------------------------------------------------------

// Waypoint is a geographic coordinate on a patrol route.
type Waypoint struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

// PatrolOfficer is an individual ranger carrying a terminal device.
type PatrolOfficer struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	DeviceID string `json:"device_id"`
}

// PatrolTeam is a group of officers that patrols together.
type PatrolTeam struct {
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Officers []PatrolOfficer `json:"officers"`
}

// PatrolRoute defines a path within the station's jurisdictional boundary.
type PatrolRoute struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Waypoints []Waypoint `json:"waypoints"`
	Boundary  string     `json:"boundary"`
}

// PatrolPlan is a daily plan submitted by a team before departure.
type PatrolPlan struct {
	ID             string          `json:"id"`
	TeamID         string          `json:"team_id"`
	Date           string          `json:"date"`
	RouteID        string          `json:"route_id"`
	EquipmentNeeds []EquipmentNeed `json:"equipment_needs"`
	Status         PlanStatus      `json:"status"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

// DispatchOrder is auto-generated from an approved plan, route, and inventory.
type DispatchOrder struct {
	ID        string              `json:"id"`
	PlanID    string              `json:"plan_id"`
	TeamID    string              `json:"team_id"`
	RouteID   string              `json:"route_id"`
	Equipment []AssignedEquipment `json:"equipment"`
	CreatedAt time.Time           `json:"created_at"`
}

// Requisition is a material requisition slip awaiting station-chief approval.
type Requisition struct {
	ID         string            `json:"id"`
	PlanID     string            `json:"plan_id"`
	TeamID     string            `json:"team_id"`
	Items      []RequisitionItem `json:"items"`
	Status     RequisitionStatus `json:"status"`
	ApprovedBy string            `json:"approved_by,omitempty"`
	CreatedAt  time.Time         `json:"created_at"`
	ApprovedAt time.Time         `json:"approved_at,omitempty"`
}

// RequisitionItem is a line in a requisition slip.
type RequisitionItem struct {
	EquipmentID string            `json:"equipment_id"`
	Type        EquipmentType     `json:"type"`
	Category    EquipmentCategory `json:"category"`
}

// ---------------------------------------------------------------------------
// Field observation domain
// ---------------------------------------------------------------------------

// CheckIn records an officer's position at a point in time along a route.
type CheckIn struct {
	ID         string    `json:"id"`
	OfficerID  string    `json:"officer_id"`
	DeviceID   string    `json:"device_id"`
	PlanID     string    `json:"plan_id"`
	Timestamp  time.Time `json:"timestamp"`
	Location   Waypoint  `json:"location"`
	RouteIndex int       `json:"route_index"`
}

// WildlifeRecord captures a wildlife sighting with optional imagery.
type WildlifeRecord struct {
	ID        string    `json:"id"`
	OfficerID string    `json:"officer_id"`
	DeviceID  string    `json:"device_id"`
	PlanID    string    `json:"plan_id"`
	Timestamp time.Time `json:"timestamp"`
	Species   string    `json:"species"`
	Location  Waypoint  `json:"location"`
	ImageRef  string    `json:"image_ref"`
}

// DisturbanceType categorises human interference observed in the field.
type DisturbanceType string

const (
	DisturbancePoaching DisturbanceType = "poaching"
	DisturbanceLogging  DisturbanceType = "logging"
	DisturbanceGrazing  DisturbanceType = "grazing"
	DisturbanceMining   DisturbanceType = "mining"
	DisturbanceFire     DisturbanceType = "fire"
	DisturbanceOther    DisturbanceType = "other"
)

// EventSeverity drives the routing decision for disturbance events.
type EventSeverity string

const (
	SeverityLow      EventSeverity = "low"
	SeverityMedium   EventSeverity = "medium"
	SeverityHigh     EventSeverity = "high"
	SeverityCritical EventSeverity = "critical"
)

// RoutingLevel determines whether an event goes to the station or the department.
type RoutingLevel string

const (
	RoutingStation    RoutingLevel = "station"
	RoutingDepartment RoutingLevel = "department"
)

// EventStatus tracks the lifecycle of a disturbance event after creation.
type EventStatus string

const (
	EventStatusPending  EventStatus = "pending"
	EventStatusRouted   EventStatus = "routed"
	EventStatusResolved EventStatus = "resolved"
)

// DisturbanceEvent records a human-disturbance incident observed during patrol.
type DisturbanceEvent struct {
	ID        string          `json:"id"`
	OfficerID string          `json:"officer_id"`
	DeviceID  string          `json:"device_id"`
	PlanID    string          `json:"plan_id"`
	Timestamp time.Time       `json:"timestamp"`
	Type      DisturbanceType `json:"type"`
	Severity  EventSeverity   `json:"severity"`
	Level     RoutingLevel    `json:"level"`
	Status    EventStatus     `json:"status"`
	Location  Waypoint        `json:"location"`
	RoutedTo  string          `json:"routed_to,omitempty"`
	RoutedAt  time.Time       `json:"routed_at,omitempty"`
}

// RouteLevelForSeverity maps an event severity to its routing destination.
// Low and medium events are handled by the station duty room; high and
// critical events escalate to the superior forestry and grassland department.
func RouteLevelForSeverity(s EventSeverity) RoutingLevel {
	switch s {
	case SeverityHigh, SeverityCritical:
		return RoutingDepartment
	default:
		return RoutingStation
	}
}

// DedupKey builds the canonical deduplication key for a field record. The
// combination of device ID, officer ID and Unix-nano timestamp guarantees
// that a trajectory point or event submitted multiple times (e.g. during an
// offline re-sync) collapses to a single traceable record.
func DedupKey(deviceID, officerID string, ts time.Time) string {
	return deviceID + "|" + officerID + "|" + ts.UTC().Format(time.RFC3339Nano)
}
