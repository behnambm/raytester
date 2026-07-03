package core

import (
	"context"
	"fmt"
	"log"
	"runtime/debug"
	"sort"
	"sync"
	"time"

	"raytest/core/probe"
	"raytest/core/xray"
)

const MaxWorkers = 200

type Tester struct {
	cfg          Config
	hooks        Hooks
	stats        Stats
	mu           sync.RWMutex
	latencies    []time.Duration
	runningTotal time.Duration
}

func NewTester(cfg Config, hooks Hooks) *Tester {
	log.Printf("[core:Tester] NewTester: workers=%d maxLatency=%v xrayPath=%s", cfg.Workers, cfg.MaxLatency, cfg.XrayPath)
	return &Tester{
		cfg: cfg,
		hooks: hooks,
		stats: Stats{
			Status: StatusIdle,
		},
	}
}

func (t *Tester) GetStats() Stats {
	t.mu.RLock()
	defer t.mu.RUnlock()

	stats := t.stats
	if stats.TestedCount > 0 {
		stats.Progress = float64(stats.TestedCount) / float64(stats.TotalConfigs)
	}
	return stats
}

func (t *Tester) SetStatus(status string) {
	t.mu.Lock()
	t.stats.Status = status
	t.mu.Unlock()

	log.Printf("[core:Tester] SetStatus: status=%s", status)

	if t.hooks.OnStatsUpdate != nil {
			t.invokeHookSafe(t.hooks.OnStatsUpdate, t.GetStats())
		}
	}

	func (t *Tester) invokeHookSafe(hook func(Stats), stats Stats) {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[core:Tester] hook panic: %v\n%s", r, debug.Stack())
			}
		}()
		hook(stats)
	}

	func (t *Tester) updateStats(result TestResult) {
		t.mu.Lock()

		t.stats.TestedCount++

		if result.Error == "" {
			t.stats.SuccessCount++
			t.latencies = append(t.latencies, result.Latency)
			t.runningTotal += result.Latency

			if t.stats.MinLatency == 0 || result.Latency < t.stats.MinLatency {
				t.stats.MinLatency = result.Latency
			}
			if result.Latency > t.stats.MaxLatency {
				t.stats.MaxLatency = result.Latency
			}

			t.stats.AvgLatency = t.runningTotal / time.Duration(len(t.latencies))

			log.Printf("[core:Tester] updateStats: success #%d latency=%v country=%s", t.stats.SuccessCount, result.Latency, result.Country)
		} else {
			t.stats.FailCount++
			log.Printf("[core:Tester] updateStats: fail #%d err=%v", t.stats.FailCount, result.Error)
		}

		if t.stats.TotalConfigs > 0 {
			t.stats.Progress = float64(t.stats.TestedCount) / float64(t.stats.TotalConfigs)
		}

		// Copy stats before releasing lock to avoid GetStats() deadlock
		snapshot := t.stats
		t.mu.Unlock()

		if t.hooks.OnStatsUpdate != nil {
			t.invokeHookSafe(t.hooks.OnStatsUpdate, snapshot)
		}
	}

