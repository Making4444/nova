package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"novabot/internal/emotion"
	"novabot/internal/tools"
	"novabot/internal/trigger"
)

// MockLLMClient allows configuring mock responses for agents and router in tests.
type MockLLMClient struct {
	customHandler func(ctx context.Context, model string, messages []LLMMessage, toolDefs []tools.ToolDefinition) (*LLMResult, error)
}

func (m *MockLLMClient) Call(ctx context.Context, model string, messages []LLMMessage, toolDefs []tools.ToolDefinition) (*LLMResult, error) {
	if m.customHandler != nil {
		return m.customHandler(ctx, model, messages, toolDefs)
	}
	return &LLMResult{
		Content:   "Mock response from " + model,
		ModelUsed: model,
	}, nil
}

// 1. Test Router Classification across all 4 categories
func TestRouterClassification(t *testing.T) {
	router := NewRouter(nil, "")
	ctx := context.Background()

	testCases := []struct {
		name        string
		text        string
		hasMedia    bool
		expectAgent AgentType
	}{
		// Research Agent cases
		{"Currency Rate", "سعر الدولار اليوم في البنوك المصرية كام؟", false, AgentTypeResearch},
		{"Gold Price", "بكام الذهب عيار 21 النهاردة؟", false, AgentTypeResearch},
		{"Web Search", "ابحث عن أحدث أخبار الذكاء الاصطناعي في 2026", false, AgentTypeResearch},
		{"Weather Forecast", "حالة الطقس ودرجة الحرارة في الإسكندرية بكرة", false, AgentTypeResearch},
		{"Comparison Query", "مقارنة بين معالجات آبل ومعالجات كوالكوم", false, AgentTypeResearch},
		{"News", "أحدث أخبار ماتش الأهلي والزمالك اليوم", false, AgentTypeResearch},

		// Math & Coding Agent cases
		{"Calculus Integral", "احسب ناتج تكامل x^3 + 2x بالنسبة لـ x", false, AgentTypeMathCoding},
		{"Equation Solving", "حل المعادلة 2x + 5 = 15 وطلع قيمة x", false, AgentTypeMathCoding},
		{"Python Code", "اكتب كود python لتحميل ملف من رابط مع retry logic", false, AgentTypeMathCoding},
		{"Go Programming", "عندي مشكلة في golang goroutine leak وعايز أصلحها", false, AgentTypeMathCoding},
		{"Debugging", "الكود بيرمي error NullPointerException ومش عارف السبب", false, AgentTypeMathCoding},
		{"Logic Puzzle", "لغز منطقي: 3 صناديق مكتوب عليهم تسميات خاطئة", false, AgentTypeMathCoding},
		{"Algorithm", "اشرح خوارزمية Binary Search والـ Big-O complexity", false, AgentTypeMathCoding},

		// Vision & Doc Agent cases
		{"Attached Image", "شوف دي", true, AgentTypeVisionDoc},
		{"Explicit OCR Arabic", "حلل الصورة دي واستخرج النص المكتوب فيها", false, AgentTypeVisionDoc},
		{"PDF Document", "اقرأ ملف pdf ده ولخص الجداول", false, AgentTypeVisionDoc},
		{"Screenshot Analysis", "شوف سكرين شوت الشاشة دي وقولي ايه المشكلة", false, AgentTypeVisionDoc},

		// Persona Agent cases
		{"Greeting", "ازيك يا نوفا يا صاحبي عامل ايه؟", false, AgentTypePersona},
		{"Egyptian Joke", "قولي نكتة مصرية تضحك من قلبك", false, AgentTypePersona},
		{"Emotional Venting", "أنا تعبان ومخنوق وزعلان جداً النهاردة ادعيلي", false, AgentTypePersona},
		{"Casual Banter", "صباح الفل يا برنس منور الشات", false, AgentTypePersona},
		{"Teasing", "روق علينا بإفيه حلو يا فنان", false, AgentTypePersona},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var mediaPtr *string
			if tc.hasMedia {
				m := "data:image/jpeg;base64,mock"
				mediaPtr = &m
			}

			res, err := router.Route(ctx, &trigger.RequestPayload{
				MessageText:  tc.text,
				MediaDataURL: mediaPtr,
			})

			if err != nil {
				t.Fatalf("Route failed unexpectedly: %v", err)
			}

			if res.TargetAgent != tc.expectAgent {
				t.Errorf("query %q: expected agent %s, got %s (reason: %s)", tc.text, tc.expectAgent, res.TargetAgent, res.Reason)
			}

			if res.Confidence <= 0 || res.Confidence > 1.0 {
				t.Errorf("invalid confidence score: %f", res.Confidence)
			}
		})
	}
}

