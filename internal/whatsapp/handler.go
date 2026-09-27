package whatsapp

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"

	"novabot/internal/admin"
	"novabot/internal/ai"
	"novabot/internal/dashboard"
	"novabot/internal/emotion"
	"novabot/internal/games"
	"novabot/internal/memory"
	"novabot/internal/pacing"
	"novabot/internal/storage"
	"novabot/internal/tools"
	"novabot/internal/trigger"
	"novabot/internal/voice"
)

// SchedulerEngineInterface defines methods expected from the scheduler.
type SchedulerEngineInterface interface {
	RecordIncomingMessageForRush(chatID string, wasTriggerMatched bool)
	TriggerNewMemberJoined(chatID, newMemberName string)
}

// EventHandler coordinates incoming WhatsApp messages, logging, triggers, admin commands, and AI replies.
type EventHandler struct {
	waClient         *Client
	aiClient         *ai.OpenRouterClient
	ttsClient        *voice.OpenRouterTTS
	groqSTT          *voice.GroqSTT
	emotionEngine    *emotion.Engine
	memoryEngine     *memory.TriTierEngine
	toolRegistry     *tools.Registry
	chatLogger       *storage.ChatLogger
	memStore         *storage.MemoryStore
	adminState       *admin.State
	schedulerEngine  SchedulerEngineInterface
	statsProvider    admin.StatsProvider
	gameEngine       *games.Engine
	limiter          *trigger.ChatLimiter
	attentionGate    *trigger.AttentionGate
	coalescer        *pacing.TurnCoalescer
	pacer            *pacing.HumanPacer
	diversityFilter  *pacing.StyleDiversityFilter
	dashServer       *dashboard.Server
	historyLimit     int
	logger           waLog.Logger
	novaMsgMu        sync.RWMutex
	novaMsgIDs       map[string]time.Time
	pendingMu        sync.Mutex
	pendingDecisions map[string]trigger.TriggerDecision
	aiGenerateFn     func(ctx context.Context, payload *trigger.RequestPayload) (*ai.ResponsePayload, error)
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
		waClient:         waClient,
		aiClient:         aiClient,
		chatLogger:       chatLogger,
		memStore:         memStore,
		adminState:       adminState,
		limiter:          limiter,
		attentionGate:    trigger.NewAttentionGate(90 * time.Second),
		coalescer:        pacing.NewTurnCoalescer(1800*time.Millisecond, 1500*time.Millisecond, 4500*time.Millisecond),
		pacer:            pacing.NewHumanPacer(),
		diversityFilter:  pacing.NewStyleDiversityFilter(),
		historyLimit:     historyLimit,
		logger:           logger,
		novaMsgIDs:       make(map[string]time.Time),
		pendingDecisions: make(map[string]trigger.TriggerDecision),
	}
}

// SetDashboard attaches the live web admin dashboard server for metrics and logs.
func (h *EventHandler) SetDashboard(srv *dashboard.Server) {
	h.dashServer = srv
}

// SetAttentionGate overrides the default AttentionGate (useful for custom TTLs or tests).
func (h *EventHandler) SetAttentionGate(gate *trigger.AttentionGate) {
	if gate != nil {
		h.attentionGate = gate
	}
}

// SetTurnCoalescer overrides the default TurnCoalescer (useful for custom debounce windows or tests).
func (h *EventHandler) SetTurnCoalescer(c *pacing.TurnCoalescer) {
	if c != nil {
		h.coalescer = c
	}
}

// SetHumanPacer overrides the default HumanPacer.
func (h *EventHandler) SetHumanPacer(p *pacing.HumanPacer) {
	if p != nil {
		h.pacer = p
	}
}

// SetStyleDiversityFilter overrides the default StyleDiversityFilter.
func (h *EventHandler) SetStyleDiversityFilter(f *pacing.StyleDiversityFilter) {
	if f != nil {
		h.diversityFilter = f
	}
}

// SetTTSClient attaches the TTS speech synthesis engine.
func (h *EventHandler) SetTTSClient(tts *voice.OpenRouterTTS) {
	h.ttsClient = tts
}

// SetGroqSTT attaches the Groq Whisper speech-to-text engine.
func (h *EventHandler) SetGroqSTT(stt *voice.GroqSTT) {
	h.groqSTT = stt
}

// SetEmotionEngine attaches the human emotion simulation engine.
func (h *EventHandler) SetEmotionEngine(eng *emotion.Engine) {
	h.emotionEngine = eng
}

// SetMemoryEngine attaches the Tri-Tier Vector & Semantic memory engine.
func (h *EventHandler) SetMemoryEngine(eng *memory.TriTierEngine) {
	h.memoryEngine = eng
}

// SetToolRegistry attaches the Agentic Tools registry.
func (h *EventHandler) SetToolRegistry(reg *tools.Registry) {
	h.toolRegistry = reg
}

// SetSchedulerEngine attaches the scheduler engine.
func (h *EventHandler) SetSchedulerEngine(eng SchedulerEngineInterface) {
	h.schedulerEngine = eng
}

// SetStatsProvider attaches stats metrics provider for /status command.
func (h *EventHandler) SetStatsProvider(stats admin.StatsProvider) {
	h.statsProvider = stats
}

