package storage

import "time"

// ScheduledTask represents a scheduled proxy testing task.
type ScheduledTask struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	URL        string    `json:"url"`
	CronExpr   string    `json:"cron_expr"`
	MaxLatency string    `json:"max_latency"`
	Workers    int       `json:"workers"`
	XrayPath   string    `json:"xray_path,omitempty"`
	OutputPath string    `json:"output_path"`
	Enabled    bool      `json:"enabled"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// RunMetrics tracks execution statistics for a scheduled task.
type RunMetrics struct {
	TotalRuns       int           `json:"total_runs"`
	CompletedRuns   int           `json:"completed_runs"`
	FailureRuns     int           `json:"failure_runs"`
	LastRunTime     *time.Time    `json:"last_run_time"`
	LastRunDuration time.Duration `json:"last_run_duration"`
	AvgRunDuration  time.Duration `json:"avg_run_duration"`
	LastResultCount int           `json:"last_result_count"`
}