// 2. Test Custom Routing Rules
func TestRouterCustomRules(t *testing.T) {
	router := NewRouter(nil, "")
	ctx := context.Background()

	// Register a custom rule that forces queries mentioning "سري للغاية" to ResearchAgent
	router.RegisterRule("confidential_research", func(text string, hasMedia bool) (AgentType, bool) {
		if strings.Contains(text, "سري للغاية") {
			return AgentTypeResearch, true
		}
		return "", false
	})

	res, err := router.Route(ctx, &trigger.RequestPayload{
		MessageText: "موضوع سري للغاية ازيك يا نوفا",
	})
	if err != nil {
		t.Fatalf("Route failed: %v", err)
	}

	if res.TargetAgent != AgentTypeResearch {
		t.Errorf("expected custom rule to route to research, got %s", res.TargetAgent)
	}
}

// 3. Test LLM-based Fallback Classification
func TestRouterLLMFallback(t *testing.T) {
	mockLLM := &MockLLMClient{
		customHandler: func(ctx context.Context, model string, messages []LLMMessage, toolDefs []tools.ToolDefinition) (*LLMResult, error) {
			jsonResp := `{"agent": "math_coding", "confidence": 0.96, "reason": "Ambiguous math puzzle identified by LLM"}`
			return &LLMResult{Content: jsonResp}, nil
		},
	}

	router := NewRouter(mockLLM, "meta-llama/llama-3.3-70b-instruct")
	ctx := context.Background()

	// Force an ambiguous text that falls into low confidence heuristic
	res, err := router.classifyWithLLM(ctx, mockLLM, "meta-llama/llama-3.3-70b-instruct", "xyz 123 logic query", false)
	if err != nil {
		t.Fatalf("classifyWithLLM failed: %v", err)
	}

	if res.TargetAgent != AgentTypeMathCoding {
		t.Errorf("expected LLM classification to yield math_coding, got %s", res.TargetAgent)
	}
	if res.Confidence != 0.96 {
		t.Errorf("expected confidence 0.96, got %f", res.Confidence)
	}
}

// 4. Test ResearchAgent Execution with Tool-Calling Loop
func TestResearchAgentExecution(t *testing.T) {
	reg := tools.NewRegistry()
	calc := tools.NewCalculatorTool()
	_ = reg.Register(calc)

	stepCount := 0
	mockLLM := &MockLLMClient{
		customHandler: func(ctx context.Context, model string, messages []LLMMessage, toolDefs []tools.ToolDefinition) (*LLMResult, error) {
			stepCount++
			if stepCount == 1 {
				// Step 1: Request tool execution
				return &LLMResult{
					Content: "",
					ToolCalls: []LLMToolCall{
						{
							ID:   "call_1",
							Type: "function",
							Function: LLMToolCallFunction{
								Name:      "calculator",
								Arguments: `{"expression":"50 * 30.5"}`,
							},
						},
					},
				}, nil
			}
			// Step 2: Final response after receiving tool output
			return &LLMResult{
				Content: "سعر 50 دولار يساوي 1525 جنيه مصري بالسعر الحالي.",
			}, nil
		},
	}

	agent := NewResearchAgent(mockLLM, "google/gemini-2.5-flash", reg)
	ctx := context.Background()

	resp, err := agent.Execute(ctx, &AgentRequest{
		Payload: &trigger.RequestPayload{
			MessageText: "احسبلي 50 دولار بالجنيه",
		},
	})

	if err != nil {
		t.Fatalf("ResearchAgent Execute failed: %v", err)
	}

	if resp.AgentType != AgentTypeResearch {
		t.Errorf("expected AgentTypeResearch, got %s", resp.AgentType)
	}
	if len(resp.ToolsUsed) != 1 || resp.ToolsUsed[0] != "calculator" {
		t.Errorf("expected calculator tool used, got %v", resp.ToolsUsed)
	}
	if !strings.Contains(resp.Content, "1525") {
		t.Errorf("expected calculated output in response, got %s", resp.Content)
	}
}

