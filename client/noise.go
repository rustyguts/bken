package main

import "sync"

// NoiseCanceller is a compatibility shim retained after removing external
// RNNoise dependencies from the client build. It currently stores only a
// boolean enable state.
type NoiseCanceller struct {
	mu      sync.Mutex
	enabled bool
}

func NewNoiseCanceller() *NoiseCanceller {
	return &NoiseCanceller{}
}

func (nc *NoiseCanceller) SetEnabled(on bool) {
	nc.mu.Lock()
	nc.enabled = on
	nc.mu.Unlock()
}

// SetLevel is retained for API compatibility.
func (nc *NoiseCanceller) SetLevel(level float32) {
	_ = level
}

// Process is a no-op compatibility shim.
func (nc *NoiseCanceller) Process(buf []float32) {
	_ = buf
}

// VADProbability is retained for API compatibility.
func (nc *NoiseCanceller) VADProbability() float32 {
	return 0
}

func (nc *NoiseCanceller) Destroy() {}
