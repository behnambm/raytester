package probe

import (
	"encoding/json"
	"fmt"
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
	return &Probe{
		Client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (p *Probe) Test(port int) (time.Duration, error) {
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
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		return 0, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	return latency, nil
}

type GeoResult struct {
	Country string
	Name    string
}

func (p *Probe) GeoLookup(port int) (GeoResult, error) {
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
		return GeoResult{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return GeoResult{}, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	var result struct {
		Country string `json:"country"`
		Name    string `json:"name"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return GeoResult{}, err
	}

	return GeoResult{Country: result.Country, Name: result.Name}, nil
}