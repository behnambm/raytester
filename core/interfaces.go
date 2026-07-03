package core

import (
	"time"

	"raytest/core/xray"
)

const (
	DefaultMaxLatency = 500 * time.Millisecond
	DefaultWorkers    = 20
	DefaultXrayPath   = "xray"
	MaxBodySize       = 20 * 1024 * 1024
	MaxConfigs        = 10000
	ReadinessTimeout  = 5 * time.Second
)

const (
	StatusIdle        = "idle"
	StatusDownloading = "downloading"
	StatusTesting     = "testing"
	StatusDone        = "done"
	StatusStopped     = "stopped"
)

type ProxyConfig = xray.ProxyConfig

type Config struct {
	MaxLatency time.Duration
	Workers    int
	XrayPath   string
}

type TestResult struct {
	Config      ProxyConfig
	Latency     time.Duration
	Country     string
	CountryName string
	Error       string // empty if success, error message if failed
}

type Progress struct {
	Done  int
	Total int
}

type Stats struct {
	TotalConfigs int           `json:"total_configs"`
	TestedCount  int           `json:"tested_count"`
	SuccessCount int           `json:"success_count"`
	FailCount    int           `json:"fail_count"`
	Progress     float64       `json:"progress"`
	Status       string        `json:"status"`
	MinLatency   time.Duration `json:"min_latency"`
	MaxLatency   time.Duration `json:"max_latency"`
	AvgLatency   time.Duration `json:"avg_latency"`
}

type Hooks struct {
	OnTestStart    func(config ProxyConfig)
	OnTestComplete func(result TestResult)
	OnProgress     func(p Progress)
	OnStatsUpdate  func(stats Stats)
	OnComplete     func(results []TestResult)
}
