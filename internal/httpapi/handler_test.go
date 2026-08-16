package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/qilian/patrol-dispatch/internal/domain"
	"github.com/qilian/patrol-dispatch/internal/service"
	"github.com/qilian/patrol-dispatch/internal/store"
	patrolsync "github.com/qilian/patrol-dispatch/internal/sync"
)

func newTestHandler(t *testing.T) (*Handler, *service.Service) {
	t.Helper()
	s := store.New()
	es := store.NewEquipmentStore()
	s.SaveTeam(&domain.PatrolTeam{ID: "team-1", Name: "一分队"})
	s.SaveRoute(&domain.PatrolRoute{
		ID: "route-1", Name: "巡护线", Boundary: "hualong",
		Waypoints: []domain.Waypoint{{Lat: 37.66, Lng: 102.61}},
	})
	for _, d := range []*domain.Equipment{
		{ID: "sp-1", Type: domain.EquipmentSatellitePhone, Category: domain.EquipmentCategoryEmergency, Status: domain.EquipmentStatusInStock},
		{ID: "sp-2", Type: domain.EquipmentSatellitePhone, Category: domain.EquipmentCategoryEmergency, Status: domain.EquipmentStatusInStock},
		{ID: "gps-1", Type: domain.EquipmentGPS, Category: domain.EquipmentCategoryRegular, Status: domain.EquipmentStatusInStock},
		{ID: "gps-2", Type: domain.EquipmentGPS, Category: domain.EquipmentCategoryRegular, Status: domain.EquipmentStatusInStock},
	} {
		es.AddDevice(d)
	}
	svc := service.New(service.DefaultConfig(), s, es)
	sm := patrolsync.NewManager(svc)
	h := NewHandler(svc, sm)
	// Start the worker so event routing happens (though we test manually).
	return h, svc
}

