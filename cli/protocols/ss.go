package protocols

import (
	"encoding/base64"
	"errors"
	"net/url"
	"strings"

	"raytest/core"
)

var ErrNotSS = errors.New("not a shadowsocks config")

func ParseSS(raw string) (core.ProxyConfig, error) {
	if !strings.HasPrefix(raw, "ss://") && !strings.HasPrefix(raw, "shadowsocks://") {
		return core.ProxyConfig{}, ErrNotSS
	}

	prefix := "ss://"
	if strings.HasPrefix(raw, "shadowsocks://") {
		prefix = "shadowsocks://"
	}

	content := strings.TrimPrefix(raw, prefix)

	parts := strings.SplitN(content, "#", 2)
	main := parts[0]

	var userInfo, hostPort string
	if idx := strings.LastIndex(main, "@"); idx != -1 {
		userInfo = main[:idx]
		hostPort = main[idx+1:]
	} else {
		decoded, err := base64.RawURLEncoding.DecodeString(main)
		if err != nil {
			decoded, err = base64.StdEncoding.DecodeString(main)
			if err != nil {
				return core.ProxyConfig{}, err
			}
		}
		full := string(decoded)
		if idx := strings.LastIndex(full, "@"); idx != -1 {
			userInfo = full[:idx]
			hostPort = full[idx+1:]
		}
	}

	pc := core.ProxyConfig{
		Protocol: "ss",
		Raw:      raw,
	}

	if userInfo != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(userInfo)
		if err != nil {
			decoded, err = base64.StdEncoding.DecodeString(userInfo)
			if err == nil {
				userInfo = string(decoded)
			}
		} else {
			userInfo = string(decoded)
		}

		if idx := strings.Index(userInfo, ":"); idx != -1 {
			pc.Method = userInfo[:idx]
			pc.Password = userInfo[idx+1:]
		}
	}

	if hostPort != "" {
		u, err := url.Parse("//" + hostPort)
		if err == nil {
			pc.Address = u.Hostname()
			pc.Port = parsePort(u.Port())
		}
	}

	return pc, nil
}
