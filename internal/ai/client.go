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
	maxToolSteps     = 4
)

// TaskScheduler represents an engine that can schedule future autonomous messages.
type TaskScheduler interface {
	ScheduleTask(chatID, chatType, targetUser, reason, durationStr string) string
}

// OpenRouterClient interacts with OpenRouter API to generate Nova responses with Search, Scheduler, and Vision capabilities.
type OpenRouterClient struct {
	apiKey              string
	model               string
	systemPrompt        string
	searchEngine        *SearchEngine
	scheduler           TaskScheduler
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
		searchEngine: NewSearchEngine(apiKey, "perplexity/sonar"),
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// SetScheduler attaches a task scheduler instance to the AI client.
func (c *OpenRouterClient) SetScheduler(scheduler TaskScheduler) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.scheduler = scheduler
}

type openRouterMessage struct {
	Role       string      `json:"role"`
	Content    interface{} `json:"content"` // string or []contentPart
	ToolCallID string      `json:"tool_call_id,omitempty"`
	ToolCalls  []toolCall  `json:"tool_calls,omitempty"`
}

type contentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *imageURL `json:"image_url,omitempty"`
}

type imageURL struct {
	URL string `json:"url"`
}

type toolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function toolFunction `json:"function"`
}

type toolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type toolDefinition struct {
	Type     string      `json:"type"`
	Function functionDef `json:"function"`
}

type functionDef struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Parameters  interface{} `json:"parameters"`
}

var webSearchTool = toolDefinition{
	Type: "function",
	Function: functionDef{
		Name:        "web_search",
		Description: "ابحث في الإنترنت لحظياً عن أخبار، أسعار، أحداث، حقائق، مواعيد، أو أي معلومات تحتاجها نوفا قبل الرد",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "جملة أو كلمات البحث للبحث عنها في الإنترنت",
				},
			},
			"required": []string{"query"},
		},
	},
}

var scheduleFollowupTool = toolDefinition{
	Type: "function",
	Function: functionDef{
		Name:        "schedule_followup",
		Description: "جدولة موعد لاحق لتقوم نوفا بالرجوع والتحدث في الشات بعد فترة زمنية معينة (ساعة، ساعتين، بكرة، إلخ)",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"duration": map[string]interface{}{
					"type":        "string",
					"description": "المدة الزمنية للرجوع، مثلاً: '1h', '2h', '30m', '1d', 'ساعتين'",
				},
				"reason": map[string]interface{}{
					"type":        "string",
					"description": "السبب أو سياق المتابعة (مثلاً: 'اسأل مكاري عمل ايه عند الدكتور')",
				},
				"target_user": map[string]interface{}{
					"type":        "string",
					"description": "اسم أو معرف الشخص المستهدف بالسؤال (اختياري)",
				},
			},
			"required": []string{"duration", "reason"},
		},
	},
}

type openRouterRequest struct {
	Model    string              `json:"model"`
	Messages []openRouterMessage `json:"messages"`
	Tools    []toolDefinition    `json:"tools,omitempty"`
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

// GenerateResponse sends the payload to OpenRouter, executes tool calls, and parses the final JSON response.
func (c *OpenRouterClient) GenerateResponse(ctx context.Context, payload *trigger.RequestPayload) (*ResponsePayload, error) {
	if c.apiKey == "" {
		return nil, errors.New("OPENROUTER_API_KEY is not set")
	}

	// Prepare user message content (Multimodal if MediaDataURL exists)
	var userContent interface{}
	mediaURL := payload.MediaDataURL

	// Temporarily remove base64 from JSON payload text to avoid inflating token size in text
	payloadForText := *payload
	payloadForText.MediaDataURL = nil
	payloadBytes, err := json.Marshal(payloadForText)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request payload: %w", err)
	}

	if mediaURL != nil && *mediaURL != "" {
		userContent = []contentPart{
			{
				Type: "text",
				Text: string(payloadBytes),
			},
			{
				Type: "image_url",
				ImageURL: &imageURL{
					URL: *mediaURL,
				},
			},
		}
	} else {
		userContent = string(payloadBytes)
	}

	messages := []openRouterMessage{
		{Role: "system", Content: c.systemPrompt},
		{Role: "user", Content: userContent},
	}

	// 1. Execute initial attempt with Tool Calling loop
	rawResp, usedSearch, err := c.executeConversation(ctx, messages, payload)
	if err != nil {
		c.recordFailure()
		return nil, fmt.Errorf("openrouter request failed: %w", err)
	}

	parsed, parseErr := ParseResponse(rawResp)
	if parseErr == nil {
		c.recordSuccess()
		if usedSearch && parsed.ReplyText != nil && *parsed.ReplyText != "" {
			trimmed := strings.TrimSpace(*parsed.ReplyText)
			modified := "تم البحث\n\n" + trimmed
			parsed.ReplyText = &modified
		}
		return parsed, nil
	}

	// 2. Single retry with correction prompt if JSON parsing fails
	retryMessages := append(messages,
		openRouterMessage{Role: "assistant", Content: rawResp},
		openRouterMessage{
			Role:    "user",
			Content: "Your previous response was not valid JSON matching the schema. Please return ONLY a valid raw JSON object matching the required schema with no markdown code fences or explanations.",
		},
	)

	rawRetryResp, retryUsedSearch, retryErr := c.executeConversation(ctx, retryMessages, payload)
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
	if (usedSearch || retryUsedSearch) && parsedRetry.ReplyText != nil && *parsedRetry.ReplyText != "" {
		trimmed := strings.TrimSpace(*parsedRetry.ReplyText)
		modified := "تم البحث\n\n" + trimmed
		parsedRetry.ReplyText = &modified
	}

	return parsedRetry, nil
}

