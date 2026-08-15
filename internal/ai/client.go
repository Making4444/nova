package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"novabot/internal/trigger"
)

const (
	openRouterAPIURL = "https://openrouter.ai/api/v1/chat/completions"
	maxFailures      = 3
)

// OpenRouterClient interacts with OpenRouter API to generate Nova responses.
type OpenRouterClient struct {
	apiKey              string
	model               string
	systemPrompt        string
	httpClient          *http.Client
	mu                  sync.Mutex
	consecutiveFailures int
}

// NewOpenRouterClient creates a new client for OpenRouter.
func NewOpenRouterClient(apiKey, model, systemPrompt string) *OpenRouterClient {
	return &OpenRouterClient{
		apiKey:       apiKey,
		model:        model,
		systemPrompt: systemPrompt,
		httpClient: &http.Client{
			Timeout: 45 * time.Second,
		},
	}
}

type openRouterMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openRouterRequest struct {
	Model          string              `json:"model"`
	Messages       []openRouterMessage `json:"messages"`
	ResponseFormat *responseFormat     `json:"response_format,omitempty"`
}

type responseFormat struct {
	Type string `json:"type"`
}

type openRouterChoice struct {
	Message openRouterMessage `json:"message"`
}

type openRouterResponse struct {
	Choices []openRouterChoice `json:"choices"`
	Error   *struct {
		Message string `json:"message"`
		Code    int    `json:"code"`
	} `json:"error,omitempty"`
}

// GenerateResponse sends the payload to OpenRouter, performs strict JSON parsing, and retries once if parsing fails.
func (c *OpenRouterClient) GenerateResponse(ctx context.Context, payload *trigger.RequestPayload) (*ResponsePayload, error) {
	if c.apiKey == "" {
		return nil, errors.New("OPENROUTER_API_KEY is not set")
	}

	c.mu.Lock()
	if c.consecutiveFailures >= maxFailures {
		// Log circuit breaker notice, but allow trying after some backoff
	}
	c.mu.Unlock()

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request payload: %w", err)
	}

	messages := []openRouterMessage{
		{Role: "system", Content: c.systemPrompt},
		{Role: "user", Content: string(payloadBytes)},
	}

	// 1. Initial attempt
	rawResp, err := c.callAPI(ctx, messages)
	if err != nil {
		c.recordFailure()
		return nil, fmt.Errorf("openrouter request failed: %w", err)
	}

	parsed, parseErr := ParseResponse(rawResp)
	if parseErr == nil {
		c.recordSuccess()
		return parsed, nil
	}

	// 2. Single retry with correction prompt
	retryMessages := append(messages,
		openRouterMessage{Role: "assistant", Content: rawResp},
		openRouterMessage{
			Role:    "user",
			Content: "Your previous response was not valid JSON matching the schema. Please return ONLY a valid raw JSON object matching the required schema with no markdown code fences or explanations.",
		},
	)

	rawRetryResp, retryErr := c.callAPI(ctx, retryMessages)
	if retryErr != nil {
		c.recordFailure()
		return nil, fmt.Errorf("retry openrouter request failed: %w", retryErr)
	}

	parsedRetry, retryParseErr := ParseResponse(rawRetryResp)
	if retryParseErr != nil {
		c.recordFailure()
		return nil, fmt.Errorf("failed to parse AI response after retry (%w): initial raw: %q, retry raw: %q", retryParseErr, rawResp, rawRetryResp)
	}

	c.recordSuccess()
	return parsedRetry, nil
}

func (c *OpenRouterClient) callAPI(ctx context.Context, messages []openRouterMessage) (string, error) {
	reqBody := openRouterRequest{
		Model:          c.model,
		Messages:       messages,
		ResponseFormat: &responseFormat{Type: "json_object"},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, openRouterAPIURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", err
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("HTTP-Referer", "https://github.com/makari/novabot")
	req.Header.Set("X-Title", "Nova WhatsApp Bot")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("openrouter API error (status %d): %s", resp.StatusCode, string(respBytes))
	}

	var orResp openRouterResponse
	if err := json.Unmarshal(respBytes, &orResp); err != nil {
		return "", fmt.Errorf("failed to decode openrouter response: %w", err)
	}

	if orResp.Error != nil {
		return "", fmt.Errorf("openrouter returned error: %s (code: %d)", orResp.Error.Message, orResp.Error.Code)
	}

	if len(orResp.Choices) == 0 {
		return "", errors.New("openrouter returned empty choices list")
	}

	return strings.TrimSpace(orResp.Choices[0].Message.Content), nil
}

func (c *OpenRouterClient) recordSuccess() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.consecutiveFailures = 0
}

func (c *OpenRouterClient) recordFailure() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.consecutiveFailures++
}
