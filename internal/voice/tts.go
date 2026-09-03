package voice

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

const (
	openRouterChatURL   = "https://openrouter.ai/api/v1/chat/completions"
	openRouterSpeechURL = "https://openrouter.ai/api/v1/audio/speech"
)

// OpenRouterTTS synthesizes speech using OpenRouter Audio Speech or OpenAI Native Audio and converts to WhatsApp PTT OGG Opus.
type OpenRouterTTS struct {
	apiKey     string
	model      string
	voice      string
	httpClient *http.Client
}

// NewOpenRouterTTS creates a new OpenRouter TTS client.
func NewOpenRouterTTS(apiKey, model, voice string) *OpenRouterTTS {
	if model == "" {
		model = "openai/gpt-audio-mini"
	}
	if voice == "" {
		voice = "onyx"
	}
	return &OpenRouterTTS{
		apiKey: apiKey,
		model:  model,
		voice:  voice,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

type audioConfig struct {
	Voice  string `json:"voice"`
	Format string `json:"format"`
}

type messagePayload struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatAudioRequest struct {
	Model      string           `json:"model"`
	Modalities []string         `json:"modalities"`
	Audio      audioConfig      `json:"audio"`
	Stream     bool             `json:"stream"`
	Messages   []messagePayload `json:"messages"`
}

type audioSpeechRequest struct {
	Model          string `json:"model"`
	Input          string `json:"input"`
	Voice          string `json:"voice,omitempty"`
	ResponseFormat string `json:"response_format,omitempty"`
}

type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
			Audio   struct {
				Data string `json:"data"`
			} `json:"audio"`
		} `json:"delta"`
	} `json:"choices"`
}

// CleanTextForSpeech removes markdown formatting, emojis, search headers, and symbols for clean spoken voice.
func CleanTextForSpeech(text string) string {
	s := strings.TrimSpace(text)
	s = strings.TrimPrefix(s, "تم البحث")
	s = strings.TrimSpace(s)

	// Remove markdown formatting characters
	s = strings.ReplaceAll(s, "*", "")
	s = strings.ReplaceAll(s, "_", "")
	s = strings.ReplaceAll(s, "~", "")
	s = strings.ReplaceAll(s, "`", "")
	s = strings.ReplaceAll(s, "#", "")
	s = strings.ReplaceAll(s, ">", "")

	return strings.TrimSpace(s)
}

// SynthesizeToOggOpus generates speech from text and converts it to WhatsApp OGG Opus format.
// It automatically prefers pure TTS (/audio/speech) and falls back to multimodal chat audio if configured.
func (t *OpenRouterTTS) SynthesizeToOggOpus(ctx context.Context, text string) ([]byte, uint32, error) {
	if t == nil {
		return nil, 0, fmt.Errorf("openRouter TTS is nil")
	}
	cleanText := CleanTextForSpeech(text)
	if cleanText == "" {
		return nil, 0, fmt.Errorf("input text cannot be empty")
	}

	// 1. If configured with a pure TTS model, use the dedicated /api/v1/audio/speech endpoint
	if t.model != "openai/gpt-audio-mini" {
		oggBytes, dur, err := t.synthesizePureSpeech(ctx, cleanText)
		if err == nil && len(oggBytes) > 0 {
			return oggBytes, dur, nil
		}
		fmt.Printf("[⚠️ Pure TTS Warning] /audio/speech failed (%v), trying chat audio fallback...\n", err)
	}

	// 2. Fallback to multimodal chat completions audio
	return t.synthesizeChatAudio(ctx, cleanText)
}

func (t *OpenRouterTTS) synthesizePureSpeech(ctx context.Context, cleanText string) ([]byte, uint32, error) {
	format := "mp3"
	if strings.Contains(strings.ToLower(t.model), "gemini") {
		format = "pcm"
	}

	reqBody := audioSpeechRequest{
		Model:          t.model,
		Input:          cleanText,
		Voice:          t.voice,
		ResponseFormat: format,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to marshal pure speech request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, openRouterSpeechURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create speech request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+t.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("HTTP-Referer", "https://github.com/makari/novabot")
	req.Header.Set("X-Title", "Nova WhatsApp Bot Voice")

	fmt.Printf("\n[🎙️ OpenRouter Pure TTS] Synthesizing speech via %s (voice: %s, format: %s) for text (%d chars)...\n", t.model, t.voice, format, len(cleanText))
	resp, err := t.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("pure speech API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBytes, _ := io.ReadAll(resp.Body)
		return nil, 0, fmt.Errorf("pure speech API error (status %d): %s", resp.StatusCode, string(respBytes))
	}

	audioBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to read speech audio stream: %w", err)
	}

	fmt.Printf("[✅ Pure TTS] Received %d bytes audio, converting to WhatsApp OGG Opus...\n", len(audioBytes))
	var oggBytes []byte
	var convErr error
	if format == "pcm" {
		oggBytes, convErr = ConvertPCMToOggOpus(ctx, audioBytes)
	} else {
		oggBytes, convErr = ConvertAnyAudioToOggOpus(ctx, audioBytes)
	}
	if convErr != nil {
		return nil, 0, fmt.Errorf("failed to convert audio to ogg opus: %w", convErr)
	}

	durationSec := uint32(len(cleanText) / 12)
	if durationSec < 1 {
		durationSec = 1
	}

	return oggBytes, durationSec, nil
}

