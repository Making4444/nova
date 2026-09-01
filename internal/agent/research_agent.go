package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"novabot/internal/tools"
)

// ResearchAgent specializes in deep web research, live facts, news, currency rates, and data lookups.
type ResearchAgent struct {
	client        LLMClient
	model         string
	toolsRegistry *tools.Registry
	maxSteps      int
}

// NewResearchAgent creates a new ResearchAgent.
func NewResearchAgent(client LLMClient, model string, toolsRegistry *tools.Registry) *ResearchAgent {
	if model == "" {
		model = "google/gemini-2.5-flash"
	}
	return &ResearchAgent{
		client:        client,
		model:         model,
		toolsRegistry: toolsRegistry,
		maxSteps:      4,
	}
}

func (a *ResearchAgent) Name() string {
	return "ResearchAgent"
}

func (a *ResearchAgent) Type() AgentType {
	return AgentTypeResearch
}

func (a *ResearchAgent) Model() string {
	return a.model
}

func (a *ResearchAgent) Description() string {
	return "خبير البحث المتقدم، استخراج الأخبار الحية، أسعار العملات والذهب، الطقس، والتحقق من الحقائق والمقارنات عبر الإنترنت"
}

// Execute performs deep research using available tools and returns structured factual data.
func (a *ResearchAgent) Execute(ctx context.Context, req *AgentRequest) (*AgentResponse, error) {
	if a == nil || a.client == nil {
		return nil, fmt.Errorf("ResearchAgent LLM client is not configured")
	}

	systemPrompt := `أنت ResearchAgent، عميل البحث والاستقصاء المتقدم والمعلومات الحية لبوت نوفا.
مهمتك:
1. البحث في الإنترنت واستخراج أحدث الحقائق، الأخبار الحية، أسعار العملات والذهب، حالة الطقس، والإحصائيات بدقة مطلقة.
2. إذا تطلب السؤال استدعاء أدوات (مثل web_search أو web_reader أو weather)، استخدمها فوراً للحصول على المعلومات الحية قبل الإجابة.
3. صياغة المعلومات بطريقة دقيقة وموجزة وموثوقة، مع ذكر المصادر والأرقام الواضحة.`

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
			return nil, fmt.Errorf("ResearchAgent execution failed: %w", err)
		}

		if len(result.ToolCalls) == 0 {
			return &AgentResponse{
				AgentType:   AgentTypeResearch,
				AgentName:   a.Name(),
				ModelUsed:   a.model,
				Content:     strings.TrimSpace(result.Content),
				ToolsUsed:   toolsUsed,
				Confidence:  0.95,
				ShouldReply: true,
			}, nil
		}

		// Append assistant message with tool calls
		currentMessages = append(currentMessages, LLMMessage{
			Role:      "assistant",
			Content:   result.Content,
			ToolCalls: result.ToolCalls,
		})

		// Execute tool calls
		for _, tc := range result.ToolCalls {
			toolsUsed = append(toolsUsed, tc.Function.Name)
			toolOutput := "Tool execution failed"

			if a.toolsRegistry != nil {
				out, execErr := a.toolsRegistry.Execute(ctx, tc.Function.Name, json.RawMessage(tc.Function.Arguments), execCtx)
				if execErr != nil {
					toolOutput = fmt.Sprintf("Error executing tool %s: %v", tc.Function.Name, execErr)
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

	// Final completion if max tool steps reached
	finalResult, err := a.client.Call(ctx, a.model, currentMessages, nil)
	if err != nil {
		return nil, fmt.Errorf("ResearchAgent final synthesis failed: %w", err)
	}

	return &AgentResponse{
		AgentType:   AgentTypeResearch,
		AgentName:   a.Name(),
		ModelUsed:   a.model,
		Content:     strings.TrimSpace(finalResult.Content),
		ToolsUsed:   toolsUsed,
		Confidence:  0.90,
		ShouldReply: true,
	}, nil
}
