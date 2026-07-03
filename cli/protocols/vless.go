package protocols

import (
	"net/url"
	"strconv"
	"strings"

	"raytest/core"
)

func ParseVLESS(raw string) (core.ProxyConfig, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return core.ProxyConfig{}, err
	}

	pc := core.ProxyConfig{
		Protocol: "vless",
		Raw:      raw,
		Address:  u.Hostname(),
		UUID:     u.User.Username(),
	}

	if port := u.Port(); port != "" {
		pc.Port = parsePort(port)
	}

	q := u.Query()
	pc.Security = q.Get("security")
	pc.Network = q.Get("type")
	pc.Path = q.Get("path")
	pc.Host = q.Get("host")
	pc.SNI = q.Get("sni")
	pc.AlterID = 0

	if pc.Security == "tls" || pc.Security == "reality" {
		pc.TLS = true
	}

	pc.Path = strings.TrimPrefix(pc.Path, "/")

	return pc, nil
}

func parsePort(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}
