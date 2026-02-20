package main

/*
#cgo pkg-config: rnnoise
#include <rnnoise.h>
#include <stdlib.h>
*/
import "C"
import (
	"sync"
	"unsafe"
)

// NoiseCanceller applies RNNoise-based ML noise suppression to audio buffers.
// It splits each 960-sample frame into two 480-sample halves (RNNoise's native
// frame size) and processes each with its own persistent state instance.
type NoiseCanceller struct {
	mu      sync.Mutex
	st0     *C.DenoiseState // processes samples [0:480]
	st1     *C.DenoiseState // processes samples [480:960]
	level   float32         // 0.0 = bypass, 1.0 = full suppression
	enabled bool
}

// NewNoiseCanceller allocates two RNNoise state instances.
func NewNoiseCanceller() *NoiseCanceller {
	return &NoiseCanceller{
		st0:     C.rnnoise_create(nil),
		st1:     C.rnnoise_create(nil),
		level:   1.0,
		enabled: false,
	}
}

// SetEnabled enables or disables noise suppression.
func (nc *NoiseCanceller) SetEnabled(on bool) {
	nc.mu.Lock()
	nc.enabled = on
	nc.mu.Unlock()
}

// SetLevel sets the suppression blend level (0.0 = bypass, 1.0 = full suppression).
// Values are clamped to [0, 1].
func (nc *NoiseCanceller) SetLevel(level float32) {
	if level < 0 {
		level = 0
	}
	if level > 1 {
		level = 1
	}
	nc.mu.Lock()
	nc.level = level
	nc.mu.Unlock()
}

// Process applies noise suppression in-place to buf (must be exactly 960 samples).
// No-op when disabled or level == 0.
func (nc *NoiseCanceller) Process(buf []float32) {
	nc.mu.Lock()
	enabled := nc.enabled
	level := nc.level
	nc.mu.Unlock()

	if !enabled || level == 0 {
		return
	}

	// RNNoise expects float32 samples scaled to int16 range [-32768, 32767].
	// Allocate C buffers for each half-frame (480 samples each).
	const halfFrame = 480
	cIn := (*C.float)(C.malloc(C.size_t(halfFrame) * C.size_t(unsafe.Sizeof(C.float(0)))))
	defer C.free(unsafe.Pointer(cIn))
	cOut := (*C.float)(C.malloc(C.size_t(halfFrame) * C.size_t(unsafe.Sizeof(C.float(0)))))
	defer C.free(unsafe.Pointer(cOut))

	inSlice := unsafe.Slice(cIn, halfFrame)
	outSlice := unsafe.Slice(cOut, halfFrame)

	// Process first half [0:480].
	for i := 0; i < halfFrame; i++ {
		inSlice[i] = C.float(buf[i] * 32767.0)
	}
	C.rnnoise_process_frame(nc.st0, cOut, cIn)
	for i := 0; i < halfFrame; i++ {
		denoised := float32(outSlice[i]) / 32767.0
		buf[i] = buf[i]*(1-level) + denoised*level
	}

	// Process second half [480:960].
	for i := 0; i < halfFrame; i++ {
		inSlice[i] = C.float(buf[halfFrame+i] * 32767.0)
	}
	C.rnnoise_process_frame(nc.st1, cOut, cIn)
	for i := 0; i < halfFrame; i++ {
		denoised := float32(outSlice[i]) / 32767.0
		buf[halfFrame+i] = buf[halfFrame+i]*(1-level) + denoised*level
	}
}

// Destroy frees the underlying C RNNoise state instances.
func (nc *NoiseCanceller) Destroy() {
	nc.mu.Lock()
	defer nc.mu.Unlock()
	if nc.st0 != nil {
		C.rnnoise_destroy(nc.st0)
		nc.st0 = nil
	}
	if nc.st1 != nil {
		C.rnnoise_destroy(nc.st1)
		nc.st1 = nil
	}
}
