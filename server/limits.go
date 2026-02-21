package main

import "time"

// Operational limits — named constants for values that were previously
// scattered across multiple source files.
const (
	// circuitBreakerThreshold is the number of consecutive SendDatagram
	// failures before the per-client circuit breaker opens (~1 s of voice
	// at 50 fps).
	circuitBreakerThreshold uint32 = 50

	// circuitBreakerProbeInterval is the number of skipped sends between
	// probe attempts when the circuit breaker is open.
	circuitBreakerProbeInterval uint32 = 25

	// maxRecordingDuration is the maximum wall-clock duration for a single
	// server-side voice recording before it is automatically stopped.
	maxRecordingDuration = 2 * time.Hour

	// maxMsgOwners is the maximum number of message-to-sender mappings to
	// retain. Once exceeded, the oldest entries are evicted. 10 000
	// messages is roughly a few hours of active chat.
	maxMsgOwners = 10000

	// maxPinnedPerChannel is the maximum number of pinned messages allowed
	// per channel.
	maxPinnedPerChannel = 25

	// maxMsgBuffer is the maximum number of messages buffered per channel
	// for replay on reconnect.
	maxMsgBuffer = 500
)
