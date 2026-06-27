package xray

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

const (
	BasePort = 30000
	TempDir  = "/tmp/xray-subscription-tester"
)

type ProxyConfig struct {
	Protocol string
	Address  string
	Port     int
	UUID     string
	AlterID  int
	Security string
	Network  string
	Path     string
	Host     string
	TLS      bool
	SNI      string
	Method   string
	Password string
	Raw      string
}

type XrayInstance struct {
	Port       int
	ConfigPath string
	Cmd        *exec.Cmd
}

func NewInstance(workerID int) *XrayInstance {
	port := BasePort + workerID
	configPath := filepath.Join(TempDir, fmt.Sprintf("worker-%d.json", workerID))
	return &XrayInstance{
		Port:       port,
		ConfigPath: configPath,
	}
}

func (xi *XrayInstance) WriteConfig(pc ProxyConfig) error {
	cfg := buildConfig(pc, xi.Port)
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	if err := os.MkdirAll(TempDir, 0755); err != nil {
		return err
	}

	return os.WriteFile(xi.ConfigPath, data, 0644)
}

func (xi *XrayInstance) Start(xrayPath string) error {
	if xi.Cmd != nil && xi.Cmd.Process != nil {
		xi.Cmd.Process.Kill()
		xi.Cmd.Wait()
	}

	xi.Cmd = exec.Command(xrayPath, "run", "-config", xi.ConfigPath)
	return xi.Cmd.Start()
}

func (xi *XrayInstance) Stop() error {
	if xi.Cmd != nil && xi.Cmd.Process != nil {
		return xi.Cmd.Process.Kill()
	}
	return nil
}

func (xi *XrayInstance) WaitReady(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if isPortOpen(xi.Port) {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

func (xi *XrayInstance) Cleanup() {
	xi.Stop()
	os.Remove(xi.ConfigPath)
}

func isPortOpen(port int) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 200*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func buildConfig(pc ProxyConfig, port int) map[string]interface{} {
	outbound := buildOutbound(pc)
	inbound := map[string]interface{}{
		"port":     port,
		"protocol": "socks",
		"settings": map[string]interface{}{
			"auth": "noauth",
			"udp":  true,
		},
	}

	return map[string]interface{}{
		"inbounds":  []interface{}{inbound},
		"outbounds": []interface{}{outbound},
	}
}

func buildOutbound(pc ProxyConfig) map[string]interface{} {
	switch pc.Protocol {
	case "vless":
		return buildVLESSOutbound(pc)
	case "vmess":
		return buildVMessOutbound(pc)
	case "ss":
		return buildSSOutbound(pc)
	default:
		return nil
	}
}

func buildVLESSOutbound(pc ProxyConfig) map[string]interface{} {
	settings := map[string]interface{}{
		"vnext": []interface{}{
			map[string]interface{}{
				"address": pc.Address,
				"port":    pc.Port,
				"users": []interface{}{
					map[string]interface{}{
						"id":         pc.UUID,
						"encryption": "none",
						"flow":       "",
					},
				},
			},
		},
	}

	streamSettings := map[string]interface{}{
		"network":  pc.Network,
		"security": pc.Security,
	}

	if pc.Network == "ws" || pc.Network == "http" {
		streamSettings["wsSettings"] = map[string]interface{}{
			"path": pc.Path,
			"headers": map[string]interface{}{
				"Host": pc.Host,
			},
		}
	}

	if pc.TLS {
		streamSettings["tlsSettings"] = map[string]interface{}{
			"serverName":    pc.SNI,
			"allowInsecure": true,
		}
	}

	if pc.Security == "reality" {
		streamSettings["realitySettings"] = map[string]interface{}{
			"serverName": pc.SNI,
		}
	}

	return map[string]interface{}{
		"protocol":      "vless",
		"settings":      settings,
		"streamSettings": streamSettings,
	}
}

func buildVMessOutbound(pc ProxyConfig) map[string]interface{} {
	settings := map[string]interface{}{
		"vnext": []interface{}{
			map[string]interface{}{
				"address": pc.Address,
				"port":    pc.Port,
				"users": []interface{}{
					map[string]interface{}{
						"id":       pc.UUID,
						"alterId":  pc.AlterID,
						"security": pc.Security,
					},
				},
			},
		},
	}

	streamSettings := map[string]interface{}{
		"network":  pc.Network,
		"security": "none",
	}

	if pc.Network == "ws" || pc.Network == "http" {
		streamSettings["wsSettings"] = map[string]interface{}{
			"path": pc.Path,
			"headers": map[string]interface{}{
				"Host": pc.Host,
			},
		}
	}

	if pc.TLS {
		streamSettings["security"] = "tls"
		streamSettings["tlsSettings"] = map[string]interface{}{
			"serverName":    pc.SNI,
			"allowInsecure": true,
		}
	}

	return map[string]interface{}{
		"protocol":      "vmess",
		"settings":      settings,
		"streamSettings": streamSettings,
	}
}

func buildSSOutbound(pc ProxyConfig) map[string]interface{} {
	return map[string]interface{}{
		"protocol": "shadowsocks",
		"settings": map[string]interface{}{
			"servers": []interface{}{
				map[string]interface{}{
					"address":  pc.Address,
					"port":     pc.Port,
					"method":   pc.Method,
					"password": pc.Password,
				},
			},
		},
	}
}
