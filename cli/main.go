package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/schollz/progressbar/v3"

	"raytest/cli/dedupe"
	"raytest/cli/logger"
	"raytest/cli/parser"
	"raytest/cli/subscription"
	"raytest/core"
)

func main() {
	url := flag.String("url", "", "Subscription URL (required)")
	maxLatency := flag.Duration("max-latency", core.DefaultMaxLatency, "Maximum allowed latency")
	workers := flag.Int("workers", core.DefaultWorkers, "Worker count")
	xrayPath := flag.String("xray-path", core.DefaultXrayPath, "Path to xray binary")
	flag.Parse()

	if *url == "" {
		flag.Usage()
		fmt.Println("error: --url is required")
		os.Exit(1)
	}

	logger.Info.Printf("Downloading subscription from %s", *url)
	content, err := subscription.Download(&subscription.DownloadConfig{URL: *url})
	if err != nil {
		logger.Error.Printf("Download failed: %v", err)
		os.Exit(1)
	}

	configs := parser.Parse(content)
	logger.Info.Printf("Parsed %d configs", len(configs))

	configs = dedupe.Deduplicate(configs)
	logger.Info.Printf("After dedupe: %d configs", len(configs))

	if len(configs) == 0 {
		logger.Info.Println("No configs to test")
		return
	}

	var bar *progressbar.ProgressBar

	hooks := core.Hooks{
		OnTestComplete: func(r core.TestResult) {
			country := r.Country
			countryName := r.CountryName
			if country == "" {
				country = "??"
			}
			if countryName == "" {
				countryName = "Unknown"
			}
			logger.Info.Printf("Working: %s (%v) [%s - %s]", r.Config.Raw[:40], r.Latency, country, countryName)
		},
		OnProgress: func(p core.Progress) {
			if bar == nil {
				bar = progressbar.NewOptions(p.Total,
					progressbar.OptionSetDescription("Testing"),
					progressbar.OptionSetWidth(40),
					progressbar.OptionShowCount(),
					progressbar.OptionShowIts(),
					progressbar.OptionSetTheme(progressbar.Theme{
						Saucer:        "=",
						SaucerHead:    ">",
						SaucerPadding: " ",
						BarStart:      "[",
						BarEnd:        "]",
					}),
				)
			}
			bar.Add(1)
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		logger.Info.Printf("Interrupted, stopping workers...")
		cancel()
	}()

	tester := core.NewTester(core.Config{
		MaxLatency: *maxLatency,
		Workers:    *workers,
		XrayPath:   *xrayPath,
	}, hooks)

	results := tester.Run(ctx, configs)

	if bar != nil {
		bar.Finish()
		fmt.Println()
	}

	working := 0
	for _, r := range results {
			if r.Error == "" {
			fmt.Println(r.Config.Raw)
			working++
		}
	}

	logger.Info.Printf("Done. %d/%d working configs", working, len(results))
}
