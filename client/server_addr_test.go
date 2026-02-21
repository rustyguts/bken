package main

import (
	"testing"
)

func TestNormalizeServerAddrPlainHostname(t *testing.T) {
	addr, err := normalizeServerAddr("myserver")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if addr != "myserver:8080" {
		t.Errorf("expected 'myserver:8080', got %q", addr)
	}
}

func TestNormalizeServerAddrWithPort(t *testing.T) {
	addr, err := normalizeServerAddr("myserver:5000")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if addr != "myserver:5000" {
		t.Errorf("expected 'myserver:5000', got %q", addr)
	}
}

func TestNormalizeServerAddrBkenPrefix(t *testing.T) {
	addr, err := normalizeServerAddr("bken://192.168.1.10:8080")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if addr != "192.168.1.10:8080" {
		t.Errorf("expected '192.168.1.10:8080', got %q", addr)
	}
}

func TestNormalizeServerAddrBkenPrefixNoPort(t *testing.T) {
	addr, err := normalizeServerAddr("bken://192.168.1.10")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if addr != "192.168.1.10:8080" {
		t.Errorf("expected '192.168.1.10:8080', got %q", addr)
	}
}

func TestNormalizeServerAddrWssPrefix(t *testing.T) {
	addr, err := normalizeServerAddr("wss://example.com:8080")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if addr != "example.com:8080" {
		t.Errorf("expected 'example.com:8080', got %q", addr)
	}
}

func TestNormalizeServerAddrHttpsPrefix(t *testing.T) {
	addr, err := normalizeServerAddr("https://example.com:9000")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if addr != "example.com:9000" {
		t.Errorf("expected 'example.com:9000', got %q", addr)
	}
}

func TestNormalizeServerAddrHttpsPrefixNoPort(t *testing.T) {
	addr, err := normalizeServerAddr("https://example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if addr != "example.com:8080" {
		t.Errorf("expected 'example.com:8080', got %q", addr)
	}
}

func TestNormalizeServerAddrEmpty(t *testing.T) {
	_, err := normalizeServerAddr("")
	if err == nil {
		t.Error("expected error for empty address")
	}
}

func TestNormalizeServerAddrWhitespaceOnly(t *testing.T) {
	_, err := normalizeServerAddr("   ")
	if err == nil {
		t.Error("expected error for whitespace-only address")
	}
}

func TestNormalizeServerAddrLeadingTrailingWhitespace(t *testing.T) {
	addr, err := normalizeServerAddr("  myhost:8080  ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if addr != "myhost:8080" {
		t.Errorf("expected 'myhost:8080', got %q", addr)
	}
}

func TestNormalizeServerAddrIPv4(t *testing.T) {
	addr, err := normalizeServerAddr("10.0.0.1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if addr != "10.0.0.1:8080" {
		t.Errorf("expected '10.0.0.1:8080', got %q", addr)
	}
}

func TestNormalizeServerAddrIPv4WithPort(t *testing.T) {
	addr, err := normalizeServerAddr("10.0.0.1:9000")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if addr != "10.0.0.1:9000" {
		t.Errorf("expected '10.0.0.1:9000', got %q", addr)
	}
}

func TestNormalizeServerAddrIPv6Bracketed(t *testing.T) {
	addr, err := normalizeServerAddr("[::1]:8080")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if addr != "[::1]:8080" {
		t.Errorf("expected '[::1]:8080', got %q", addr)
	}
}

func TestNormalizeServerAddrIPv6BracketedNoPort(t *testing.T) {
	addr, err := normalizeServerAddr("[::1]")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if addr != "[::1]:8080" {
		t.Errorf("expected '[::1]:8080', got %q", addr)
	}
}

func TestNormalizeServerAddrIPv6Raw(t *testing.T) {
	addr, err := normalizeServerAddr("::1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if addr != "[::1]:8080" {
		t.Errorf("expected '[::1]:8080', got %q", addr)
	}
}

func TestNormalizeServerAddrTrailingSlash(t *testing.T) {
	addr, err := normalizeServerAddr("myserver:8080/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if addr != "myserver:8080" {
		t.Errorf("expected 'myserver:8080', got %q", addr)
	}
}

func TestNormalizeServerAddrTrailingPath(t *testing.T) {
	addr, err := normalizeServerAddr("myserver:8080/ws")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if addr != "myserver:8080" {
		t.Errorf("expected 'myserver:8080', got %q", addr)
	}
}

func TestNormalizeServerAddrInvalidPort(t *testing.T) {
	_, err := normalizeServerAddr("myserver:0")
	if err == nil {
		t.Error("expected error for port 0")
	}
}

func TestNormalizeServerAddrPortTooHigh(t *testing.T) {
	_, err := normalizeServerAddr("myserver:99999")
	if err == nil {
		t.Error("expected error for port > 65535")
	}
}

func TestNormalizeServerAddrNonNumericPort(t *testing.T) {
	_, err := normalizeServerAddr("myserver:abc")
	if err == nil {
		t.Error("expected error for non-numeric port")
	}
}

func TestNormalizeServerAddrDefaultPort(t *testing.T) {
	if defaultServerPort != "8080" {
		t.Errorf("expected default port '8080', got %q", defaultServerPort)
	}
}

func TestNormalizeServerAddrBkenPrefixWithPath(t *testing.T) {
	addr, err := normalizeServerAddr("bken://192.168.1.10:8080/join")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if addr != "192.168.1.10:8080" {
		t.Errorf("expected '192.168.1.10:8080', got %q", addr)
	}
}

func TestNormalizeServerAddrPort1(t *testing.T) {
	addr, err := normalizeServerAddr("host:1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if addr != "host:1" {
		t.Errorf("expected 'host:1', got %q", addr)
	}
}

func TestNormalizeServerAddrPort65535(t *testing.T) {
	addr, err := normalizeServerAddr("host:65535")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if addr != "host:65535" {
		t.Errorf("expected 'host:65535', got %q", addr)
	}
}

func TestNormalizeServerAddrLocalhostDefault(t *testing.T) {
	addr, err := normalizeServerAddr("localhost")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if addr != "localhost:8080" {
		t.Errorf("expected 'localhost:8080', got %q", addr)
	}
}
