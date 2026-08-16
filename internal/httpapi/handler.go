package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/qilian/patrol-dispatch/internal/domain"
	"github.com/qilian/patrol-dispatch/internal/service"
	"github.com/qilian/patrol-dispatch/internal/store"
	patrolsync "github.com/qilian/patrol-dispatch/internal/sync"
)

// Handler wires HTTP endpoints to the application service and sync manager.
type Handler struct {
	svc  *service.Service
	sync *patrolsync.Manager
}

// NewHandler creates a handler bound to the given service and sync manager.
func NewHandler(svc *service.Service, sm *patrolsync.Manager) *Handler {
	return &Handler{svc: svc, sync: sm}
}

// NewRouter builds the HTTP mux with all patrol-dispatch routes registered.
// Uses Go 1.22+ method+pattern routing.
func (h *Handler) NewRouter() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.health)
	mux.HandleFunc("POST /api/teams", h.createTeam)
	mux.HandleFunc("POST /api/routes", h.createRoute)
	mux.HandleFunc("POST /api/plans", h.submitPlan)
	mux.HandleFunc("POST /api/plans/{id}/approve", h.approvePlan)
	mux.HandleFunc("POST /api/plans/{id}/cancel", h.cancelPlan)
	mux.HandleFunc("POST /api/plans/{id}/dispatch", h.dispatchPlan)
	mux.HandleFunc("POST /api/plans/{id}/start", h.startPatrol)
	mux.HandleFunc("POST /api/plans/{id}/complete", h.completePatrol)
	mux.HandleFunc("GET /api/plans/{id}", h.getPlan)
	mux.HandleFunc("GET /api/dispatches/{id}", h.getDispatch)
	mux.HandleFunc("POST /api/requisitions/{id}/approve", h.approveRequisition)
	mux.HandleFunc("POST /api/requisitions/{id}/reject", h.rejectRequisition)
	mux.HandleFunc("POST /api/checkins", h.recordCheckIn)
	mux.HandleFunc("POST /api/wildlife", h.recordWildlife)
	mux.HandleFunc("POST /api/disturbances", h.recordDisturbance)
	mux.HandleFunc("POST /api/events/route", h.routeEvents)
	mux.HandleFunc("POST /api/sync", h.syncBatch)
	mux.HandleFunc("GET /api/equipment", h.listEquipment)
	mux.HandleFunc("GET /api/plans/{id}/trajectory", h.getTrajectory)
	return mux
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func decodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// ---------------------------------------------------------------------------
// Endpoints
// ---------------------------------------------------------------------------

func (h *Handler) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "ok",
		"time":   time.Now().Format(time.RFC3339),
	})
}

func (h *Handler) createTeam(w http.ResponseWriter, r *http.Request) {
	var team domain.PatrolTeam
	if err := decodeJSON(r, &team); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if team.ID == "" {
		team.ID = domain.NewID("team")
	}
	h.svc.Store().SaveTeam(&team)
	writeJSON(w, http.StatusCreated, team)
}

func (h *Handler) createRoute(w http.ResponseWriter, r *http.Request) {
	var route domain.PatrolRoute
	if err := decodeJSON(r, &route); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if route.ID == "" {
		route.ID = domain.NewID("route")
	}
	h.svc.Store().SaveRoute(&route)
	writeJSON(w, http.StatusCreated, route)
}

type submitPlanRequest struct {
	TeamID         string                 `json:"team_id"`
	Date           string                 `json:"date"`
	RouteID        string                 `json:"route_id"`
	EquipmentNeeds []domain.EquipmentNeed `json:"equipment_needs"`
}

func (h *Handler) submitPlan(w http.ResponseWriter, r *http.Request) {
	var req submitPlanRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	plan := &domain.PatrolPlan{
		TeamID:         req.TeamID,
		Date:           req.Date,
		RouteID:        req.RouteID,
		EquipmentNeeds: req.EquipmentNeeds,
	}
	if err := h.svc.SubmitPlan(plan); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, plan)
}

func (h *Handler) approvePlan(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.svc.ApprovePlan(id); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "approved"})
}

