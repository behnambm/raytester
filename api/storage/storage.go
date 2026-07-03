package storage

import "raytest/core"

// Storage abstracts persistence for scheduled tasks, results, and metrics.
// Implementations can be file-based, database-backed, S3-backed, etc.
type Storage interface {
	// Task CRUD
	CreateTask(task ScheduledTask) error
	GetTask(id string) (*ScheduledTask, error)
	ListTasks() ([]ScheduledTask, error)
	UpdateTask(task ScheduledTask) error
	DeleteTask(id string) error

	// Results
	SaveResults(taskID string, results []core.TestResult) error
	GetResults(taskID string) ([]core.TestResult, error)

	// Metrics
	SaveMetrics(taskID string, metrics RunMetrics) error
	GetMetrics(taskID string) (*RunMetrics, error)
}
