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
	CategoryChat     QueryCategory = "chat"
	CategoryMath     QueryCategory = "math"
	CategoryAcademic QueryCategory = "academic"
	CategoryVision   QueryCategory = "vision"
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
	if r == nil || r.apiKey == "" || strings.TrimSpace(text) == "" {
		return fallbackClassify(text)
	}

	reqBody := groqChatRequest{
		Model: r.model,
		Messages: []openRouterMessage{
			{
				Role:    "system",
				Content: "You are a fast query classifier for an AI assistant. Analyze the user message and determine its category:\n- 'math': advanced mathematical calculations, algebra, calculus, geometry, arithmetic, logic puzzles.\n- 'academic': questions about school/university curricula, Egyptian secondary school (ثانوي), subjects like psychology, sociology (علم نفس / اجتماع), history, biology, chemistry, physics, integrated science (علوم متكاملة), Arabic grammar/rules (نحو/بلاغة), English/French lessons, textbook exercises.\n- 'chat': normal conversation, greetings, humor, advice, personal chat, general queries.\nRespond with ONLY one word: 'math', 'academic', or 'chat'.",
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
		if strings.Contains(clean, "academic") {
			return CategoryAcademic
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

	academicKeywords := []string{
		"ثانوي", "أولى ثانوي", "تانية ثانوي", "منهج", "كتاب الوزارة", "علم النفس", "علم الاجتماع",
		"البناء الاجتماعي", "العلوم المتكاملة", "تاريخ مصر", "فلسفة ومنطق", "نحو", "اعراب", "إعراب",
		"بلاغة", "قصة واسلاماه", "عنترة", "المنهج الجديد", "مادة",
	}
	for _, kw := range academicKeywords {
		if strings.Contains(lower, kw) {
			return CategoryAcademic
		}
	}

	return CategoryChat
}
