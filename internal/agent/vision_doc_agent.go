package agent

import (
	"context"
	"fmt"
	"strings"
)

// VisionDocAgent specializes in multimodal image analysis, OCR text extraction, PDF documents, and visual reasoning.
type VisionDocAgent struct {
	client LLMClient
	model  string
}

// NewVisionDocAgent creates a new VisionDocAgent.
func NewVisionDocAgent(client LLMClient, model string) *VisionDocAgent {
	if model == "" {
		model = "openai/gpt-5.6-luna"
	}
	return &VisionDocAgent{
		client: client,
		model:  model,
	}
}

func (a *VisionDocAgent) Name() string {
	return "VisionDocAgent"
}

func (a *VisionDocAgent) Type() AgentType {
	return AgentTypeVisionDoc
}

func (a *VisionDocAgent) Model() string {
	return a.model
}

func (a *VisionDocAgent) Description() string {
	return "خبير الرؤية البصرية، تحليل الصور، قراءة المستندات والـ PDF، استخراج النصوص (OCR)، وتفسير الرسوم البيانية والشاشات"
}

// Execute analyzes visual media, documents, or OCR text queries.
func (a *VisionDocAgent) Execute(ctx context.Context, req *AgentRequest) (*AgentResponse, error) {
	if a.client == nil {
		return nil, fmt.Errorf("VisionDocAgent LLM client is not configured")
	}

	systemPrompt := `أنت VisionDocAgent، خبير التحليل البصري واستخراج النصوص والمستندات لبوت نوفا.
مهامك وقواعدك:
1. تحليل محتوى الصورة المرفقة أو المستند بدقة متناهية (عناصر الصورة، النصوص، الرسوم البيانية، الجداول، لقطات الشاشة).
2. استخراج النصوص المكتوبة (OCR) باللغتين العربية والإنجليزية بدقة تامة وبدون تحريف.
3. الإجابة على أي سؤال متعلق بتفاصيل الصورة أو المستند بوضوح وشرح وافٍ.`

	var userContent interface{}
	userText := req.Payload.MessageText
	if strings.TrimSpace(userText) == "" {
		userText = "حلل هذه الصورة واشرح محتواها وما تحتويه من نصوص أو تفاصيل"
	}

	if req.Payload.MediaDataURL != nil && *req.Payload.MediaDataURL != "" {
		userContent = []ContentPart{
			{
				Type: "text",
				Text: userText,
			},
			{
				Type: "image_url",
				ImageURL: &ImageURLParam{
					URL: *req.Payload.MediaDataURL,
				},
			},
		}
	} else {
		userContent = userText
	}

	messages := []LLMMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userContent},
	}

	result, err := a.client.Call(ctx, a.model, messages, nil)
	if err != nil {
		return nil, fmt.Errorf("VisionDocAgent execution failed: %w", err)
	}

	return &AgentResponse{
		AgentType:   AgentTypeVisionDoc,
		AgentName:   a.Name(),
		ModelUsed:   a.model,
		Content:     strings.TrimSpace(result.Content),
		Confidence:  0.95,
		ShouldReply: true,
	}, nil
}
