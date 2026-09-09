package ai

import (
	"strings"
	"testing"

	"novabot/internal/trigger"
)

func TestBuildSystemPrompt(t *testing.T) {
	client := NewOpenRouterClient("fake-key", "model-chat", "أنت نوفا، مصري جدع وبتحب الهزار.")

	userMem := "مكاري هو المطور وصاحبك وأخوك."
	emotionCtx := "رايق ومبسوط (طاقة: 8/10)"
	chatSum := "كنا بنتكلم عن مشاريع الذكاء الاصطناعي."
	groupName := "شلة هندسة"

	payload := &trigger.RequestPayload{
		ChatType:       "group",
		ChatID:         "120363@g.us",
		ChatName:       &groupName,
		SenderName:     "Making",
		CurrentTime:    "الإثنين 7 سبتمبر 2026، 05:00 مساءً",
		TimeOfDay:      "فترة العصر",
		UserMemory:     &userMem,
		EmotionContext: &emotionCtx,
		ChatSummary:    &chatSum,
	}

	systemPrompt := client.buildSystemPrompt(payload)

	if !strings.Contains(systemPrompt, "أنت نوفا، مصري جدع وبتحب الهزار.") {
		t.Errorf("expected base prompt in systemPrompt")
	}
	if !strings.Contains(systemPrompt, "جروب واتساب (اسم الجروب: \"شلة هندسة\")") {
		t.Errorf("expected group name in systemPrompt")
	}
	if !strings.Contains(systemPrompt, "Making") {
		t.Errorf("expected sender name in systemPrompt")
	}
	if !strings.Contains(systemPrompt, "مكاري هو المطور وصاحبك وأخوك.") {
		t.Errorf("expected user memory in systemPrompt")
	}
	if !strings.Contains(systemPrompt, "رايق ومبسوط") {
		t.Errorf("expected emotion context in systemPrompt")
	}
	if !strings.Contains(systemPrompt, "كنا بنتكلم عن مشاريع الذكاء الاصطناعي.") {
		t.Errorf("expected chat summary in systemPrompt")
	}
}

func TestBuildMessages_NativeTurns(t *testing.T) {
	client := NewOpenRouterClient("fake-key", "model-chat", "أنت نوفا.")

	repliedText := "الامتحان بكرة الساعة كام؟"
	payload := &trigger.RequestPayload{
		ChatType:    "group",
		SenderName:  "Making",
		MessageText: "الامتحان الساعة 9 الصبح يا شباب",
		IsReply:     true,
		RepliedTo: &trigger.RepliedToInfo{
			SenderName: "Ahmed",
			Text:       repliedText,
		},
		RecentContext: []trigger.ContextMessage{
			{
				SenderName: "Ahmed",
				Text:       "الامتحان بكرة الساعة كام؟",
				IsNova:     false,
				Timestamp:  "2026-09-07T16:50:00Z",
			},
			{
				SenderName: "Nova",
				Text:       "تقريباً كان الصبح بدري، استنى نتأكد",
				IsNova:     true,
				Timestamp:  "2026-09-07T16:51:00Z",
			},
			{
				SenderName: "Sara",
				Text:       "ياريت حد يفيدنا يا جماعة",
				IsNova:     false,
				Timestamp:  "2026-09-07T16:52:00Z",
			},
		},
	}

	messages := client.buildMessages(payload)

	// Total messages: 1 system + 3 context + 1 incoming = 5 messages
	if len(messages) != 5 {
		t.Fatalf("expected 5 messages, got %d", len(messages))
	}

	// 1. System message
	if messages[0].Role != "system" {
		t.Errorf("expected messages[0] to be system, got %s", messages[0].Role)
	}

	// 2. Ahmed's message
	if messages[1].Role != "user" {
		t.Errorf("expected messages[1] to be user, got %s", messages[1].Role)
	}
	if messages[1].Content != "[Ahmed]: الامتحان بكرة الساعة كام؟" {
		t.Errorf("unexpected content for messages[1]: %v", messages[1].Content)
	}

	// 3. Nova's prior answer (Assistant!)
	if messages[2].Role != "assistant" {
		t.Errorf("expected messages[2] to be assistant, got %s", messages[2].Role)
	}
	if messages[2].Content != "تقريباً كان الصبح بدري، استنى نتأكد" {
		t.Errorf("unexpected content for messages[2]: %v", messages[2].Content)
	}

	// 4. Sara's message
	if messages[3].Role != "user" {
		t.Errorf("expected messages[3] to be user, got %s", messages[3].Role)
	}
	if messages[3].Content != "[Sara]: ياريت حد يفيدنا يا جماعة" {
		t.Errorf("unexpected content for messages[3]: %v", messages[3].Content)
	}

	// 5. Making's incoming reply message
	if messages[4].Role != "user" {
		t.Errorf("expected messages[4] to be user, got %s", messages[4].Role)
	}
	expectedCurrent := "[Making]: (رداً على Ahmed: \"الامتحان بكرة الساعة كام؟\")\nالامتحان الساعة 9 الصبح يا شباب"
	if messages[4].Content != expectedCurrent {
		t.Errorf("expected messages[4] content:\n%q\ngot:\n%q", expectedCurrent, messages[4].Content)
	}
}

