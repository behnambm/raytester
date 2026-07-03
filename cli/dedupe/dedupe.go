package dedupe

import (
	"crypto/sha256"
	"fmt"
	"strings"

	"raytest/core"
)

func Deduplicate(configs []core.ProxyConfig) []core.ProxyConfig {
	seen := make(map[string]bool)
	var result []core.ProxyConfig

	for _, pc := range configs {
		fp := fingerprint(pc)
		if !seen[fp] {
			seen[fp] = true
			result = append(result, pc)
		}
	}

	if len(result) > core.MaxConfigs {
		result = result[:core.MaxConfigs]
	}

	return result
}

func fingerprint(pc core.ProxyConfig) string {
	var parts []string
	parts = append(parts, pc.Protocol)
	parts = append(parts, strings.ToLower(strings.TrimSpace(pc.Address)))
	parts = append(parts, fmt.Sprintf("%d", pc.Port))
	parts = append(parts, pc.UUID)
	parts = append(parts, fmt.Sprintf("%d", pc.AlterID))
	parts = append(parts, pc.Security)
	parts = append(parts, pc.Network)
	parts = append(parts, pc.Path)
	parts = append(parts, strings.ToLower(strings.TrimSpace(pc.Host)))
	parts = append(parts, strings.ToLower(strings.TrimSpace(pc.SNI)))
	parts = append(parts, pc.Method)
	parts = append(parts, pc.Password)
	parts = append(parts, fmt.Sprintf("%t", pc.TLS))

	normalized := strings.Join(parts, "|")
	hash := sha256.Sum256([]byte(normalized))
	return fmt.Sprintf("%x", hash)
}
