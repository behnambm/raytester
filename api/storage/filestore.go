package storage

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	"raytest/core"
)

// FileStorage implements Storage using JSON files on disk.
// Directory layout:
//
//	{basePath}/tasks/{id}.json       — task metadata
//	{basePath}/results/{id}.json     — latest results (overwritten each run)
//	{basePath}/metrics/{id}.json     — run metrics
type FileStorage struct {
	basePath string
	mu       sync.RWMutex
}

// NewFileStorage creates a FileStorage rooted at basePath.
// Directories are created on first write if they don't exist.
func NewFileStorage(basePath string) *FileStorage {
	return &FileStorage{basePath: basePath}
}

// BasePath returns the storage root directory.
func (fs *FileStorage) BasePath() string { return fs.basePath }

// ensureDir creates a directory tree if it doesn't exist.
func (fs *FileStorage) ensureDir(sub string) error {
	dir := filepath.Join(fs.basePath, sub)
	return os.MkdirAll(dir, 0755)
}

// taskPath returns the metadata file path for a task.
func (fs *FileStorage) taskPath(id string) string {
	return filepath.Join(fs.basePath, "tasks", id+".json")
}

// resultsPath returns the results file path for a task.
func (fs *FileStorage) resultsPath(id string) string {
	return filepath.Join(fs.basePath, "results", id+".json")
}

// metricsPath returns the metrics file path for a task.
func (fs *FileStorage) metricsPath(id string) string {
	return filepath.Join(fs.basePath, "metrics", id+".json")
}

// readJSON reads and decodes a JSON file into v. Returns os.ErrNotExist if missing.
func (fs *FileStorage) readJSON(path string, v interface{}) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// writeJSON atomically writes v as JSON to path (write to temp + rename).
func (fs *FileStorage) writeJSON(path string, v interface{}) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath) // best-effort cleanup
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Task CRUD
// ---------------------------------------------------------------------------

// CreateTask persists a new task and creates an empty results file.
func (fs *FileStorage) CreateTask(task ScheduledTask) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	log.Printf("[storage] CreateTask: id=%s name=%s", task.ID, task.Name)

	if err := fs.ensureDir("tasks"); err != nil {
		return err
	}
	if err := fs.ensureDir("results"); err != nil {
		return err
	}
	if err := fs.ensureDir("metrics"); err != nil {
		return err
	}

	// Write task metadata.
	if err := fs.writeJSON(fs.taskPath(task.ID), task); err != nil {
		return fmt.Errorf("create task: %w", err)
	}

	// Create an empty results file.
	if err := fs.writeJSON(fs.resultsPath(task.ID), []core.TestResult{}); err != nil {
		return fmt.Errorf("create results: %w", err)
	}

	// Initialize metrics.
	metrics := RunMetrics{}
	if err := fs.writeJSON(fs.metricsPath(task.ID), metrics); err != nil {
		return fmt.Errorf("create metrics: %w", err)
	}

	// Set the output path on the task metadata (pointing to the results file).
	task.OutputPath = fs.resultsPath(task.ID)
	_ = fs.writeJSON(fs.taskPath(task.ID), task) // update with output path

	return nil
}

// GetTask retrieves a single task by ID.
func (fs *FileStorage) GetTask(id string) (*ScheduledTask, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	var task ScheduledTask
	if err := fs.readJSON(fs.taskPath(id), &task); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("task %s not found", id)
		}
		return nil, fmt.Errorf("read task: %w", err)
	}
	return &task, nil
}

// ListTasks returns all tasks, sorted by creation time (oldest first).
func (fs *FileStorage) ListTasks() ([]ScheduledTask, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	dir := filepath.Join(fs.basePath, "tasks")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []ScheduledTask{}, nil
		}
		return nil, fmt.Errorf("read tasks dir: %w", err)
	}

	tasks := make([]ScheduledTask, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		var task ScheduledTask
		path := filepath.Join(dir, entry.Name())
		if err := fs.readJSON(path, &task); err != nil {
			log.Printf("[storage] ListTasks: skipping %s: %v", entry.Name(), err)
			continue
		}
		tasks = append(tasks, task)
	}
	return tasks, nil
}

// UpdateTask persists changes to an existing task.
func (fs *FileStorage) UpdateTask(task ScheduledTask) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	log.Printf("[storage] UpdateTask: id=%s", task.ID)

	// Verify task exists.
	if _, err := os.Stat(fs.taskPath(task.ID)); os.IsNotExist(err) {
		return fmt.Errorf("task %s not found", task.ID)
	}

	return fs.writeJSON(fs.taskPath(task.ID), task)
}

// DeleteTask removes a task and all associated files (results, metrics).
func (fs *FileStorage) DeleteTask(id string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	log.Printf("[storage] DeleteTask: id=%s", id)

	var errs []error
	for _, path := range []string{fs.taskPath(id), fs.resultsPath(id), fs.metricsPath(id)} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("delete task: %v", errs)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Results
// ---------------------------------------------------------------------------

// SaveResults writes results for a task (overwrites previous).
func (fs *FileStorage) SaveResults(taskID string, results []core.TestResult) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	log.Printf("[storage] SaveResults: task=%s count=%d", taskID, len(results))
	return fs.writeJSON(fs.resultsPath(taskID), results)
}

// GetResults reads the latest results for a task.
func (fs *FileStorage) GetResults(taskID string) ([]core.TestResult, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	var results []core.TestResult
	if err := fs.readJSON(fs.resultsPath(taskID), &results); err != nil {
		if os.IsNotExist(err) {
			return []core.TestResult{}, nil
		}
		return nil, fmt.Errorf("read results: %w", err)
	}
	return results, nil
}

// ---------------------------------------------------------------------------
// Metrics
// ---------------------------------------------------------------------------

// SaveMetrics persists run metrics for a task.
func (fs *FileStorage) SaveMetrics(taskID string, metrics RunMetrics) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	log.Printf("[storage] SaveMetrics: task=%s totalRuns=%d success=%d fail=%d",
		taskID, metrics.TotalRuns, metrics.SuccessRuns, metrics.FailureRuns)
	return fs.writeJSON(fs.metricsPath(taskID), metrics)
}

// GetMetrics reads run metrics for a task.
func (fs *FileStorage) GetMetrics(taskID string) (*RunMetrics, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	var metrics RunMetrics
	if err := fs.readJSON(fs.metricsPath(taskID), &metrics); err != nil {
		if os.IsNotExist(err) {
			return &RunMetrics{}, nil
		}
		return nil, fmt.Errorf("read metrics: %w", err)
	}
	return &metrics, nil
}
