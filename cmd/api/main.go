package main

import (
	"flag"
	"log"
	"os"
	"path/filepath"

	"raytest/api"
	"raytest/core"
)

func main() {
	addr := flag.String("addr", ":4433", "HTTP listen address")
	frontendDir := flag.String("frontend", "frontend", "Frontend directory")
	dataDir := flag.String("data-dir", defaultDataDir(), "Data directory for scheduler storage")
	maxLatency := flag.Duration("max-latency", core.DefaultMaxLatency, "Maximum allowed latency")
	workers := flag.Int("workers", core.DefaultWorkers, "Worker count")
	xrayPath := flag.String("xray-path", core.DefaultXrayPath, "Path to xray binary")
	apiKey := flag.String("api-key", "", "API key for authentication (empty = no auth, for local use only)")
	flag.Parse()

	cfg := core.Config{
		MaxLatency: *maxLatency,
		Workers:    *workers,
		XrayPath:   *xrayPath,
	}

	server := api.NewServer(cfg, *dataDir, *apiKey)
	log.Fatal(server.Start(*addr, *frontendDir))
}

func defaultDataDir() string {
	if dir := os.Getenv("RAYTESTER_DATA_DIR"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".raytester/scheduler"
	}
	return filepath.Join(home, ".raytester", "scheduler")
}
