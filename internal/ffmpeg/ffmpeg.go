// Package ffmpeg wraps ffmpeg / ffprobe via os/exec. Kept minimal — each
// pipeline stage composes its own arg list from these primitives.
package ffmpeg

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// Probe is the subset of ffprobe output we care about.
type Probe struct {
	DurationS     float64
	Width         int
	Height        int
	FPS           float64
	VideoCodec    string
	AudioCodec    string
	AudioChannels int
	AudioRate     int
}

type rawStream struct {
	CodecType    string `json:"codec_type"`
	CodecName    string `json:"codec_name"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	RFrameRate   string `json:"r_frame_rate"`
	AvgFrameRate string `json:"avg_frame_rate"`
	Channels     int    `json:"channels"`
	SampleRate   string `json:"sample_rate"`
}

type rawFormat struct {
	Duration string `json:"duration"`
}

type rawProbe struct {
	Streams []rawStream `json:"streams"`
	Format  rawFormat   `json:"format"`
}

// Ffprobe reads container + stream metadata.
func Ffprobe(ctx context.Context, path string) (*Probe, error) {
	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "error",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		path,
	)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffprobe %s: %w (%s)", path, err, out.String())
	}
	var raw rawProbe
	if err := json.Unmarshal(out.Bytes(), &raw); err != nil {
		return nil, fmt.Errorf("parse ffprobe json: %w", err)
	}
	p := &Probe{}
	if raw.Format.Duration != "" {
		p.DurationS, _ = strconv.ParseFloat(raw.Format.Duration, 64)
	}
	for _, s := range raw.Streams {
		switch s.CodecType {
		case "video":
			if p.VideoCodec == "" {
				p.VideoCodec = s.CodecName
				p.Width = s.Width
				p.Height = s.Height
				p.FPS = parseFrameRate(s.RFrameRate, s.AvgFrameRate)
			}
		case "audio":
			if p.AudioCodec == "" {
				p.AudioCodec = s.CodecName
				p.AudioChannels = s.Channels
				if s.SampleRate != "" {
					p.AudioRate, _ = strconv.Atoi(s.SampleRate)
				}
			}
		}
	}
	return p, nil
}

func parseFrameRate(rate, avg string) float64 {
	for _, s := range []string{rate, avg} {
		if s == "" || s == "0/0" {
			continue
		}
		parts := strings.SplitN(s, "/", 2)
		if len(parts) != 2 {
			if f, err := strconv.ParseFloat(s, 64); err == nil {
				return f
			}
			continue
		}
		num, err1 := strconv.ParseFloat(parts[0], 64)
		den, err2 := strconv.ParseFloat(parts[1], 64)
		if err1 == nil && err2 == nil && den > 0 {
			return num / den
		}
	}
	return 0
}

// Run executes ffmpeg with the given args, capturing stderr for error reporting.
func Run(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "ffmpeg", append([]string{"-y", "-loglevel", "error"}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg: %w (%s)", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}
