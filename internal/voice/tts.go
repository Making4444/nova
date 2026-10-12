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

// OpenRouterTTS synthesizes speech using OpenRouter Audio Speech or OpenAI/Signor Native Audio and converts to WhatsApp PTT OGG Opus.
type OpenRouterTTS struct {
	apiKey     string
	baseURL    string
	model      string
	voice      string
	httpClient *http.Client
}

// NewTTSClient creates a new TTS client with configurable baseURL, model, and voice (e.g. for Signor AI).
func NewTTSClient(apiKey, baseURL, model, voice string) *OpenRouterTTS {
	if model == "" {
		model = "gemini-3.8-flash-tts"
	}
	if voice == "" {
		voice = "alloy"
	}
	return &OpenRouterTTS{
		apiKey:  apiKey,
		baseURL: strings.TrimSpace(baseURL),
		model:   model,
		voice:   voice,
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

// SetBaseURL updates the TTS base URL.
func (t *OpenRouterTTS) SetBaseURL(baseURL string) {
	t.baseURL = strings.TrimSpace(baseURL)
}

func (t *OpenRouterTTS) getSpeechURL() string {
	if strings.TrimSpace(t.baseURL) != "" {
		base := strings.TrimSpace(t.baseURL)
		if strings.HasSuffix(base, "/audio/speech") {
			return base
		}
		return strings.TrimSuffix(base, "/") + "/audio/speech"
	}
	return openRouterSpeechURL
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
			Timeout: 120 * time.Second,
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

// CleanTextForSpeech removes markdown formatting, search headers, and symbols for clean spoken voice,
// while strictly preserving audio expressiveness tags like [laughs], [chuckles], [whispers], [pause], etc.
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
		return nil, 0, fmt.Errorf("TTS client is nil")
	}
	cleanText := CleanTextForSpeech(text)
	if cleanText == "" {
		return nil, 0, fmt.Errorf("input text cannot be empty")
	}

	endpoint := t.getSpeechURL()
	isSignor := strings.Contains(endpoint, "signor") || (t.baseURL != "" && !strings.Contains(endpoint, "openrouter.ai"))

	// 1. If configured with a pure TTS model or Signor AI, use the dedicated /audio/speech endpoint
	if isSignor || t.model != "openai/gpt-audio-mini" {
		oggBytes, dur, err := t.synthesizePureSpeech(ctx, cleanText)
		if err == nil && len(oggBytes) > 0 {
			return oggBytes, dur, nil
		}
		if isSignor {
			return nil, 0, fmt.Errorf("signor speech synthesis failed: %w", err)
		}
		fmt.Printf("[⚠️ Pure TTS Warning] /audio/speech failed (%v), trying chat audio fallback...\n", err)
	}

	// 2. Fallback to multimodal chat completions audio
	return t.synthesizeChatAudio(ctx, cleanText)
}

// SplitTextIntoChunks divides text into chunks of at most maxChunkLen characters (default 220),
// preserving all content without dropping words, respecting explicit delimiters like "||"
// and natural boundaries (paragraphs, lines, sentence terminators), while keeping audio expression
// tags ([laughs], [whispers], [pause]) intact without cutting them in the middle.
func SplitTextIntoChunks(text string, maxChunkLen int) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if maxChunkLen <= 0 {
		maxChunkLen = 220
	}

	// 1. If text contains explicit delimiter "||", split on it first
	if strings.Contains(text, "||") {
		rawParts := strings.Split(text, "||")
		var result []string
		for _, part := range rawParts {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if len([]rune(part)) <= maxChunkLen {
				result = append(result, part)
			} else {
				// Sub-split this part if it exceeds maxChunkLen
				sub := splitTextByBoundaries(part, maxChunkLen)
				result = append(result, sub...)
			}
		}
		if len(result) > 0 {
			return result
		}
	}

	return splitTextByBoundaries(text, maxChunkLen)
}

