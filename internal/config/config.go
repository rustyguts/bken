// Package config holds paths + tunables. Single source of truth so
// moving the data dir is a one-line env override.
package config

import (
	"os"
	"path/filepath"
	"strconv"
)

type DensityScale struct {
	HighPct float64
	LowPct  float64
}

type Config struct {
	Root         string
	Data         string
	Models       string
	DBPath       string
	AudioDir     string
	ClipsDir     string
	ThumbsDir    string
	ArtifactsDir string

	// Audio
	AudioSampleRate int
	AudioChannels   int

	// ASR
	WhisperModel       string
	WhisperComputeType string

	// LLM ranking
	RankModel string

	// Scoring
	DensitySmoothSigma   float64
	DensityGapTolerance  int
	DensityScales        []DensityScale
	TopNCandidates       int

	// Clip
	ClipPadBefore    float64
	ClipPadAfter     float64
	ClipMinSeconds   float64
	ClipMaxSeconds   float64
	ClipSnapWindow   float64
	ClipVideoCodec   string
	ClipVideoCRF     int
	ClipVideoPreset  string
	ClipAudioBitrate string
	ClipVideoBitrate string

	// Vision
	VisionFPS            float64
	VisionSceneDetect    bool
	VisionSceneThreshold float64
	VisionMaxFrames      int
	VisionBatchSize      int
	VisionScoreEnabled   bool

	// Web + queue
	RedisAddr  string
	HTTPListen string
	BaseURL    string

	// LLM ranking via Ollama (OpenAI-compatible HTTP)
	LLMBaseURL string
	LLMModel   string

	// Whisper ASR HTTP service (onerahmet/openai-whisper-asr-webservice)
	ASRBaseURL  string
	ASRLanguage string
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getenvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func getenvFloat(key string, def float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}

func getenvBool(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	return v == "1" || v == "true" || v == "TRUE"
}

// Load resolves config from env. Caller-independent; safe to call repeatedly.
func Load() *Config {
	wd, _ := os.Getwd()
	root := getenv("BKEN_ROOT", wd)
	data := getenv("BKEN_DATA", filepath.Join(root, "data"))
	models := getenv("BKEN_MODELS", filepath.Join(root, "models"))

	c := &Config{
		Root:         root,
		Data:         data,
		Models:       models,
		DBPath:       filepath.Join(data, "index.db"),
		AudioDir:     filepath.Join(data, "audio"),
		ClipsDir:     filepath.Join(data, "clips"),
		ThumbsDir:    filepath.Join(data, "thumbs"),
		ArtifactsDir: filepath.Join(data, "artifacts"),

		AudioSampleRate: 16000,
		AudioChannels:   1,

		WhisperModel:       getenv("BKEN_WHISPER_MODEL", "large-v3"),
		WhisperComputeType: getenv("BKEN_WHISPER_COMPUTE", "float16"),
		RankModel:          getenv("BKEN_RANK_MODEL", "Qwen/Qwen2.5-1.5B-Instruct"),

		DensitySmoothSigma:  3.0,
		DensityGapTolerance: 4,
		DensityScales: []DensityScale{
			{HighPct: 95.0, LowPct: 85.0},
			{HighPct: 90.0, LowPct: 75.0},
			{HighPct: 85.0, LowPct: 65.0},
		},
		TopNCandidates: 30,

		ClipPadBefore:    3.0,
		ClipPadAfter:     8.0,
		ClipMinSeconds:   8.0,
		ClipMaxSeconds:   90.0,
		ClipSnapWindow:   2.0,
		ClipVideoCodec:   getenv("BKEN_CLIP_VCODEC", "libsvtav1"),
		ClipVideoCRF:     getenvInt("BKEN_CLIP_CRF", 28),
		ClipVideoPreset:  getenv("BKEN_CLIP_PRESET", "5"),
		ClipAudioBitrate: getenv("BKEN_CLIP_ABITRATE", "192k"),
		ClipVideoBitrate: getenv("BKEN_CLIP_VBITRATE", "10M"),

		VisionFPS:            getenvFloat("BKEN_VISION_FPS", 1.0),
		VisionSceneDetect:    getenvBool("BKEN_VISION_SCENE", true),
		VisionSceneThreshold: getenvFloat("BKEN_VISION_SCENE_THRESH", 27.0),
		VisionMaxFrames:      getenvInt("BKEN_VISION_MAX_FRAMES", 5000),
		VisionBatchSize:      getenvInt("BKEN_VISION_BATCH", 4),
		VisionScoreEnabled:   getenvBool("BKEN_VISION_SCORE", true),

		RedisAddr:  getenv("BKEN_REDIS", "127.0.0.1:6379"),
		HTTPListen: getenv("BKEN_HTTP", ":3000"),
		BaseURL:    os.Getenv("BKEN_BASE_URL"),

		LLMBaseURL: getenv("BKEN_LLM_BASE_URL", "http://ollama:11434"),
		LLMModel:   getenv("BKEN_LLM_MODEL", "qwen2.5:1.5b-instruct"),

		ASRBaseURL:  getenv("BKEN_ASR_BASE_URL", "http://whisper:9000"),
		ASRLanguage: getenv("BKEN_ASR_LANGUAGE", "en"),
	}
	return c
}

// EnsureDirs creates runtime directories.
func (c *Config) EnsureDirs() error {
	for _, p := range []string{c.Data, c.Models, c.AudioDir, c.ClipsDir, c.ThumbsDir, c.ArtifactsDir} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			return err
		}
	}
	return nil
}

// ResolveData rewrites a stored clip/thumb path so it works in the current
// environment even if DB moved across hosts or containers. Mirrors the
// Python `config.resolve_data` helper.
func (c *Config) ResolveData(stored string) string {
	if stored == "" {
		return ""
	}
	if !filepath.IsAbs(stored) {
		return filepath.Join(c.Data, stored)
	}
	if _, err := os.Stat(stored); err == nil {
		return stored
	}
	// Strip up to last "data" segment, re-root under c.Data.
	parts := splitAll(stored)
	for i := len(parts) - 1; i >= 0; i-- {
		if parts[i] == "data" {
			return filepath.Join(append([]string{c.Data}, parts[i+1:]...)...)
		}
	}
	return stored
}

func splitAll(p string) []string {
	var out []string
	for {
		dir, file := filepath.Split(p)
		if file != "" {
			out = append([]string{file}, out...)
		}
		if dir == "" || dir == "/" || dir == string(filepath.Separator) {
			if dir != "" {
				out = append([]string{"/"}, out...)
			}
			return out
		}
		p = filepath.Clean(dir)
		if p == "/" {
			out = append([]string{"/"}, out...)
			return out
		}
	}
}
