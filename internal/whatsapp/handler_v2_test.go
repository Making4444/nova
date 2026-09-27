package whatsapp

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"

	"novabot/internal/ai"
	"novabot/internal/pacing"
	"novabot/internal/storage"
	"novabot/internal/trigger"
)

type recordedTransport struct {
	mu            sync.Mutex
	readReceipts  [][]types.MessageID
	presences     []types.ChatPresence
	sentReplies   []string
	sentTexts     []string
	sentReactions []string
	nextSentSeq   int
}

func newTestClient(rec *recordedTransport) *Client {
	botJID := types.NewJID("201202172699", types.DefaultUserServer)
	return &Client{
		userJID: botJID,
		markReadFn: func(ctx context.Context, msgIDs []types.MessageID, timestamp time.Time, chatJID, senderJID types.JID) error {
			rec.mu.Lock()
			defer rec.mu.Unlock()
			cp := make([]types.MessageID, len(msgIDs))
			copy(cp, msgIDs)
			rec.readReceipts = append(rec.readReceipts, cp)
			return nil
		},
		sendPresenceFn: func(ctx context.Context, chatJID types.JID, state types.ChatPresence, media types.ChatPresenceMedia) error {
			rec.mu.Lock()
			defer rec.mu.Unlock()
			rec.presences = append(rec.presences, state)
			return nil
		},
		sendReplyFn: func(ctx context.Context, chatJID types.JID, replyText, replyToID, replyToSender, quotedText string) (types.MessageID, error) {
			rec.mu.Lock()
			defer rec.mu.Unlock()
			rec.nextSentSeq++
			rec.sentReplies = append(rec.sentReplies, replyText)
			return types.MessageID("nova-msg-" + string(rune('0'+rec.nextSentSeq))), nil
		},
		sendTextFn: func(ctx context.Context, chatJID types.JID, text string) (types.MessageID, error) {
			rec.mu.Lock()
			defer rec.mu.Unlock()
			rec.nextSentSeq++
			rec.sentTexts = append(rec.sentTexts, text)
			return types.MessageID("nova-msg-" + string(rune('0'+rec.nextSentSeq))), nil
		},
		sendReactionFn: func(ctx context.Context, chatJID, senderJID types.JID, targetMsgID types.MessageID, emoji string) (types.MessageID, error) {
			rec.mu.Lock()
			defer rec.mu.Unlock()
			rec.sentReactions = append(rec.sentReactions, emoji)
			return types.MessageID("nova-react-1"), nil
		},
	}
}

func makeMessageEvent(msgID, chatUser, senderUser, pushName, text string, isGroup bool, replyToID, quotedText string) *events.Message {
	server := types.DefaultUserServer
	if isGroup {
		server = types.GroupServer
	}
	chatJID := types.NewJID(chatUser, server)
	senderJID := types.NewJID(senderUser, types.DefaultUserServer)

	var msg *waE2E.Message
	if replyToID != "" {
		msg = &waE2E.Message{
			ExtendedTextMessage: &waE2E.ExtendedTextMessage{
				Text: proto.String(text),
				ContextInfo: &waE2E.ContextInfo{
					StanzaID:    proto.String(replyToID),
					Participant: proto.String(senderJID.String()),
					QuotedMessage: &waE2E.Message{
						Conversation: proto.String(quotedText),
					},
				},
			},
		}
	} else {
		msg = &waE2E.Message{
			Conversation: proto.String(text),
		}
	}

	return &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:     chatJID,
				Sender:   senderJID,
				IsFromMe: false,
				IsGroup:  isGroup,
			},
			ID:        types.MessageID(msgID),
			PushName:  pushName,
			Timestamp: time.Now(),
		},
		Message: msg,
	}
}