// 5. Test MathCodingAgent Execution
func TestMathCodingAgentExecution(t *testing.T) {
	mockLLM := &MockLLMClient{
		customHandler: func(ctx context.Context, model string, messages []LLMMessage, toolDefs []tools.ToolDefinition) (*LLMResult, error) {
			return &LLMResult{
				Content: `خطوات الحل:
1. الدالة المراد تكاملها هي f(x) = x^2
2. نطبق قاعدة القوى: ∫ x^n dx = (x^(n+1))/(n+1) + C
3. الناتج النهائي: (x^3)/3 + C`,
			}, nil
		},
	}

	agent := NewMathCodingAgent(mockLLM, "deepseek/deepseek-r1", nil)
	ctx := context.Background()

	resp, err := agent.Execute(ctx, &AgentRequest{
		Payload: &trigger.RequestPayload{
			MessageText: "احسب تكامل x^2",
		},
	})

	if err != nil {
		t.Fatalf("MathCodingAgent Execute failed: %v", err)
	}

	if resp.AgentType != AgentTypeMathCoding {
		t.Errorf("expected AgentTypeMathCoding, got %s", resp.AgentType)
	}
	if !strings.Contains(resp.Content, "x^3") {
		t.Errorf("expected integration result in content, got: %s", resp.Content)
	}
	if resp.Confidence < 0.9 {
		t.Errorf("expected high confidence for math coding, got %f", resp.Confidence)
	}
}

// 6. Test VisionDocAgent Execution
func TestVisionDocAgentExecution(t *testing.T) {
	mockLLM := &MockLLMClient{
		customHandler: func(ctx context.Context, model string, messages []LLMMessage, toolDefs []tools.ToolDefinition) (*LLMResult, error) {
			// Verify image part was passed
			if len(messages) >= 2 {
				if parts, ok := messages[1].Content.([]ContentPart); ok {
					if len(parts) == 2 && parts[1].ImageURL != nil {
						return &LLMResult{
							Content: "الصورة تحتوي على فاتورة مطعم بمبلغ 250 جنيه مصري.",
						}, nil
					}
				}
			}
			return &LLMResult{Content: "تحليل بصري للصورة"}, nil
		},
	}

	agent := NewVisionDocAgent(mockLLM, "openai/gpt-5.6-luna")
	ctx := context.Background()

	mediaURL := "data:image/jpeg;base64,aGVsbG8="
	resp, err := agent.Execute(ctx, &AgentRequest{
		Payload: &trigger.RequestPayload{
			MessageText:  "اقرأ الفاتورة اللي في الصورة",
			MediaDataURL: &mediaURL,
		},
	})

	if err != nil {
		t.Fatalf("VisionDocAgent Execute failed: %v", err)
	}

	if resp.AgentType != AgentTypeVisionDoc {
		t.Errorf("expected AgentTypeVisionDoc, got %s", resp.AgentType)
	}
	if !strings.Contains(resp.Content, "فاتورة") {
		t.Errorf("expected invoice details in vision response, got: %s", resp.Content)
	}
}

// 7. Test PersonaAgent Execution with Emotion Context
func TestPersonaAgentExecution(t *testing.T) {
	mockLLM := &MockLLMClient{
		customHandler: func(ctx context.Context, model string, messages []LLMMessage, toolDefs []tools.ToolDefinition) (*LLMResult, error) {
			jsonResp := `{
				"should_reply": true,
				"reply_text": "يا صباح الفل والياسمين! منور الدنيا كلها يا غالي، يومك كله روقان وضحك إن شاء الله! 😂❤️",
				"mood": "Joyful",
				"reaction_emoji": "❤️",
				"memory_note": "شخص ودود وصباحه رايق"
			}`
			return &LLMResult{Content: jsonResp}, nil
		},
	}

	agent := NewPersonaAgent(mockLLM, "google/gemini-2.5-flash")
	ctx := context.Background()

	resp, err := agent.Execute(ctx, &AgentRequest{
		Payload: &trigger.RequestPayload{
			MessageText: "صباح الخير يا أحلى نوفا",
			SenderName:  "Ahmed",
			SenderID:    "201000000001@s.whatsapp.net",
		},
		EmotionPrompt: "- المزاج: رايق ومبسوط\n- درجة الطاقة: 8/10",
	})

	if err != nil {
		t.Fatalf("PersonaAgent Execute failed: %v", err)
	}

	if resp.AgentType != AgentTypePersona {
		t.Errorf("expected AgentTypePersona, got %s", resp.AgentType)
	}
	if !resp.ShouldReply {
		t.Errorf("expected should_reply to be true")
	}
	if resp.Mood != "Joyful" {
		t.Errorf("expected Joyful mood, got %s", resp.Mood)
	}
	if resp.ReactionEmoji != "❤️" {
		t.Errorf("expected ❤️ reaction, got %s", resp.ReactionEmoji)
	}
	if resp.MemoryNote != "شخص ودود وصباحه رايق" {
		t.Errorf("expected memory note, got %s", resp.MemoryNote)
	}
}

