package voice

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

const openRouterChatURL = "https://openrouter.ai/api/v1/chat/completions"

// OpenRouterTTS synthesizes speech using OpenRouter OpenAI Native Audio (gpt-audio-mini) and converts to WhatsApp PTT OGG Opus.
type OpenRouterTTS struct {
	apiKey     string
	model      string
	voice      string
	httpClient *http.Client
}

// NewOpenRouterTTS creates a new OpenRouter TTS client.
func NewOpenRouterTTS(apiKey, model, voice string) *OpenRouterTTS {
	if model == "" || model == "openai/tts-1" {
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

// SynthesizeToOggOpus generates speech from text via OpenRouter gpt-audio-mini and converts it to WhatsApp OGG Opus format.
func (t *OpenRouterTTS) SynthesizeToOggOpus(ctx context.Context, text string) ([]byte, uint32, error) {
	cleanText := CleanTextForSpeech(text)
	if cleanText == "" {
		return nil, 0, fmt.Errorf("input text cannot be empty")
	}

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

	// 2. Convert PCM16 (24kHz, 1-channel, s16le) to WhatsApp OGG Opus via ffmpeg
	oggBytes, err := ConvertPCMToOggOpus(ctx, rawPCM)
	if err != nil {
		return nil, 0, fmt.Errorf("ffmpeg conversion failed: %w", err)
	}

	fmt.Printf("[✅ Audio Converted] WhatsApp Ogg Opus ready (%d bytes)\n", len(oggBytes))

	// Duration calculation: 24000 samples/sec * 2 bytes/sample (16-bit) = 48000 bytes/sec
	durationSec := uint32(len(rawPCM) / 48000)
	if durationSec < 1 {
		durationSec = 1
	}

	return oggBytes, durationSec, nil
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
