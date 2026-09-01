package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"novabot/internal/tools"
)

// MathCodingAgent specializes in mathematical derivations, calculus, programming, debugging, and logic puzzles.
type MathCodingAgent struct {
	client        LLMClient
	model         string
	toolsRegistry *tools.Registry
	maxSteps      int
}

// NewMathCodingAgent creates a new MathCodingAgent.
func NewMathCodingAgent(client LLMClient, model string, toolsRegistry *tools.Registry) *MathCodingAgent {
	if model == "" {
		model = "deepseek/deepseek-r1"
	}
	return &MathCodingAgent{
		client:        client,
		model:         model,
		toolsRegistry: toolsRegistry,
		maxSteps:      3,
	}
}

func (a *MathCodingAgent) Name() string {
	return "MathCodingAgent"
}

func (a *MathCodingAgent) Type() AgentType {
	return AgentTypeMathCoding
}

func (a *MathCodingAgent) Model() string {
	return a.model
}

func (a *MathCodingAgent) Description() string {
	return "خبير الرياضيات المتقدمة، حل المعادلات والتفاضل والتكامل، كتابة الأكواد البرمجية وتصحيح الأخطاء، وحل الألغاز المنطقية المعقدة (High-IQ Reasoning)"
}

// Execute performs high-IQ reasoning, computation, or code generation.
func (a *MathCodingAgent) Execute(ctx context.Context, req *AgentRequest) (*AgentResponse, error) {
	if a == nil || a.client == nil {
		return nil, fmt.Errorf("MathCodingAgent LLM client is not configured")
	}

	systemPrompt := `أنت MathCodingAgent، المحرك الرياضي والبرمجي فائق الذكاء (High-IQ Reasoning Engine) لبوت نوفا.
قواعدك الصارمة:
1. في المسائل الرياضية: قم بحل المسألة خطوة بخطوة بتسلسل منطقي دقيق، مع كتابة المعادلات بوضوح تام وذكر النتيجة النهائية بدقة 100%.
2. في البرمجة وهندسة البرمجيات: اكتب كوداً نظيفاً، احترافياً، معلقاً، ومعالجاً للأخطاء (Clean Code & Error Handling)، واشرح فكرة الحل والتعقيد الزمني والمكاني (Big-O).
3. في الألغاز المنطقية والذكاء: اتبع الاستنتاج المنطقي الدقيق وأثبت صحة الحل بالبراهين.
4. إذا احتجت لحسابات دقيقة يمكنك استخدام أداة الآلة الحاسبة المتاحة.`

	userMsgText := ""
	isAdmin := false
	var execCtx tools.ExecutionContext
	if req != nil {
		isAdmin = req.IsAdmin
		if req.Payload != nil {
			userMsgText = req.Payload.MessageText
		}
		execCtx = tools.ExecutionContext{
			SenderID:   req.SenderID,
			SenderName: req.SenderName,
			ChatID:     req.ChatID,
			ChatType:   req.ChatType,
			IsAdmin:    req.IsAdmin,
		}
	}

	messages := []LLMMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userMsgText},
	}

	var toolDefs []tools.ToolDefinition
	if a.toolsRegistry != nil {
		toolDefs = a.toolsRegistry.ToToolDefinitions(isAdmin)
	}

	toolsUsed := make([]string, 0)
	currentMessages := make([]LLMMessage, len(messages))
	copy(currentMessages, messages)

	for step := 0; step < a.maxSteps; step++ {
		result, err := a.client.Call(ctx, a.model, currentMessages, toolDefs)
		if err != nil {
			return nil, fmt.Errorf("MathCodingAgent execution failed: %w", err)
		}

		if len(result.ToolCalls) == 0 {
			return &AgentResponse{
				AgentType:   AgentTypeMathCoding,
				AgentName:   a.Name(),
				ModelUsed:   a.model,
				Content:     strings.TrimSpace(result.Content),
				ToolsUsed:   toolsUsed,
				Confidence:  0.98,
				ShouldReply: true,
			}, nil
		}

		currentMessages = append(currentMessages, LLMMessage{
			Role:      "assistant",
			Content:   result.Content,
			ToolCalls: result.ToolCalls,
		})

		for _, tc := range result.ToolCalls {
			toolsUsed = append(toolsUsed, tc.Function.Name)
			toolOutput := "Calculator tool execution failed"

			if a.toolsRegistry != nil {
				out, execErr := a.toolsRegistry.Execute(ctx, tc.Function.Name, json.RawMessage(tc.Function.Arguments), execCtx)
				if execErr != nil {
					toolOutput = fmt.Sprintf("Error in tool %s: %v", tc.Function.Name, execErr)
				} else {
					toolOutput = out
				}
			}

			currentMessages = append(currentMessages, LLMMessage{
				Role:       "tool",
				ToolCallID: tc.ID,
				Content:    toolOutput,
			})
		}
	}

	finalResult, err := a.client.Call(ctx, a.model, currentMessages, nil)
	if err != nil {
		return nil, fmt.Errorf("MathCodingAgent final synthesis failed: %w", err)
	}

	return &AgentResponse{
		AgentType:   AgentTypeMathCoding,
		AgentName:   a.Name(),
		ModelUsed:   a.model,
		Content:     strings.TrimSpace(finalResult.Content),
		ToolsUsed:   toolsUsed,
		Confidence:  0.95,
		ShouldReply: true,
	}, nil
}
