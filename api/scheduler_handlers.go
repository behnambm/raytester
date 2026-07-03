package api

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"raytest/api/storage"
	"raytest/core"
)

// ---------------------------------------------------------------------------
// POST /api/scheduler/tasks
// ---------------------------------------------------------------------------

func (s *Server) handleCreateScheduledTask(w http.ResponseWriter, r *http.Request) {
	log.Printf("[api:scheduler] handleCreateScheduledTask: request received")

	var req CreateScheduledTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Name == "" {
		s.writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.URL == "" {
		s.writeError(w, http.StatusBadRequest, "url is required")
		return
	}
	if req.CronExpr == "" {
		s.writeError(w, http.StatusBadRequest, "cron_expr is required")
		return
	}

	task := storage.ScheduledTask{
		Name:       req.Name,
		URL:        req.URL,
		CronExpr:   req.CronExpr,
		MaxLatency: req.MaxLatency,
		Workers:    req.Workers,
		Enabled:    req.Enabled,
	}

	if err := s.scheduler.AddTask(&task); err != nil {
		log.Printf("[api:scheduler] handleCreateScheduledTask: AddTask FAILED: %v", err)
		s.writeError(w, http.StatusInternalServerError, "Failed to create task")
		return
	}

	log.Printf("[api:scheduler] handleCreateScheduledTask: task created id=%s name=%s", task.ID, req.Name)

	if req.RunNow {
		log.Printf("[api:scheduler] handleCreateScheduledTask: run_now set, triggering immediate run")
		go func() {
			if err := s.scheduler.RunTaskNow(task.ID); err != nil {
				log.Printf("[api:scheduler] handleCreateScheduledTask: RunTaskNow FAILED: %v", err)
			}
		}()
	}

	s.writeJSON(w, http.StatusOK, map[string]string{"message": "Task created"})
}

// ---------------------------------------------------------------------------
// GET /api/scheduler/tasks
// ---------------------------------------------------------------------------

func (s *Server) handleListScheduledTasks(w http.ResponseWriter, r *http.Request) {
	log.Printf("[api:scheduler] handleListScheduledTasks: request received")

	tasks, err := s.scheduler.ListTasks()
	if err != nil {
		log.Printf("[api:scheduler] handleListScheduledTasks: FAILED: %v", err)
		s.writeError(w, http.StatusInternalServerError, "Failed to list tasks")
		return
	}

	// Enrich each task with metrics.
	type taskWithMetrics struct {
		storage.ScheduledTask
		Metrics  storage.RunMetrics `json:"metrics"`
		Running  bool               `json:"running"`
		NextRun  string             `json:"next_run"`
	}

	out := make([]taskWithMetrics, 0, len(tasks))
	for _, task := range tasks {
		metrics, _ := s.scheduler.GetMetrics(task.ID)
		if metrics == nil {
			metrics = &storage.RunMetrics{}
		}
		twm := taskWithMetrics{
			ScheduledTask: task,
			Metrics:       *metrics,
			Running:       s.scheduler.IsRunning(task.ID),
		}
		if nr := s.scheduler.NextRun(task.ID); nr != nil {
			twm.NextRun = nr.Format(time.RFC3339)
		}
		out = append(out, twm)
	}

	s.writeJSON(w, http.StatusOK, out)
}

// ---------------------------------------------------------------------------
// GET /api/scheduler/tasks/{id}
// ---------------------------------------------------------------------------

func (s *Server) handleGetScheduledTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	log.Printf("[api:scheduler] handleGetScheduledTask: id=%s", id)

	task, err := s.scheduler.GetTask(id)
	if err != nil {
		s.writeError(w, http.StatusNotFound, "Task not found")
		return
	}

	metrics, _ := s.scheduler.GetMetrics(id)
	if metrics == nil {
		metrics = &storage.RunMetrics{}
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"task":    task,
		"metrics": metrics,
		"running": s.scheduler.IsRunning(id),
	})
}