func TestPersonalAccountSafety_UntriggeredDMIgnored(t *testing.T) {
	tmpDir := t.TempDir()
	chatLogger, err := storage.NewChatLogger(tmpDir + "/chats")
	if err != nil {
		t.Fatalf("NewChatLogger: %v", err)
	}
	memStore, err := storage.NewMemoryStore(tmpDir + "/users")
	if err != nil {
		t.Fatalf("NewMemoryStore: %v", err)
	}

	rec := &recordedTransport{}
	cli := newTestClient(rec)
	limiter := trigger.NewChatLimiter(0)
	h := NewEventHandler(cli, nil, chatLogger, memStore, nil, limiter, 25, waLog.Noop)
	h.SetTurnCoalescer(pacing.NewTurnCoalescer(25*time.Millisecond, 25*time.Millisecond, 80*time.Millisecond))

	aiCalled := false
	h.aiGenerateFn = func(ctx context.Context, payload *trigger.RequestPayload) (*ai.ResponsePayload, error) {
		aiCalled = true
		reply := "رد من نوفا"
		return &ai.ResponsePayload{ShouldReply: true, Action: "reply", ReplyText: &reply}, nil
	}

	// 1. Normal 1-on-1 DM to the human owner without wake word
	dmEvt := makeMessageEvent("dm-1", "201000000001", "201000000001", "Friend", "ازيك يا صاحبي هعدي عليك النهاردة بالليل", false, "", "")
	h.HandleEvent(dmEvt)

	// 2. Reply in a 1-on-1 DM quoting a human owner message (NOT sent by Nova)
	replyOwnerEvt := makeMessageEvent("dm-2", "201000000001", "201000000001", "Friend", "تمام ماشي نتقابل الساعة 8", false, "human-owner-msg-99", "نتقابل امتى؟")
	h.HandleEvent(replyOwnerEvt)

	time.Sleep(60 * time.Millisecond)

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if aiCalled {
		t.Fatalf("expected AI never to be called on untriggered 1-on-1 DMs")
	}
	if len(rec.readReceipts) != 0 {
		t.Fatalf("expected zero MarkRead calls on untriggered DMs, got %d", len(rec.readReceipts))
	}
	if len(rec.presences) != 0 {
		t.Fatalf("expected zero SendChatPresence calls on untriggered DMs, got %d", len(rec.presences))
	}
	if len(rec.sentReplies) != 0 || len(rec.sentTexts) != 0 {
		t.Fatalf("expected zero outbound messages on untriggered DMs")
	}
}

func TestReplyToNovaMessageIDAndAttentionLeaseLifecycle(t *testing.T) {
	tmpDir := t.TempDir()
	chatLogger, err := storage.NewChatLogger(tmpDir + "/chats")
	if err != nil {
		t.Fatalf("NewChatLogger: %v", err)
	}
	memStore, err := storage.NewMemoryStore(tmpDir + "/users")
	if err != nil {
		t.Fatalf("NewMemoryStore: %v", err)
	}

	rec := &recordedTransport{}
	cli := newTestClient(rec)
	limiter := trigger.NewChatLimiter(0)
	h := NewEventHandler(cli, nil, chatLogger, memStore, nil, limiter, 25, waLog.Noop)
	h.SetTurnCoalescer(pacing.NewTurnCoalescer(20*time.Millisecond, 20*time.Millisecond, 60*time.Millisecond))

	var mu sync.Mutex
	var payloads []*trigger.RequestPayload
	nextAction := "reply"

	h.aiGenerateFn = func(ctx context.Context, payload *trigger.RequestPayload) (*ai.ResponsePayload, error) {
		mu.Lock()
		payloads = append(payloads, payload)
		act := nextAction
		mu.Unlock()

		if act == "ignore" {
			return &ai.ResponsePayload{ShouldReply: false, Action: "ignore"}, nil
		}
		reply := "أهلاً بيك، أنا معاك"
		return &ai.ResponsePayload{
			ShouldReply: true,
			Action:      "reply",
			ReplyText:   &reply,
			Bubbles:     []string{reply},
		}, nil
	}

	// Step 1: Explicit wake word in DM triggers Nova and grants 90s attention lease
	wakeEvt := makeMessageEvent("msg-wake", "201000000002", "201000000002", "Omar", "يا نوفا محتاج مساعدة", false, "", "")
	h.HandleEvent(wakeEvt)
	time.Sleep(60 * time.Millisecond)

	mu.Lock()
	if len(payloads) != 1 {
		t.Fatalf("expected 1 AI turn after wake word, got %d", len(payloads))
	}
	if payloads[0].AttentionLeaseActive {
		t.Errorf("expected first wake turn to have AttentionLeaseActive=false")
	}
	mu.Unlock()

	// Step 2: Follow-up message without wake word during active 90s lease triggers Nova
	followUpEvt := makeMessageEvent("msg-followup-1", "201000000002", "201000000002", "Omar", "عندي امتحان فيزياء بكرة", false, "", "")
	h.HandleEvent(followUpEvt)
	time.Sleep(60 * time.Millisecond)

	mu.Lock()
	if len(payloads) != 2 {
		t.Fatalf("expected 2nd AI turn via attention lease, got %d", len(payloads))
	}
	if !payloads[1].AttentionLeaseActive {
		t.Errorf("expected follow-up turn to have AttentionLeaseActive=true")
	}
	nextAction = "ignore"
	mu.Unlock()

	// Step 3: User switches topic to someone else -> AI returns action="ignore", revoking lease
	offTopicEvt := makeMessageEvent("msg-offtopic", "201000000002", "201000000002", "Omar", "ثانية واحدة هرد على التليفون", false, "", "")
	h.HandleEvent(offTopicEvt)
	time.Sleep(60 * time.Millisecond)

	mu.Lock()
	nextAction = "reply"
	mu.Unlock()

	// Step 4: Subsequent un-triggered message after lease revocation must be ignored
	afterRevokeEvt := makeMessageEvent("msg-after-revoke", "201000000002", "201000000002", "Omar", "ألو يا أحمد انت فين", false, "", "")
	h.HandleEvent(afterRevokeEvt)
	time.Sleep(60 * time.Millisecond)

	mu.Lock()
	if len(payloads) != 3 {
		t.Fatalf("expected post-revocation message to be ignored (still 3 AI calls), got %d", len(payloads))
	}
	mu.Unlock()

	// Step 5: Simulate restart (clear in-memory novaMsgIDs) and quote-reply to Nova's earlier message ID ("nova-msg-1")
	h.novaMsgMu.Lock()
	h.novaMsgIDs = make(map[string]time.Time)
	h.novaMsgMu.Unlock()

	replyNovaEvt := makeMessageEvent("msg-quote-nova", "201000000002", "201000000002", "Omar", "نرجع لموضوع الامتحان بقى", false, "nova-msg-1", "أهلاً بيك، أنا معاك")
	h.HandleEvent(replyNovaEvt)
	time.Sleep(60 * time.Millisecond)

	mu.Lock()
	if len(payloads) != 4 {
		t.Fatalf("expected quote-reply to persisted Nova message ID to trigger AI across restart, got %d", len(payloads))
	}
	if payloads[3].TriggerReason != string(trigger.ReasonRepliedToNova) {
		t.Errorf("expected TriggerReason=%q, got %q", trigger.ReasonRepliedToNova, payloads[3].TriggerReason)
	}
	mu.Unlock()
}

