package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"raytest/cli/dedupe"
	"raytest/cli/parser"
	"raytest/cli/subscription"
	"raytest/core"
)

func (s *Server) handleStartTest(w http.ResponseWriter, r *http.Request) {
	log.Printf("[api:handlers] handleStartTest: request received")

	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req StartTestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("[api:handlers] handleStartTest: JSON decode FAILED: %v", err)
		s.writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	log.Printf("[api:handlers] handleStartTest: url=%s maxLatency=%s workers=%d xrayPath=%s",
		req.URL, req.MaxLatency, req.Workers, req.XrayPath)

	if req.URL == "" {
		s.writeError(w, http.StatusBadRequest, "URL is required")
		return
	}

	cfg := s.cfg
	if req.MaxLatency != "" {
		maxLatency, err := time.ParseDuration(req.MaxLatency)
		if err != nil {
			log.Printf("[api:handlers] handleStartTest: invalid maxLatency: %v", err)
			s.writeError(w, http.StatusBadRequest, "Invalid max_latency")
			return
		}
		cfg.MaxLatency = maxLatency
	}
	if req.Workers > 0 {
		cfg.Workers = req.Workers
	}
	if req.XrayPath != "" {
		cfg.XrayPath = req.XrayPath
	}

	log.Printf("[api:handlers] handleStartTest: effective config: maxLatency=%v workers=%d xrayPath=%s",
		cfg.MaxLatency, cfg.Workers, cfg.XrayPath)

	id := s.nextSessionID()

	ctx, cancel := context.WithCancel(context.Background())

	wsHub := NewWSHub()
	go wsHub.Run()

	hooks := core.Hooks{
		OnTestComplete: func(r core.TestResult) {
			session := s.getSession(id)
			if session == nil {
				log.Printf("[api:handlers] handleStartTest hook: OnTestComplete session %s not found", id)
				return
			}
			session.AppendResult(r)
			s.broadcastResult(session, r)
		},
		OnStatsUpdate: func(stats core.Stats) {
			session := s.getSession(id)
			if session == nil {
				log.Printf("[api:handlers] handleStartTest hook: OnStatsUpdate session %s not found", id)
				return
			}
			s.broadcastStats(session)
		},
		OnComplete: func(results []core.TestResult) {
			session := s.getSession(id)
			if session == nil {
				log.Printf("[api:handlers] handleStartTest hook: OnComplete session %s not found", id)
				return
			}
			session.SetResults(results)
			s.broadcastStatus(session, core.StatusDone)
		},
	}

	tester := core.NewTester(cfg, hooks)

	session := &TestSession{
		ID:        id,
		URL:       req.URL,
		CreatedAt: time.Now(),
		Config:    cfg,
		Tester:    tester,
		Cancel:    cancel,
		WsHub:     wsHub,
	}

	s.mu.Lock()
	s.sessions[id] = session
	s.mu.Unlock()

	log.Printf("[api:handlers] handleStartTest: session %s created and stored, launching test goroutine", id)

	go func() {
		log.Printf("[api:handlers] session-%s: goroutine started, setting status to downloading", id)

		tester.SetStatus(core.StatusDownloading)

		content, err := subscription.Download(&subscription.DownloadConfig{URL: req.URL})
		if err != nil {
			log.Printf("[api:handlers] session-%s: Download FAILED: %v", id, err)
			tester.SetStatus(core.StatusDone)
			return
		}

		log.Printf("[api:handlers] session-%s: downloaded %d bytes", id, len(content))

		configs := parser.Parse(content)
		log.Printf("[api:handlers] session-%s: parsed %d configs", id, len(configs))

		configs = dedupe.Deduplicate(configs)
		log.Printf("[api:handlers] session-%s: after dedupe: %d configs", id, len(configs))

		if len(configs) == 0 {
			log.Printf("[api:handlers] session-%s: no configs to test", id)
			tester.SetStatus(core.StatusDone)
			return
		}

		log.Printf("[api:handlers] session-%s: starting tester.Run with %d configs", id, len(configs))
		tester.Run(ctx, configs)
		log.Printf("[api:handlers] session-%s: tester.Run returned", id)
	}()

	log.Printf("[api:handlers] handleStartTest: responding 200 OK, id=%s", id)
	s.writeJSON(w, http.StatusOK, StartTestResponse{ID: id, Message: "Test started"})
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	log.Printf("[api:handlers] handleListSessions: request received")

	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	infos := make([]SessionInfo, 0, len(s.sessions))
	for _, session := range s.sessions {
		stats := session.Tester.GetStats()
		infos = append(infos, SessionInfo{
			ID:           session.ID,
			URL:          session.URL,
			Status:       stats.Status,
			CreatedAt:    session.CreatedAt.Format(time.RFC3339),
			TotalConfigs: stats.TotalConfigs,
			TestedCount:  stats.TestedCount,
			SuccessCount: stats.SuccessCount,
			FailCount:    stats.FailCount,
		})
	}

	log.Printf("[api:handlers] handleListSessions: returning %d sessions", len(infos))
	s.writeJSON(w, http.StatusOK, infos)
}

