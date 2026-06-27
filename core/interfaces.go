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
	Error       error
}

type Progress struct {
	Done  int
	Total int
}

type Hooks struct {
	OnTestStart    func(config ProxyConfig)
	OnTestComplete func(result TestResult)
	OnProgress     func(p Progress)
	OnComplete     func(results []TestResult)
}
