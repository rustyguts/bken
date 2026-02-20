package main

import (
	"context"
	"crypto/tls"
	"net/http"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/webtransport-go"
)

func TestProbeRemoteWebTransport(t *testing.T) {
	target := "10.0.8.85:4443"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	d := webtransport.Dialer{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		QUICConfig: &quic.Config{
			EnableDatagrams:                  true,
			EnableStreamResetPartialDelivery: true,
		},
	}

	_, sess, err := d.Dial(ctx, "https://"+target, http.Header{})
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	_ = sess.CloseWithError(0, "probe")
}
