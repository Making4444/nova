package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

const groqChatCompletionsURL = "https://api.groq.com/openai/v1/chat/completions"

// QueryCategory represents the category of the incoming prompt.
type QueryCategory string

const (
	CategoryChat   QueryCategory = "chat"
	CategoryMath   QueryCategory = "math"
	CategoryVision QueryCategory = "vision"
)

// GroqRouter fast-classifies text queries to pick the optimal model.
type GroqRouter struct {
	apiKey     string
	model      string
	httpClient *http.Client
}

// NewGroqRouter creates a new fast router instance using Groq.
func NewGroqRouter(apiKey, model string) *GroqRouter {
	if model == "" {
		model = "llama-3.3-70b-versatile"
	}
	return &GroqRouter{
		apiKey: apiKey,
		model:  model,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

type groqChatRequest struct {
	Model       string              `json:"model"`
	Messages    []openRouterMessage `json:"messages"`
	MaxTokens   int                 `json:"max_tokens"`
	Temperature float64             `json:"temperature"`
}

// ClassifyQuery returns "math" if the query is a mathematical/logic problem, or "chat" otherwise.
func (r *GroqRouter) ClassifyQuery(ctx context.Context, text string) QueryCategory {
	if r.apiKey == "" || strings.TrimSpace(text) == "" {
		return fallbackClassify(text)
	}

	reqBody := groqChatRequest{
		Model: r.model,
		Messages: []openRouterMessage{
			{
				Role:    "system",
				Content: "You are a fast binary query classifier for an AI assistant. Analyze the user message and determine if it requires advanced mathematical solving, calculus, algebraic equations, geometry, arithmetic calculations, or complex physics/logic puzzles ('math'), or if it is normal conversation, general question, greeting, joke, roleplay, search query, or advice ('chat'). Respond with ONLY one word: 'math' or 'chat'.",
			},
			{
				Role:    "user",
				Content: text,
			},
		},
		MaxTokens:   5,
		Temperature: 0.0,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fallbackClassify(text)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, groqChatCompletionsURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return fallbackClassify(text)
	}

	req.Header.Set("Authorization", "Bearer "+r.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return fallbackClassify(text)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fallbackClassify(text)
	}

	var orResp openRouterResponse
	if err := json.Unmarshal(respBytes, &orResp); err != nil || len(orResp.Choices) == 0 {
		return fallbackClassify(text)
	}

	if content, ok := orResp.Choices[0].Message.Content.(string); ok {
		clean := strings.ToLower(strings.TrimSpace(content))
		if strings.Contains(clean, "math") {
			return CategoryMath
		}
	}

	return CategoryChat
}

func fallbackClassify(text string) QueryCategory {
	lower := strings.ToLower(text)
	mathKeywords := []string{
		"احسب", "معادلة", "تفاضل", "تكامل", "جذر", "مسألة", "احسبلي", "ناتج",
		"رياضيات", "هندسة", "مثلثات", "+", "*", "/", "^", "integral", "derivative",
		"solve", "equation", "calculate", "math",
	}

	for _, kw := range mathKeywords {
		if strings.Contains(lower, kw) {
			return CategoryMath
		}
	}
	return CategoryChat
}
