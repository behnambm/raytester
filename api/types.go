package api

import (
	"context"
	"raytest/core"
	"sync"
	"time"
)

type TestSession struct {
	ID        string
	URL       string
	CreatedAt time.Time
	Config    core.Config
	Tester    *core.Tester
	Results   []core.TestResult
	Cancel    context.CancelFunc
	WsHub     *WSHub
	mu        sync.RWMutex
}

func (s *TestSession) AppendResult(r core.TestResult) {
	s.mu.Lock()
	s.Results = append(s.Results, r)
	s.mu.Unlock()
}

func (s *TestSession) GetResults() []core.TestResult {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cp := make([]core.TestResult, len(s.Results))
	copy(cp, s.Results)
	return cp
}

func (s *TestSession) SetResults(results []core.TestResult) {
	s.mu.Lock()
	s.Results = results
	s.mu.Unlock()
}

type SessionInfo struct {
	ID           string `json:"id"`
	URL          string `json:"url"`
	Status       string `json:"status"`
	CreatedAt    string `json:"created_at"`
	TotalConfigs int    `json:"total_configs"`
	TestedCount  int    `json:"tested_count"`
	SuccessCount int    `json:"success_count"`
	FailCount    int    `json:"fail_count"`
}

type StartTestRequest struct {
	URL        string `json:"url"`
	MaxLatency string `json:"max_latency,omitempty"`
	Workers    int    `json:"workers,omitempty"`
	XrayPath   string `json:"xray_path,omitempty"`
}

type StartTestResponse struct {
	ID      string `json:"id"`
	Message string `json:"message"`
}

type StopResponse struct {
	Message string `json:"message"`
}

type StatusResponse struct {
	Status string `json:"status"`
}

type ConfigResponse struct {
	MaxLatency string `json:"max_latency"`
	Workers    int    `json:"workers"`
	XrayPath   string `json:"xray_path"`
}

type StatsResponse struct {
	ID           string          `json:"id"`
	TotalConfigs int             `json:"total_configs"`
	TestedCount  int             `json:"tested_count"`
	SuccessCount int             `json:"success_count"`
	FailCount    int             `json:"fail_count"`
	Progress     float64         `json:"progress"`
	Status       string          `json:"status"`
	MinLatency   time.Duration   `json:"min_latency"`
	MaxLatency   time.Duration   `json:"max_latency"`
	AvgLatency   time.Duration   `json:"avg_latency"`
}

type ResultResponse struct {
	Config      core.ProxyConfig `json:"config"`
	Latency     time.Duration    `json:"latency"`
	Country     string           `json:"country"`
	CountryName string           `json:"country_name"`
}

type WSMessage struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload"`
}

// --- Scheduler types ---

type CreateScheduledTaskRequest struct {
	Name       string `json:"name"`
	URL        string `json:"url"`
	CronExpr   string `json:"cron_expr"`
	MaxLatency string `json:"max_latency,omitempty"`
	Workers    int    `json:"workers,omitempty"`
	Enabled    bool   `json:"enabled"`
	RunNow     bool   `json:"run_now"`
}

type UpdateScheduledTaskRequest struct {
	Name       string `json:"name,omitempty"`
	URL        string `json:"url,omitempty"`
	CronExpr   string `json:"cron_expr,omitempty"`
	MaxLatency string `json:"max_latency,omitempty"`
	Workers    int    `json:"workers,omitempty"`
	Enabled    *bool  `json:"enabled,omitempty"`
}

