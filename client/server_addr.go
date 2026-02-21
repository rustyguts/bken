package main

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

const defaultServerPort = "8080"

// normalizeServerAddr accepts host, host:port, IPv6, bken:// links, and
// http(s) URLs and returns a canonical host:port for transport dialing.
func normalizeServerAddr(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", fmt.Errorf("server address is required")
	}

	if strings.HasPrefix(s, "bken://") {
		s = strings.TrimPrefix(s, "bken://")
	}

	if strings.Contains(s, "://") {
		u, err := url.Parse(s)
		if err != nil {
			return "", fmt.Errorf("invalid server address: %w", err)
		}
		if u.Host == "" {
			return "", fmt.Errorf("invalid server address: missing host")
		}
		s = u.Host
	}

	// Ignore accidental trailing slashes/paths in manual input.
	if i := strings.IndexByte(s, '/'); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return "", fmt.Errorf("invalid server address: missing host")
	}

	host := s
	port := defaultServerPort

	if h, p, err := net.SplitHostPort(s); err == nil {
		host = h
		port = p
	} else {
		// Raw IPv6 (without brackets): treat as host-only.
		if ip := net.ParseIP(s); ip != nil && strings.Contains(s, ":") {
			host = s
			port = defaultServerPort
		} else if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
			// Bracketed IPv6 without port.
			host = strings.TrimPrefix(strings.TrimSuffix(s, "]"), "[")
			port = defaultServerPort
		} else if strings.Contains(s, ":") {
			// Looks like host:port but split failed.
			return "", fmt.Errorf("invalid server address: %q", raw)
		}
	}

	if host == "" {
		return "", fmt.Errorf("invalid server address: missing host")
	}

	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return "", fmt.Errorf("invalid server port: %q", port)
	}

	return net.JoinHostPort(host, strconv.Itoa(n)), nil
}
