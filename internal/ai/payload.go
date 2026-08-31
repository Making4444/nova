package ai

// ResponsePayload defines the strict JSON response contract expected from the LLM.
type ResponsePayload struct {
	ShouldReply      bool    `json:"should_reply"`
	ReplyText        *string `json:"reply_text"`
	ReplyToMessageID *string `json:"reply_to_message_id"`
	MemoryNote       *string `json:"memory_note"`
	Mood             *string `json:"mood"`
	SendAsVoice      *bool   `json:"send_as_voice,omitempty"`
	ReactionEmoji    *string `json:"reaction_emoji,omitempty"`
}
