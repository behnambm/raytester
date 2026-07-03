package scheduler

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/robfig/cron/v3"

	"raytest/api/storage"
	"raytest/cli/dedupe"
	"raytest/cli/parser"
	"raytest/cli/subscription"
	"raytest/core"
)

const defaultRunTimeout = 10 * time.Minute

var allowedXrayPaths = []string{
	"/usr/local/bin/xray",
	"/usr/bin/xray",
	"/opt/xray/xray",
}

// Scheduler manages scheduled proxy testing tasks.
type Scheduler struct {
	store     storage.Storage
	cron      *cron.Cron
	entries   map[string]cron.EntryID // taskID → cron entry
	serverCfg core.Config
	running   map[string]bool
	nextID    atomic.Int64
	mu        sync.RWMutex
}

// New creates a new Scheduler.
func New(store storage.Storage, serverCfg core.Config) *Scheduler {
	return &Scheduler{
		store:     store,
		serverCfg: serverCfg,
		entries:   make(map[string]cron.EntryID),
		running:   make(map[string]bool),
	}
}

// Start begins the cron engine and loads all enabled tasks from storage.
func (s *Scheduler) Start() error {
	s.cron = cron.New(cron.WithSeconds()) // support 6-field cron for flexibility

	// Load and schedule all enabled tasks from storage.
	tasks, err := s.store.ListTasks()
	if err != nil {
		return fmt.Errorf("scheduler start: list tasks: %w", err)
	}

	log.Printf("[scheduler] Start: loading %d tasks from storage", len(tasks))
	scheduled := 0
	for _, task := range tasks {
		if !task.Enabled {
			continue
		}
		if err := s.scheduleTask(task); err != nil {
			log.Printf("[scheduler] Start: failed to schedule task %s (%s): %v", task.ID, task.Name, err)
			continue
		}
		scheduled++
	}
	log.Printf("[scheduler] Start: scheduled %d/%d enabled tasks", scheduled, len(tasks))

	s.cron.Start()
	log.Printf("[scheduler] Start: cron engine started")
	return nil
}

// Stop gracefully stops the cron engine, waiting for running jobs to finish.
func (s *Scheduler) Stop() {
	log.Printf("[scheduler] Stop: stopping cron engine")
	if s.cron != nil {
		<-s.cron.Stop().Done()
	}
	log.Printf("[scheduler] Stop: cron engine stopped")
}

// nextTaskID generates a unique task ID.
func (s *Scheduler) nextTaskID() string {
	return strconv.FormatInt(s.nextID.Add(1), 10)
}

// AddTask persists a task and schedules it if enabled.
// task.ID and task.CreatedAt are filled in if empty.
func (s *Scheduler) AddTask(task *storage.ScheduledTask) error {
	if task.ID == "" {
		task.ID = s.nextTaskID()
	}
	task.CreatedAt = time.Now()
	task.UpdatedAt = task.CreatedAt

	log.Printf("[scheduler] AddTask: id=%s name=%s cron=%s enabled=%v", task.ID, task.Name, task.CronExpr, task.Enabled)

	if err := s.store.CreateTask(*task); err != nil {
		return fmt.Errorf("add task: %w", err)
	}

	if task.Enabled {
		if err := s.scheduleTask(*task); err != nil {
			return fmt.Errorf("schedule new task: %w", err)
		}
	}
	return nil
}

// UpdateTask persists changes and reschedules the task.
func (s *Scheduler) UpdateTask(task storage.ScheduledTask) error {
	task.UpdatedAt = time.Now()

	log.Printf("[scheduler] UpdateTask: id=%s enabled=%v cron=%s", task.ID, task.Enabled, task.CronExpr)

	// Unschedule old entry.
	s.unscheduleTask(task.ID)

	if err := s.store.UpdateTask(task); err != nil {
		return fmt.Errorf("update task: %w", err)
	}

	if task.Enabled {
		if err := s.scheduleTask(task); err != nil {
			log.Printf("[scheduler] UpdateTask: failed to schedule: %v", err)
			// Don't fail the update — the task is persisted, just won't run.
		}
	}
	return nil
}

// RemoveTask unschedules and deletes a task from storage.
func (s *Scheduler) RemoveTask(id string) error {
	log.Printf("[scheduler] RemoveTask: id=%s", id)
	s.unscheduleTask(id)
	return s.store.DeleteTask(id)
}

// RunTaskNow triggers an immediate asynchronous execution of a task.
func (s *Scheduler) RunTaskNow(id string) error {
	task, err := s.store.GetTask(id)
	if err != nil {
		return fmt.Errorf("run task: %w", err)
	}

	log.Printf("[scheduler] RunTaskNow: id=%s name=%s", id, task.Name)

	go s.executeTask(*task)
	return nil
}