// SetThinkingEffort updates reasoning effort on the AI client.
func (h *EventHandler) SetThinkingEffort(effort string) {
	if h.aiClient != nil {
		h.aiClient.SetThinkingEffort(effort)
	}
}

// SetGameEngine attaches the interactive competition and game engine.
func (h *EventHandler) SetGameEngine(ge *games.Engine) {
	h.gameEngine = ge
}

// ArchiveChatSession summarizes and archives the current chat, updating user profiles.
func (h *EventHandler) ArchiveChatSession(ctx context.Context, chatType, chatID string) (string, int, error) {
	transcript, msgs, err := h.chatLogger.GetAllMessages(chatType, chatID)
	if err != nil {
		return "", 0, fmt.Errorf("failed to read chat messages: %w", err)
	}
	if len(msgs) == 0 {
		return "", 0, fmt.Errorf("chat has no messages to archive")
	}

	summary, userProfiles, sumErr := h.aiClient.SummarizeChatHistory(ctx, transcript)
	if sumErr != nil {
		return "", 0, fmt.Errorf("فشل تلخيص الذكاء الاصطناعي: %w (تم إلغاء الأرشفة للحفاظ على سجل المحادثة سليماً)", sumErr)
	}

	// Update user profile cards
	for userKey, profileNote := range userProfiles {
		_ = h.memStore.UpdateUserProfile(userKey, userKey, profileNote)
	}

	idx, archivedPath, err := h.chatLogger.ArchiveCurrentChat(chatType, chatID)
	if err != nil {
		return "", 0, fmt.Errorf("failed to archive chat file: %w", err)
	}

	_ = h.chatLogger.SaveSummary(chatType, chatID, idx, summary)

	return archivedPath, idx, nil
}

// RestoreArchivedChatSession restores a previously archived chat by index.
func (h *EventHandler) RestoreArchivedChatSession(chatType, chatID string, archiveIndex int) error {
	return h.chatLogger.RestoreArchivedChat(chatType, chatID, archiveIndex)
}

// ListChatArchives lists all archived chats for a given chat ID.
func (h *EventHandler) ListChatArchives(chatType, chatID string) ([]string, error) {
	archives, err := h.chatLogger.ListArchivedChats(chatType, chatID)
	if err != nil {
		return nil, err
	}
	var res []string
	for _, a := range archives {
		res = append(res, fmt.Sprintf("رقم %d — %d رسالة (%s)", a.Index, a.MessagesCount, a.ModTime.Format("2006-01-02 15:04")))
	}
	return res, nil
}

// SwitchPersona dynamically switches the active persona system prompt between mode 1 (Bro) and mode 2 (Charming/Female).
func (h *EventHandler) SwitchPersona(mode int) (string, error) {
	if mode < 1 || mode > 2 {
		mode = 1
	}

	promptPath := "config/system_prompt.md"
	personaName := "النمط 1 (الشباب)"
	if mode == 2 {
		promptPath = "config/system_prompt_female.md"
		personaName = "النمط 2 (البنات)"
	}

	data, err := os.ReadFile(promptPath)
	if err != nil {
		h.logger.Errorf("Failed to read persona file %s: %v", promptPath, err)
		return "", fmt.Errorf("تعذر قراءة ملف البرومبت %s: %w", promptPath, err)
	}

	newPrompt := strings.TrimSpace(string(data))
	if h.aiClient != nil {
		h.aiClient.SetSystemPrompt(newPrompt)
	}

	if h.adminState != nil {
		_ = h.adminState.SetPersona(mode)
	}

	h.logger.Infof("Successfully switched active persona to %s (%s)", personaName, promptPath)
	fmt.Printf("\n[🎭 Persona Switched] Successfully loaded persona %s from %s\n", personaName, promptPath)
	return personaName, nil
}

