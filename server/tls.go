package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"fmt"
	"math/big"
	"time"
)

// generateTLSConfig creates a self-signed TLS certificate for the HTTPS server.
// Returns the tls.Config, the SHA-256 fingerprint, and any error.
// validity controls how long the certificate is valid for.
// hostname is used as the Common Name and added to the DNS SANs alongside "localhost".
func generateTLSConfig(validity time.Duration, hostname string) (*tls.Config, string, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, "", fmt.Errorf("[tls] generate key: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, "", fmt.Errorf("[tls] generate serial: %w", err)
	}

	cn := "bken"
	if hostname != "" {
		cn = hostname
	}

	sans := []string{"localhost"}
	if hostname != "" && hostname != "localhost" {
		sans = append(sans, hostname)
	}

	tmpl := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(validity),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              sans,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, "", fmt.Errorf("[tls] create certificate: %w", err)
	}

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return nil, "", fmt.Errorf("[tls] parse certificate: %w", err)
	}

	fp := sha256.Sum256(certDER)
	fingerprint := hex.EncodeToString(fp[:])

	tlsCert := tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  key,
		Leaf:        cert,
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{tlsCert},
	}

	return tlsConfig, fingerprint, nil
}
