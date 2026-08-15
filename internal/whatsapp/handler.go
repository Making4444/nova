package whatsapp

import (
	"context"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"

	"novabot/internal/ai"
	"novabot/internal/storage"
	"novabot/internal/trigger"
)

// EventHandler coordinates incoming WhatsApp messages, logging, triggers, and AI replies.
type EventHandler struct {
	waClient     *Client
	aiClient     *ai.OpenRouterClient
	chatLogger   *storage.ChatLogger
	memStore     *storage.MemoryStore
	limiter      *trigger.ChatLimiter
	historyLimit int
	logger       waLog.Logger
	novaMsgMu    sync.RWMutex
	novaMsgIDs   map[string]time.Time
}

// NewEventHandler creates a new EventHandler instance.
func NewEventHandler(
	waClient *Client,
	aiClient *ai.OpenRouterClient,
	chatLogger *storage.ChatLogger,
	memStore *storage.MemoryStore,
	limiter *trigger.ChatLimiter,
	historyLimit int,
	logger waLog.Logger,
) *EventHandler {
	return &EventHandler{
		waClient:     waClient,
		aiClient:     aiClient,
		chatLogger:   chatLogger,
		memStore:     memStore,
		limiter:      limiter,
		historyLimit: historyLimit,
		logger:       logger,
		novaMsgIDs:   make(map[string]time.Time),
	}
}

func (h *EventHandler) markAsNovaSent(msgID string) {
	if msgID == "" {
		return
	}
	h.novaMsgMu.Lock()
	defer h.novaMsgMu.Unlock()
	h.novaMsgIDs[msgID] = time.Now()

	// Clean up entries older than 10 minutes
	now := time.Now()
	for id, t := range h.novaMsgIDs {
		if now.Sub(t) > 10*time.Minute {
			delete(h.novaMsgIDs, id)
		}
	}
}

func (h *EventHandler) isNovaSent(msgID string) bool {
	if msgID == "" {
		return false
	}
	h.novaMsgMu.RLock()
	defer h.novaMsgMu.RUnlock()
	_, exists := h.novaMsgIDs[msgID]
	return exists
}

// HandleEvent handles all raw whatsmeow events and routes Message events.
func (h *EventHandler) HandleEvent(rawEvt interface{}) {
	evt, ok := rawEvt.(*events.Message)
	if !ok || evt == nil || evt.Message == nil {
		return
	}

	// Ignore WhatsApp status broadcasts
	if evt.Info.Chat == types.StatusBroadcastJID {
		return
	}

	messageID := evt.Info.ID

	// If this message was sent by the Nova bot itself, skip processing to avoid loops
	if h.isNovaSent(messageID) {
		return
	}

	// 1. Determine chat details
	chatType := "private"
	if evt.Info.IsGroup || strings.Contains(evt.Info.Chat.String(), "@g.us") {
		chatType = "group"
	}
	chatID := evt.Info.Chat.ToNonAD().String()
	senderID := evt.Info.Sender.ToNonAD().String()
	senderName := evt.Info.PushName
	if senderName == "" {
		senderName = evt.Info.Sender.User
	}

	// Extract message text / caption and reply info safely
	text, isReply, repliedMsgID, repliedSender, repliedText := ExtractTextAndReply(evt.Message)

	// 2. Rule 2: Local recording of every incoming/outgoing message in its chat log
	logEntry := storage.LogMessage{
		MessageID:   messageID,
		SenderID:    senderID,
		SenderName:  senderName,
		Text:        text,
		IsNova:      false,
		Timestamp:   evt.Info.Timestamp.Format(time.RFC3339),
		IsReply:     isReply,
		RepliedToID: repliedMsgID,
	}

	if err := h.chatLogger.AppendMessage(chatType, chatID, logEntry); err != nil {
		h.logger.Errorf("Failed to log message in chat %s: %v", chatID, err)
	}

	// 3. Rule 3: Trigger Matching (Check "يا نوفا" in text or in replied-to message)
	triggerMatched := trigger.CheckTrigger(text, isReply, repliedText)
	if !triggerMatched {
		return
	}

	// Process matched trigger asynchronously to avoid blocking the event loop
	go h.processTrigger(evt, chatType, chatID, senderID, senderName, messageID, text, isReply, repliedMsgID, repliedSender, repliedText)
}