func (h *Handler) cancelPlan(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.svc.CancelPlan(id); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

func (h *Handler) dispatchPlan(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	result, err := h.svc.GenerateDispatch(id)
	if err != nil {
		// Check if it's an equipment conflict to give a 409.
		if isConflictErr(err) {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (h *Handler) startPatrol(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.svc.StartPatrol(id); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "in_progress"})
}

func (h *Handler) completePatrol(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.svc.CompletePatrol(id); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "completed"})
}

func (h *Handler) getPlan(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	plan, ok := h.svc.Store().GetPlan(id)
	if !ok {
		writeError(w, http.StatusNotFound, "plan not found")
		return
	}
	writeJSON(w, http.StatusOK, plan)
}

func (h *Handler) getDispatch(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	d, ok := h.svc.GetDispatch(id)
	if !ok {
		writeError(w, http.StatusNotFound, "dispatch not found")
		return
	}
	writeJSON(w, http.StatusOK, d)
}

type approveReqRequest struct {
	ApproverID string `json:"approver_id"`
}

func (h *Handler) approveRequisition(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req approveReqRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.svc.ApproveRequisition(id, req.ApproverID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "issued"})
}

func (h *Handler) rejectRequisition(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req approveReqRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.svc.RejectRequisition(id, req.ApproverID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "rejected"})
}

func (h *Handler) recordCheckIn(w http.ResponseWriter, r *http.Request) {
	var c domain.CheckIn
	if err := decodeJSON(r, &c); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	id, inserted, err := h.svc.RecordCheckIn(&c)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	code := http.StatusCreated
	if !inserted {
		code = http.StatusOK
	}
	writeJSON(w, code, map[string]any{
		"id":       id,
		"inserted": inserted,
	})
}

func (h *Handler) recordWildlife(w http.ResponseWriter, r *http.Request) {
	var wl domain.WildlifeRecord
	if err := decodeJSON(r, &wl); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	id, inserted, err := h.svc.RecordWildlife(&wl)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	code := http.StatusCreated
	if !inserted {
		code = http.StatusOK
	}
	writeJSON(w, code, map[string]any{
		"id":       id,
		"inserted": inserted,
	})
}

func (h *Handler) recordDisturbance(w http.ResponseWriter, r *http.Request) {
	var e domain.DisturbanceEvent
	if err := decodeJSON(r, &e); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	id, inserted, merged, err := h.svc.RecordDisturbance(&e)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":       id,
		"inserted": inserted,
		"merged":   merged,
		"level":    e.Level,
	})
}

func (h *Handler) routeEvents(w http.ResponseWriter, r *http.Request) {
	count, err := h.svc.RoutePendingEvents()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"routed": count})
}

func (h *Handler) syncBatch(w http.ResponseWriter, r *http.Request) {
	var batch patrolsync.SyncBatch
	if err := decodeJSON(r, &batch); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := batch.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	stats := h.sync.ProcessBatch(batch)
	resp := map[string]any{
		"checkins_accepted":     stats.CheckInsAccepted,
		"checkins_duplicate":    stats.CheckInsDuplicate,
		"wildlife_accepted":     stats.WildlifeAccepted,
		"wildlife_duplicate":    stats.WildlifeDuplicate,
		"disturbance_accepted":  stats.DisturbanceAccepted,
		"disturbance_duplicate": stats.DisturbanceDuplicate,
		"disturbance_merged":    stats.DisturbanceMerged,
	}
	if len(stats.Errors) > 0 {
		resp["errors"] = stats.Errors
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) listEquipment(w http.ResponseWriter, r *http.Request) {
	t := domain.EquipmentType(r.URL.Query().Get("type"))
	devices := h.svc.EquipmentStore().ListDevices(t)
	writeJSON(w, http.StatusOK, devices)
}

func (h *Handler) getTrajectory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	checkIns := h.svc.Store().ListCheckInsByPlan(id)
	wildlife := h.svc.Store().ListWildlifeByPlan(id)
	events := h.svc.Store().ListEventsByPlan(id)
	writeJSON(w, http.StatusOK, map[string]any{
		"check_ins":    checkIns,
		"wildlife":     wildlife,
		"disturbances": events,
	})
}

// isConflictErr returns true if the error wraps an EquipmentConflictError,
// indicating the dispatcher should be told to reassign.
func isConflictErr(err error) bool {
	var conflict *store.EquipmentConflictError
	return errors.As(err, &conflict)
}
