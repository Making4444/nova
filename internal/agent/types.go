package agent

import (
	"context"

	"novabot/internal/ai"
	"novabot/internal/emotion"
	"novabot/internal/tools"
	"novabot/internal/trigger"
)

// AgentType represents the category/specialization of an agent in the swarm.
type AgentType string

const (
	// AgentTypeResearch specializes in web lookups, news, live facts, currency rates, comparisons.
	AgentTypeResearch AgentType = "research"
	// AgentTypeMathCoding specializes in equations, calculations, code snippets, algorithms, logic puzzles.
	AgentTypeMathCoding AgentType = "math_coding"
	// AgentTypeVisionDoc specializes in image analysis, diagrams, PDF documents, OCR text extraction.
	AgentTypeVisionDoc AgentType = "vision_doc"
	// AgentTypePersona specializes in general chat, humor, banter, emotional conversation in Egyptian dialect.
	AgentTypePersona AgentType = "persona"
)

// Agent defines the interface that all specialized agents in the swarm must implement.
type Agent interface {
	// Name returns the descriptive name of the agent.
	Name() string
	// Type returns the agent's category.
	Type() AgentType
	// Model returns the primary model ID used by this agent.
	Model() string
	// Description returns what the agent specializes in.
	Description() string
	// Execute executes the agent task with the provided request.
	Execute(ctx context.Context, req *AgentRequest) (*AgentResponse, error)
}

// AgentRequest represents the context and parameters passed to an agent for execution.
type AgentRequest struct {
	Payload              *trigger.RequestPayload
	SenderID             string
	SenderName           string
	ChatID               string
	ChatType             string // "private" or "group"
	IsAdmin              bool
	IsMaker              bool
	EmotionState         *emotion.EmotionalState
	EmotionPrompt        string
	Tools                []tools.ToolDefinition
	UserMemory           string
	ChatSummary          string
	RecentContext        []trigger.ContextMessage
	CurrentTime          string
	TimeOfDay            string
	TimeSinceLastMessage string
	MediaDataURL         *string
	SystemPrompt         string
	ExtraMetadata        map[string]interface{}
}

// AgentResponse represents the result of an agent execution.
type AgentResponse struct {
	AgentType        AgentType              `json:"agent_type"`
	AgentName        string                 `json:"agent_name"`
	ModelUsed        string                 `json:"model_used"`
	Content          string                 `json:"content"`
	StructuredResult map[string]interface{} `json:"structured_result,omitempty"`
	ToolsUsed        []string               `json:"tools_used,omitempty"`
	Confidence       float64                `json:"confidence"`
	ShouldReply      bool                   `json:"should_reply"`
	Mood             string                 `json:"mood,omitempty"`
	ReactionEmoji    string                 `json:"reaction_emoji,omitempty"`
	MemoryNote       string                 `json:"memory_note,omitempty"`
	SendAsVoice      bool                   `json:"send_as_voice,omitempty"`
	RawJSON          string                 `json:"raw_json,omitempty"`
}

// LLMMessage represents a message in LLM conversations.
type LLMMessage struct {
	Role       string        `json:"role"`
	Content    interface{}   `json:"content"` // string or []ContentPart
	ToolCallID string        `json:"tool_call_id,omitempty"`
	ToolCalls  []LLMToolCall `json:"tool_calls,omitempty"`
}

// ContentPart represents multimodal parts (text or image_url).
type ContentPart struct {
	Type     string         `json:"type"`
	Text     string         `json:"text,omitempty"`
	ImageURL *ImageURLParam `json:"image_url,omitempty"`
}

// ImageURLParam holds the image URL or base64 data URL.
type ImageURLParam struct {
	URL string `json:"url"`
}

// LLMToolCall represents a tool invocation from an LLM.
type LLMToolCall struct {
	ID       string              `json:"id"`
	Type     string              `json:"type"`
	Function LLMToolCallFunction `json:"function"`
}

// LLMToolCallFunction represents function call arguments.
type LLMToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// LLMResult represents the raw completion response from an LLM call.
type LLMResult struct {
	Content      string
	ToolCalls    []LLMToolCall
	FinishReason string
	ModelUsed    string
}

// LLMClient defines the interface for making LLM API calls with function calling and vision.
type LLMClient interface {
	Call(ctx context.Context, model string, messages []LLMMessage, toolDefs []tools.ToolDefinition) (*LLMResult, error)
}

// SwarmConfig holds model configuration and operational parameters for the swarm.
type SwarmConfig struct {
	ModelResearch           string  `json:"model_research"`
	ModelMathCoding         string  `json:"model_math_coding"`
	ModelVisionDoc          string  `json:"model_vision_doc"`
	ModelPersona            string  `json:"model_persona"`
	ModelSynthesizer        string  `json:"model_synthesizer"`
	ModelRouter             string  `json:"model_router"`
	EnableEgyptianSynthesis bool    `json:"enable_egyptian_synthesis"`
	MaxToolSteps            int     `json:"max_tool_steps"`
	Temperature             float64 `json:"temperature"`
	SystemPrompt            string  `json:"system_prompt"`
}

// DefaultSwarmConfig returns standard recommended models for the Swarm.
func DefaultSwarmConfig() *SwarmConfig {
	return &SwarmConfig{
		ModelResearch:           "google/gemini-2.5-flash", // Fast, accurate web & fact synthesizer
		ModelMathCoding:         "deepseek/deepseek-r1",     // High-IQ reasoning model
		ModelVisionDoc:          "openai/gpt-5.6-luna",      // High-precision multimodal vision & OCR model
		ModelPersona:            "google/gemini-2.5-flash", // Low-cost, witty conversational model
		ModelSynthesizer:        "google/gemini-2.5-flash", // Authentic Egyptian persona compiler
		ModelRouter:             "meta-llama/llama-3.3-70b-instruct",
		EnableEgyptianSynthesis: true,
		MaxToolSteps:            4,
		Temperature:             0.7,
	}
}

// ToResponsePayload converts an AgentResponse to the standard ai.ResponsePayload.
func (r *AgentResponse) ToResponsePayload(targetMsgID string) *ai.ResponsePayload {
	reply := r.Content
	var moodPtr *string
	if r.Mood != "" {
		moodPtr = &r.Mood
	}
	var emojiPtr *string
	if r.ReactionEmoji != "" {
		emojiPtr = &r.ReactionEmoji
	}
	var memPtr *string
	if r.MemoryNote != "" {
		memPtr = &r.MemoryNote
	}
	var voicePtr *bool
	if r.SendAsVoice {
		v := true
		voicePtr = &v
	}

	var replyToPtr *string
	if targetMsgID != "" {
		replyToPtr = &targetMsgID
	}

	return &ai.ResponsePayload{
		ShouldReply:      r.ShouldReply,
		ReplyText:        &reply,
		ReplyToMessageID: replyToPtr,
		MemoryNote:       memPtr,
		Mood:             moodPtr,
		SendAsVoice:      voicePtr,
		ReactionEmoji:    emojiPtr,
	}
}