func (h *EventHandler) processTrigger(
	evt *events.Message,
	chatType string,
	chatID string,
	senderID string,
	senderName string,
	messageID string,
	text string,
	isReply bool,
	repliedMsgID string,
	repliedSender string,
	repliedText string,
) {
	// 4. Rate Limiter / Cooldown check per chat
	if !h.limiter.Allow(chatID) {
		h.logger.Infof("Rate limiter / cooldown active for chat %s, skipping trigger", chatID)
		return
	}

	// Concurrency lock per chat
	chatLock := h.limiter.GetChatLock(chatID)
	chatLock.Lock()
	defer chatLock.Unlock()

	var repliedTo *trigger.RepliedToInfo
	if isReply {
		repliedTo = &trigger.RepliedToInfo{
			MessageID:  repliedMsgID,
			SenderName: repliedSender,
			Text:       repliedText,
		}
	}

	// 5. Build full context payload for the AI
	payload, err := trigger.BuildContext(
		h.chatLogger,
		h.memStore,
		chatType,
		chatID,
		"", // Chat name
		senderID,
		senderName,
		messageID,
		text,
		isReply,
		repliedTo,
		h.historyLimit,
	)
	if err != nil {
		h.logger.Errorf("Failed to build AI context for message %s: %v", messageID, err)
		return
	}

	// 6. Call OpenRouter AI
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	h.logger.Infof("Trigger matched in chat %s by %s, generating AI response...", chatID, senderName)
	aiResp, err := h.aiClient.GenerateResponse(ctx, payload)
	if err != nil {
		h.logger.Errorf("Failed to generate AI response for message %s: %v", messageID, err)
		return
	}

	// 7. Rule 9: If model returns should_reply: false, do not send anything
	if !aiResp.ShouldReply {
		h.logger.Infof("AI decided not to reply (should_reply: false) for message %s", messageID)
		return
	}

	if aiResp.ReplyText == nil || strings.TrimSpace(*aiResp.ReplyText) == "" {
		h.logger.Warnf("AI returned should_reply: true but empty reply text for message %s", messageID)
		return
	}

	// Target message to reply (quote) to
	targetMsgID := messageID
	if aiResp.ReplyToMessageID != nil && *aiResp.ReplyToMessageID != "" {
		targetMsgID = *aiResp.ReplyToMessageID
	}

	// 8. Rule 6: Send WhatsApp Reply (Quote)
	sentMsgID, err := h.waClient.SendReply(ctx, evt.Info.Chat, *aiResp.ReplyText, targetMsgID, evt.Info.Sender.String())
	if err != nil {
		h.logger.Errorf("Failed to send WhatsApp reply for message %s: %v", messageID, err)
		return
	}

	// Mark sent message ID as generated by Nova so the bot doesn't reply to itself
	h.markAsNovaSent(string(sentMsgID))

	h.logger.Infof("Nova replied successfully to message %s (Sent ID: %s)", targetMsgID, sentMsgID)

	// 9. Rule 7: Record Nova's response in chat log with SenderName: "Nova" and IsNova: true
	novaLog := storage.LogMessage{
		MessageID:   string(sentMsgID),
		SenderID:    h.waClient.GetUserJID().ToNonAD().String(),
		SenderName:  "Nova",
		Text:        *aiResp.ReplyText,
		IsNova:      true,
		Timestamp:   time.Now().Format(time.RFC3339),
		IsReply:     true,
		RepliedToID: targetMsgID,
	}
	if err := h.chatLogger.AppendMessage(chatType, chatID, novaLog); err != nil {
		h.logger.Errorf("Failed to log Nova's sent reply in chat %s: %v", chatID, err)
	}

	// 10. Rule 8 & 10: Save memory note if returned by AI
	if aiResp.MemoryNote != nil && strings.TrimSpace(*aiResp.MemoryNote) != "" {
		if err := h.memStore.AppendMemoryNote(senderID, senderName, *aiResp.MemoryNote); err != nil {
			h.logger.Errorf("Failed to save memory note for user %s: %v", senderID, err)
		} else {
			h.logger.Infof("Saved new memory note for user %s (%s)", senderName, senderID)
		}
	}
}
