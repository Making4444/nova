package image

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

var dataURIRegex = regexp.MustCompile(`data:image/([a-zA-Z0-9+.-]+);base64,([A-Za-z0-9+/=\r\n]+)`)

// Generator handles AI image generation with Signor AI and OpenAI-compatible providers.
type Generator struct {
	apiKey     string
	baseURL    string
	model      string
	httpClient *http.Client
	mu         sync.RWMutex
}

// NewGenerator creates a new Image Generator instance.
func NewGenerator(apiKey, baseURL, model string) *Generator {
	if baseURL == "" {
		baseURL = "https://api.code.signor.ai/v1"
	}
	if model == "" {
		model = "gpt-image-2.5-sunburst"
	}
	return &Generator{
		apiKey:  strings.TrimSpace(apiKey),
		baseURL: strings.TrimSpace(baseURL),
		model:   strings.TrimSpace(model),
		httpClient: &http.Client{
			Timeout: 90 * time.Second,
		},
	}
}

// SetConfig dynamically updates the image generator configuration at runtime.
func (g *Generator) SetConfig(baseURL, model string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if strings.TrimSpace(baseURL) != "" {
		g.baseURL = strings.TrimSpace(baseURL)
	}
	if strings.TrimSpace(model) != "" {
		g.model = strings.TrimSpace(model)
	}
}

// GetConfig returns current base URL and model.
func (g *Generator) GetConfig() (string, string) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.baseURL, g.model
}

type chatCompletionResponse struct {
	ID      string `json:"id"`
	Choices []struct {
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Code    string `json:"code"`
	} `json:"error,omitempty"`
}

type openAIImageResponse struct {
	Data []struct {
		URL     string `json:"url,omitempty"`
		B64JSON string `json:"b64_json,omitempty"`
	} `json:"data,omitempty"`
}

// GenerateImage generates an image from prompt and returns image bytes, mime type, USD cost, or error.
func (g *Generator) GenerateImage(ctx context.Context, prompt string) ([]byte, string, float64, error) {
	if strings.TrimSpace(prompt) == "" {
		return nil, "", 0, errors.New("empty image prompt")
	}

	g.mu.RLock()
	apiKey := g.apiKey
	baseURL := g.baseURL
	model := g.model
	g.mu.RUnlock()

	// Try primary configured endpoint
	imgBytes, mime, cost, err := g.callGenerate(ctx, apiKey, baseURL, model, prompt)
	if err == nil && len(imgBytes) > 0 {
		return imgBytes, mime, cost, nil
	}

	// Automatic Fallback: If primary failed and it's Signor AI, try api-2 with gpt-image-2.5-sunburst-c
	if strings.Contains(baseURL, "signor.ai") {
		fallbackBase := "https://api.code.signor.ai/api-2/v1"
		fallbackModel := "gpt-image-2.5-sunburst-c"
		if baseURL != fallbackBase || model != fallbackModel {
			fmt.Printf("\n[🎨 Image Generator] Primary endpoint (%s / %s) failed: %v. Trying verified fallback: %s / %s...\n", baseURL, model, err, fallbackBase, fallbackModel)
			fBytes, fMime, fCost, fErr := g.callGenerate(ctx, apiKey, fallbackBase, fallbackModel, prompt)
			if fErr == nil && len(fBytes) > 0 {
				return fBytes, fMime, fCost, nil
			}
		}
	}

	return nil, "", 0, fmt.Errorf("failed to generate image: %w", err)
}

func (g *Generator) callGenerate(ctx context.Context, apiKey, baseURL, model, prompt string) ([]byte, string, float64, error) {
	endpoint := strings.TrimRight(baseURL, "/")
	if !strings.HasSuffix(endpoint, "/chat/completions") && !strings.HasSuffix(endpoint, "/images/generations") {
		endpoint += "/chat/completions"
	}

	reqBody := map[string]interface{}{
		"model": model,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, "", 0, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, "", 0, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return nil, "", 0, err
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", 0, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, "", 0, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBytes))
	}

	// 1. Try parsing chat completions response with markdown data:image URI
	var chatResp chatCompletionResponse
	if err := json.Unmarshal(respBytes, &chatResp); err == nil && len(chatResp.Choices) > 0 {
		content := chatResp.Choices[0].Message.Content
		if match := dataURIRegex.FindStringSubmatch(content); len(match) == 3 {
			ext := strings.ToLower(match[1])
			cleanB64 := strings.ReplaceAll(match[2], "\n", "")
			cleanB64 = strings.ReplaceAll(cleanB64, "\r", "")
			cleanB64 = strings.TrimSpace(cleanB64)
			decoded, dErr := base64.StdEncoding.DecodeString(cleanB64)
			if dErr == nil && len(decoded) > 0 {
				mime := "image/" + ext
				if ext == "jpg" {
					mime = "image/jpeg"
				}
				return decoded, mime, 0.010, nil // ~$0.010 USD upstream cost
			}
		}

		// Or if content is a direct URL
		if strings.HasPrefix(strings.TrimSpace(content), "http://") || strings.HasPrefix(strings.TrimSpace(content), "https://") {
			rawURL := strings.TrimSpace(content)
			return g.downloadImage(ctx, rawURL)
		}
	}

	// 2. Try parsing OpenAI standard images response
	var imgResp openAIImageResponse
	if err := json.Unmarshal(respBytes, &imgResp); err == nil && len(imgResp.Data) > 0 {
		item := imgResp.Data[0]
		if item.B64JSON != "" {
			decoded, dErr := base64.StdEncoding.DecodeString(item.B64JSON)
			if dErr == nil && len(decoded) > 0 {
				return decoded, "image/png", 0.010, nil
			}
		}
		if item.URL != "" {
			return g.downloadImage(ctx, item.URL)
		}
	}

	return nil, "", 0, fmt.Errorf("could not extract image from response: %s", string(respBytes[:min(len(respBytes), 200)]))
}

func (g *Generator) downloadImage(ctx context.Context, imgURL string) ([]byte, string, float64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imgURL, nil)
	if err != nil {
		return nil, "", 0, err
	}
	resp, err := g.httpClient.Do(req)
	if err != nil {
		return nil, "", 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", 0, fmt.Errorf("failed to download image: HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", 0, err
	}

	mime := resp.Header.Get("Content-Type")
	if mime == "" {
		mime = "image/png"
	}
	return data, mime, 0.010, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