func (t *OpenRouterTTS) synthesizeChatAudio(ctx context.Context, cleanText string) ([]byte, uint32, error) {
	reqBody := chatAudioRequest{
		Model:      t.model,
		Modalities: []string{"text", "audio"},
		Audio: audioConfig{
			Voice:  t.voice,
			Format: "pcm16",
		},
		Stream: true,
		Messages: []messagePayload{
			{
				Role:    "system",
				Content: "You are a pure Text-to-Speech (TTS) voice synthesizer. You speak authentic, expressive Egyptian Arabic with a natural human tone. Your ONLY job is to read aloud the exact text provided by the user. NEVER add any introductory remarks, greetings, or filler words like 'حاضر' or 'تمام' or 'إليك'. Start speaking the user text immediately.",
			},
			{
				Role:    "user",
				Content: cleanText,
			},
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to marshal speech request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, openRouterChatURL, bytes.NewReader(bodyBytes))
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

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, 0, fmt.Errorf("speech API error (status %d)", resp.StatusCode)
	}

	// Read Server-Sent Events (SSE) stream
	scanner := bufio.NewScanner(resp.Body)
	var audioBase64Builder strings.Builder

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.HasPrefix(line, "data: ") {
			continue
		}
		dataStr := strings.TrimPrefix(line, "data: ")
		if dataStr == "[DONE]" {
			break
		}

		var chunk streamChunk
		if err := json.Unmarshal([]byte(dataStr), &chunk); err == nil {
			if len(chunk.Choices) > 0 {
				if chunk.Choices[0].Delta.Audio.Data != "" {
					audioBase64Builder.WriteString(chunk.Choices[0].Delta.Audio.Data)
				}
			}
		}
	}

	fullBase64 := audioBase64Builder.String()
	if len(fullBase64) == 0 {
		return nil, 0, fmt.Errorf("no audio stream data received from model")
	}

	rawPCM, err := base64.StdEncoding.DecodeString(fullBase64)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to decode PCM audio base64: %w", err)
	}

	fmt.Printf("[✅ OpenRouter Voice] Received %d bytes PCM16 audio, converting to WhatsApp OGG Opus...\n", len(rawPCM))

	// Convert PCM16 (24kHz, 1-channel, s16le) to WhatsApp OGG Opus via ffmpeg
	oggBytes, err := ConvertPCMToOggOpus(ctx, rawPCM)
	if err != nil {
		return nil, 0, fmt.Errorf("ffmpeg conversion failed: %w", err)
	}

	fmt.Printf("[✅ Audio Converted] WhatsApp Ogg Opus ready (%d bytes)\n", len(oggBytes))

	durationSec := uint32(len(rawPCM) / 48000)
	if durationSec < 1 {
		durationSec = 1
	}

	return oggBytes, durationSec, nil
}

// ConvertAnyAudioToOggOpus converts any audio stream (MP3, WAV, AAC, OGG) to WhatsApp OGG Opus PTT format.
func ConvertAnyAudioToOggOpus(ctx context.Context, audioData []byte) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "ffmpeg", "-y", "-i", "pipe:0", "-c:a", "libopus", "-b:a", "32k", "-vbr", "on", "-f", "ogg", "pipe:1")
	cmd.Stdin = bytes.NewReader(audioData)

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

// ConvertPCMToOggOpus converts 24kHz s16le 1ch raw PCM into audio/ogg; codecs=opus for WhatsApp Voice Notes (PTT).
func ConvertPCMToOggOpus(ctx context.Context, pcmData []byte) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "ffmpeg", "-y", "-f", "s16le", "-ar", "24000", "-ac", "1", "-i", "pipe:0", "-c:a", "libopus", "-b:a", "32k", "-vbr", "on", "-f", "ogg", "pipe:1")
	cmd.Stdin = bytes.NewReader(pcmData)

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