// GetTask returns a task by ID.
func (s *Scheduler) GetTask(id string) (*storage.ScheduledTask, error) {
	return s.store.GetTask(id)
}

// ListTasks returns all tasks.
func (s *Scheduler) ListTasks() ([]storage.ScheduledTask, error) {
	return s.store.ListTasks()
}

// GetResults returns the latest results for a task.
func (s *Scheduler) GetResults(taskID string) ([]core.TestResult, error) {
	return s.store.GetResults(taskID)
}

// GetMetrics returns run metrics for a task.
func (s *Scheduler) GetMetrics(taskID string) (*storage.RunMetrics, error) {
	return s.store.GetMetrics(taskID)
}

// IsRunning returns whether a task is currently executing.
func (s *Scheduler) IsRunning(taskID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running[taskID]
}

// NextRun returns the next scheduled run time for a task, or nil if not scheduled.
func (s *Scheduler) NextRun(taskID string) *time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entryID, ok := s.entries[taskID]
	if !ok {
		return nil
	}
	for _, entry := range s.cron.Entries() {
		if entry.ID == entryID {
			t := entry.Next
			return &t
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Internal
// ---------------------------------------------------------------------------

// scheduleTask adds a cron entry for the given task.
func (s *Scheduler) scheduleTask(task storage.ScheduledTask) error {
	taskID := task.ID
	s.mu.Lock()
	defer s.mu.Unlock()

	// If already scheduled, remove first.
	if entryID, ok := s.entries[taskID]; ok {
		s.cron.Remove(entryID)
	}

	// Capture task by value so the closure is safe.
	t := task
	entryID, err := s.cron.AddFunc(t.CronExpr, func() {
		s.executeTask(t)
	})
	if err != nil {
		return fmt.Errorf("cron add: %w", err)
	}

	s.entries[taskID] = entryID
	log.Printf("[scheduler] scheduleTask: id=%s cron=%s entry=%d", taskID, t.CronExpr, entryID)
	return nil
}

// unscheduleTask removes a cron entry for the given task.
func (s *Scheduler) unscheduleTask(taskID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if entryID, ok := s.entries[taskID]; ok {
		s.cron.Remove(entryID)
		delete(s.entries, taskID)
		log.Printf("[scheduler] unscheduleTask: id=%s entry=%d", taskID, entryID)
	}
}

// executeTask runs the full pipeline for a scheduled task.
// It is designed to be called from a goroutine and recovers from panics.
func (s *Scheduler) executeTask(task storage.ScheduledTask) {
	// Mark as running.
	s.mu.Lock()
	if s.running[task.ID] {
		s.mu.Unlock()
		log.Printf("[scheduler] executeTask: task %s (%s) is already running, skipping", task.ID, task.Name)
		return
	}
	s.running[task.ID] = true
	s.mu.Unlock()

	// Single defer that handles panic recovery AND always clears the running flag.
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[scheduler] executeTask: PANIC in task %s (%s): %v", task.ID, task.Name, r)
			s.recordFailure(task)
		}
		s.mu.Lock()
		delete(s.running, task.ID)
		s.mu.Unlock()
	}()

	log.Printf("[scheduler] executeTask: starting task %s (%s)", task.ID, task.Name)
	startTime := time.Now()

	// Build task-specific config.
	cfg := s.serverCfg
	if task.MaxLatency != "" {
		if d, err := time.ParseDuration(task.MaxLatency); err == nil {
			cfg.MaxLatency = d
		}
	}
	if task.Workers > 0 {
		cfg.Workers = task.Workers
	}
	if task.XrayPath != "" {
		if err := validateXrayPath(task.XrayPath); err != nil {
			log.Printf("[scheduler] executeTask: task=%s invalid xray_path: %v", task.ID, err)
			s.recordFailure(task)
			return
		}
		cfg.XrayPath = task.XrayPath
	}

	// Validate the URL before downloading (SSRF protection).
	if err := validateURL(task.URL); err != nil {
		log.Printf("[scheduler] executeTask: task=%s invalid URL: %v", task.ID, err)
		s.recordFailure(task)
		return
	}

	// Create context with timeout.
	ctx, cancel := context.WithTimeout(context.Background(), defaultRunTimeout)
	defer cancel()

	// Step 1: Download subscription.
	log.Printf("[scheduler] executeTask: task=%s downloading %s", task.ID, task.URL)
	content, err := subscription.Download(&subscription.DownloadConfig{URL: task.URL})
	if err != nil {
		log.Printf("[scheduler] executeTask: task=%s download FAILED: %v", task.ID, err)
		s.recordFailure(task)
		return
	}
	log.Printf("[scheduler] executeTask: task=%s downloaded %d bytes", task.ID, len(content))

	// Step 2: Parse configs.
	configs := parser.Parse(content)
	log.Printf("[scheduler] executeTask: task=%s parsed %d configs", task.ID, len(configs))

	// Step 3: Deduplicate.
	configs = dedupe.Deduplicate(configs)
	log.Printf("[scheduler] executeTask: task=%s after dedupe: %d configs", task.ID, len(configs))

	if len(configs) == 0 {
		log.Printf("[scheduler] executeTask: task=%s no configs to test", task.ID)
		s.recordRun(task, startTime, nil)
		return
	}

	// Step 4: Run tester.
	log.Printf("[scheduler] executeTask: task=%s running tester with %d configs", task.ID, len(configs))
	tester := core.NewTester(cfg, core.Hooks{})
	results := tester.Run(ctx, configs)
	log.Printf("[scheduler] executeTask: task=%s tester returned %d results", task.ID, len(results))

	// Step 5: Save results.
	if err := s.store.SaveResults(task.ID, results); err != nil {
		log.Printf("[scheduler] executeTask: task=%s save results FAILED: %v", task.ID, err)
	}

	// Step 6: Update metrics.
	s.recordRun(task, startTime, results)
}