// ---------------------------------------------------------------------------
// PUT /api/scheduler/tasks/{id}
// ---------------------------------------------------------------------------

func (s *Server) handleUpdateScheduledTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	log.Printf("[api:scheduler] handleUpdateScheduledTask: id=%s", id)

	task, err := s.scheduler.GetTask(id)
	if err != nil {
		s.writeError(w, http.StatusNotFound, "Task not found")
		return
	}

	var req UpdateScheduledTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Name != "" {
		task.Name = req.Name
	}
	if req.URL != "" {
		task.URL = req.URL
	}
	if req.CronExpr != "" {
		task.CronExpr = req.CronExpr
	}
	if req.MaxLatency != "" {
		task.MaxLatency = req.MaxLatency
	}
	if req.Workers > 0 {
		task.Workers = req.Workers
	}
	if req.Enabled != nil {
		task.Enabled = *req.Enabled
	}

	if err := s.scheduler.UpdateTask(*task); err != nil {
		log.Printf("[api:scheduler] handleUpdateScheduledTask: UpdateTask FAILED: %v", err)
		s.writeError(w, http.StatusInternalServerError, "Failed to update task")
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{"message": "Task updated"})
}

// ---------------------------------------------------------------------------
// DELETE /api/scheduler/tasks/{id}
// ---------------------------------------------------------------------------

func (s *Server) handleDeleteScheduledTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	log.Printf("[api:scheduler] handleDeleteScheduledTask: id=%s", id)

	if err := s.scheduler.RemoveTask(id); err != nil {
		log.Printf("[api:scheduler] handleDeleteScheduledTask: RemoveTask FAILED: %v", err)
		s.writeError(w, http.StatusInternalServerError, "Failed to delete task")
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{"message": "Task deleted"})
}

// ---------------------------------------------------------------------------
// POST /api/scheduler/tasks/{id}/run
// ---------------------------------------------------------------------------

func (s *Server) handleRunScheduledTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	log.Printf("[api:scheduler] handleRunScheduledTask: id=%s", id)

	if err := s.scheduler.RunTaskNow(id); err != nil {
		log.Printf("[api:scheduler] handleRunScheduledTask: RunTaskNow FAILED: %v", err)
		s.writeError(w, http.StatusInternalServerError, "Failed to run task")
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{"message": "Task triggered"})
}

// ---------------------------------------------------------------------------
// GET /api/scheduler/tasks/{id}/results
// ---------------------------------------------------------------------------

func (s *Server) handleGetScheduledTaskResults(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	log.Printf("[api:scheduler] handleGetScheduledTaskResults: id=%s", id)

	results, err := s.scheduler.GetResults(id)
	if err != nil {
		log.Printf("[api:scheduler] handleGetScheduledTaskResults: FAILED: %v", err)
		s.writeError(w, http.StatusInternalServerError, "Failed to get results")
		return
	}

	// Convert to ResultResponse, filter successes only.
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

	s.writeJSON(w, http.StatusOK, out)
}

// ---------------------------------------------------------------------------
// GET /api/scheduler/tasks/{id}/metrics
// ---------------------------------------------------------------------------

func (s *Server) handleGetScheduledTaskMetrics(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	log.Printf("[api:scheduler] handleGetScheduledTaskMetrics: id=%s", id)

	metrics, err := s.scheduler.GetMetrics(id)
	if err != nil {
		log.Printf("[api:scheduler] handleGetScheduledTaskMetrics: FAILED: %v", err)
		s.writeError(w, http.StatusInternalServerError, "Failed to get metrics")
		return
	}

	s.writeJSON(w, http.StatusOK, metrics)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func resultToResponse(r core.TestResult) ResultResponse {
	return ResultResponse{
		Config:      r.Config,
		Latency:     r.Latency,
		Country:     r.Country,
		CountryName: r.CountryName,
	}
}
