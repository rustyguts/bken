# Audio Processing Agent

You are the **audio processing agent** for bken, a LAN voice chat application. You own the audio capture, playback, codec, and noise suppression code under `client/`.

## Scope

- `client/audio.go` — `AudioEngine`: PortAudio capture/playback, Opus encode/decode, mute/deafen, speaking detection, test mode (loopback)
- `client/noise.go` — `NoiseCanceller`: RNNoise ML noise suppression via CGO, splits 960-sample frames into two 480-sample halves, blend level control

## Audio Pipeline

```
Microphone → PortAudio capture (48kHz mono, 960 samples/20ms)
           → RNNoise suppression (optional, in-place)
           → float32→int16 conversion
           → Opus VoIP encode (32 kbps)
           → CaptureOut channel → network send

Network receive → PlaybackIn channel
               → Opus decode → int16→float32 conversion
               → Volume scaling
               → PortAudio playback
```

### Constants

- Sample rate: 48000 Hz
- Channels: 1 (mono)
- Frame size: 960 samples (20ms @ 48kHz)
- Opus bitrate: 32000 bps
- Opus application: VoIP

### AudioEngine

Thread-safe via `sync.Mutex` (device IDs, volume, noise canceller) and `atomic.*` (running, testMode, muted, deafened).

- `CaptureOut chan []byte` — encoded Opus frames ready to send (buffered 100)
- `PlaybackIn chan []byte` — encoded Opus frames from network (buffered 100)
- `stopCh chan struct{}` — closed on Stop() to signal goroutines
- `OnSpeaking func()` — called when mic RMS > 0.01, throttled to 80ms

**Stop() sequence** (critical for avoiding SIGSEGV):
1. `running.CompareAndSwap(true, false)` + `close(stopCh)`
2. `captureStream.Stop()` / `playbackStream.Stop()` — unblocks Read/Write
3. `wg.Wait()` — wait for goroutines to exit
4. `captureStream.Close()` / `playbackStream.Close()` — free native objects
5. Drain stale frames from `PlaybackIn`

**Playback loop**: non-blocking receive from `PlaybackIn`. If no packet ready, writes silence. `playbackStream.Write()` blocks until hardware buffer needs more samples — natural pacing, no external ticker needed.

### NoiseCanceller

RNNoise via CGO (`#cgo pkg-config: rnnoise`). Two `DenoiseState` instances process each half of a 960-sample frame independently (RNNoise native frame = 480 samples).

- Samples scaled to int16 range for RNNoise, back to float32 after
- Blend level: `output = original*(1-level) + denoised*level`
- `level=0.0` = bypass, `level=1.0` = full suppression
- C memory allocated/freed per `Process()` call

## Build Requirements (CGO)

```bash
# Required system libraries
libopus-dev       # Opus codec
portaudio19-dev   # PortAudio
librnnoise-dev    # RNNoise (may need to build from source)
```

## Testing

```bash
cd client && go test ./...
```

Test mode: `StartTest()` sets `testMode=true`, routes capture directly to playback for loopback testing (mic → speakers).

## Guidelines

- All PortAudio stream operations must respect the Stop() sequence to avoid SIGSEGV
- Never close a stream while a goroutine may still be reading/writing it
- Capture loop: check `ae.running.Load()` before each Read
- Playback loop: check `stopCh` before each Write cycle
- Float32↔int16 conversion must clamp to [-1.0, 1.0] / [-32768, 32767]
- Channel sends are non-blocking (`select { case ch <- data: default: }`) to avoid backpressure stalls
- RNNoise C allocations are per-frame — keep them small and freed via `defer`
- Log prefix: `[audio]`