func splitTextByBoundaries(text string, maxChunkLen int) []string {
	text = strings.TrimSpace(text)
	runes := []rune(text)
	if len(runes) <= maxChunkLen {
		return []string{text}
	}

	// Step 1: break into small atomic segments (lines or sentence fragments)
	var segments []string
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if len([]rune(line)) <= maxChunkLen {
			segments = append(segments, line)
		} else {
			// Split long line by punctuation or words
			sub := splitLineIntoSentences(line, maxChunkLen)
			segments = append(segments, sub...)
		}
	}

	if len(segments) == 0 {
		return []string{text}
	}

	// Step 2: Combine segments into chunks up to maxChunkLen
	var chunks []string
	var currentChunk strings.Builder
	currentLen := 0

	for _, seg := range segments {
		segLen := len([]rune(seg))
		// If adding seg exceeds maxChunkLen and currentChunk is not empty, push currentChunk
		if currentLen > 0 && currentLen+segLen+1 > maxChunkLen {
			chunks = append(chunks, currentChunk.String())
			currentChunk.Reset()
			currentLen = 0
		}

		if currentChunk.Len() > 0 {
			currentChunk.WriteString("\n")
			currentLen++
		}
		currentChunk.WriteString(seg)
		currentLen += segLen
	}

	if currentChunk.Len() > 0 {
		chunks = append(chunks, currentChunk.String())
	}

	return chunks
}

func splitLineIntoSentences(line string, maxLen int) []string {
	line = strings.TrimSpace(line)
	lineRunes := []rune(line)
	if len(lineRunes) <= maxLen {
		return []string{line}
	}

	var results []string
	var current strings.Builder
	currRunes := 0
	inBracket := false

	isPunct := func(r rune) bool {
		return r == '.' || r == '!' || r == '?' || r == '؟' || r == ':' || r == '،' || r == ',' || r == ';'
	}

	for i := 0; i < len(lineRunes); i++ {
		r := lineRunes[i]
		if r == '[' {
			inBracket = true
		} else if r == ']' {
			inBracket = false
		}

		current.WriteRune(r)
		currRunes++

		if !inBracket {
			// Check if we hit punctuation followed by space or end of string
			if isPunct(r) && (i+1 == len(lineRunes) || lineRunes[i+1] == ' ') {
				if currRunes >= maxLen/3 || currRunes >= maxLen {
					seg := strings.TrimSpace(current.String())
					if seg != "" {
						results = append(results, seg)
					}
					current.Reset()
					currRunes = 0
				}
			} else if currRunes >= maxLen && r == ' ' {
				seg := strings.TrimSpace(current.String())
				if seg != "" {
					results = append(results, seg)
				}
				current.Reset()
				currRunes = 0
			} else if currRunes >= maxLen+80 {
				seg := strings.TrimSpace(current.String())
				if seg != "" {
					results = append(results, seg)
				}
				current.Reset()
				currRunes = 0
			}
		}
	}

	if current.Len() > 0 {
		seg := strings.TrimSpace(current.String())
		if seg != "" {
			results = append(results, seg)
		}
	}

	return results
}

func (t *OpenRouterTTS) fetchSingleSpeechAudio(ctx context.Context, chunk string, format string, isSignor bool) ([]byte, error) {
	endpoint := t.getSpeechURL()

	var reqBody audioSpeechRequest
	if isSignor {
		// Signor AI follows OpenAI Audio Speech standard without requiring response_format
		reqBody = audioSpeechRequest{
			Model: t.model,
			Input: chunk,
			Voice: t.voice,
		}
	} else {
		reqBody = audioSpeechRequest{
			Model:          t.model,
			Input:          chunk,
			Voice:          t.voice,
			ResponseFormat: format,
		}
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal pure speech request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create speech request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+t.apiKey)
	req.Header.Set("Content-Type", "application/json")
	if strings.Contains(endpoint, "openrouter.ai") {
		req.Header.Set("HTTP-Referer", "https://github.com/makari/novabot")
		req.Header.Set("X-Title", "Nova WhatsApp Bot Voice")
	}

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("pure speech API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBytes, _ := io.ReadAll(resp.Body)
		// If Signor AI returns 503 or 500 (service_error), attempt fallback to alternate endpoints like /v1/audio/speech
		if isSignor && (resp.StatusCode == http.StatusServiceUnavailable || resp.StatusCode == http.StatusInternalServerError) {
			var altEndpoints []string
			if strings.Contains(endpoint, "api-2") {
				altEndpoints = append(altEndpoints, strings.Replace(endpoint, "api-2/v1", "v1", 1))
			} else if !strings.Contains(endpoint, "api-2") {
				altEndpoints = append(altEndpoints, strings.Replace(endpoint, "/v1", "/api-2/v1", 1))
			}

			for _, altEndpoint := range altEndpoints {
				fmt.Printf("[⚠️ TTS Fallback] %s returned status %d. Trying alternate endpoint: %s...\n", endpoint, resp.StatusCode, altEndpoint)
				altReq, altErr := http.NewRequestWithContext(ctx, http.MethodPost, altEndpoint, bytes.NewReader(bodyBytes))
				if altErr == nil {
					altReq.Header.Set("Authorization", "Bearer "+t.apiKey)
					altReq.Header.Set("Content-Type", "application/json")
					altResp, altDoErr := t.httpClient.Do(altReq)
					if altDoErr == nil {
						defer altResp.Body.Close()
						if altResp.StatusCode >= 200 && altResp.StatusCode < 300 {
							altAudio, altReadErr := io.ReadAll(altResp.Body)
							if altReadErr == nil && len(altAudio) > 0 {
								fmt.Printf("[✅ TTS Fallback Success] Alternate endpoint %s succeeded! Switching base URL...\n", altEndpoint)
								t.baseURL = strings.TrimSuffix(altEndpoint, "/audio/speech")
								return altAudio, nil
							}
						} else {
							altErrBytes, _ := io.ReadAll(altResp.Body)
							fmt.Printf("[⚠️ TTS Fallback Failed] %s returned status %d: %s\n", altEndpoint, altResp.StatusCode, string(altErrBytes))
						}
					}
				}
			}
		}
		return nil, fmt.Errorf("pure speech API error (status %d): %s", resp.StatusCode, string(respBytes))
	}

	audioBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read speech audio stream: %w", err)
	}

	if len(audioBytes) == 0 {
		return nil, fmt.Errorf("received empty audio from speech API")
	}

	return audioBytes, nil
}

