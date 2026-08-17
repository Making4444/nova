package whatsapp

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"

	"novabot/internal/admin"
	"novabot/internal/ai"
	"novabot/internal/storage"
	"novabot/internal/trigger"
)

// SchedulerEngineInterface defines methods expected from the scheduler.
type SchedulerEngineInterface interface {
	RecordIncomingMessageForRush(chatID string, wasTriggerMatched bool)
	TriggerNewMemberJoined(chatID, newMemberName string)
}

// EventHandler coordinates incoming WhatsApp messages, logging, triggers, admin commands, and AI replies.
type EventHandler struct {
	waClient        *Client
	aiClient        *ai.OpenRouterClient
	chatLogger      *storage.ChatLogger
	memStore        *storage.MemoryStore
	adminState      *admin.State
	schedulerEngine SchedulerEngineInterface
	statsProvider   admin.StatsProvider
	limiter         *trigger.ChatLimiter
	historyLimit    int
	logger          waLog.Logger
	novaMsgMu       sync.RWMutex
	novaMsgIDs      map[string]time.Time
}

// NewEventHandler creates a new EventHandler instance.
func NewEventHandler(
	waClient *Client,
	aiClient *ai.OpenRouterClient,
	chatLogger *storage.ChatLogger,
	memStore *storage.MemoryStore,
	adminState *admin.State,
	limiter *trigger.ChatLimiter,
	historyLimit int,
	logger waLog.Logger,
) *EventHandler {
	return &EventHandler{
		waClient:     waClient,
		aiClient:     aiClient,
		chatLogger:   chatLogger,
		memStore:     memStore,
		adminState:   adminState,
		limiter:      limiter,
		historyLimit: historyLimit,
		logger:       logger,
		novaMsgIDs:   make(map[string]time.Time),
	}
}

// SetSchedulerEngine attaches the scheduler engine.
func (h *EventHandler) SetSchedulerEngine(eng SchedulerEngineInterface) {
	h.schedulerEngine = eng
}