// 8. Test End-to-End Swarm Orchestration with Egyptian Synthesis
func TestSwarmOrchestration_EndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	emoEngine, err := emotion.NewEngine(tempDir)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	reg := tools.NewRegistry()

	mockLLM := &MockLLMClient{
		customHandler: func(ctx context.Context, model string, messages []LLMMessage, toolDefs []tools.ToolDefinition) (*LLMResult, error) {
			sysMsg := ""
			if len(messages) > 0 {
				if s, ok := messages[0].Content.(string); ok {
					sysMsg = s
				}
			}

			// If called for Math/Coding Agent
			if strings.Contains(sysMsg, "MathCodingAgent") {
				return &LLMResult{
					Content: "ناتج حل المعادلة 3x + 9 = 0 هو x = -3",
				}, nil
			}

			// If called for Egyptian Synthesizer
			if strings.Contains(sysMsg, "النتائج المكتشفة") || strings.Contains(sysMsg, "صياغة وتقديم هذه النتائج") {
				synthJSON := `{
					"should_reply": true,
					"reply_text": "حسبتهالك يا باشا في ثانية: قيمة x بتطلع -3 بالظبط! مسألة مياه بيضاء 😂👌",
					"mood": "Hyped",
					"reaction_emoji": "🔥"
				}`
				return &LLMResult{Content: synthJSON}, nil
			}

			return &LLMResult{Content: "رد افتراضي"}, nil
		},
	}

	cfg := DefaultSwarmConfig()
	cfg.EnableEgyptianSynthesis = true

	swarm := NewSwarm(cfg, emoEngine, reg, mockLLM)

	payload := &trigger.RequestPayload{
		ChatID:      "group_egypt@g.us",
		ChatType:    "group",
		SenderID:    "201011122233@s.whatsapp.net",
		SenderName:  "Karim",
		MessageID:   "MSG_TEST_999",
		MessageText: "حل المعادلة 3x + 9 = 0 يا نوفا",
	}

	ctx := context.Background()
	resp, err := swarm.Process(ctx, payload)
	if err != nil {
		t.Fatalf("Swarm.Process failed: %v", err)
	}

	if !resp.ShouldReply {
		t.Errorf("expected ShouldReply: true")
	}
	if resp.ReplyText == nil || !strings.Contains(*resp.ReplyText, "-3") {
		t.Errorf("expected synthesized reply containing -3, got: %v", resp.ReplyText)
	}
	if resp.ReplyToMessageID == nil || *resp.ReplyToMessageID != "MSG_TEST_999" {
		t.Errorf("expected ReplyToMessageID MSG_TEST_999, got: %v", resp.ReplyToMessageID)
	}
}

