package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"novabot/internal/tools"
)

const defaultOpenRouterURL = "https://openrouter.ai/api/v1/chat/completions"

// OpenRouterLLMClient implements LLMClient using OpenRouter or OpenAI-compatible Chat Completions API.
type OpenRouterLLMClient struct {
	apiKey     string
	apiURL     string
	httpClient *http.Client
}

// NewOpenRouterLLMClient creates a new OpenRouterLLMClient.
func NewOpenRouterLLMClient(apiKey string) *OpenRouterLLMClient {
	return &OpenRouterLLMClient{
		apiKey: apiKey,
		apiURL: defaultOpenRouterURL,
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

// SetBaseURL allows overriding the API endpoint for unit testing or custom proxies.
func (c *OpenRouterLLMClient) SetBaseURL(url string) {
	if url != "" {
		c.apiURL = url
	}
}

type openRouterRequestBody struct {
	Model     string                 `json:"model"`
	Messages  []LLMMessage           `json:"messages"`
	Tools     []tools.ToolDefinition `json:"tools,omitempty"`
	MaxTokens int                    `json:"max_tokens,omitempty"`
}

type openRouterResponseBody struct {
	Choices []struct {
		Message struct {
			Role       string        `json:"role"`
			Content    interface{}   `json:"content"`
			ToolCalls  []LLMToolCall `json:"tool_calls,omitempty"`
			ToolCallID string        `json:"tool_call_id,omitempty"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Code    int    `json:"code"`
	} `json:"error,omitempty"`
}

// Call makes a chat completion request to the LLM API.
func (c *OpenRouterLLMClient) Call(
	ctx context.Context,
	model string,
	messages []LLMMessage,
	toolDefs []tools.ToolDefinition,
) (*LLMResult, error) {
	reqBody := openRouterRequestBody{
		Model:     model,
		Messages:  messages,
		Tools:     toolDefs,
		MaxTokens: 2000,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create http request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("HTTP-Referer", "https://github.com/makari/novabot")
	req.Header.Set("X-Title", "Nova WhatsApp Bot Agent Swarm")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("api returned status %d: %s", resp.StatusCode, string(respBytes))
	}

	var parsed openRouterResponseBody
	if err := json.Unmarshal(respBytes, &parsed); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if parsed.Error != nil {
		return nil, fmt.Errorf("api error: %s (code: %d)", parsed.Error.Message, parsed.Error.Code)
	}

	if len(parsed.Choices) == 0 {
		return nil, fmt.Errorf("empty choices in response")
	}

	choice := parsed.Choices[0]
	var contentStr string
	if str, ok := choice.Message.Content.(string); ok {
		contentStr = str
	}

	return &LLMResult{
		Content:      contentStr,
		ToolCalls:    choice.Message.ToolCalls,
		FinishReason: choice.FinishReason,
		ModelUsed:    model,
	}, nil
}
