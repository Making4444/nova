package voice

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

const openRouterSpeechURL = "https://openrouter.ai/api/v1/audio/speech"

// OpenRouterTTS synthesizes speech using OpenRouter OpenAI Speech API and converts to WhatsApp PTT OGG Opus.
type OpenRouterTTS struct {
	apiKey     string
	model      string
	voice      string
	httpClient *http.Client
}

// NewOpenRouterTTS creates a new OpenRouter TTS client.
func NewOpenRouterTTS(apiKey, model, voice string) *OpenRouterTTS {
	if model == "" {
		model = "openai/tts-1"
	}
	if voice == "" {
		voice = "onyx"
	}
	return &OpenRouterTTS{
		apiKey: apiKey,
		model:  model,
		voice:  voice,
		httpClient: &http.Client{
			Timeout: 45 * time.Second,
		},
	}
}

type speechRequest struct {
	Model          string `json:"model"`
	Input          string `json:"input"`
	Voice          string `json:"voice"`
	ResponseFormat string `json:"response_format"`
}

// SynthesizeToOggOpus generates speech from text via OpenRouter and converts it to WhatsApp OGG Opus format.
func (t *OpenRouterTTS) SynthesizeToOggOpus(ctx context.Context, text string) ([]byte, uint32, error) {
	cleanText := strings.TrimSpace(text)
	if cleanText == "" {
		return nil, 0, fmt.Errorf("input text cannot be empty")
	}

	// 1. Call OpenRouter /api/v1/audio/speech
	reqBody := speechRequest{
		Model:          t.model,
		Input:          cleanText,
		Voice:          t.voice,
		ResponseFormat: "mp3",
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to marshal speech request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, openRouterSpeechURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create speech request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+t.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("HTTP-Referer", "https://github.com/makari/novabot")
	req.Header.Set("X-Title", "Nova WhatsApp Bot Voice")

	fmt.Printf("\n[🎙️ OpenRouter Voice] Generating speech via %s (voice: %s) for text (%d chars)...\n", t.model, t.voice, len(cleanText))
	resp, err := t.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("speech API request failed: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to read speech audio response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, 0, fmt.Errorf("speech API error (status %d): %s", resp.StatusCode, string(respBytes))
	}

	fmt.Printf("[✅ OpenRouter Voice] Received %d bytes MP3 audio, converting to WhatsApp OGG Opus...\n", len(respBytes))

	// 2. Convert MP3 to WhatsApp OGG Opus via ffmpeg
	oggBytes, err := ConvertMP3ToOggOpus(ctx, respBytes)
	if err != nil {
		fmt.Printf("[⚠️ FFmpeg Warning] Could not convert via ffmpeg (%v), using raw bytes\n", err)
		oggBytes = respBytes
	} else {
		fmt.Printf("[✅ Audio Converted] WhatsApp Ogg Opus ready (%d bytes)\n", len(oggBytes))
	}

	// Approximate duration: ~12-15 chars per second in Arabic speech
	durationSec := uint32(len(cleanText) / 14)
	if durationSec < 1 {
		durationSec = 1
	}

	return oggBytes, durationSec, nil
}

// ConvertMP3ToOggOpus converts MP3 audio bytes into audio/ogg; codecs=opus for WhatsApp Voice Notes (PTT).
func ConvertMP3ToOggOpus(ctx context.Context, mp3Data []byte) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "ffmpeg", "-y", "-i", "pipe:0", "-c:a", "libopus", "-b:a", "32k", "-vbr", "on", "-f", "ogg", "pipe:1")
	cmd.Stdin = bytes.NewReader(mp3Data)

	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg conversion failed: %w (stderr: %s)", err, errBuf.String())
	}

	if outBuf.Len() == 0 {
		return nil, fmt.Errorf("ffmpeg produced empty output")
	}

	return outBuf.Bytes(), nil
}