// SetStatsProvider attaches stats metrics provider for /status command.
func (h *EventHandler) SetStatsProvider(stats admin.StatsProvider) {
	h.statsProvider = stats
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

// SendMessage sends a message to WhatsApp and records it in chat log.
func (h *EventHandler) SendMessage(ctx context.Context, chatID string, text string, replyToID string) error {
	targetJID, err := types.ParseJID(chatID)
	if err != nil {
		return fmt.Errorf("invalid chat JID: %w", err)
	}

	sentID, err := h.waClient.SendReply(ctx, targetJID, text, replyToID, "")
	if err != nil {
		return err
	}

	h.markAsNovaSent(string(sentID))

	chatType := "private"
	if strings.Contains(chatID, "@g.us") {
		chatType = "group"
	}

	novaLog := storage.LogMessage{
		MessageID:   string(sentID),
		SenderID:    h.waClient.GetUserJID().ToNonAD().String(),
		SenderName:  "Nova",
		Text:        text,
		IsNova:      true,
		Timestamp:   time.Now().Format(time.RFC3339),
		IsReply:     replyToID != "",
		RepliedToID: replyToID,
	}
	_ = h.chatLogger.AppendMessage(chatType, chatID, novaLog)
	return nil
}

// HandleEvent is the main entry point called by whatsmeow for all WhatsApp events.
func (h *EventHandler) HandleEvent(rawEvt interface{}) {
	switch evt := rawEvt.(type) {
	case *events.Message:
		h.handleMessageEvent(evt)
	case *events.GroupInfo:
		if h.schedulerEngine != nil && len(evt.Join) > 0 {
			chatID := evt.JID.String()
			for _, userJID := range evt.Join {
				h.schedulerEngine.TriggerNewMemberJoined(chatID, userJID.User)
			}
		}
	}
}

func (h *EventHandler) handleMessageEvent(evt *events.Message) {
	// 1. Loop Protection: Ignore messages sent by Nova's bot process
	if h.isNovaSent(evt.Info.ID) {
		return
	}

	// Extract message data
	text, isReply, repliedMsgID, repliedSender, repliedText := ExtractTextAndReply(evt.Message)
	if text == "" {
		return
	}

	chatID := evt.Info.Chat.String()
	senderID := evt.Info.Sender.ToNonAD().String()
	senderName := evt.Info.PushName
	if senderName == "" {
		senderName = evt.Info.Sender.User
	}
	messageID := evt.Info.ID

	chatType := "private"
	if evt.Info.IsGroup {
		chatType = "group"
	}

	// 2. Rule 7: Record EVERY incoming message in the JSONL chat log first
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
		h.logger.Errorf("Failed to log message %s in chat %s: %v", messageID, chatID, err)
	}

	// Clean text from invisible unicode marks
	cleanText := admin.CleanInvisibleMarks(text)

	// 3. Command Check (Available to any user)
	if h.adminState != nil {
		cmdRes := admin.HandleAdminCommand(h.adminState, chatID, senderID, senderName, evt.Info.IsFromMe, cleanText, h.statsProvider)
		if cmdRes.Handled {
			h.logger.Infof("Command executed by %s (%s): %s", senderName, senderID, cleanText)
			fmt.Printf("\n[👑 Command Executed] %s (%s) executed %q in chat %s\n", senderName, senderID, cleanText, chatID)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			err := h.SendMessage(ctx, chatID, cmdRes.ReplyText, messageID)
			cancel()
			if err != nil {
				h.logger.Errorf("Failed to send command reply: %v", err)
				fmt.Printf("[❌ Command Error] Failed to send reply: %v\n", err)
			}
			return
		}
	}

	// 4. Trigger Matching ("يا نوفا" in text or reply)
	triggerMatched := trigger.CheckTrigger(cleanText, isReply, admin.CleanInvisibleMarks(repliedText))

	// Feed message to rush tracker for Trigger 3
	if h.schedulerEngine != nil && evt.Info.IsGroup {
		h.schedulerEngine.RecordIncomingMessageForRush(chatID, triggerMatched)
	}

	if !triggerMatched {
		return
	}

	// 5. Shutdown Mode Check
	if h.adminState != nil && h.adminState.GetShutdown() {
		h.logger.Infof("Server is in shutdown mode, sending maintenance notice to %s", chatID)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		_ = h.SendMessage(ctx, chatID, "عذراً، تم إغلاق السيرفرات مؤقتاً. يمكنك التواصل مع الأدمن لفتح السيرفرات مرة أخرى.", messageID)
		cancel()
		return
	}

	// 6. Rate Limiter / Cooldown check per chat
	if !h.limiter.Allow(chatID) {
		h.logger.Infof("Rate limiter active for chat %s, skipping trigger", chatID)
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

	// Download media (images, PDFs) from direct message or quoted reply if present
	downloadCtx, downloadCancel := context.WithTimeout(context.Background(), 30*time.Second)
	mediaDataURL, mediaErr := DownloadMediaAsBase64(downloadCtx, h.waClient.WAClient, evt.Message)
	downloadCancel()
	if mediaErr != nil {
		h.logger.Warnf("Could not download media for message %s: %v", messageID, mediaErr)
	} else if mediaDataURL != nil {
		h.logger.Infof("Successfully downloaded and attached media for message %s", messageID)
	}

	// Dynamic history limit from admin state or default
	effectiveHistoryLimit := h.historyLimit
	if h.adminState != nil {
		effectiveHistoryLimit = h.adminState.GetChatLimit(chatID, h.historyLimit)
	}

	// 7. Build full context payload for the AI
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
		mediaDataURL,
		effectiveHistoryLimit,
	)
	if err != nil {
		h.logger.Errorf("Failed to build AI context for message %s: %v", messageID, err)
		return
	}

	// 8. Call OpenRouter AI
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
	defer cancel()

	h.logger.Infof("Trigger matched in chat %s by %s, generating AI response...", chatID, senderName)
	aiResp, err := h.aiClient.GenerateResponse(ctx, payload)
	if err != nil {
		h.logger.Errorf("Failed to generate AI response for message %s: %v", messageID, err)
		return
	}

	// 9. Rule 9: If model returns should_reply: false, do not send anything
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

	// 10. Send WhatsApp Reply (Quote)
	sentMsgID, err := h.waClient.SendReply(ctx, evt.Info.Chat, *aiResp.ReplyText, targetMsgID, evt.Info.Sender.String())
	if err != nil {
		h.logger.Errorf("Failed to send WhatsApp reply for message %s: %v", messageID, err)
		return
	}

	h.markAsNovaSent(string(sentMsgID))
	h.logger.Infof("Nova replied successfully to message %s (Sent ID: %s)", targetMsgID, sentMsgID)

	// Record Nova's response in chat log
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
	_ = h.chatLogger.AppendMessage(chatType, chatID, novaLog)

	// Save memory note if returned by AI
	if aiResp.MemoryNote != nil && strings.TrimSpace(*aiResp.MemoryNote) != "" {
		if err := h.memStore.AppendMemoryNote(senderID, senderName, *aiResp.MemoryNote); err != nil {
			h.logger.Errorf("Failed to save memory note for user %s: %v", senderID, err)
		} else {
			h.logger.Infof("Saved new memory note for user %s (%s)", senderName, senderID)
		}
	}
}