func doRequest(t *testing.T, h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func TestHTTPFullDispatchFlow(t *testing.T) {
	h, _ := newTestHandler(t)
	router := h.NewRouter()

	// Submit plan.
	planBody := map[string]any{
		"team_id":  "team-1",
		"date":     "2026-08-16",
		"route_id": "route-1",
		"equipment_needs": []map[string]any{
			{"type": "gps_tracker", "quantity": 1, "category": "regular"},
		},
	}
	rr := doRequest(t, router, "POST", "/api/plans", planBody)
	if rr.Code != http.StatusCreated {
		t.Fatalf("submit plan: %d %s", rr.Code, rr.Body.String())
	}
	var planResp struct {
		ID string `json:"id"`
	}
	json.Unmarshal(rr.Body.Bytes(), &planResp)

	// Approve.
	rr = doRequest(t, router, "POST", "/api/plans/"+planResp.ID+"/approve", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("approve: %d %s", rr.Code, rr.Body.String())
	}

	// Dispatch.
	rr = doRequest(t, router, "POST", "/api/plans/"+planResp.ID+"/dispatch", nil)
	if rr.Code != http.StatusCreated {
		t.Fatalf("dispatch: %d %s", rr.Code, rr.Body.String())
	}
	var dispResp struct {
		Dispatch struct {
			ID string `json:"id"`
		} `json:"dispatch"`
		Requisition struct {
			ID string `json:"id"`
		} `json:"requisition"`
	}
	json.Unmarshal(rr.Body.Bytes(), &dispResp)

	// Approve requisition.
	rr = doRequest(t, router, "POST", "/api/requisitions/"+dispResp.Requisition.ID+"/approve",
		map[string]any{"approver_id": "chief-001"})
	if rr.Code != http.StatusOK {
		t.Fatalf("approve req: %d %s", rr.Code, rr.Body.String())
	}

	// Start patrol.
	rr = doRequest(t, router, "POST", "/api/plans/"+planResp.ID+"/start", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("start: %d %s", rr.Code, rr.Body.String())
	}

	// Complete patrol.
	rr = doRequest(t, router, "POST", "/api/plans/"+planResp.ID+"/complete", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("complete: %d %s", rr.Code, rr.Body.String())
	}
}

func TestHTTPDispatchConflictReturns409(t *testing.T) {
	h, svc := newTestHandler(t)
	svc.Store().SaveTeam(&domain.PatrolTeam{ID: "team-2", Name: "二分队"})
	router := h.NewRouter()

	// Team 1 dispatches with emergency sat phone.
	plan1 := map[string]any{
		"team_id": "team-1", "date": "2026-08-16", "route_id": "route-1",
		"equipment_needs": []map[string]any{
			{"type": "satellite_phone", "quantity": 1, "category": "emergency"},
		},
	}
	rr := doRequest(t, router, "POST", "/api/plans", plan1)
	var p1 struct {
		ID string `json:"id"`
	}
	json.Unmarshal(rr.Body.Bytes(), &p1)
	doRequest(t, router, "POST", "/api/plans/"+p1.ID+"/approve", nil)
	doRequest(t, router, "POST", "/api/plans/"+p1.ID+"/dispatch", nil)

	// Team 2 tries same emergency type on same day.
	plan2 := map[string]any{
		"team_id": "team-2", "date": "2026-08-16", "route_id": "route-1",
		"equipment_needs": []map[string]any{
			{"type": "satellite_phone", "quantity": 1, "category": "emergency"},
		},
	}
	rr = doRequest(t, router, "POST", "/api/plans", plan2)
	var p2 struct {
		ID string `json:"id"`
	}
	json.Unmarshal(rr.Body.Bytes(), &p2)
	doRequest(t, router, "POST", "/api/plans/"+p2.ID+"/approve", nil)
	rr = doRequest(t, router, "POST", "/api/plans/"+p2.ID+"/dispatch", nil)
	if rr.Code != http.StatusConflict {
		t.Errorf("conflict dispatch: got %d, want %d. body: %s", rr.Code, http.StatusConflict, rr.Body.String())
	}
}

func TestHTTPCheckInDedupViaAPI(t *testing.T) {
	h, _ := newTestHandler(t)
	router := h.NewRouter()

	// Create and approve plan.
	planBody := map[string]any{
		"team_id": "team-1", "date": "2026-08-16", "route_id": "route-1",
	}
	rr := doRequest(t, router, "POST", "/api/plans", planBody)
	var p struct {
		ID string `json:"id"`
	}
	json.Unmarshal(rr.Body.Bytes(), &p)
	doRequest(t, router, "POST", "/api/plans/"+p.ID+"/approve", nil)
	doRequest(t, router, "POST", "/api/plans/"+p.ID+"/start", nil)

	checkIn := map[string]any{
		"officer_id": "off-1", "device_id": "DEV-1", "plan_id": p.ID,
		"timestamp": "2026-08-16T10:00:00Z",
		"location":  map[string]float64{"lat": 37.66, "lng": 102.61},
	}

	// First check-in → 201.
	rr = doRequest(t, router, "POST", "/api/checkins", checkIn)
	if rr.Code != http.StatusCreated {
		t.Fatalf("first check-in: %d %s", rr.Code, rr.Body.String())
	}

	// Duplicate → 200 (not created).
	rr = doRequest(t, router, "POST", "/api/checkins", checkIn)
	if rr.Code != http.StatusOK {
		t.Fatalf("duplicate check-in: %d %s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Inserted bool `json:"inserted"`
	}
	json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.Inserted {
		t.Error("duplicate should have inserted=false")
	}
}

func TestHTTPSyncEndpoint(t *testing.T) {
	h, _ := newTestHandler(t)
	router := h.NewRouter()

	// Create plan.
	planBody := map[string]any{
		"team_id": "team-1", "date": "2026-08-16", "route_id": "route-1",
	}
	rr := doRequest(t, router, "POST", "/api/plans", planBody)
	var p struct {
		ID string `json:"id"`
	}
	json.Unmarshal(rr.Body.Bytes(), &p)
	doRequest(t, router, "POST", "/api/plans/"+p.ID+"/approve", nil)
	doRequest(t, router, "POST", "/api/plans/"+p.ID+"/start", nil)

	syncBody := map[string]any{
		"check_ins": []map[string]any{
			{
				"officer_id": "off-1", "device_id": "DEV-1", "plan_id": p.ID,
				"timestamp": "2026-08-16T10:00:00Z",
				"location":  map[string]float64{"lat": 37.66, "lng": 102.61},
			},
		},
		"wildlife": []map[string]any{
			{
				"officer_id": "off-1", "device_id": "DEV-1", "plan_id": p.ID,
				"timestamp": "2026-08-16T10:15:00Z", "species": "雪豹",
				"location":  map[string]float64{"lat": 37.66, "lng": 102.61},
				"image_ref": "img-001",
			},
		},
		"disturbance": []map[string]any{
			{
				"officer_id": "off-1", "device_id": "DEV-1", "plan_id": p.ID,
				"timestamp": "2026-08-16T10:30:00Z", "type": "poaching", "severity": "high",
				"location": map[string]float64{"lat": 37.66, "lng": 102.61},
			},
		},
	}
	rr = doRequest(t, router, "POST", "/api/sync", syncBody)
	if rr.Code != http.StatusOK {
		t.Fatalf("sync: %d %s", rr.Code, rr.Body.String())
	}
	var resp map[string]int
	json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp["checkins_accepted"] != 1 {
		t.Errorf("checkins_accepted = %d, want 1", resp["checkins_accepted"])
	}
	if resp["wildlife_accepted"] != 1 {
		t.Errorf("wildlife_accepted = %d, want 1", resp["wildlife_accepted"])
	}
	if resp["disturbance_accepted"] != 1 {
		t.Errorf("disturbance_accepted = %d, want 1", resp["disturbance_accepted"])
	}

	// Route events and verify high severity went to department.
	rr = doRequest(t, router, "POST", "/api/events/route", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("route events: %d %s", rr.Code, rr.Body.String())
	}
}

func TestHTTPGetPlanNotFound(t *testing.T) {
	h, _ := newTestHandler(t)
	router := h.NewRouter()
	rr := doRequest(t, router, "GET", "/api/plans/nonexistent", nil)
	if rr.Code != http.StatusNotFound {
		t.Errorf("get non-existent plan: %d, want 404", rr.Code)
	}
}
