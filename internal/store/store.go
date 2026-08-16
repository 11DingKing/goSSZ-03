package store

import (
	"fmt"
	"sync"
	"time"

	"github.com/qilian/patrol-dispatch/internal/domain"
)

// Store is the central thread-safe in-memory persistence layer for all
// patrol-domain aggregates. It is the single source of truth during a
// server's lifetime and replaces an external database so the service stays
// self-contained.
type Store struct {
	mu sync.RWMutex

	teams        map[string]*domain.PatrolTeam
	routes       map[string]*domain.PatrolRoute
	plans        map[string]*domain.PatrolPlan
	dispatches   map[string]*domain.DispatchOrder
	requisitions map[string]*domain.Requisition
	checkIns     map[string]*domain.CheckIn
	wildlife     map[string]*domain.WildlifeRecord
	events       map[string]*domain.DisturbanceEvent

	// Dedup indices: dedup key -> record ID. These guarantee that a record
	// submitted more than once (e.g. during offline re-sync) resolves to a
	// single traceable entry.
	checkInDedup  map[string]string
	wildlifeDedup map[string]string
	eventDedup    map[string]string
}

// New returns an empty Store ready to accept data.
func New() *Store {
	return &Store{
		teams:         make(map[string]*domain.PatrolTeam),
		routes:        make(map[string]*domain.PatrolRoute),
		plans:         make(map[string]*domain.PatrolPlan),
		dispatches:    make(map[string]*domain.DispatchOrder),
		requisitions:  make(map[string]*domain.Requisition),
		checkIns:      make(map[string]*domain.CheckIn),
		wildlife:      make(map[string]*domain.WildlifeRecord),
		events:        make(map[string]*domain.DisturbanceEvent),
		checkInDedup:  make(map[string]string),
		wildlifeDedup: make(map[string]string),
		eventDedup:    make(map[string]string),
	}
}

// ---------------------------------------------------------------------------
// Teams & routes
// ---------------------------------------------------------------------------

// SaveTeam inserts or replaces a team.
func (s *Store) SaveTeam(t *domain.PatrolTeam) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.teams[t.ID] = t
}

// GetTeam returns a team by ID.
func (s *Store) GetTeam(id string) (*domain.PatrolTeam, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.teams[id]
	return t, ok
}

// SaveRoute inserts or replaces a route.
func (s *Store) SaveRoute(r *domain.PatrolRoute) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.routes[r.ID] = r
}

// GetRoute returns a route by ID.
func (s *Store) GetRoute(id string) (*domain.PatrolRoute, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.routes[id]
	return r, ok
}

// ---------------------------------------------------------------------------
// Plans
// ---------------------------------------------------------------------------

// SavePlan inserts or replaces a plan.
func (s *Store) SavePlan(p *domain.PatrolPlan) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	p.UpdatedAt = now
	s.plans[p.ID] = p
}

// GetPlan returns a plan by ID.
func (s *Store) GetPlan(id string) (*domain.PatrolPlan, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.plans[id]
	return p, ok
}

// UpdatePlanStatus applies a status change under the store lock, returning
// the old status so callers can decide whether to roll back on failure.
func (s *Store) UpdatePlanStatus(id string, target domain.PlanStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.plans[id]
	if !ok {
		return fmt.Errorf("plan %q not found", id)
	}
	newStatus, err := p.Status.Transition(target)
	if err != nil {
		return err
	}
	p.Status = newStatus
	p.UpdatedAt = time.Now()
	return nil
}

// ListPlans returns all plans, optionally filtered by status. Pass an empty
// string to return every plan regardless of state.
func (s *Store) ListPlans(status domain.PlanStatus) []*domain.PatrolPlan {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*domain.PatrolPlan, 0, len(s.plans))
	for _, p := range s.plans {
		if status == "" || p.Status == status {
			result = append(result, p)
		}
	}
	return result
}

// ListPlansByTeam returns all plans for a given team.
func (s *Store) ListPlansByTeam(teamID string) []*domain.PatrolPlan {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []*domain.PatrolPlan
	for _, p := range s.plans {
		if p.TeamID == teamID {
			result = append(result, p)
		}
	}
	return result
}

// ---------------------------------------------------------------------------
// Dispatches & requisitions
// ---------------------------------------------------------------------------

// SaveDispatch inserts or replaces a dispatch order.
func (s *Store) SaveDispatch(d *domain.DispatchOrder) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d.CreatedAt.IsZero() {
		d.CreatedAt = time.Now()
	}
	s.dispatches[d.ID] = d
}

// GetDispatch returns a dispatch order by ID.
func (s *Store) GetDispatch(id string) (*domain.DispatchOrder, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d, ok := s.dispatches[id]
	return d, ok
}

// GetDispatchByPlan returns the dispatch order for a plan, if any.
func (s *Store) GetDispatchByPlan(planID string) (*domain.DispatchOrder, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, d := range s.dispatches {
		if d.PlanID == planID {
			return d, true
		}
	}
	return nil, false
}

// SaveRequisition inserts or replaces a requisition.
func (s *Store) SaveRequisition(r *domain.Requisition) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now()
	}
	s.requisitions[r.ID] = r
}

// GetRequisition returns a requisition by ID.
func (s *Store) GetRequisition(id string) (*domain.Requisition, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.requisitions[id]
	return r, ok
}

// GetRequisitionByPlan returns the requisition for a plan, if any.
func (s *Store) GetRequisitionByPlan(planID string) (*domain.Requisition, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, r := range s.requisitions {
		if r.PlanID == planID {
			return r, true
		}
	}
	return nil, false
}