func (t *Tester) Run(ctx context.Context, configs []ProxyConfig) []TestResult {
	log.Printf("[core:Tester] Run: starting with %d configs", len(configs))

	workers := t.cfg.Workers
	if workers <= 0 {
		workers = DefaultWorkers
	}
	if workers > MaxWorkers {
		log.Printf("[core:Tester] Run: workers=%d exceeds maximum %d, clamping", workers, MaxWorkers)
		workers = MaxWorkers
	}

	total := len(configs)

	t.mu.Lock()
	t.stats.TotalConfigs = total
	t.stats.TestedCount = 0
	t.stats.SuccessCount = 0
	t.stats.FailCount = 0
	t.stats.Progress = 0
	t.stats.MinLatency = 0
	t.stats.MaxLatency = 0
	t.stats.AvgLatency = 0
	t.stats.Status = StatusTesting
	t.latencies = nil
	t.mu.Unlock()

	if t.hooks.OnStatsUpdate != nil {
		t.hooks.OnStatsUpdate(t.GetStats())
	}

	jobs := make(chan ProxyConfig, total)
	results := make(chan TestResult, total)

	for _, pc := range configs {
		jobs <- pc
	}
	close(jobs)

	var wg sync.WaitGroup
	for i := 1; i <= workers; i++ {
		wg.Add(1)
		go t.runWorker(ctx, i, jobs, results, &wg)
	}

	log.Printf("[core:Tester] Run: started %d workers, waiting for results", workers)

	go func() {
		wg.Wait()
		close(results)
	}()

	var collected []TestResult
loop:
	for r := range results {
		select {
		case <-ctx.Done():
			log.Printf("[core:Tester] Run: context cancelled, breaking result collection")
			break loop
		default:
		}

		collected = append(collected, r)

		t.updateStats(r)

		if t.hooks.OnTestComplete != nil {
			t.hooks.OnTestComplete(r)
		}
		if t.hooks.OnProgress != nil {
			t.hooks.OnProgress(Progress{Done: len(collected), Total: total})
		}
	}

	if ctx.Err() != nil {
		log.Printf("[core:Tester] Run: stopped by context cancellation")
		t.SetStatus(StatusStopped)
	} else {
		log.Printf("[core:Tester] Run: completed all %d configs, %d successful", total, t.stats.SuccessCount)
		t.SetStatus(StatusDone)
	}

	sort.Slice(collected, func(i, j int) bool {
		if collected[i].Error != "" && collected[j].Error == "" {
			return false
		}
		if collected[i].Error == "" && collected[j].Error != "" {
			return true
		}
		return collected[i].Latency < collected[j].Latency
	})

	if t.hooks.OnComplete != nil {
		t.hooks.OnComplete(collected)
	}

	return collected
}

func (t *Tester) runWorker(ctx context.Context, id int, jobs <-chan ProxyConfig, results chan<- TestResult, wg *sync.WaitGroup) {
	defer wg.Done()

	log.Printf("[core:Tester] runWorker-%d: started", id)

	p := probe.New()
	inst := xray.NewInstance(id)
	defer inst.Cleanup()

	processed := 0
	for pc := range jobs {
		select {
		case <-ctx.Done():
			log.Printf("[core:Tester] runWorker-%d: context cancelled, exiting (processed %d)", id, processed)
			return
		default:
		}

		if t.hooks.OnTestStart != nil {
			t.hooks.OnTestStart(pc)
		}

		result := t.testConfig(ctx, pc, p, inst)
		results <- result
		processed++
	}

	log.Printf("[core:Tester] runWorker-%d: done, processed %d configs", id, processed)
}

func (t *Tester) testConfig(ctx context.Context, pc ProxyConfig, p *probe.Probe, inst *xray.XrayInstance) TestResult {
	log.Printf("[core:Tester] testConfig: testing protocol=%s address=%s:%d", pc.Protocol, pc.Address, pc.Port)

	if err := inst.WriteConfig(pc); err != nil {
		log.Printf("[core:Tester] testConfig: WriteConfig FAILED: %v", err)
		return TestResult{Config: pc, Error: err.Error()}
	}

	if err := inst.Start(t.cfg.XrayPath); err != nil {
		log.Printf("[core:Tester] testConfig: Start FAILED: %v", err)
		return TestResult{Config: pc, Error: err.Error()}
	}

	if !inst.WaitReady(ReadinessTimeout) {
		log.Printf("[core:Tester] testConfig: WaitReady FAILED (timeout=%v)", ReadinessTimeout)
		inst.Stop()
		return TestResult{Config: pc, Error: "xray not ready"}
	}

	latency, err := p.Test(ctx, inst.Port)
	if err != nil {
		log.Printf("[core:Tester] testConfig: Probe.Test FAILED: %v", err)
		inst.Stop()
		return TestResult{Config: pc, Error: err.Error()}
	}

	log.Printf("[core:Tester] testConfig: latency=%v", latency)

	if latency > t.cfg.MaxLatency {
		log.Printf("[core:Tester] testConfig: latency %v exceeds max %v, skipping geo lookup", latency, t.cfg.MaxLatency)
		inst.Stop()
		return TestResult{Config: pc, Error: fmt.Sprintf("latency %v exceeds max %v", latency, t.cfg.MaxLatency)}
	}

	country, _ := p.GeoLookup(ctx, inst.Port)
	log.Printf("[core:Tester] testConfig: geo=%s/%s", country.Country, country.Name)
	inst.Stop()

	return TestResult{Config: pc, Latency: latency, Country: country.Country, CountryName: country.Name}
}
