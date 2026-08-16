package sync

import (
	"fmt"
	"time"

	"github.com/qilian/patrol-dispatch/internal/domain"
	"github.com/qilian/patrol-dispatch/internal/service"
)

// RecordKind identifies the type of field record being synced.
type RecordKind string

const (
	KindCheckIn     RecordKind = "check_in"
	KindWildlife    RecordKind = "wildlife"
	KindDisturbance RecordKind = "disturbance"
)

// SyncBatch is the payload pushed by a patrol terminal after signal recovery.
// It contains records that were stored locally while the network was down.
type SyncBatch struct {
	CheckIns    []domain.CheckIn          `json:"check_ins"`
	Wildlife    []domain.WildlifeRecord   `json:"wildlife"`
	Disturbance []domain.DisturbanceEvent `json:"disturbance"`
}

// SyncStats summarises the outcome of processing a sync batch.
type SyncStats struct {
	CheckInsAccepted     int
	CheckInsDuplicate    int
	WildlifeAccepted     int
	WildlifeDuplicate    int
	DisturbanceAccepted  int
	DisturbanceDuplicate int
	DisturbanceMerged    int
	Errors               []error
}

// TotalAccepted returns the count of newly stored records across all kinds.
func (st *SyncStats) TotalAccepted() int {
	return st.CheckInsAccepted + st.WildlifeAccepted + st.DisturbanceAccepted
}

// TotalDuplicate returns the count of records that were already present.
func (st *SyncStats) TotalDuplicate() int {
	return st.CheckInsDuplicate + st.WildlifeDuplicate + st.DisturbanceDuplicate
}

// Manager coordinates offline-to-online synchronisation. When a patrol
// terminal loses signal it stores records locally; on reconnection it pushes
// a SyncBatch. The Manager processes each record with dedup so that every
// trajectory point and event has exactly one traceable copy on the server.
type Manager struct {
	svc *service.Service
}

// NewManager creates a sync Manager bound to the given service.
func NewManager(svc *service.Service) *Manager {
	return &Manager{svc: svc}
}

// ProcessBatch ingests a batch of offline records. Each record is checked
// against the store's dedup index (device ID + officer ID + timestamp).
// Duplicates are silently skipped; disturbance events may be merged if the
// incoming version carries a higher severity (escalation recovery).
func (m *Manager) ProcessBatch(batch SyncBatch) SyncStats {
	var stats SyncStats

	for i := range batch.CheckIns {
		c := batch.CheckIns[i]
		id, inserted, err := m.svc.RecordCheckIn(&c)
		if err != nil {
			stats.Errors = append(stats.Errors, fmt.Errorf("check_in %s: %w", c.ID, err))
			continue
		}
		_ = id
		if inserted {
			stats.CheckInsAccepted++
		} else {
			stats.CheckInsDuplicate++
		}
	}

	for i := range batch.Wildlife {
		w := batch.Wildlife[i]
		id, inserted, err := m.svc.RecordWildlife(&w)
		if err != nil {
			stats.Errors = append(stats.Errors, fmt.Errorf("wildlife %s: %w", w.ID, err))
			continue
		}
		_ = id
		if inserted {
			stats.WildlifeAccepted++
		} else {
			stats.WildlifeDuplicate++
		}
	}

	for i := range batch.Disturbance {
		e := batch.Disturbance[i]
		id, inserted, merged, err := m.svc.RecordDisturbance(&e)
		if err != nil {
			stats.Errors = append(stats.Errors, fmt.Errorf("disturbance %s: %w", e.ID, err))
			continue
		}
		_ = id
		switch {
		case merged:
			stats.DisturbanceMerged++
		case inserted:
			stats.DisturbanceAccepted++
		default:
			stats.DisturbanceDuplicate++
		}
	}

	return stats
}

// MergeConflict resolves a version conflict between two records of the same
// kind by keeping the one with the later timestamp. If timestamps are equal
// the existing record wins (first-writer-wins), preventing flapping during
// concurrent re-syncs.
func MergeConflict(existingTS, incomingTS time.Time) bool {
	return incomingTS.After(existingTS)
}

// DedupKeyForCheckIn builds the canonical dedup key for a check-in.
func DedupKeyForCheckIn(c *domain.CheckIn) string {
	return domain.DedupKey(c.DeviceID, c.OfficerID, c.Timestamp)
}

// DedupKeyForWildlife builds the canonical dedup key for a wildlife record.
func DedupKeyForWildlife(w *domain.WildlifeRecord) string {
	return domain.DedupKey(w.DeviceID, w.OfficerID, w.Timestamp)
}

// DedupKeyForDisturbance builds the canonical dedup key for a disturbance event.
func DedupKeyForDisturbance(e *domain.DisturbanceEvent) string {
	return domain.DedupKey(e.DeviceID, e.OfficerID, e.Timestamp)
}

// ValidateBatch performs lightweight validation before processing: each
// record must carry a device ID, officer ID and non-zero timestamp. This
// catches malformed offline payloads early.
func (b SyncBatch) Validate() error {
	for i, c := range b.CheckIns {
		if c.DeviceID == "" || c.OfficerID == "" || c.Timestamp.IsZero() {
			return fmt.Errorf("check_in[%d]: missing device_id, officer_id or timestamp", i)
		}
	}
	for i, w := range b.Wildlife {
		if w.DeviceID == "" || w.OfficerID == "" || w.Timestamp.IsZero() {
			return fmt.Errorf("wildlife[%d]: missing device_id, officer_id or timestamp", i)
		}
	}
	for i, e := range b.Disturbance {
		if e.DeviceID == "" || e.OfficerID == "" || e.Timestamp.IsZero() {
			return fmt.Errorf("disturbance[%d]: missing device_id, officer_id or timestamp", i)
		}
	}
	return nil
}