func TestBurstCoalescingAndMultiBubbleDelivery(t *testing.T) {
	tmpDir := t.TempDir()
	chatLogger, err := storage.NewChatLogger(tmpDir + "/chats")
	if err != nil {
		t.Fatalf("NewChatLogger: %v", err)
	}
	memStore, err := storage.NewMemoryStore(tmpDir + "/users")
	if err != nil {
		t.Fatalf("NewMemoryStore: %v", err)
	}

	chatID := "120363044444444444@g.us"
	// Seed 2 repetitive Nova turns in chatLogger to verify StyleGuidance anti-repetition injection
	_ = chatLogger.AppendMessage("group", chatID, storage.LogMessage{
		MessageID:  "seed-1",
		SenderID:   "201202172699@s.whatsapp.net",
		SenderName: "Nova",
		Text:       "منور الدنيا يا مكاري 😂🔥",
		IsNova:     true,
		Timestamp:  time.Now().Add(-2 * time.Minute).Format(time.RFC3339),
	})
	_ = chatLogger.AppendMessage("group", chatID, storage.LogMessage{
		MessageID:  "seed-2",
		SenderID:   "201202172699@s.whatsapp.net",
		SenderName: "Nova",
		Text:       "منور الدنيا يا مكاري والنبى 😂🔥",
		IsNova:     true,
		Timestamp:  time.Now().Add(-1 * time.Minute).Format(time.RFC3339),
	})

	rec := &recordedTransport{}
	cli := newTestClient(rec)
	limiter := trigger.NewChatLimiter(0)
	h := NewEventHandler(cli, nil, chatLogger, memStore, nil, limiter, 25, waLog.Noop)
	h.SetTurnCoalescer(pacing.NewTurnCoalescer(45*time.Millisecond, 40*time.Millisecond, 150*time.Millisecond))

	var mu sync.Mutex
	var capturedPayloads []*trigger.RequestPayload

	h.aiGenerateFn = func(ctx context.Context, payload *trigger.RequestPayload) (*ai.ResponsePayload, error) {
		mu.Lock()
		capturedPayloads = append(capturedPayloads, payload)
		mu.Unlock()

		fullText := "الجزء الأول من الإجابة || الجزء التاني بالتفصيل"
		emoji := "🔥"
		return &ai.ResponsePayload{
			ShouldReply:   true,
			Action:        "reply",
			ReplyText:     &fullText,
			Bubbles:       []string{"الجزء الأول من الإجابة", "الجزء التاني بالتفصيل"},
			ReactionEmoji: &emoji,
		}, nil
	}

	// Send 3 rapid bubbles in a burst + typing presence
	b1 := makeMessageEvent("burst-1", "120363044444444444", "201111111111", "Makari", "يا نوفا", true, "", "")
	b2 := makeMessageEvent("burst-2", "120363044444444444", "201111111111", "Makari", "عندي سؤال سريع", true, "", "")
	b3 := makeMessageEvent("burst-3", "120363044444444444", "201111111111", "Makari", "إيه الفرق بين السرعة والعجلة؟", true, "", "")

	h.HandleEvent(b1)
	time.Sleep(10 * time.Millisecond)
	h.HandleEvent(&events.ChatPresence{
		MessageSource: types.MessageSource{
			Chat:     b1.Info.Chat,
			Sender:   b1.Info.Sender,
			IsFromMe: false,
			IsGroup:  true,
		},
		State: types.ChatPresenceComposing,
	})
	h.HandleEvent(b2)
	time.Sleep(10 * time.Millisecond)
	h.HandleEvent(b3)

	time.Sleep(140 * time.Millisecond)

	mu.Lock()
	if len(capturedPayloads) != 1 {
		t.Fatalf("expected 3 burst bubbles to coalesce into 1 AI call, got %d", len(capturedPayloads))
	}
	p := capturedPayloads[0]
	mu.Unlock()

	expectedCombined := "يا نوفا\nعندي سؤال سريع\nإيه الفرق بين السرعة والعجلة؟"
	if p.MessageText != expectedCombined {
		t.Errorf("expected coalesced MessageText=%q, got %q", expectedCombined, p.MessageText)
	}
	if p.StyleGuidance == nil || !strings.Contains(*p.StyleGuidance, "STYLE DIVERSITY") {
		t.Errorf("expected StyleGuidance directive to be injected, got %v", p.StyleGuidance)
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.readReceipts) != 1 || len(rec.readReceipts[0]) != 3 {
		t.Errorf("expected MarkRead with 3 coalesced message IDs, got %+v", rec.readReceipts)
	}
	if len(rec.sentReactions) != 1 || rec.sentReactions[0] != "🔥" {
		t.Errorf("expected 1 reaction '🔥', got %+v", rec.sentReactions)
	}
	// In group chat: bubble 1 is quoted (SendReply), bubble 2 is unquoted (SendText)
	if len(rec.sentReplies) != 1 || rec.sentReplies[0] != "الجزء الأول من الإجابة" {
		t.Errorf("expected bubble 1 via SendReply, got %+v", rec.sentReplies)
	}
	if len(rec.sentTexts) != 1 || rec.sentTexts[0] != "الجزء التاني بالتفصيل" {
		t.Errorf("expected bubble 2 via SendText, got %+v", rec.sentTexts)
	}
}

