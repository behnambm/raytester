package parser

import (
	"strings"

	"raytest/cli/protocols"
	"raytest/core"
)

func Parse(content string) []core.ProxyConfig {
	lines := strings.Split(content, "\n")
	var configs []core.ProxyConfig

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		protocol := detectProtocol(line)
		switch protocol {
		case "vless":
			if pc, err := protocols.ParseVLESS(line); err == nil {
				configs = append(configs, pc)
			}
		case "vmess":
			if pc, err := protocols.ParseVMess(line); err == nil {
				configs = append(configs, pc)
			}
		case "ss", "shadowsocks":
			if pc, err := protocols.ParseSS(line); err == nil {
				configs = append(configs, pc)
			}
		}
	}

	return configs
}

func detectProtocol(line string) string {
	if strings.HasPrefix(line, "vless://") {
		return "vless"
	}
	if strings.HasPrefix(line, "vmess://") {
		return "vmess"
	}
	if strings.HasPrefix(line, "ss://") || strings.HasPrefix(line, "shadowsocks://") {
		return "ss"
	}
	return ""
}