func (h *EventHandler) markAsNovaSent(msgID string) {
	if msgID == "" {
		return
	}
	h.novaMsgMu.Lock()
	defer h.novaMsgMu.Unlock()
	h.novaMsgIDs[msgID] = time.Now()

	// Clean up entries older than 24 hours
	now := time.Now()
	for id, t := range h.novaMsgIDs {
		if now.Sub(t) > 24*time.Hour {
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

	sentID, err := h.waClient.SendReply(ctx, targetJID, text, replyToID, "", "")
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
	if h.chatLogger != nil {
		_ = h.chatLogger.AppendMessage(chatType, chatID, novaLog)
	}
	return nil
}

// HandleEvent is the main entry point called by whatsmeow for all WhatsApp events.
func (h *EventHandler) HandleEvent(rawEvt interface{}) {
	switch evt := rawEvt.(type) {
	case *events.Message:
		h.handleMessageEvent(evt)
	case *events.ChatPresence:
		if evt.State == types.ChatPresenceComposing && !evt.MessageSource.IsFromMe && h.coalescer != nil {
			h.coalescer.NotifyTyping(evt.MessageSource.Chat.String(), evt.MessageSource.Sender.ToNonAD().String())
		}
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
	if evt == nil || evt.Message == nil {
		return
	}

	// 1. Loop Protection: Ignore messages sent by Nova's bot process
	if h.isNovaSent(evt.Info.ID) {
		return
	}

	// Ignore pure reaction messages early so reaction events never clutter .jsonl logs or trigger AI turns
	unwrapped := unwrapMessage(evt.Message)
	if unwrapped != nil && unwrapped.GetReactionMessage() != nil {
		return
	}

	// Extract message data
	text, isReply, repliedMsgID, repliedSender, repliedText := ExtractTextAndReply(evt.Message)

	chatID := evt.Info.Chat.String()
	senderID := evt.Info.Sender.ToNonAD().String()
	senderName := evt.Info.PushName
	if senderName == "" {
		senderName = evt.Info.Sender.User
	}
	if h.adminState != nil {
		if customNick := h.adminState.GetChatNickname(chatID, senderID); customNick != "" {
			senderName = customNick
		}
		if isReply && repliedSender != "" {
			if customNick := h.adminState.GetChatNickname(chatID, repliedSender); customNick != "" {
				repliedSender = customNick
			}
		}
	}
	messageID := evt.Info.ID

	chatType := "private"
	if evt.Info.IsGroup {
		chatType = "group"
	}

	// 1.5 Handle Voice Note via Groq Whisper STT
	isVoiceIncoming := (unwrapped != nil && unwrapped.GetAudioMessage() != nil)
	if isVoiceIncoming && h.groqSTT != nil && h.waClient != nil && h.waClient.WAClient != nil {
		downloadCtx, downloadCancel := context.WithTimeout(context.Background(), 25*time.Second)
		audioBytes, dlErr := h.waClient.WAClient.Download(downloadCtx, unwrapped.GetAudioMessage())
		downloadCancel()

		if dlErr == nil && len(audioBytes) > 0 {
			transcribeCtx, transcribeCancel := context.WithTimeout(context.Background(), 25*time.Second)
			transcribedText, sttErr := h.groqSTT.TranscribeAudio(transcribeCtx, audioBytes, "voice.ogg")
			transcribeCancel()

			if sttErr == nil && strings.TrimSpace(transcribedText) != "" {
				text = transcribedText
				fmt.Printf("\n[🎙️ Groq Whisper STT] Transcribed voice from %s (%s): %q\n", senderName, senderID, transcribedText)
			} else if sttErr != nil {
				h.logger.Warnf("Groq Whisper transcription failed for message %s: %v", messageID, sttErr)
			}
		}
	}

	if text == "" {
		return
	}

	// 2. Record EVERY incoming message in the JSONL chat log first
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

	if h.chatLogger != nil {
		if err := h.chatLogger.AppendMessage(chatType, chatID, logEntry); err != nil {
			h.logger.Errorf("Failed to log message %s in chat %s: %v", messageID, chatID, err)
		}
	}

	// Clean text from invisible unicode marks
	cleanText := admin.CleanInvisibleMarks(text)

	if h.memoryEngine != nil {
		_, _ = h.memoryEngine.RecordMessage(context.Background(), chatID, memory.MemoryMessage{
			MessageID:  messageID,
			SenderID:   senderID,
			SenderName: senderName,
			Text:       cleanText,
			IsNova:     false,
			Timestamp:  evt.Info.Timestamp,
		})
	}

	if h.dashServer != nil {
		if m := h.dashServer.GetMetrics(); m != nil {
			m.RecordEvent(false, 0, "", "", senderName, chatID, cleanText)
		}
	}

	// 3. Command Check (Admin Commands & Archiving)
	if h.adminState != nil {
		cmdRes := admin.HandleAdminCommand(h.adminState, chatID, senderID, senderName, evt.Info.IsFromMe, cleanText, repliedSender, h.statsProvider, h)
		if cmdRes.Handled {
			h.logger.Infof("Command executed by %s (%s): %s", senderName, senderID, cleanText)
			fmt.Printf("\n[👑 Command Executed] %s (%s) executed %q in chat %s\n", senderName, senderID, cleanText, chatID)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			err := h.SendMessage(ctx, chatID, cmdRes.ReplyText, messageID)
			cancel()
			if err != nil {
				h.logger.Errorf("Failed to send command reply: %v", err)
			}
			return
		}
	}

	// 3.5 Game Commands & Automatic Answer Interception
	if h.gameEngine != nil {
		lowerClean := strings.ToLower(cleanText)
		isGameCmd := strings.HasPrefix(lowerClean, "/game") || strings.HasPrefix(lowerClean, "!game") ||
			strings.HasPrefix(lowerClean, ".game") || strings.HasPrefix(lowerClean, "/لعبة") ||
			strings.HasPrefix(lowerClean, "/top") || strings.HasPrefix(lowerClean, "!top") ||
			strings.HasPrefix(lowerClean, ".top") || strings.HasPrefix(lowerClean, "/ترتيب")

		if isGameCmd {
			parts := strings.Fields(cleanText)
			baseCmd := strings.ToLower(parts[0])
			if strings.HasPrefix(baseCmd, "/") || strings.HasPrefix(baseCmd, "!") || strings.HasPrefix(baseCmd, ".") {
				baseCmd = baseCmd[1:]
			}

			if baseCmd == "top" || baseCmd == "ترتيب" || baseCmd == "ليدربورد" {
				topMsg := h.gameEngine.GetLeaderboardText(chatID)
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				_ = h.SendMessage(ctx, chatID, topMsg, messageID)
				cancel()
				return
			}

			if baseCmd == "game" || baseCmd == "لعبة" || baseCmd == "مسابقة" {
				subCmd := ""
				if len(parts) > 1 {
					subCmd = strings.ToLower(parts[1])
				}

				if subCmd == "stop" || subCmd == "وقف" || subCmd == "قفل" || subCmd == "الغاء" || subCmd == "إلغاء" {
					stopReply, _ := h.gameEngine.StopGame(chatID)
					ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
					_ = h.SendMessage(ctx, chatID, stopReply, messageID)
					cancel()
					return
				}

				if subCmd == "help" || subCmd == "مساعدة" || subCmd == "اقسام" || subCmd == "أقسام" {
					helpReply := h.gameEngine.GetHelpText()
					ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
					_ = h.SendMessage(ctx, chatID, helpReply, messageID)
					cancel()
					return
				}

				if subCmd == "config" || subCmd == "اعدادات" || subCmd == "إعدادات" || subCmd == "ضبط" {
					if len(parts) >= 4 && (strings.ToLower(parts[2]) == "rounds" || strings.ToLower(parts[2]) == "جولات" || strings.ToLower(parts[2]) == "اسئلة") {
						if n, err := strconv.Atoi(parts[3]); err == nil {
							reply, err := h.gameEngine.SetRounds(chatID, n)
							if err != nil {
								reply = fmt.Sprintf("⚠️ %v", err)
							}
							ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
							_ = h.SendMessage(ctx, chatID, reply, messageID)
							cancel()
							return
						}
					} else if len(parts) >= 4 && (strings.ToLower(parts[2]) == "time" || strings.ToLower(parts[2]) == "timer" || strings.ToLower(parts[2]) == "وقت") {
						if s, err := strconv.Atoi(parts[3]); err == nil {
							reply, err := h.gameEngine.SetTimeout(chatID, s)
							if err != nil {
								reply = fmt.Sprintf("⚠️ %v", err)
							}
							ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
							_ = h.SendMessage(ctx, chatID, reply, messageID)
							cancel()
							return
						}
					} else if len(parts) >= 3 && (strings.ToLower(parts[2]) == "reset" || strings.ToLower(parts[2]) == "افتراضي") {
						reply := h.gameEngine.ResetConfig(chatID)
						ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
						_ = h.SendMessage(ctx, chatID, reply, messageID)
						cancel()
						return
					}

					cfgReply := h.gameEngine.GetConfigText(chatID)
					ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
					_ = h.SendMessage(ctx, chatID, cfgReply, messageID)
					cancel()
					return
				}

				// Start game
				msg, _ := h.gameEngine.StartGame(chatID, subCmd)
				if msg != "" {
					ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
					_ = h.SendMessage(ctx, chatID, msg, messageID)
					cancel()
				}
				return
			}
		}

		// If a game is active in this chat, intercept and check answer automatically
		if h.gameEngine.HasActiveGame(chatID) {
			matched, _ := h.gameEngine.ProcessAnswer(chatID, senderID, senderName, cleanText, messageID)
			if matched {
				h.logger.Infof("Game answer matched by %s (%s) in chat %s: %s", senderName, senderID, chatID, cleanText)
				return
			}
		}
	}

	// 4. Personal Account Safe AttentionGate & Burst Coalescing
	var mentionedJIDs []string
	if unwrapped != nil {
		switch {
		case unwrapped.GetExtendedTextMessage() != nil && unwrapped.GetExtendedTextMessage().GetContextInfo() != nil:
			mentionedJIDs = unwrapped.GetExtendedTextMessage().GetContextInfo().GetMentionedJID()
		case unwrapped.GetImageMessage() != nil && unwrapped.GetImageMessage().GetContextInfo() != nil:
			mentionedJIDs = unwrapped.GetImageMessage().GetContextInfo().GetMentionedJID()
		case unwrapped.GetVideoMessage() != nil && unwrapped.GetVideoMessage().GetContextInfo() != nil:
			mentionedJIDs = unwrapped.GetVideoMessage().GetContextInfo().GetMentionedJID()
		}
	}

	botJID := ""
	if h.waClient != nil {
		botJID = h.waClient.GetUserJID().String()
	}

	repliedToNovaMsg := isReply && (h.isNovaSent(repliedMsgID) || (h.chatLogger != nil && h.chatLogger.IsNovaMessage(chatType, chatID, repliedMsgID)))
	hasPendingBurst := h.coalescer != nil && h.coalescer.HasPending(chatID, senderID)

	var decision trigger.TriggerDecision
	if h.attentionGate != nil {
		decision = h.attentionGate.Evaluate(trigger.TriggerEvalParams{
			ChatID:           chatID,
			SenderID:         senderID,
			IsGroup:          evt.Info.IsGroup,
			IsFromMe:         evt.Info.IsFromMe,
			MessageText:      cleanText,
			IsReply:          isReply,
			RepliedToText:    admin.CleanInvisibleMarks(repliedText),
			RepliedToNovaMsg: repliedToNovaMsg,
			MentionedJIDs:    mentionedJIDs,
			BotJID:           botJID,
		})
	}

	// Feed message to rush tracker & silence breaker
	if h.schedulerEngine != nil && evt.Info.IsGroup {
		h.schedulerEngine.RecordIncomingMessageForRush(chatID, decision.Matched || hasPendingBurst)
	}

	// CRITICAL INVARIANT: Never emit MarkRead, SendChatPresence, or reply on un-triggered messages!
	if !decision.Matched && !hasPendingBurst {
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

	// Store or upgrade pending trigger decision for this burst
	pendKey := chatID + "|" + senderID
	h.pendingMu.Lock()
	existingDec, hasDec := h.pendingDecisions[pendKey]
	if !hasDec || (decision.Matched && (!existingDec.Matched || (existingDec.IsLeaseTurn && !decision.IsLeaseTurn))) {
		h.pendingDecisions[pendKey] = decision
	}
	h.pendingMu.Unlock()

	item := pacing.CoalescedItem{
		MessageID:     messageID,
		Text:          text,
		IsReply:       isReply,
		RepliedMsgID:  repliedMsgID,
		RepliedSender: repliedSender,
		RepliedText:   repliedText,
		IsVoice:       isVoiceIncoming,
		Timestamp:     evt.Info.Timestamp,
		RawEvent:      evt,
	}

	if h.coalescer != nil {
		h.coalescer.Enqueue(chatID, senderID, item, func(cID, sID string, merged []pacing.CoalescedItem) {
			h.processCoalescedTurn(evt, merged)
		})
	} else {
		h.processCoalescedTurn(evt, []pacing.CoalescedItem{item})
	}
}

func (h *EventHandler) processCoalescedTurn(lastEvt *events.Message, merged []pacing.CoalescedItem) {
	if lastEvt == nil || len(merged) == 0 {
		return
	}
	if raw, ok := merged[len(merged)-1].RawEvent.(*events.Message); ok && raw != nil {
		lastEvt = raw
	}

	chatID := lastEvt.Info.Chat.String()
	senderID := lastEvt.Info.Sender.ToNonAD().String()
	senderName := lastEvt.Info.PushName
	if senderName == "" {
		senderName = lastEvt.Info.Sender.User
	}
	if h.adminState != nil {
		if customNick := h.adminState.GetChatNickname(chatID, senderID); customNick != "" {
			senderName = customNick
		}
	}
	chatType := "private"
	if lastEvt.Info.IsGroup {
		chatType = "group"
	}
	messageID := lastEvt.Info.ID

	pendKey := chatID + "|" + senderID
	h.pendingMu.Lock()
	decision := h.pendingDecisions[pendKey]
	delete(h.pendingDecisions, pendKey)
	h.pendingMu.Unlock()

	// Concurrency lock per chat
	if h.limiter != nil {
		chatLock := h.limiter.GetChatLock(chatID)
		chatLock.Lock()
		defer chatLock.Unlock()
	}

	combinedText := pacing.CombineTexts(merged)
	cleanCombined := admin.CleanInvisibleMarks(combinedText)
	if strings.TrimSpace(cleanCombined) == "" {
		return
	}

	var isReply bool
	var repliedMsgID, repliedSender, repliedText string
	var isVoiceIncoming bool
	var msgIDs []types.MessageID

	for _, it := range merged {
		if it.MessageID != "" {
			msgIDs = append(msgIDs, types.MessageID(it.MessageID))
		}
		if it.IsVoice {
			isVoiceIncoming = true
		}
		if it.IsReply && it.RepliedMsgID != "" {
			isReply = true
			repliedMsgID = it.RepliedMsgID
			repliedSender = it.RepliedSender
			repliedText = it.RepliedText
		}
	}

	var repliedTo *trigger.RepliedToInfo
	if isReply {
		repliedTo = &trigger.RepliedToInfo{
			MessageID:  repliedMsgID,
			SenderName: repliedSender,
			Text:       repliedText,
		}
	}

	// Human Read Delay & Presence
	if h.waClient != nil && h.waClient.HasTransport() {
		if h.waClient.WAClient != nil && h.pacer != nil {
			time.Sleep(h.pacer.ComputeReadDelay(cleanCombined))
		}
		readCtx, readCancel := context.WithTimeout(context.Background(), 10*time.Second)
		_ = h.waClient.MarkRead(readCtx, msgIDs, time.Now(), lastEvt.Info.Chat, lastEvt.Info.Sender)
		readCancel()

		presCtx, presCancel := context.WithTimeout(context.Background(), 10*time.Second)
		_ = h.waClient.SendChatPresence(presCtx, lastEvt.Info.Chat, types.ChatPresenceComposing, types.ChatPresenceMediaText)
		presCancel()
	}

	// Download media (images only; PDFs ignored) from any bubble in the burst or quoted reply
	var waCli *whatsmeow.Client
	if h.waClient != nil {
		waCli = h.waClient.WAClient
	}
	var mediaDataURL *string
	for _, it := range merged {
		if rawEvt, ok := it.RawEvent.(*events.Message); ok && rawEvt != nil && rawEvt.Message != nil {
			downloadCtx, downloadCancel := context.WithTimeout(context.Background(), 30*time.Second)
			urlStr, mediaErr := DownloadMediaAsBase64(downloadCtx, waCli, rawEvt.Message)
			downloadCancel()
			if mediaErr != nil {
				h.logger.Warnf("Could not download media for message %s: %v", it.MessageID, mediaErr)
			} else if urlStr != nil {
				h.logger.Infof("Successfully downloaded and attached image for message %s", it.MessageID)
				mediaDataURL = urlStr
				break
			}
		}
	}

	// Dynamic history limit from admin state or default
	effectiveHistoryLimit := h.historyLimit
	if h.adminState != nil {
		effectiveHistoryLimit = h.adminState.GetChatLimit(chatID, h.historyLimit)
	}

	// Build full context payload for the AI
	payload, err := trigger.BuildContext(
		h.chatLogger,
		h.memStore,
		chatType,
		chatID,
		"", // Chat name
		senderID,
		senderName,
		messageID,
		combinedText,
		isReply,
		repliedTo,
		mediaDataURL,
		effectiveHistoryLimit,
	)
	if err != nil {
		h.logger.Errorf("Failed to build AI context for message %s: %v", messageID, err)
		return
	}

	payload.AttentionLeaseActive = decision.IsLeaseTurn
	payload.TriggerReason = string(decision.Reason)

	if h.diversityFilter != nil {
		var recentNovaMsgs []string
		for _, m := range payload.RecentContext {
			if m.IsNova && strings.TrimSpace(m.Text) != "" {
				recentNovaMsgs = append(recentNovaMsgs, m.Text)
			}
		}
		if styleDir := h.diversityFilter.BuildPromptDirective(recentNovaMsgs); styleDir != "" {
			payload.StyleGuidance = &styleDir
		}
	}

	aiCtx, aiCancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer aiCancel()

	// Inject Emotion Engine State and Mood
	isMaker := (h.adminState != nil && h.adminState.IsAdmin(senderID, senderName, lastEvt.Info.IsFromMe))
	if h.emotionEngine != nil {
		h.emotionEngine.UpdateMood(chatID, senderID, senderName, cleanCombined, isMaker, "")
		emotionCtx := h.emotionEngine.BuildPromptContext(chatID, senderID, senderName, isMaker)
		payload.EmotionContext = &emotionCtx
	}

	// Inject Tri-Tier Comprehensive Memory Context
	if h.memoryEngine != nil {
		if vectorCtx, err := h.memoryEngine.GetComprehensiveContext(aiCtx, chatID, senderID, cleanCombined); err == nil && vectorCtx != "" {
			payload.VectorMemories = &vectorCtx
		}
	}

	if h.aiClient == nil && h.aiGenerateFn == nil {
		h.logger.Errorf("AI client not configured")
		return
	}

	h.logger.Infof("Trigger matched (%s) in chat %s by %s (%d coalesced bubble(s)), generating AI response...", decision.Reason, chatID, senderName, len(merged))
	var aiResp *ai.ResponsePayload
	if h.aiGenerateFn != nil {
		aiResp, err = h.aiGenerateFn(aiCtx, payload)
	} else {
		aiResp, err = h.aiClient.GenerateResponse(aiCtx, payload)
	}
	if err != nil {
		h.logger.Errorf("Failed to generate AI response for message %s: %v", messageID, err)
		if h.waClient != nil && h.waClient.HasTransport() {
			presCtx, presCancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = h.waClient.SendChatPresence(presCtx, lastEvt.Info.Chat, types.ChatPresencePaused, types.ChatPresenceMediaText)
			presCancel()
		}
		return
	}

	hasReaction := aiResp != nil && aiResp.ReactionEmoji != nil && strings.TrimSpace(*aiResp.ReactionEmoji) != ""
	if aiResp == nil || aiResp.Action == "ignore" || (!aiResp.ShouldReply && !hasReaction) {
		h.logger.Infof("AI decided not to reply (should_reply: false / action: ignore) for message %s", messageID)
		if h.waClient != nil && h.waClient.HasTransport() {
			presCtx, presCancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = h.waClient.SendChatPresence(presCtx, lastEvt.Info.Chat, types.ChatPresencePaused, types.ChatPresenceMediaText)
			presCancel()
		}
		if decision.IsLeaseTurn && h.attentionGate != nil {
			h.attentionGate.RevokeLease(chatID, senderID)
		}
		return
	}

	// Grant or refresh 90s attention lease
	if h.attentionGate != nil {
		h.attentionGate.GrantLease(chatID, senderID, lastEvt.Info.IsFromMe)
	}

	// Target message to reply (quote) or react to
	targetMsgID := messageID
	if aiResp.ReplyToMessageID != nil && strings.TrimSpace(*aiResp.ReplyToMessageID) != "" {
		targetMsgID = strings.TrimSpace(*aiResp.ReplyToMessageID)
	}

	// Native Emoji Reaction
	reactionEmojiStr := ""
	if hasReaction {
		reactionEmojiStr = strings.TrimSpace(*aiResp.ReactionEmoji)
		if h.waClient != nil && h.waClient.HasTransport() {
			reactCtx, reactCancel := context.WithTimeout(context.Background(), 15*time.Second)
			reactID, rErr := h.waClient.SendReaction(reactCtx, lastEvt.Info.Chat, lastEvt.Info.Sender, types.MessageID(targetMsgID), reactionEmojiStr)
			reactCancel()
			if rErr == nil && reactID != "" {
				h.markAsNovaSent(string(reactID))
			} else if rErr != nil {
				h.logger.Warnf("Failed to send reaction to message %s: %v", targetMsgID, rErr)
			}
		}
	}

	saveMemoryNote := func() {
		if aiResp.MemoryNote != nil && strings.TrimSpace(*aiResp.MemoryNote) != "" {
			note := strings.TrimSpace(*aiResp.MemoryNote)
			if h.memStore != nil {
				if err := h.memStore.AppendMemoryNote(senderID, senderName, note); err != nil {
					h.logger.Errorf("Failed to save memory note for user %s: %v", senderID, err)
				} else {
					h.logger.Infof("Saved new memory note for user %s (%s)", senderName, senderID)
				}
			}
			if h.memoryEngine != nil {
				_ = h.memoryEngine.SaveMemory(chatID, senderID, senderName, note, nil)
			}
		}
	}

	if aiResp.Action == "react_only" || !aiResp.ShouldReply || aiResp.ReplyText == nil || strings.TrimSpace(*aiResp.ReplyText) == "" {
		if h.waClient != nil && h.waClient.HasTransport() {
			presCtx, presCancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = h.waClient.SendChatPresence(presCtx, lastEvt.Info.Chat, types.ChatPresencePaused, types.ChatPresenceMediaText)
			presCancel()
		}
		saveMemoryNote()
		return
	}

	// Voice Note vs Multi-Bubble Text Delivery
	isVoiceRequested := IsVoiceRequested(cleanCombined)
	aiWantsVoice := (aiResp.SendAsVoice != nil && *aiResp.SendAsVoice)
	shouldSendVoice := (isVoiceRequested || isVoiceIncoming || aiWantsVoice)

	if h.waClient == nil {
		h.logger.Errorf("WhatsApp client not configured, cannot send reply")
		saveMemoryNote()
		return
	}

	botSenderID := h.waClient.GetUserJID().ToNonAD().String()

	var sentMsgID types.MessageID
	if shouldSendVoice && h.ttsClient != nil {
		if h.waClient.HasTransport() {
			presCtx, presCancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = h.waClient.SendChatPresence(presCtx, lastEvt.Info.Chat, types.ChatPresenceComposing, types.ChatPresenceMediaAudio)
			presCancel()
		}
		ttsCtx, ttsCancel := context.WithTimeout(context.Background(), 120*time.Second)
		oggBytes, durSec, ttsErr := h.ttsClient.SynthesizeToOggOpus(ttsCtx, *aiResp.ReplyText)
		ttsCancel()

		if ttsErr == nil && len(oggBytes) > 0 {
			sendCtx, sendCancel := context.WithTimeout(context.Background(), 60*time.Second)
			sentMsgID, err = h.waClient.SendVoiceNote(sendCtx, lastEvt.Info.Chat, oggBytes, durSec, targetMsgID, lastEvt.Info.Sender.String(), combinedText)
			sendCancel()
			if err == nil && sentMsgID != "" {
				h.markAsNovaSent(string(sentMsgID))
				h.logger.Infof("Nova replied with WhatsApp Voice Note successfully (duration: %ds, Sent ID: %s)", durSec, sentMsgID)
				cleanVoiceText := strings.TrimSpace(StripAudioTags(*aiResp.ReplyText))
				novaLog := storage.LogMessage{
					MessageID:   string(sentMsgID),
					SenderID:    botSenderID,
					SenderName:  "Nova",
					Text:        cleanVoiceText,
					IsNova:      true,
					Timestamp:   time.Now().Format(time.RFC3339),
					IsReply:     true,
					RepliedToID: targetMsgID,
				}
				if h.chatLogger != nil {
					_ = h.chatLogger.AppendMessage(chatType, chatID, novaLog)
				}
				if h.memoryEngine != nil {
					_, _ = h.memoryEngine.RecordMessage(context.Background(), chatID, memory.MemoryMessage{
						MessageID:  string(sentMsgID),
						SenderID:   botSenderID,
						SenderName: "Nova",
						Text:       cleanVoiceText,
						IsNova:     true,
						Timestamp:  time.Now(),
					})
				}
			} else {
				h.logger.Errorf("Failed to send voice note to WhatsApp: %v", err)
			}
		} else {
			h.logger.Warnf("TTS synthesis failed (%v), falling back to text reply", ttsErr)
		}
	}

	// Multi-Bubble Text Delivery
	if sentMsgID == "" {
		bubbles := aiResp.Bubbles
		if len(bubbles) == 0 {
			bubbles = pacing.SplitIntoBubbles(StripAudioTags(*aiResp.ReplyText), 3)
		}

		sentIdx := 0
		for _, rawBubble := range bubbles {
			bText := strings.TrimSpace(StripAudioTags(rawBubble))
			if bText == "" {
				continue
			}

			if sentIdx > 0 && h.waClient != nil && h.waClient.HasTransport() {
				presCtx, presCancel := context.WithTimeout(context.Background(), 5*time.Second)
				_ = h.waClient.SendChatPresence(presCtx, lastEvt.Info.Chat, types.ChatPresenceComposing, types.ChatPresenceMediaText)
				presCancel()
				if h.waClient.WAClient != nil && h.pacer != nil {
					time.Sleep(h.pacer.ComputeTypingDuration(bText))
				}
			}

			shouldQuote := false
			if sentIdx == 0 {
				if h.pacer != nil {
					shouldQuote = h.pacer.ShouldQuoteReply(lastEvt.Info.IsGroup, isReply, 0)
				} else {
					shouldQuote = pacing.ShouldQuoteReply(lastEvt.Info.IsGroup, isReply, 0)
				}
			}

			replyCtx, replyCancel := context.WithTimeout(context.Background(), 30*time.Second)
			var bSentID types.MessageID
			var sendErr error
			if shouldQuote {
				bSentID, sendErr = h.waClient.SendReply(replyCtx, lastEvt.Info.Chat, bText, targetMsgID, lastEvt.Info.Sender.String(), combinedText)
			} else {
				bSentID, sendErr = h.waClient.SendText(replyCtx, lastEvt.Info.Chat, bText)
			}
			replyCancel()

			if sendErr != nil {
				h.logger.Errorf("Failed to send WhatsApp bubble %d for message %s: %v", sentIdx+1, messageID, sendErr)
				continue
			}

			sentIdx++
			h.markAsNovaSent(string(bSentID))
			h.logger.Infof("Nova sent bubble %d to message %s (Sent ID: %s, quoted=%v)", sentIdx, targetMsgID, bSentID, shouldQuote)

			repliedToForLog := ""
			if shouldQuote {
				repliedToForLog = targetMsgID
			}
			novaLog := storage.LogMessage{
				MessageID:   string(bSentID),
				SenderID:    botSenderID,
				SenderName:  "Nova",
				Text:        bText,
				IsNova:      true,
				Timestamp:   time.Now().Format(time.RFC3339),
				IsReply:     shouldQuote,
				RepliedToID: repliedToForLog,
			}
			if h.chatLogger != nil {
				_ = h.chatLogger.AppendMessage(chatType, chatID, novaLog)
			}
			if h.memoryEngine != nil {
				_, _ = h.memoryEngine.RecordMessage(context.Background(), chatID, memory.MemoryMessage{
					MessageID:  string(bSentID),
					SenderID:   botSenderID,
					SenderName: "Nova",
					Text:       bText,
					IsNova:     true,
					Timestamp:  time.Now(),
				})
			}
		}

		if h.waClient != nil && h.waClient.HasTransport() {
			presCtx, presCancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = h.waClient.SendChatPresence(presCtx, lastEvt.Info.Chat, types.ChatPresencePaused, types.ChatPresenceMediaText)
			presCancel()
		}
	}

	if h.dashServer != nil {
		moodStr := "witty"
		if aiResp.Mood != nil && strings.TrimSpace(*aiResp.Mood) != "" {
			moodStr = strings.TrimSpace(*aiResp.Mood)
		}
		snippet := StripAudioTags(*aiResp.ReplyText)
		if m := h.dashServer.GetMetrics(); m != nil {
			m.RecordEvent(true, len([]rune(snippet)), moodStr, reactionEmojiStr, senderName, chatID, snippet)
		}
		if lh := h.dashServer.GetLogHub(); lh != nil {
			lh.AddLog("INFO", fmt.Sprintf("Nova replied in %s to %s (reason=%s, mood=%s)", chatID, senderName, decision.Reason, moodStr))
		}
	}

	saveMemoryNote()
}

// IsVoiceRequested detects whether the user's message is asking for a voice note, pronunciation, or speaking.
func IsVoiceRequested(text string) bool {
	if strings.TrimSpace(text) == "" {
		return false
	}

	lower := strings.ToLower(text)
	normalized := trigger.NormalizeForMatch(text)

	// Explicit keywords
	voiceKeywords := []string{
		"فويس", "ريكورد", "صوتك", "بصوتك", "صوتي", "اتكلم", "تكلم", "تسجيل",
		"voice", "audio", "speech", "pronounce", "record",
	}
	for _, kw := range voiceKeywords {
		if strings.Contains(lower, kw) || strings.Contains(normalized, kw) {
			return true
		}
	}

	// Pronunciation / reading / speaking intent keywords in Egyptian & Arabic
	speechIntents := []string{
		"انطق", "انطقلي", "انطقها", "انطق ده", "انطق دا", "انطق دي", "انطق الكلام",
		"قول الكلام", "قول الجمله", "قولهالي", "قولها", "قول دا", "قول ده", "قول دي",
		"اقرا الكلام", "اقرا الجمله", "اقرالي", "اقراها", "اقرا ده", "اقرا دا", "اقرأ",
		"سمعني", "سمعنا", "اسمع صوتك", "عايز اسمع", "حابب اسمع", "خليني اسمع",
	}
	for _, intent := range speechIntents {
		normIntent := trigger.NormalizeForMatch(intent)
		if strings.Contains(normalized, normIntent) || strings.Contains(text, intent) {
			return true
		}
	}

	return false
}

var audioTagsRegex = regexp.MustCompile(`\[(laughs|chuckles|sighs|whispers|gasp|pause|excited|cough|slow|rushed|clears throat)\]`)

// StripAudioTags removes internal speech synthesis tags like [laughs], [chuckles], [sighs] so they never appear in plain text chat.
func StripAudioTags(s string) string {
	clean := audioTagsRegex.ReplaceAllString(s, "")
	clean = strings.ReplaceAll(clean, "  ", " ")
	return strings.TrimSpace(clean)
}
