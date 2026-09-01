package voice

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
)

const groqAudioTranscriptionsURL = "https://api.groq.com/openai/v1/audio/transcriptions"

// GroqSTT handles transcribing voice notes using Groq's whisper-large-v3 model.
type GroqSTT struct {
	apiKey     string
	model      string
	httpClient *http.Client
}

// NewGroqSTT creates a new Groq Speech-to-Text client.
func NewGroqSTT(apiKey, model string) *GroqSTT {
	if model == "" {
		model = "whisper-large-v3"
	}
	return &GroqSTT{
		apiKey: apiKey,
		model:  model,
		httpClient: &http.Client{
			Timeout: 45 * time.Second,
		},
	}
}

type groqTranscriptionResponse struct {
	Text  string `json:"text"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

// TranscribeAudio sends audio bytes (e.g. ogg/opus/mp3/m4a) to Groq Whisper and returns the transcribed text.
func (g *GroqSTT) TranscribeAudio(ctx context.Context, audioBytes []byte, filename string) (string, error) {
	if g == nil {
		return "", fmt.Errorf("groq STT is nil")
	}
	if g.apiKey == "" {
		return "", fmt.Errorf("GROQ_API_KEY is not configured")
	}
	if len(audioBytes) == 0 {
		return "", fmt.Errorf("audio bytes cannot be empty")
	}
	if filename == "" {
		filename = "voice_note.ogg"
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// 1. Model field
	if err := writer.WriteField("model", g.model); err != nil {
		return "", fmt.Errorf("failed to write model field: %w", err)
	}

	// 2. Language hint for Arabic
	if err := writer.WriteField("language", "ar"); err != nil {
		return "", fmt.Errorf("failed to write language field: %w", err)
	}

	// 3. Response format
	if err := writer.WriteField("response_format", "json"); err != nil {
		return "", fmt.Errorf("failed to write response_format field: %w", err)
	}

	// 4. File part
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return "", fmt.Errorf("failed to create form file: %w", err)
	}
	if _, err := part.Write(audioBytes); err != nil {
		return "", fmt.Errorf("failed to write audio data to form: %w", err)
	}

	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("failed to close multipart writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, groqAudioTranscriptionsURL, body)
	if err != nil {
		return "", fmt.Errorf("failed to create http request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+g.apiKey)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("groq transcription request failed: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read groq response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("groq audio API error (status %d): %s", resp.StatusCode, string(respBytes))
	}

	var transResp groqTranscriptionResponse
	if err := json.Unmarshal(respBytes, &transResp); err != nil {
		return "", fmt.Errorf("failed to decode groq transcription response: %w", err)
	}

	if transResp.Error != nil {
		return "", fmt.Errorf("groq transcription error: %s", transResp.Error.Message)
	}

	return strings.TrimSpace(transResp.Text), nil
}
