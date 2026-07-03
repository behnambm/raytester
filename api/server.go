package api

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"raytest/api/scheduler"
	"raytest/api/storage"
	"raytest/core"
)

type Server struct {
	cfg       core.Config
	sessions  map[string]*TestSession
	mu        sync.RWMutex
	nextID    atomic.Int64
	store     storage.Storage
	scheduler *scheduler.Scheduler
}

func NewServer(cfg core.Config, storagePath string) *Server {
	log.Printf("[api:server] NewServer: maxLatency=%v workers=%d xrayPath=%s storagePath=%s",
		cfg.MaxLatency, cfg.Workers, cfg.XrayPath, storagePath)

	store := storage.NewFileStorage(storagePath)
	sch := scheduler.New(store, cfg)

	return &Server{
		cfg:       cfg,
		sessions:  make(map[string]*TestSession),
		store:     store,
		scheduler: sch,
	}
}

func (s *Server) Start(addr string, frontendDir string) error {
	log.Printf("[api:server] Start: registering routes on addr=%s frontend=%s", addr, frontendDir)

	// Start the scheduler.
	if err := s.scheduler.Start(); err != nil {
		log.Printf("[api:server] Start: scheduler start FAILED: %v", err)
		return err
	}
	log.Printf("[api:server] Start: scheduler started")

	mux := http.NewServeMux()

	// Session-scoped routes
	mux.HandleFunc("POST /api/test", s.corsMiddleware(s.handleStartTest))
	mux.HandleFunc("GET /api/tests", s.corsMiddleware(s.handleListSessions))
	mux.HandleFunc("GET /api/test/{id}/stats", s.corsMiddleware(s.handleGetStats))
	mux.HandleFunc("GET /api/test/{id}/results", s.corsMiddleware(s.handleGetResults))
	mux.HandleFunc("POST /api/test/{id}/stop", s.corsMiddleware(s.handleStopTest))
	mux.HandleFunc("GET /api/test/{id}/status", s.corsMiddleware(s.handleGetStatus))
	mux.HandleFunc("DELETE /api/test/{id}", s.corsMiddleware(s.handleDeleteSession))
	mux.HandleFunc("GET /api/config", s.corsMiddleware(s.handleGetConfig))
	mux.HandleFunc("GET /api/ws", s.handleWebSocket)

	// Scheduler routes
	mux.HandleFunc("POST /api/scheduler/tasks", s.corsMiddleware(s.handleCreateScheduledTask))
	mux.HandleFunc("GET /api/scheduler/tasks", s.corsMiddleware(s.handleListScheduledTasks))
	mux.HandleFunc("GET /api/scheduler/tasks/{id}", s.corsMiddleware(s.handleGetScheduledTask))
	mux.HandleFunc("PUT /api/scheduler/tasks/{id}", s.corsMiddleware(s.handleUpdateScheduledTask))
	mux.HandleFunc("DELETE /api/scheduler/tasks/{id}", s.corsMiddleware(s.handleDeleteScheduledTask))
	mux.HandleFunc("POST /api/scheduler/tasks/{id}/run", s.corsMiddleware(s.handleRunScheduledTask))
	mux.HandleFunc("GET /api/scheduler/tasks/{id}/results", s.corsMiddleware(s.handleGetScheduledTaskResults))
	mux.HandleFunc("GET /api/scheduler/tasks/{id}/metrics", s.corsMiddleware(s.handleGetScheduledTaskMetrics))

	if frontendDir != "" {
		fs := http.FileServer(http.Dir(frontendDir))
		mux.Handle("/", fs)
		log.Printf("[api:server] Start: serving frontend from %s", frontendDir)
	}

	go s.cleanupLoop()

	// Graceful shutdown on SIGINT/SIGTERM.
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigCh
		log.Printf("[api:server] received signal %v, shutting down scheduler", sig)
		s.scheduler.Stop()
		log.Printf("[api:server] scheduler stopped")
		os.Exit(0)
	}()

	log.Printf("[api:server] Start: listening on %s", addr)
	return http.ListenAndServe(addr, mux)
}

func (s *Server) cleanupLoop() {
	log.Printf("[api:server] cleanupLoop: started (every 5min, TTL=1h)")
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		s.cleanupExpiredSessions()
	}
}

