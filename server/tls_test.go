package main

import (
	"crypto/x509"
	"testing"
	"time"
)

func TestGenerateTLSConfigReturnsValidCert(t *testing.T) {
	validity := 2 * time.Hour
	tlsCfg, fingerprint, err := generateTLSConfig(validity, "")
	if err != nil {
		t.Fatalf("generateTLSConfig: %v", err)
	}

	if tlsCfg == nil {
		t.Fatal("expected non-nil tls.Config")
	}
	if fingerprint == "" {
		t.Fatal("expected non-empty fingerprint")
	}
	if len(fingerprint) != 64 { // SHA-256 hex = 32 bytes = 64 chars
		t.Errorf("fingerprint length: got %d, want 64", len(fingerprint))
	}
	if len(tlsCfg.Certificates) != 1 {
		t.Fatalf("expected 1 certificate, got %d", len(tlsCfg.Certificates))
	}

	leaf := tlsCfg.Certificates[0].Leaf
	if leaf == nil {
		t.Fatal("expected parsed leaf certificate")
	}
	if leaf.Subject.CommonName != "bken" {
		t.Errorf("CN: got %q, want %q", leaf.Subject.CommonName, "bken")
	}

	// Cert should be valid now.
	now := time.Now()
	if now.Before(leaf.NotBefore) || now.After(leaf.NotAfter) {
		t.Errorf("cert not valid at current time: NotBefore=%v NotAfter=%v", leaf.NotBefore, leaf.NotAfter)
	}

	// NotAfter should be within the requested validity window (plus the 1-hour backdating).
	expectedAfter := now.Add(validity)
	if leaf.NotAfter.Before(expectedAfter.Add(-2 * time.Hour)) {
		t.Errorf("NotAfter too early: %v (expected near %v)", leaf.NotAfter, expectedAfter)
	}
}

func TestGenerateTLSConfigUniqueCerts(t *testing.T) {
	_, fp1, err := generateTLSConfig(time.Hour, "")
	if err != nil {
		t.Fatalf("generateTLSConfig: %v", err)
	}
	_, fp2, err := generateTLSConfig(time.Hour, "")
	if err != nil {
		t.Fatalf("generateTLSConfig: %v", err)
	}
	if fp1 == fp2 {
		t.Error("two calls should produce different certificates")
	}
}

func TestGenerateTLSConfigSelfSigned(t *testing.T) {
	tlsCfg, _, err := generateTLSConfig(time.Hour, "")
	if err != nil {
		t.Fatalf("generateTLSConfig: %v", err)
	}
	leaf := tlsCfg.Certificates[0].Leaf

	// The cert should be self-signed (issuer == subject).
	if leaf.Issuer.CommonName != leaf.Subject.CommonName {
		t.Errorf("expected self-signed cert: issuer=%q subject=%q", leaf.Issuer.CommonName, leaf.Subject.CommonName)
	}

	// Should include localhost in DNS names.
	found := false
	for _, name := range leaf.DNSNames {
		if name == "localhost" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected localhost in DNS names, got %v", leaf.DNSNames)
	}

	// Verify against itself.
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	_, err = leaf.Verify(x509.VerifyOptions{
		DNSName: "localhost",
		Roots:   pool,
	})
	if err != nil {
		t.Errorf("self-verification failed: %v", err)
	}
}

func TestGenerateTLSConfigCustomHostname(t *testing.T) {
	tlsCfg, _, err := generateTLSConfig(time.Hour, "myhost.local")
	if err != nil {
		t.Fatalf("generateTLSConfig: %v", err)
	}
	leaf := tlsCfg.Certificates[0].Leaf

	// CN should be the provided hostname.
	if leaf.Subject.CommonName != "myhost.local" {
		t.Errorf("CN: got %q, want %q", leaf.Subject.CommonName, "myhost.local")
	}

	// SANs should include both localhost and the custom hostname.
	wantSANs := map[string]bool{"localhost": false, "myhost.local": false}
	for _, name := range leaf.DNSNames {
		if _, ok := wantSANs[name]; ok {
			wantSANs[name] = true
		}
	}
	for name, found := range wantSANs {
		if !found {
			t.Errorf("expected %q in DNS names, got %v", name, leaf.DNSNames)
		}
	}

	// Verify against the custom hostname.
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	_, err = leaf.Verify(x509.VerifyOptions{
		DNSName: "myhost.local",
		Roots:   pool,
	})
	if err != nil {
		t.Errorf("verification against custom hostname failed: %v", err)
	}
}
