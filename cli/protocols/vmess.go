package protocols

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"raytest/core"
)

var ErrNotVMess = errors.New("not a vmess config")

type vmessJSON struct {
	Add  string `json:"add"`
	Port string `json:"port"`
	ID   string `json:"id"`
	Aid  string `json:"aid"`
	Scy  string `json:"scy"`
	Net  string `json:"net"`
	Path string `json:"path"`
	Host string `json:"host"`
	TLS  string `json:"tls"`
	Sni  string `json:"sni"`
	Type string `json:"type"`
}

func ParseVMess(raw string) (core.ProxyConfig, error) {
	if !strings.HasPrefix(raw, "vmess://") {
		return core.ProxyConfig{}, ErrNotVMess
	}

	b64 := strings.TrimPrefix(raw, "vmess://")
	decoded, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return core.ProxyConfig{}, err
	}

	var v vmessJSON
	if err := json.Unmarshal(decoded, &v); err != nil {
		return core.ProxyConfig{}, err
	}

	pc := core.ProxyConfig{
		Protocol: "vmess",
		Raw:      raw,
		Address:  v.Add,
		Port:     parsePort(v.Port),
		UUID:     v.ID,
		AlterID:  parseInt(v.Aid),
		Security: v.Scy,
		Network:  v.Net,
		Path:     strings.TrimPrefix(v.Path, "/"),
		Host:     v.Host,
		SNI:      v.Sni,
	}

	if v.TLS == "tls" || v.TLS == "1" || v.TLS == "true" {
		pc.TLS = true
	}

	if v.Type == "http" {
		pc.Network = "http"
	}

	return pc, nil
}

func parseInt(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}