// recordRun updates metrics after a successful pipeline execution.
func (s *Scheduler) recordRun(task storage.ScheduledTask, startTime time.Time, results []core.TestResult) {
	metrics, err := s.store.GetMetrics(task.ID)
	if err != nil {
		log.Printf("[scheduler] recordRun: task=%s get metrics FAILED: %v", task.ID, err)
		metrics = &storage.RunMetrics{}
	}

	duration := time.Since(startTime)
	now := time.Now()
	successCount := 0
	for _, r := range results {
		if r.Error == "" {
			successCount++
		}
	}

	metrics.TotalRuns++
	metrics.CompletedRuns++ // the pipeline completed (download + test succeeded)
	metrics.LastRunTime = &now
	metrics.LastRunDuration = duration
	metrics.LastResultCount = successCount

	// Running average.
	if metrics.AvgRunDuration == 0 {
		metrics.AvgRunDuration = duration
	} else {
		// Weighted moving average (90% old, 10% new).
		n := float64(metrics.TotalRuns)
		oldWeight := (n - 1) / n
		newWeight := 1.0 / n
		metrics.AvgRunDuration = time.Duration(float64(metrics.AvgRunDuration)*oldWeight + float64(duration)*newWeight)
	}

	if err := s.store.SaveMetrics(task.ID, *metrics); err != nil {
		log.Printf("[scheduler] recordRun: task=%s save metrics FAILED: %v", task.ID, err)
	}

	log.Printf("[scheduler] recordRun: task=%s duration=%v success=%d/%d totalRuns=%d",
		task.ID, duration.Round(time.Millisecond), successCount, len(results), metrics.TotalRuns)
}

// recordFailure records a failed run (download/parse error).
func (s *Scheduler) recordFailure(task storage.ScheduledTask) {
	metrics, err := s.store.GetMetrics(task.ID)
	if err != nil {
		log.Printf("[scheduler] recordFailure: task=%s get metrics FAILED: %v", task.ID, err)
		metrics = &storage.RunMetrics{}
	}

	now := time.Now()
	metrics.TotalRuns++
	metrics.FailureRuns++
	metrics.LastRunTime = &now

	if err := s.store.SaveMetrics(task.ID, *metrics); err != nil {
		log.Printf("[scheduler] recordFailure: task=%s save metrics FAILED: %v", task.ID, err)
	}

	log.Printf("[scheduler] recordFailure: task=%s totalRuns=%d failures=%d",
		task.ID, metrics.TotalRuns, metrics.FailureRuns)
}

// ---------------------------------------------------------------------------
// Validation helpers (shared with handlers)
// ---------------------------------------------------------------------------

func validateXrayPath(path string) error {
	for _, allowed := range allowedXrayPaths {
		if path == allowed {
			return nil
		}
	}
	resolved, err := exec.LookPath("xray")
	if err != nil {
		return fmt.Errorf("xray binary not found in PATH")
	}
	if resolved != path {
		return fmt.Errorf("xray binary path not allowed: %s", path)
	}
	return nil
}

func validateURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("unsupported URL scheme: %s (only http/https allowed)", parsed.Scheme)
	}

	host := parsed.Hostname()
	if host == "" {
		return fmt.Errorf("URL has no host")
	}

	ips, err := net.LookupIP(host)
	if err != nil {
		log.Printf("[scheduler] validateURL: DNS lookup failed for %s: %v", host, err)
		return nil
	}

	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return fmt.Errorf("URL resolves to a private/internal address (%s): %s", ip.String(), host)
		}
	}
	return nil
}
