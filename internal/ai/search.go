package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// SearchEngine handles querying Perplexity on OpenRouter for live web search results.
type SearchEngine struct {
	apiKey     string
	searchModel string
	httpClient *http.Client
}

// NewSearchEngine creates a new search engine instance.
func NewSearchEngine(apiKey string, searchModel string) *SearchEngine {
	if searchModel == "" {
		searchModel = "perplexity/sonar"
	}
	return &SearchEngine{
		apiKey:      apiKey,
		searchModel: searchModel,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

type searchRequest struct {
	Model    string              `json:"model"`
	Messages []openRouterMessage `json:"messages"`
}

// Search executes a real-time web query via Perplexity on OpenRouter and returns the text results.
func (s *SearchEngine) Search(ctx context.Context, query string) (string, error) {
	if strings.TrimSpace(query) == "" {
		return "", fmt.Errorf("search query cannot be empty")
	}

	reqBody := searchRequest{
		Model: s.searchModel,
		Messages: []openRouterMessage{
			{
				Role:    "system",
				Content: "You are a fast, factual real-time web search engine. Search the internet and provide a concise, factual, and accurate summary with relevant details, numbers, dates, and prices for the user's query.",
			},
			{
				Role:    "user",
				Content: query,
			},
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal search request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, openRouterAPIURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("failed to create search http request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("HTTP-Referer", "https://github.com/makari/novabot")
	req.Header.Set("X-Title", "Nova WhatsApp Bot Search")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("perplexity search request failed: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read search response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("search API error (status %d): %s", resp.StatusCode, string(respBytes))
	}

	var orResp openRouterResponse
	if err := json.Unmarshal(respBytes, &orResp); err != nil {
		return "", fmt.Errorf("failed to decode search response: %w", err)
	}

	if orResp.Error != nil {
		return "", fmt.Errorf("search API returned error: %s", orResp.Error.Message)
	}

	if len(orResp.Choices) == 0 {
		return "", fmt.Errorf("empty choices returned from search API")
	}

	if str, ok := orResp.Choices[0].Message.Content.(string); ok {
		return strings.TrimSpace(str), nil
	}

	return fmt.Sprintf("%v", orResp.Choices[0].Message.Content), nil
}
