package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/qilian/patrol-dispatch/internal/domain"
	"github.com/qilian/patrol-dispatch/internal/httpapi"
	"github.com/qilian/patrol-dispatch/internal/service"
	"github.com/qilian/patrol-dispatch/internal/store"
	"github.com/qilian/patrol-dispatch/internal/sync"
	"github.com/qilian/patrol-dispatch/internal/worker"
)

func main() {
	port := envOr("PORT", "53115")
	cfg := service.Config{
		StationChiefID: envOr("STATION_CHIEF_ID", "chief-001"),
		DutyRoomID:     envOr("DUTY_ROOM_ID", "duty-room-001"),
		DepartmentID:   envOr("DEPARTMENT_ID", "forestry-dept-001"),
		WorkerInterval: 5 * time.Second,
	}

	// Initialise persistence and service layers.
	dataStore := store.New()
	equipStore := store.NewEquipmentStore()
	svc := service.New(cfg, dataStore, equipStore)
	sm := sync.NewManager(svc)

	// Seed demo data so the server is immediately usable.
	seedDemoData(dataStore, equipStore)

	// Start the background worker.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := worker.New(svc, cfg.WorkerInterval)
	w.Start(ctx)

	// Build the HTTP server.
	handler := httpapi.NewHandler(svc, sm)
	server := &http.Server{
		Addr:              ":" + port,
		Handler:           handler.NewRouter(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// Graceful shutdown on SIGINT / SIGTERM.
	go func() {
		log.Printf("patrol-dispatch server listening on :%s", port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	log.Println("shutting down...")
	cancel()
	w.Stop()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown error: %v", err)
	}
	log.Println("server stopped")
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// seedDemoData populates the store with two patrol teams, two routes within
// the Hualong jurisdiction, and a starter equipment inventory including
// emergency satellite phones and infrared cameras.
func seedDemoData(s *store.Store, es *store.EquipmentStore) {
	// --- Teams ---
	teamA := &domain.PatrolTeam{
		ID:   "team-001",
		Name: "华隆一分队",
		Officers: []domain.PatrolOfficer{
			{ID: "officer-001", Name: "张志远", DeviceID: "DEV-001"},
			{ID: "officer-002", Name: "李晓峰", DeviceID: "DEV-002"},
		},
	}
	teamB := &domain.PatrolTeam{
		ID:   "team-002",
		Name: "华隆二分队",
		Officers: []domain.PatrolOfficer{
			{ID: "officer-003", Name: "王海涛", DeviceID: "DEV-003"},
			{ID: "officer-004", Name: "赵铭", DeviceID: "DEV-004"},
		},
	}
	s.SaveTeam(teamA)
	s.SaveTeam(teamB)

	// --- Routes ---
	route1 := &domain.PatrolRoute{
		ID:   "route-001",
		Name: "祁连山北麓巡护线",
		Waypoints: []domain.Waypoint{
			{Lat: 37.6614, Lng: 102.6080},
			{Lat: 37.6720, Lng: 102.6210},
			{Lat: 37.6835, Lng: 102.6340},
		},
		Boundary: "hualong",
	}
	route2 := &domain.PatrolRoute{
		ID:   "route-002",
		Name: "冷龙岭西坡巡护线",
		Waypoints: []domain.Waypoint{
			{Lat: 37.7100, Lng: 102.5500},
			{Lat: 37.7220, Lng: 102.5650},
			{Lat: 37.7340, Lng: 102.5800},
		},
		Boundary: "hualong",
	}
	s.SaveRoute(route1)
	s.SaveRoute(route2)

	// --- Equipment inventory ---
	devices := []*domain.Equipment{
		// Emergency satellite phones (2 units, single-team-per-day).
		{ID: "eq-sp-001", Type: domain.EquipmentSatellitePhone, Category: domain.EquipmentCategoryEmergency, Status: domain.EquipmentStatusInStock},
		{ID: "eq-sp-002", Type: domain.EquipmentSatellitePhone, Category: domain.EquipmentCategoryEmergency, Status: domain.EquipmentStatusInStock},
		// Emergency infrared cameras (3 units).
		{ID: "eq-ic-001", Type: domain.EquipmentInfraredCamera, Category: domain.EquipmentCategoryEmergency, Status: domain.EquipmentStatusInStock},
		{ID: "eq-ic-002", Type: domain.EquipmentInfraredCamera, Category: domain.EquipmentCategoryEmergency, Status: domain.EquipmentStatusInStock},
		{ID: "eq-ic-003", Type: domain.EquipmentInfraredCamera, Category: domain.EquipmentCategoryEmergency, Status: domain.EquipmentStatusInStock},
		// Regular gear.
		{ID: "eq-gps-001", Type: domain.EquipmentGPS, Category: domain.EquipmentCategoryRegular, Status: domain.EquipmentStatusInStock},
		{ID: "eq-gps-002", Type: domain.EquipmentGPS, Category: domain.EquipmentCategoryRegular, Status: domain.EquipmentStatusInStock},
		{ID: "eq-bin-001", Type: domain.EquipmentBinoculars, Category: domain.EquipmentCategoryRegular, Status: domain.EquipmentStatusInStock},
		{ID: "eq-bin-002", Type: domain.EquipmentBinoculars, Category: domain.EquipmentCategoryRegular, Status: domain.EquipmentStatusInStock},
		{ID: "eq-rad-001", Type: domain.EquipmentRadio, Category: domain.EquipmentCategoryRegular, Status: domain.EquipmentStatusInStock},
		{ID: "eq-rad-002", Type: domain.EquipmentRadio, Category: domain.EquipmentCategoryRegular, Status: domain.EquipmentStatusInStock},
	}
	for _, d := range devices {
		es.AddDevice(d)
	}

	log.Printf("seeded demo data: %d teams, %d routes, %d devices",
		2, 2, len(devices))
}
