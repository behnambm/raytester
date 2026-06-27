package subscription

import (
	"encoding/base64"
	"io"
	"net/http"
	"strings"

	"raytest/core"
)

type DownloadConfig struct {
	URL string
}

func Download(cfg *DownloadConfig) (string, error) {
	client := &http.Client{}
	req, err := http.NewRequest("GET", cfg.URL, nil)
	if err != nil {
		return "", err
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", io.EOF
	}

	limited := io.LimitReader(resp.Body, core.MaxBodySize)
	body, err := io.ReadAll(limited)
	if err != nil {
		return "", err
	}

	content := string(body)
	return detectBase64(content), nil
}

func detectBase64(content string) string {
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(content))
	if err != nil {
		return content
	}

	decodedStr := string(decoded)
	if strings.Contains(decodedStr, "://") {
		return decodedStr
	}

	return content
}