// UpdateRequisitionStatus applies a status change to a requisition.
func (s *Store) UpdateRequisitionStatus(id string, target domain.RequisitionStatus, approverID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.requisitions[id]
	if !ok {
		return fmt.Errorf("requisition %q not found", id)
	}
	newStatus, err := r.Status.Transition(target)
	if err != nil {
		return err
	}
	r.Status = newStatus
	if target == domain.RequisitionStatusApproved {
		r.ApprovedBy = approverID
		r.ApprovedAt = time.Now()
	}
	return nil
}

// ---------------------------------------------------------------------------
// Field records with deduplication
// ---------------------------------------------------------------------------

// SaveCheckIn stores a check-in unless an identical record (same dedup key)
// already exists. Returns the stored ID and whether it was a new insert.
func (s *Store) SaveCheckIn(c *domain.CheckIn) (id string, inserted bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := domain.DedupKey(c.DeviceID, c.OfficerID, c.Timestamp)
	if existing, ok := s.checkInDedup[key]; ok {
		return existing, false
	}
	if c.ID == "" {
		c.ID = domain.NewID("ci")
	}
	s.checkIns[c.ID] = c
	s.checkInDedup[key] = c.ID
	return c.ID, true
}

// GetCheckIn returns a check-in by ID.
func (s *Store) GetCheckIn(id string) (*domain.CheckIn, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.checkIns[id]
	return c, ok
}

// ListCheckInsByPlan returns all check-ins for a plan ordered by timestamp.
func (s *Store) ListCheckInsByPlan(planID string) []*domain.CheckIn {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []*domain.CheckIn
	for _, c := range s.checkIns {
		if c.PlanID == planID {
			result = append(result, c)
		}
	}
	// insertion sort by timestamp to keep deterministic output
	for i := 1; i < len(result); i++ {
		for j := i; j > 0 && result[j].Timestamp.Before(result[j-1].Timestamp); j-- {
			result[j], result[j-1] = result[j-1], result[j]
		}
	}
	return result
}

// FindCheckInByKey looks up a check-in by its dedup key (used by sync).
func (s *Store) FindCheckInByKey(key string) (*domain.CheckIn, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	id, ok := s.checkInDedup[key]
	if !ok {
		return nil, false
	}
	c, ok := s.checkIns[id]
	return c, ok
}

// SaveWildlife stores a wildlife record with dedup, returning the stored ID
// and whether it was a new insert.
func (s *Store) SaveWildlife(w *domain.WildlifeRecord) (id string, inserted bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := domain.DedupKey(w.DeviceID, w.OfficerID, w.Timestamp)
	if existing, ok := s.wildlifeDedup[key]; ok {
		return existing, false
	}
	if w.ID == "" {
		w.ID = domain.NewID("wl")
	}
	s.wildlife[w.ID] = w
	s.wildlifeDedup[key] = w.ID
	return w.ID, true
}

// ListWildlifeByPlan returns all wildlife records for a plan.
func (s *Store) ListWildlifeByPlan(planID string) []*domain.WildlifeRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []*domain.WildlifeRecord
	for _, w := range s.wildlife {
		if w.PlanID == planID {
			result = append(result, w)
		}
	}
	return result
}

// FindWildlifeByKey looks up a wildlife record by its dedup key.
func (s *Store) FindWildlifeByKey(key string) (*domain.WildlifeRecord, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	id, ok := s.wildlifeDedup[key]
	if !ok {
		return nil, false
	}
	w, ok := s.wildlife[id]
	return w, ok
}

// SaveEvent stores a disturbance event with dedup. When an event with the
// same dedup key already exists, the caller may request a merge: if the
// incoming record is more severe it replaces the stored one so the
// escalation is not lost. Returns the stored ID, whether it was new, and
// whether a merge occurred.
func (s *Store) SaveEvent(e *domain.DisturbanceEvent) (id string, inserted bool, merged bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := domain.DedupKey(e.DeviceID, e.OfficerID, e.Timestamp)
	if existingID, ok := s.eventDedup[key]; ok {
		existing := s.events[existingID]
		if severityRank(e.Severity) > severityRank(existing.Severity) {
			e.ID = existingID
			s.events[existingID] = e
			return existingID, false, true
		}
		return existingID, false, false
	}
	if e.ID == "" {
		e.ID = domain.NewID("ev")
	}
	s.events[e.ID] = e
	s.eventDedup[key] = e.ID
	return e.ID, true, false
}

// GetEvent returns a disturbance event by ID.
func (s *Store) GetEvent(id string) (*domain.DisturbanceEvent, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.events[id]
	return e, ok
}

// ListEventsByPlan returns all events for a plan.
func (s *Store) ListEventsByPlan(planID string) []*domain.DisturbanceEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []*domain.DisturbanceEvent
	for _, e := range s.events {
		if e.PlanID == planID {
			result = append(result, e)
		}
	}
	return result
}

// ListPendingEvents returns all events still awaiting routing.
func (s *Store) ListPendingEvents() []*domain.DisturbanceEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []*domain.DisturbanceEvent
	for _, e := range s.events {
		if e.Status == domain.EventStatusPending {
			result = append(result, e)
		}
	}
	return result
}

// UpdateEvent applies routing or resolution to an event under the lock.
func (s *Store) UpdateEvent(id string, fn func(*domain.DisturbanceEvent) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.events[id]
	if !ok {
		return fmt.Errorf("event %q not found", id)
	}
	return fn(e)
}

// severityRank provides an ordering for event-merge conflict resolution.
func severityRank(s domain.EventSeverity) int {
	switch s {
	case domain.SeverityCritical:
		return 4
	case domain.SeverityHigh:
		return 3
	case domain.SeverityMedium:
		return 2
	case domain.SeverityLow:
		return 1
	default:
		return 0
	}
}