// 9. Test Swarm Emotion Engine Propagation
func TestSwarmEmotionIntegration(t *testing.T) {
	tempDir := t.TempDir()
	emoEngine, err := emotion.NewEngine(tempDir)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	swarm := NewSwarm(DefaultSwarmConfig(), emoEngine, nil, &MockLLMClient{})

	chatID := "private_chat_1@s.whatsapp.net"
	userA := "201012345678@s.whatsapp.net"

	// 1. Send sad message -> Mood shifts to Empathetic
	payloadSad := &trigger.RequestPayload{
		ChatID:      chatID,
		ChatType:    "private",
		SenderID:    userA,
		SenderName:  "Ahmed",
		MessageID:   "M1",
		MessageText: "أنا تعبان جداً وعيان ومريض في السرير ادعيلي",
	}

	ctx := context.Background()
	_, _ = swarm.Process(ctx, payloadSad)

	state := emoEngine.GetState(chatID)
	if state.CurrentMood != emotion.Empathetic {
		t.Errorf("expected Empathetic mood after sad message, got %s", state.CurrentMood)
	}

	// 2. Send enthusiastic message -> Mood shifts to Hyped
	payloadHype := &trigger.RequestPayload{
		ChatID:      chatID,
		ChatType:    "private",
		SenderID:    userA,
		SenderName:  "Ahmed",
		MessageID:   "M2",
		MessageText: "يلا يا وحش عاااش الجون ولع الدنيا نار 🔥⚡",
	}

	_, _ = swarm.Process(ctx, payloadHype)
	state = emoEngine.GetState(chatID)
	if state.CurrentMood != emotion.Hyped {
		t.Errorf("expected Hyped mood after enthusiastic message, got %s", state.CurrentMood)
	}
}

// 10. Test Fallback when Specialized Agent Errors
func TestSwarmFallbackOnAgentError(t *testing.T) {
	tempDir := t.TempDir()
	emoEngine, _ := emotion.NewEngine(tempDir)

	mockLLM := &MockLLMClient{
		customHandler: func(ctx context.Context, model string, messages []LLMMessage, toolDefs []tools.ToolDefinition) (*LLMResult, error) {
			// If called for MathCodingAgent, simulate error
			if strings.Contains(model, "deepseek") {
				return nil, fmt.Errorf("model rate limit reached")
			}
			// PersonaAgent fallback succeeds
			jsonResp := `{
				"should_reply": true,
				"reply_text": "حقك عليا يا غالي، كان في ضغط لحظي بس أنا معاك أهو، قولي سؤالك تاني بروقان!"
			}`
			return &LLMResult{Content: jsonResp}, nil
		},
	}

	swarm := NewSwarm(DefaultSwarmConfig(), emoEngine, nil, mockLLM)

	payload := &trigger.RequestPayload{
		ChatID:      "group_test@g.us",
		SenderID:    "201099998888@s.whatsapp.net",
		MessageID:   "M_ERR_1",
		MessageText: "احسبلي معادلة تفاضلية صعبة",
	}

	ctx := context.Background()
	resp, err := swarm.Process(ctx, payload)
	if err != nil {
		t.Fatalf("expected swarm to recover via PersonaAgent fallback, but got error: %v", err)
	}

	if resp == nil || resp.ReplyText == nil || !strings.Contains(*resp.ReplyText, "معاك") {
		t.Errorf("expected fallback response text, got: %v", resp)
	}
}

// 11. Test OpenRouterLLMClient HTTP Client Serialization
func TestOpenRouterLLMClient_HTTP(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key-123" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		var req openRouterRequestBody
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		resp := openRouterResponseBody{
			Choices: []struct {
				Message struct {
					Role       string        `json:"role"`
					Content    interface{}   `json:"content"`
					ToolCalls  []LLMToolCall `json:"tool_calls,omitempty"`
					ToolCallID string        `json:"tool_call_id,omitempty"`
				} `json:"message"`
				FinishReason string `json:"finish_reason"`
			}{
				{
					Message: struct {
						Role       string        `json:"role"`
						Content    interface{}   `json:"content"`
						ToolCalls  []LLMToolCall `json:"tool_calls,omitempty"`
						ToolCallID string        `json:"tool_call_id,omitempty"`
					}{
						Role:    "assistant",
						Content: "مرحباً من السيرفر الاختباري",
					},
					FinishReason: "stop",
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer mockServer.Close()

	client := NewOpenRouterLLMClient("test-key-123")
	client.SetBaseURL(mockServer.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := client.Call(ctx, "test-model", []LLMMessage{
		{Role: "user", Content: "hello"},
	}, nil)

	if err != nil {
		t.Fatalf("client.Call failed: %v", err)
	}

	if result.Content != "مرحباً من السيرفر الاختباري" {
		t.Errorf("unexpected content: %s", result.Content)
	}
}