func (s *Server) cleanupExpiredSessions() {
	cutoff := time.Now().Add(-1 * time.Hour)
	s.mu.Lock()
	defer s.mu.Unlock()

	removed := 0
	for id, session := range s.sessions {
		status := session.Tester.GetStats().Status
		if (status == core.StatusDone || status == core.StatusStopped) && session.CreatedAt.Before(cutoff) {
			log.Printf("[api:server] cleanupExpiredSessions: removing session %s (created %v, status=%s)", id, session.CreatedAt, status)
			delete(s.sessions, id)
			removed++
		}
	}
	if removed > 0 {
		log.Printf("[api:server] cleanupExpiredSessions: removed %d expired sessions", removed)
	}
}

func (s *Server) nextSessionID() string {
	id := s.nextID.Add(1)
	log.Printf("[api:server] nextSessionID: generated id=%d", id)
	return strconv.FormatInt(id, 10)
}

func (s *Server) getSession(id string) *TestSession {
	s.mu.RLock()
	defer s.mu.RUnlock()
	session := s.sessions[id]
	if session == nil {
		log.Printf("[api:server] getSession: session %s NOT FOUND", id)
	} else {
		log.Printf("[api:server] getSession: session %s found, status=%s", id, session.Tester.GetStats().Status)
	}
	return session
}

func (s *Server) corsMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		log.Printf("[api:server] corsMiddleware: %s %s", r.Method, r.URL.Path)
		next(w, r)
	}
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("[api:server] writeJSON: encode error: %v", err)
	}
}

func (s *Server) writeError(w http.ResponseWriter, status int, message string) {
	log.Printf("[api:server] writeError: status=%d message=%s", status, message)
	s.writeJSON(w, status, map[string]string{"error": message})
}

func (s *Server) getStatsResponse(session *TestSession, tester *core.Tester) StatsResponse {
	stats := tester.GetStats()
	log.Printf("[api:server] getStatsResponse: session=%s tested=%d/%d success=%d fail=%d status=%s",
		session.ID, stats.TestedCount, stats.TotalConfigs, stats.SuccessCount, stats.FailCount, stats.Status)
	return StatsResponse{
		ID:           session.ID,
		TotalConfigs: stats.TotalConfigs,
		TestedCount:  stats.TestedCount,
		SuccessCount: stats.SuccessCount,
		FailCount:    stats.FailCount,
		Progress:     stats.Progress,
		Status:       stats.Status,
		MinLatency:   stats.MinLatency,
		MaxLatency:   stats.MaxLatency,
		AvgLatency:   stats.AvgLatency,
	}
}

func (s *Server) getResultsResponse(session *TestSession) []ResultResponse {
	results := session.GetResults()
	log.Printf("[api:server] getResultsResponse: session=%s total=%d", session.ID, len(results))
	out := make([]ResultResponse, 0, len(results))
	for _, r := range results {
		if r.Error == "" {
			out = append(out, ResultResponse{
				Config:      r.Config,
				Latency:     r.Latency,
				Country:     r.Country,
				CountryName: r.CountryName,
			})
		}
	}
	return out
}

func (s *Server) broadcastStats(session *TestSession) {
	stats := s.getStatsResponse(session, session.Tester)
	session.WsHub.Broadcast(WSMessage{
		Type:    "stats",
		Payload: stats,
	})
}

func (s *Server) broadcastResult(session *TestSession, result core.TestResult) {
	if result.Error == "" {
		log.Printf("[api:server] broadcastResult: session=%s latency=%v country=%s", session.ID, result.Latency, result.Country)
		session.WsHub.Broadcast(WSMessage{
			Type: "result",
			Payload: ResultResponse{
				Config:      result.Config,
				Latency:     result.Latency,
				Country:     result.Country,
				CountryName: result.CountryName,
			},
		})
	}
}

func (s *Server) broadcastStatus(session *TestSession, status string) {
	log.Printf("[api:server] broadcastStatus: session=%s status=%s", session.ID, status)
	session.WsHub.Broadcast(WSMessage{
		Type:    "status",
		Payload: StatusResponse{Status: status},
	})
}