func (t *OpenRouterTTS) synthesizePureSpeech(ctx context.Context, cleanText string) ([]byte, uint32, error) {
	endpoint := t.getSpeechURL()
	isSignor := strings.Contains(endpoint, "signor") || (t.baseURL != "" && !strings.Contains(endpoint, "openrouter.ai"))

	format := "mp3"
	if !isSignor && strings.Contains(strings.ToLower(t.model), "gemini") {
		format = "pcm"
	}

	// Chunk text into <= 220 characters so Signor TTS gateway processes each chunk smoothly without 503 timeouts
	chunks := SplitTextIntoChunks(cleanText, 220)
	if len(chunks) == 0 {
		return nil, 0, fmt.Errorf("no speech text to synthesize")
	}

	var combinedAudio []byte
	totalChunks := len(chunks)

	for i, chunk := range chunks {
		if totalChunks > 1 {
			fmt.Printf("\n[🎙️ TTS Chunk %d/%d] Synthesizing speech via %s (endpoint: %s) for chunk (%d chars)...\n", i+1, totalChunks, t.model, endpoint, len([]rune(chunk)))
		} else {
			fmt.Printf("\n[🎙️ TTS] Synthesizing speech via %s (endpoint: %s, voice: %s) for text (%d chars)...\n", t.model, endpoint, t.voice, len([]rune(chunk)))
		}

		var audioBytes []byte
		var err error

		// Retry up to 3 times with exponential backoff on transient 503/500
		for attempt := 1; attempt <= 3; attempt++ {
			audioBytes, err = t.fetchSingleSpeechAudio(ctx, chunk, format, isSignor)
			if err == nil && len(audioBytes) > 0 {
				break
			}
			if attempt < 3 {
				backoff := time.Duration(attempt*500) * time.Millisecond
				fmt.Printf("[⚠️ TTS Chunk %d/%d] Attempt %d failed (%v). Retrying in %v...\n", i+1, totalChunks, attempt, err, backoff)
				select {
				case <-ctx.Done():
					return nil, 0, ctx.Err()
				case <-time.After(backoff):
				}
			}
		}

		if err != nil || len(audioBytes) == 0 {
			if len(combinedAudio) == 0 {
				return nil, 0, fmt.Errorf("chunk %d/%d synthesis failed: %w", i+1, totalChunks, err)
			}
			fmt.Printf("[⚠️ TTS Chunk %d/%d] Failed after 3 attempts (%v), proceeding with available audio...\n", i+1, totalChunks, err)
			continue
		}

		combinedAudio = append(combinedAudio, audioBytes...)
	}

	fmt.Printf("[✅ Pure TTS] Received total %d bytes audio across %d chunk(s), converting to WhatsApp OGG Opus...\n", len(combinedAudio), totalChunks)
	var oggBytes []byte
	var convErr error
	if !isSignor && format == "pcm" {
		oggBytes, convErr = ConvertPCMToOggOpus(ctx, combinedAudio)
	} else {
		oggBytes, convErr = ConvertAnyAudioToOggOpus(ctx, combinedAudio)
	}
	if convErr != nil {
		return nil, 0, fmt.Errorf("failed to convert audio to ogg opus: %w", convErr)
	}

	durationSec := uint32(len([]rune(cleanText)) / 12)
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
