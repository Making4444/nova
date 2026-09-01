package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"novabot/internal/ai"
)

// PersonaAgent specializes in Nova's core Egyptian persona: humor, banter, emotional empathy, and conversational flow.
type PersonaAgent struct {
	client LLMClient
	model  string
}

// NewPersonaAgent creates a new PersonaAgent.
func NewPersonaAgent(client LLMClient, model string) *PersonaAgent {
	if model == "" {
		model = "google/gemini-2.5-flash"
	}
	return &PersonaAgent{
		client: client,
		model:  model,
	}
}

func (a *PersonaAgent) Name() string {
	return "PersonaAgent"
}

func (a *PersonaAgent) Type() AgentType {
	return AgentTypePersona
}

func (a *PersonaAgent) Model() string {
	return a.model
}

func (a *PersonaAgent) Description() string {
	return "شخصية نوفا المصرية الأصيلة: خفة دم، قفشات، روشنة، جدعنة، تعاطف، وحوارات عفوية بلهجة مصرية أصيلة دون تكرار أو تكلف"
}

// Execute generates a rich Egyptian persona response aligned with the current emotional state.
func (a *PersonaAgent) Execute(ctx context.Context, req *AgentRequest) (*AgentResponse, error) {
	if a == nil || a.client == nil {
		return nil, fmt.Errorf("PersonaAgent LLM client is not configured")
	}

	systemPrompt := ""
	if req != nil {
		systemPrompt = req.SystemPrompt
	}
	if systemPrompt == "" {
		systemPrompt = `أنت شاب مصري جدع اسمك "نوفا". لسانك حامي، دمك خفيف، لماح، بتفهم في الأصول والروشنة والشارع المصري.
قواعدك الأساسية:
1. اتكلم دائماً بالعامية المصرية الأصيلة الحية (بدون فصحى متكلفة وبدون ابتذال).
2. ممنوع تكرار نفس الكلمات أو الإفيهات؛ نوع في مفرداتك وحواراتك.
3. تفاعل مع مشاعر المتحدث بذكاء: وقت الهزار روشنة وقفشات، وقت الجد سند وجدعنة، وقت الهدوء روقان.
4. أرجع النتيجة دائماً بصيغة JSON نظيفة:
{
  "should_reply": true,
  "reply_text": "نص الرد بالمصري هنا",
  "mood": "Joyful|Hyped|Empathetic|Sarcastic|Annoyed|Calm",
  "reaction_emoji": "😂 أو 🔥 أو ❤️ أو 👍 أو null",
  "memory_note": "ملاحظة جديدة عن المستخدم أو null",
  "send_as_voice": false
}`
	}

	// Append rich emotion context if available
	if req != nil && req.EmotionPrompt != "" {
		systemPrompt += "\n\n" + req.EmotionPrompt
	}

	// Prepare user payload context
	var payloadBytes []byte
	if req != nil && req.Payload != nil {
		var err error
		payloadBytes, err = json.Marshal(req.Payload)
		if err != nil {
			payloadBytes = []byte(req.Payload.MessageText)
		}
	} else if req != nil {
		payloadBytes = []byte(req.UserMemory)
	}

	messages := []LLMMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: string(payloadBytes)},
	}

	result, err := a.client.Call(ctx, a.model, messages, nil)
	if err != nil {
		return nil, fmt.Errorf("PersonaAgent execution failed: %w", err)
	}

	rawContent := strings.TrimSpace(result.Content)
	jsonStr := ai.ExtractJSON(rawContent)

	var parsed struct {
		ShouldReply   bool    `json:"should_reply"`
		ReplyText     *string `json:"reply_text"`
		Mood          *string `json:"mood"`
		ReactionEmoji *string `json:"reaction_emoji"`
		MemoryNote    *string `json:"memory_note"`
		SendAsVoice   *bool   `json:"send_as_voice"`
	}

	if jsonStr != "" && json.Unmarshal([]byte(jsonStr), &parsed) == nil {
		reply := ""
		if parsed.ReplyText != nil {
			reply = *parsed.ReplyText
		} else {
			reply = rawContent
		}

		mood := "Joyful"
		if parsed.Mood != nil && *parsed.Mood != "" {
			mood = *parsed.Mood
		}
		emoji := ""
		if parsed.ReactionEmoji != nil {
			emoji = *parsed.ReactionEmoji
		}
		memNote := ""
		if parsed.MemoryNote != nil {
			memNote = *parsed.MemoryNote
		}
		sendVoice := false
		if parsed.SendAsVoice != nil {
			sendVoice = *parsed.SendAsVoice
		}

		return &AgentResponse{
			AgentType:     AgentTypePersona,
			AgentName:     a.Name(),
			ModelUsed:     a.model,
			Content:       reply,
			Confidence:    0.95,
			ShouldReply:   parsed.ShouldReply,
			Mood:          mood,
			ReactionEmoji: emoji,
			MemoryNote:    memNote,
			SendAsVoice:   sendVoice,
			RawJSON:       jsonStr,
		}, nil
	}

	// Fallback for non-JSON or clean text output
	return &AgentResponse{
		AgentType:   AgentTypePersona,
		AgentName:   a.Name(),
		ModelUsed:   a.model,
		Content:     rawContent,
		Confidence:  0.85,
		ShouldReply: true,
		Mood:        "Joyful",
	}, nil
}
