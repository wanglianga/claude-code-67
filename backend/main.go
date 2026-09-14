package main

import (
	"embed"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

//go:embed all:dist
var distFS embed.FS

func main() {
	initDB()
	defer db.Close()

	mux := http.NewServeMux()

	// ---- auth ----
	handle(mux, "POST /api/login", loginHandler)
	handle(mux, "POST /api/logout", logoutHandler, "any")
	handle(mux, "GET /api/me", meHandler, "any")
	handle(mux, "GET /api/health", healthHandler)

	// ---- stations / map ----
	handle(mux, "GET /api/stations", stationsHandler, "any")
	handle(mux, "GET /api/stations/{id}", stationDetailHandler, "any")
	handle(mux, "GET /api/map", mapHandler, "any")

	// ---- rides ----
	handle(mux, "POST /api/rides/borrow", borrowHandler, "user")
	handle(mux, "POST /api/rides/{id}/return", returnHandler, "user")
	handle(mux, "GET /api/rides/my", myRidesHandler, "user")
	handle(mux, "GET /api/rides/{id}", rideDetailHandler, "any")
	handle(mux, "POST /api/rides/{id}/lock-stuck", lockStuckHandler, "user")

	// ---- dashboard ----
	handle(mux, "GET /api/dashboard", dashboardHandler, "any")

	// ---- events ----
	handle(mux, "GET /api/events", eventsHandler, "any")
	handle(mux, "POST /api/events", createEventHandler, "any")
	handle(mux, "GET /api/events/{id}", eventDetailHandler, "any")
	handle(mux, "POST /api/events/{id}/join", eventJoinHandler, "staff")
	handle(mux, "POST /api/events/{id}/messages", eventMessageHandler, "any")
	handle(mux, "POST /api/events/{id}/action", eventActionHandler, "staff")
	handle(mux, "POST /api/events/{id}/resolve", eventResolveHandler, "staff")

	// ---- rebalance ----
	handle(mux, "GET /api/rebalance/tasks", rebalanceTasksHandler, "staff")
	handle(mux, "POST /api/rebalance/plan", rebalancePlanHandler, "dispatcher", "operator")
	handle(mux, "GET /api/rebalance/tasks/{id}", rebalanceTaskDetailHandler, "staff")
	handle(mux, "POST /api/rebalance/tasks/{id}/assign", rebalanceAssignHandler, "dispatcher", "operator")
	handle(mux, "POST /api/rebalance/tasks/{id}/status", rebalanceStatusHandler, "dispatcher", "operator")
	handle(mux, "GET /api/trucks", trucksHandler, "staff")

	// ---- peak rebalance routes（高峰调拨路线）----
	handle(mux, "GET /api/peak-routes", routeListHandler, "staff")
	handle(mux, "POST /api/peak-routes/plan", routePlanHandler, "dispatcher", "operator")
	handle(mux, "GET /api/peak-routes/deviations", routeDeviationsHandler, "staff")
	handle(mux, "GET /api/peak-routes/reviews", routeReviewsHandler, "staff")
	handle(mux, "GET /api/peak-routes/{id}", routeDetailHandler, "staff")
	handle(mux, "POST /api/peak-routes/{id}/assign", routeAssignHandler, "dispatcher", "operator")
	handle(mux, "POST /api/peak-routes/{id}/start", routeStartHandler, "dispatcher", "operator")
	handle(mux, "POST /api/peak-routes/{id}/stops/{seq}/arrive", routeArriveHandler, "dispatcher", "operator")
	handle(mux, "POST /api/peak-routes/{id}/stops/{seq}/execute", routeExecuteHandler, "dispatcher", "operator")
	handle(mux, "POST /api/peak-routes/{id}/stops/{seq}/deviation", routeDeviationHandler, "dispatcher", "operator")
	handle(mux, "POST /api/peak-routes/{id}/review", routeReviewHandler, "dispatcher", "operator")
	handle(mux, "GET /api/peak-weather", peakWeatherHandler, "staff")
	handle(mux, "POST /api/peak-weather", peakWeatherSetHandler, "dispatcher", "operator")

	// ---- maintenance ----
	handle(mux, "GET /api/faults", faultsHandler, "staff")
	handle(mux, "POST /api/faults/{id}/assign", faultAssignHandler, "repair", "operator")
	handle(mux, "POST /api/repairs", createRepairHandler, "repair")
	handle(mux, "GET /api/repairs", repairsHandler, "staff")
	handle(mux, "GET /api/bikes/{id}/archive", bikeArchiveHandler, "staff")
	handle(mux, "GET /api/parts", partsHandler, "staff")
	handle(mux, "GET /api/maintenance/stats", maintenanceStatsHandler, "staff")

	// ---- appeals ----
	handle(mux, "POST /api/appeals", createAppealHandler, "user")
	handle(mux, "GET /api/appeals", appealsHandler, "any")
	handle(mux, "GET /api/appeals/{id}", appealDetailHandler, "any")
	handle(mux, "POST /api/appeals/{id}/handle", appealHandleHandler, "cs", "operator")

	// ---- admin / ops ----
	handle(mux, "GET /api/stations/{id}/adjust-analysis", adjustAnalysisHandler, "operator", "station_admin", "city")
	handle(mux, "POST /api/stations/{id}/adjust", stationAdjustHandler, "operator")
	handle(mux, "GET /api/ops/overview", opsOverviewHandler, "staff")
	handle(mux, "POST /api/ops/weather", weatherHandler, "operator")
	handle(mux, "GET /api/ops/forbidden-zones", forbiddenZonesHandler, "any")
	handle(mux, "POST /api/ops/forbidden-zones/{id}/toggle", forbiddenZoneToggleHandler, "operator")
	handle(mux, "GET /api/ops/cleaning", cleaningHandler, "staff")
	handle(mux, "POST /api/ops/cleaning/{id}/done", cleaningDoneHandler, "repair", "operator", "station_admin")
	handle(mux, "GET /api/ops/shifts", shiftsHandler, "staff")
	handle(mux, "POST /api/ops/shifts", createShiftHandler, "dispatcher", "operator")
	handle(mux, "POST /api/stations/{id}/power", stationPowerHandler, "operator", "station_admin")
	handle(mux, "GET /api/users", usersHandler, "staff")

	// ---- static SPA ----
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		log.Fatal(err)
	}
	fileServer := http.FileServer(http.FS(sub))
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" {
			p = "index.html"
		}
		if f, err := sub.Open(p); err == nil {
			f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}
		// SPA fallback
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           logMiddleware(mux),
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("bike-ops server listening on :%s", port)
	log.Fatal(srv.ListenAndServe())
}

func logMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			log.Printf("%s %s", r.Method, r.URL.Path)
		}
		next.ServeHTTP(w, r)
	})
}

func healthHandler(w http.ResponseWriter, r *http.Request, _ *User) {
	if err := db.Ping(); err != nil {
		writeErr(w, 500, "db unreachable")
		return
	}
	writeJSON(w, 200, map[string]any{"status": "ok", "time": time.Now()})
}