func TestBuildMessages_Multimodal(t *testing.T) {
	client := NewOpenRouterClient("fake-key", "model-chat", "أنت نوفا.")
	mediaURL := "data:image/jpeg;base64,12345"

	payload := &trigger.RequestPayload{
		ChatType:     "private",
		SenderName:   "Making",
		MessageText:  "شوف الصورة دي كده",
		MediaDataURL: &mediaURL,
	}

	messages := client.buildMessages(payload)
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}

	parts, ok := messages[1].Content.([]contentPart)
	if !ok {
		t.Fatalf("expected content to be []contentPart for multimodal message, got %T", messages[1].Content)
	}
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts (text + image_url), got %d", len(parts))
	}
	if parts[0].Text != "شوف الصورة دي كده" {
		t.Errorf("unexpected text part: %s", parts[0].Text)
	}
	if parts[1].ImageURL == nil || parts[1].ImageURL.URL != mediaURL {
		t.Errorf("unexpected image_url part: %+v", parts[1].ImageURL)
	}
}

func TestTokenLimits(t *testing.T) {
	client := NewOpenRouterClient("fake-key", "model-chat", "أنت نوفا.")
	if client.GetMaxTokens() != 4096 {
		t.Errorf("expected default maxTokens 4096, got %d", client.GetMaxTokens())
	}

	client.SetMaxTokens(6000)
	if client.GetMaxTokens() != 6000 {
		t.Errorf("expected updated maxTokens 6000, got %d", client.GetMaxTokens())
	}

	// Should ignore negative or zero values
	client.SetMaxTokens(-10)
	if client.GetMaxTokens() != 6000 {
		t.Errorf("expected maxTokens to remain 6000 after invalid update, got %d", client.GetMaxTokens())
	}
}

func TestThinkingEffort(t *testing.T) {
	client := NewOpenRouterClient("fake-key", "model-chat", "أنت نوفا.")
	if client.GetThinkingEffort() != "auto" {
		t.Errorf("expected default thinkingEffort 'auto', got %s", client.GetThinkingEffort())
	}

	client.SetThinkingEffort("none")
	if client.GetThinkingEffort() != "none" {
		t.Errorf("expected thinkingEffort 'none', got %s", client.GetThinkingEffort())
	}

	client.SetThinkingEffort("off")
	if client.GetThinkingEffort() != "none" {
		t.Errorf("expected 'off' to map to 'none', got %s", client.GetThinkingEffort())
	}

	client.SetThinkingEffort("high")
	if client.GetThinkingEffort() != "high" {
		t.Errorf("expected thinkingEffort 'high', got %s", client.GetThinkingEffort())
	}

	client.SetThinkingEffort("auto")
	if client.GetThinkingEffort() != "auto" {
		t.Errorf("expected thinkingEffort 'auto', got %s", client.GetThinkingEffort())
	}
}