func (s *Server) handleGetStats(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	log.Printf("[api:handlers] handleGetStats: request for session %s", id)

	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	session := s.getSession(id)
	if session == nil {
		s.writeError(w, http.StatusNotFound, "Session not found")
		return
	}

	s.writeJSON(w, http.StatusOK, s.getStatsResponse(session, session.Tester))
}

func (s *Server) handleGetResults(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	log.Printf("[api:handlers] handleGetResults: request for session %s", id)

	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	session := s.getSession(id)
	if session == nil {
		s.writeError(w, http.StatusNotFound, "Session not found")
		return
	}

	s.writeJSON(w, http.StatusOK, s.getResultsResponse(session))
}

func (s *Server) handleStopTest(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	log.Printf("[api:handlers] handleStopTest: request for session %s", id)

	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	session := s.getSession(id)
	if session == nil {
		s.writeError(w, http.StatusNotFound, "Session not found")
		return
	}

	log.Printf("[api:handlers] handleStopTest: cancelling session %s", id)
	session.Cancel()
	s.writeJSON(w, http.StatusOK, StopResponse{Message: "Test stopped"})
}

func (s *Server) handleGetStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	log.Printf("[api:handlers] handleGetStatus: request for session %s", id)

	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	session := s.getSession(id)
	if session == nil {
		s.writeError(w, http.StatusNotFound, "Session not found")
		return
	}

	status := session.Tester.GetStats().Status
	log.Printf("[api:handlers] handleGetStatus: session=%s status=%s", id, status)
	s.writeJSON(w, http.StatusOK, StatusResponse{Status: status})
}

func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	log.Printf("[api:handlers] handleDeleteSession: request for session %s", id)

	if r.Method != http.MethodDelete {
		s.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	s.mu.Lock()
	session, ok := s.sessions[id]
	if !ok {
		s.mu.Unlock()
		s.writeError(w, http.StatusNotFound, "Session not found")
		return
	}

	status := session.Tester.GetStats().Status
	if status == core.StatusTesting || status == core.StatusDownloading {
		s.mu.Unlock()
		log.Printf("[api:handlers] handleDeleteSession: session %s is active (status=%s), refusing delete", id, status)
		s.writeError(w, http.StatusConflict, "Cannot delete an active test; stop it first")
		return
	}

	delete(s.sessions, id)
	s.mu.Unlock()

	log.Printf("[api:handlers] handleDeleteSession: session %s deleted", id)
	s.writeJSON(w, http.StatusOK, map[string]string{"message": "Session deleted"})
}

func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	log.Printf("[api:handlers] handleGetConfig: request received")

	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	s.writeJSON(w, http.StatusOK, ConfigResponse{
		MaxLatency: s.cfg.MaxLatency.String(),
		Workers:    s.cfg.Workers,
		XrayPath:   s.cfg.XrayPath,
	})
}
