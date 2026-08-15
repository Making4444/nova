package trigger

import (
	"novabot/internal/storage"
)

// RepliedToInfo contains details about the message being replied to.
type RepliedToInfo struct {
	MessageID  string `json:"message_id"`
	SenderName string `json:"sender_name"`
	Text       string `json:"text"`
}

// ContextMessage represents a single message in the recent conversation context.
type ContextMessage struct {
	SenderName string `json:"sender_name"`
	Text       string `json:"text"`
	IsNova     bool   `json:"is_nova"`
	Timestamp  string `json:"timestamp"`
}

// RequestPayload represents the complete payload sent to the LLM.
type RequestPayload struct {
	ChatType       string           `json:"chat_type"` // "private" | "group"
	ChatID         string           `json:"chat_id"`
	ChatName       *string          `json:"chat_name"`
	SenderID       string           `json:"sender_id"`
	SenderName     string           `json:"sender_name"`
	MessageID      string           `json:"message_id"`
	MessageText    string           `json:"message_text"` // Raw original text
	IsReply        bool             `json:"is_reply"`
	RepliedTo      *RepliedToInfo   `json:"replied_to"`
	TriggerMatched bool             `json:"trigger_matched"`
	RecentContext  []ContextMessage `json:"recent_context"`
	UserMemory     *string          `json:"user_memory"`
}

// BuildContext constructs the RequestPayload using recent chat history and user memory.
// Note: All matched triggers are passed to the AI without premature filtering so the model is the final judge.
func BuildContext(
	chatLogger *storage.ChatLogger,
	memStore *storage.MemoryStore,
	chatType string,
	chatID string,
	chatName string,
	senderID string,
	senderName string,
	messageID string,
	rawMessageText string,
	isReply bool,
	repliedTo *RepliedToInfo,
	historyLimit int,
) (*RequestPayload, error) {
	// Fetch recent messages
	recentLogs, err := chatLogger.GetRecentMessages(chatType, chatID, historyLimit)
	if err != nil {
		return nil, err
	}

	recentContext := make([]ContextMessage, 0, len(recentLogs))
	for _, log := range recentLogs {
		// Avoid duplicating the current incoming message if it was just logged
		if log.MessageID == messageID {
			continue
		}
		recentContext = append(recentContext, ContextMessage{
			SenderName: log.SenderName,
			Text:       log.Text,
			IsNova:     log.IsNova,
			Timestamp:  log.Timestamp,
		})
	}

	// Fetch user memory
	var userMemPtr *string
	if memStore != nil && senderID != "" {
		mem, err := memStore.GetUserMemory(senderID)
		if err == nil && mem != "" {
			userMemPtr = &mem
		}
	}

	var chatNamePtr *string
	if chatName != "" {
		chatNamePtr = &chatName
	}

	return &RequestPayload{
		ChatType:       chatType,
		ChatID:         chatID,
		ChatName:       chatNamePtr,
		SenderID:       senderID,
		SenderName:     senderName,
		MessageID:      messageID,
		MessageText:    rawMessageText,
		IsReply:        isReply,
		RepliedTo:      repliedTo,
		TriggerMatched: true,
		RecentContext:  recentContext,
		UserMemory:     userMemPtr,
	}, nil
}