func TestEphemeralUnwrapAndReactionIgnore(t *testing.T) {
	// 1. Verify ExtractTextAndReply unwraps EphemeralMessage wrapping ExtendedTextMessage with reply ContextInfo
	ephemeralMsg := &waE2E.Message{
		EphemeralMessage: &waE2E.FutureProofMessage{
			Message: &waE2E.Message{
				ExtendedTextMessage: &waE2E.ExtendedTextMessage{
					Text: proto.String("رسالة مؤقتة يا نوفا"),
					ContextInfo: &waE2E.ContextInfo{
						StanzaID:    proto.String("quoted-id-77"),
						Participant: proto.String("201202172699@s.whatsapp.net"),
						QuotedMessage: &waE2E.Message{
							Conversation: proto.String("الرسالة الأصلية"),
						},
					},
				},
			},
		},
	}

	text, isReply, repliedID, _, repliedText := ExtractTextAndReply(ephemeralMsg)
	if text != "رسالة مؤقتة يا نوفا" || !isReply || repliedID != "quoted-id-77" || repliedText != "الرسالة الأصلية" {
		t.Fatalf("unexpected ExtractTextAndReply on EphemeralMessage: text=%q isReply=%v repliedID=%q repliedText=%q",
			text, isReply, repliedID, repliedText)
	}

	// 2. Verify pure ReactionMessage is ignored before logging to ChatLogger
	tmpDir := t.TempDir()
	chatLogger, err := storage.NewChatLogger(tmpDir + "/chats")
	if err != nil {
		t.Fatalf("NewChatLogger: %v", err)
	}
	h := NewEventHandler(nil, nil, chatLogger, nil, nil, nil, 25, waLog.Noop)

	reactEvt := &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:   types.NewJID("201000000099", types.DefaultUserServer),
				Sender: types.NewJID("201000000099", types.DefaultUserServer),
			},
			ID:        "react-evt-1",
			Timestamp: time.Now(),
		},
		Message: &waE2E.Message{
			ReactionMessage: &waE2E.ReactionMessage{
				Text: proto.String("❤️"),
			},
		},
	}
	h.HandleEvent(reactEvt)

	logs, _ := chatLogger.GetRecentMessages("private", "201000000099@s.whatsapp.net", 10)
	if len(logs) != 0 {
		t.Fatalf("expected pure reaction event not to be logged in ChatLogger, got %d log entries", len(logs))
	}
}
