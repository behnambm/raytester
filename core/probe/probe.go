package probe

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"time"
)

const ProbeURL = "http://www.gstatic.com/generate_204"
const GeoIPURL = "https://get.geojs.io/v1/ip/country.json"

type Probe struct {
	Client *http.Client
}

func New() *Probe {
	log.Printf("[core:probe] New: creating probe")
	return &Probe{
		Client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (p *Probe) Test(port int) (time.Duration, error) {
	log.Printf("[core:probe] Test: probing port=%d", port)

	proxyURL, _ := url.Parse(fmt.Sprintf("socks5://127.0.0.1:%d", port))
	transport := &http.Transport{
		Proxy: http.ProxyURL(proxyURL),
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   10 * time.Second,
	}

	start := time.Now()
	resp, err := client.Get(ProbeURL)
	latency := time.Since(start)

	if err != nil {
		log.Printf("[core:probe] Test: request FAILED after %v: %v", latency, err)
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		log.Printf("[core:probe] Test: unexpected status=%d", resp.StatusCode)
		return 0, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	log.Printf("[core:probe] Test: SUCCESS latency=%v", latency)
	return latency, nil
}

type GeoResult struct {
	Country string
	Name    string
}

func (p *Probe) GeoLookup(port int) (GeoResult, error) {
	log.Printf("[core:probe] GeoLookup: looking up geo via port=%d", port)

	proxyURL, _ := url.Parse(fmt.Sprintf("socks5://127.0.0.1:%d", port))
	transport := &http.Transport{
		Proxy: http.ProxyURL(proxyURL),
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   10 * time.Second,
	}

	resp, err := client.Get(GeoIPURL)
	if err != nil {
		log.Printf("[core:probe] GeoLookup: FAILED: %v", err)
		return GeoResult{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("[core:probe] GeoLookup: unexpected status=%d", resp.StatusCode)
		return GeoResult{}, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	var result struct {
		Country string `json:"country"`
		Name    string `json:"name"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Printf("[core:probe] GeoLookup: JSON decode FAILED: %v", err)
		return GeoResult{}, err
	}

	log.Printf("[core:probe] GeoLookup: SUCCESS country=%s name=%s", result.Country, result.Name)
	return GeoResult{Country: result.Country, Name: result.Name}, nil
}
