package xray

import (
	"encoding/json"
	"fmt"
	"log"
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
	Protocol string `json:"protocol"`
	Address  string `json:"address"`
	Port     int    `json:"port"`
	UUID     string `json:"uuid"`
	AlterID  int    `json:"alter_id"`
	Security string `json:"security"`
	Network  string `json:"network"`
	Path     string `json:"path"`
	Host     string `json:"host"`
	TLS      bool   `json:"tls"`
	SNI      string `json:"sni"`
	Method   string `json:"method"`
	Password string `json:"password"`
	Raw      string `json:"raw"`
}

type XrayInstance struct {
	Port       int
	ConfigPath string
	Cmd        *exec.Cmd
}

func NewInstance(workerID int) *XrayInstance {
	port := BasePort + workerID
	configPath := filepath.Join(TempDir, fmt.Sprintf("worker-%d.json", workerID))
	log.Printf("[core:xray] NewInstance: workerID=%d port=%d configPath=%s", workerID, port, configPath)
	return &XrayInstance{
		Port:       port,
		ConfigPath: configPath,
	}
}

func (xi *XrayInstance) WriteConfig(pc ProxyConfig) error {
	log.Printf("[core:xray] WriteConfig: worker port=%d protocol=%s address=%s:%d", xi.Port, pc.Protocol, pc.Address, pc.Port)

	cfg := buildConfig(pc, xi.Port)
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		log.Printf("[core:xray] WriteConfig: JSON marshal FAILED: %v", err)
		return err
	}

	if err := os.MkdirAll(TempDir, 0755); err != nil {
		log.Printf("[core:xray] WriteConfig: mkdir FAILED: %v", err)
		return err
	}

	if err := os.WriteFile(xi.ConfigPath, data, 0644); err != nil {
		log.Printf("[core:xray] WriteConfig: write file FAILED: %v", err)
		return err
	}

	log.Printf("[core:xray] WriteConfig: SUCCESS, wrote %d bytes to %s", len(data), xi.ConfigPath)
	return nil
}

func (xi *XrayInstance) Start(xrayPath string) error {
	log.Printf("[core:xray] Start: port=%d xrayPath=%s configPath=%s", xi.Port, xrayPath, xi.ConfigPath)

	if xi.Cmd != nil && xi.Cmd.Process != nil {
		log.Printf("[core:xray] Start: killing previous process on port %d", xi.Port)
		xi.Cmd.Process.Kill()
		xi.Cmd.Wait()
	}

	xi.Cmd = exec.Command(xrayPath, "run", "-config", xi.ConfigPath)
	if err := xi.Cmd.Start(); err != nil {
		log.Printf("[core:xray] Start: exec FAILED: %v", err)
		return err
	}

	log.Printf("[core:xray] Start: SUCCESS, xray pid=%d", xi.Cmd.Process.Pid)
	return nil
}

func (xi *XrayInstance) Stop() error {
	log.Printf("[core:xray] Stop: port=%d", xi.Port)
	if xi.Cmd != nil && xi.Cmd.Process != nil {
		err := xi.Cmd.Process.Kill()
		log.Printf("[core:xray] Stop: killed process, err=%v", err)
		return err
	}
	return nil
}

func (xi *XrayInstance) WaitReady(timeout time.Duration) bool {
	log.Printf("[core:xray] WaitReady: port=%d timeout=%v", xi.Port, timeout)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if isPortOpen(xi.Port) {
			log.Printf("[core:xray] WaitReady: port %d is OPEN", xi.Port)
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	log.Printf("[core:xray] WaitReady: port %d TIMED OUT after %v", xi.Port, timeout)
	return false
}

func (xi *XrayInstance) Cleanup() {
	log.Printf("[core:xray] Cleanup: port=%d configPath=%s", xi.Port, xi.ConfigPath)
	xi.Stop()
	if err := os.Remove(xi.ConfigPath); err != nil {
		log.Printf("[core:xray] Cleanup: remove config file err=%v", err)
	}
}

func isPortOpen(port int) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 200*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func safeUUID(uuid string) string {
	if len(uuid) >= 8 {
		return uuid[:8] + "..."
	}
	return "(empty)"
}

func buildConfig(pc ProxyConfig, port int) map[string]interface{} {
	log.Printf("[core:xray] buildConfig: protocol=%s address=%s:%d port=%d", pc.Protocol, pc.Address, pc.Port, port)

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
	log.Printf("[core:xray] buildOutbound: protocol=%s", pc.Protocol)
	switch pc.Protocol {
	case "vless":
		return buildVLESSOutbound(pc)
	case "vmess":
		return buildVMessOutbound(pc)
	case "ss":
		return buildSSOutbound(pc)
	default:
		log.Printf("[core:xray] buildOutbound: unknown protocol=%s", pc.Protocol)
		return nil
	}
}

func buildVLESSOutbound(pc ProxyConfig) map[string]interface{} {
	log.Printf("[core:xray] buildVLESSOutbound: address=%s:%d uuid=%s", pc.Address, pc.Port, safeUUID(pc.UUID))

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
		"protocol":       "vless",
		"settings":       settings,
		"streamSettings": streamSettings,
	}
}

func buildVMessOutbound(pc ProxyConfig) map[string]interface{} {
	log.Printf("[core:xray] buildVMessOutbound: address=%s:%d uuid=%s", pc.Address, pc.Port, safeUUID(pc.UUID))

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
		"protocol":       "vmess",
		"settings":       settings,
		"streamSettings": streamSettings,
	}
}

func buildSSOutbound(pc ProxyConfig) map[string]interface{} {
	log.Printf("[core:xray] buildSSOutbound: address=%s:%d method=%s", pc.Address, pc.Port, pc.Method)

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
