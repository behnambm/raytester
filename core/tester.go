package core

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"raytest/core/probe"
	"raytest/core/xray"
)

type Tester struct {
	cfg   Config
	hooks Hooks
}

func NewTester(cfg Config, hooks Hooks) *Tester {
	return &Tester{cfg: cfg, hooks: hooks}
}

func (t *Tester) Run(ctx context.Context, configs []ProxyConfig) []TestResult {
	workers := t.cfg.Workers
	if workers <= 0 {
		workers = DefaultWorkers
	}

	total := len(configs)
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

	go func() {
		wg.Wait()
		close(results)
	}()

	var collected []TestResult
	for r := range results {
		select {
		case <-ctx.Done():
			break
		default:
		}

		collected = append(collected, r)

		if r.Error == nil && t.hooks.OnTestComplete != nil {
			t.hooks.OnTestComplete(r)
		}
		if t.hooks.OnProgress != nil {
			t.hooks.OnProgress(Progress{Done: len(collected), Total: total})
		}
	}

	sort.Slice(collected, func(i, j int) bool {
		if collected[i].Error != nil && collected[j].Error == nil {
			return false
		}
		if collected[i].Error == nil && collected[j].Error != nil {
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

	p := probe.New()
	inst := xray.NewInstance(id)

	for pc := range jobs {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if t.hooks.OnTestStart != nil {
			t.hooks.OnTestStart(pc)
		}

		results <- t.testConfig(pc, p, inst)
	}
}

func (t *Tester) testConfig(pc ProxyConfig, p *probe.Probe, inst *xray.XrayInstance) TestResult {
	if err := inst.WriteConfig(pc); err != nil {
		return TestResult{Config: pc, Error: err}
	}

	if err := inst.Start(t.cfg.XrayPath); err != nil {
		return TestResult{Config: pc, Error: err}
	}

	if !inst.WaitReady(ReadinessTimeout) {
		inst.Stop()
		return TestResult{Config: pc, Error: fmt.Errorf("xray not ready")}
	}

	latency, err := p.Test(inst.Port)
	if err != nil {
		inst.Stop()
		return TestResult{Config: pc, Error: err}
	}

	if latency > t.cfg.MaxLatency {
		inst.Stop()
		return TestResult{Config: pc, Error: fmt.Errorf("latency %v exceeds max %v", latency, t.cfg.MaxLatency)}
	}

	country, _ := p.GeoLookup(inst.Port)
	inst.Stop()

	return TestResult{Config: pc, Latency: latency, Country: country.Country, CountryName: country.Name}
}