// executeConversation executes the message list, handling any tool calls (web_search, schedule_followup).
func (c *OpenRouterClient) executeConversation(ctx context.Context, messages []openRouterMessage, payload *trigger.RequestPayload) (string, bool, error) {
	currentMessages := make([]openRouterMessage, len(messages))
	copy(currentMessages, messages)
	usedSearch := false

	for step := 0; step < maxToolSteps; step++ {
		choiceMsg, err := c.callAPI(ctx, currentMessages)
		if err != nil {
			return "", usedSearch, err
		}

		// If the model did not request any tool calls, return the text content
		if len(choiceMsg.ToolCalls) == 0 {
			if contentStr, ok := choiceMsg.Content.(string); ok {
				return strings.TrimSpace(contentStr), usedSearch, nil
			}
			return "", usedSearch, fmt.Errorf("unexpected content format in model response")
		}

		// Process tool calls
		currentMessages = append(currentMessages, choiceMsg)

		for _, call := range choiceMsg.ToolCalls {
			switch call.Function.Name {
			case "web_search":
				usedSearch = true
				var args struct {
					Query string `json:"query"`
				}
				_ = json.Unmarshal([]byte(call.Function.Arguments), &args)

				query := strings.TrimSpace(args.Query)
				if query == "" {
					query = call.Function.Arguments
				}

				fmt.Printf("\n[🔍 Web Search] Query: %q\n", query)
				searchResult, searchErr := c.searchEngine.Search(ctx, query)
				if searchErr != nil {
					fmt.Printf("[⚠️ Web Search] Error: %v\n", searchErr)
					searchResult = fmt.Sprintf("فشل البحث على الإنترنت: %v", searchErr)
				} else {
					fmt.Printf("[✅ Web Search] Success: Perplexity returned %d chars\n", len(searchResult))
				}

				currentMessages = append(currentMessages, openRouterMessage{
					Role:       "tool",
					ToolCallID: call.ID,
					Content:    searchResult,
				})

			case "schedule_followup":
				var args struct {
					Duration   string `json:"duration"`
					Reason     string `json:"reason"`
					TargetUser string `json:"target_user"`
				}
				_ = json.Unmarshal([]byte(call.Function.Arguments), &args)

				fmt.Printf("\n[⏰ Scheduler Tool] Nova scheduled follow-up: duration=%q, reason=%q, user=%q\n", args.Duration, args.Reason, args.TargetUser)

				toolResult := "تم تسجيل المتابعة المجدولة في السيرفر بنجاح وسيتم تذكيرك بها."
				c.mu.Lock()
				sch := c.scheduler
				c.mu.Unlock()

				if sch != nil && payload != nil {
					target := args.TargetUser
					if target == "" {
						target = payload.SenderName
					}
					taskID := sch.ScheduleTask(payload.ChatID, payload.ChatType, target, args.Reason, args.Duration)
					toolResult = fmt.Sprintf("تمت جدولة المهمة برقم %s للمتابعة بعد %s", taskID, args.Duration)
				}

				currentMessages = append(currentMessages, openRouterMessage{
					Role:       "tool",
					ToolCallID: call.ID,
					Content:    toolResult,
				})
			}
		}
	}

	return "", usedSearch, fmt.Errorf("exceeded maximum tool calling steps")
}

func (c *OpenRouterClient) callAPI(ctx context.Context, messages []openRouterMessage) (openRouterMessage, error) {
	reqBody := openRouterRequest{
		Model:    c.model,
		Messages: messages,
		Tools:    []toolDefinition{webSearchTool, scheduleFollowupTool},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return openRouterMessage{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, openRouterAPIURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return openRouterMessage{}, err
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("HTTP-Referer", "https://github.com/makari/novabot")
	req.Header.Set("X-Title", "Nova WhatsApp Bot")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return openRouterMessage{}, err
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return openRouterMessage{}, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return openRouterMessage{}, fmt.Errorf("openrouter API error (status %d): %s", resp.StatusCode, string(respBytes))
	}

	var orResp openRouterResponse
	if err := json.Unmarshal(respBytes, &orResp); err != nil {
		return openRouterMessage{}, fmt.Errorf("failed to decode openrouter response: %w", err)
	}

	if orResp.Error != nil {
		return openRouterMessage{}, fmt.Errorf("openrouter returned error: %s (code: %d)", orResp.Error.Message, orResp.Error.Code)
	}

	if len(orResp.Choices) == 0 {
		return openRouterMessage{}, errors.New("openrouter returned empty choices list")
	}

	return orResp.Choices[0].Message, nil
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
