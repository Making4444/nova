package trigger

import (
	"os"
	"testing"
	"time"

	"novabot/internal/storage"
)

func TestNormalizeForMatchDoesNotMutateOriginal(t *testing.T) {
	original := "يَا نُوفَا، إيهِ الأَخْبَار؟"
	originalCopy := original

	norm := NormalizeForMatch(original)

	if original != originalCopy {
		t.Errorf("original string was mutated! expected %q, got %q", originalCopy, original)
	}

	if norm == original {
		t.Errorf("normalized text should differ from text with diacritics")
	}
}

func TestContainsTrigger(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		expected bool
	}{
		{"Exact standard", "يا نوفا", true},
		{"With Tashkeel", "يَا نُوفَا عاملة ايه؟", true},
		{"Connected", "يانوفا ازيك", true},
		{"Multiple spaces", "يا    نوفا صباح الخير", true},
		{"With Teh Marbuta", "يا نوفة كيفك", true},
		{"Middle of sentence", "عايز اقولك يا نوفا ان الدنيا حر", true},
		{"With Punctuation", "يا نوفا!؟ شوفي كدة.", true},
		{"With Alef variants", "إزيك يا نوفا", true},
		{"Negative - normal sentence", "انا رايح الشغل دلوقتي", false},
		{"Negative - nova without ya", "نوفا اسم حلو", false},
		{"Positive - at nova", "@nova ازيك", true},
		{"Positive - at nova arabic", "@نوفا صباح الفل", true},
		{"Negative - empty", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ContainsTrigger(tc.input)
			if got != tc.expected {
				t.Errorf("ContainsTrigger(%q) = %v, expected %v", tc.input, got, tc.expected)
			}
		})
	}
}

func TestCheckTriggerWithReply(t *testing.T) {
	// 1. Direct message matched
	if !CheckTrigger("يا نوفا قوليلي نكتة", false, "") {
		t.Errorf("expected direct trigger to match")
	}

	// 2. Reply to a message that had trigger
	if !CheckTrigger("تمام فهمتك", true, "سألتك قبل كدة يا نوفا عن الموضوع ده") {
		t.Errorf("expected reply trigger to match")
	}

	// 3. Reply to a message without trigger, and current has no trigger
	if CheckTrigger("تمام", true, "صباح الخير يا شباب") {
		t.Errorf("expected no match for non-trigger reply")
	}
}

func TestCheckTriggerWithMentions(t *testing.T) {
	botJID := "201202172699@s.whatsapp.net"

	// 1. Mention bot by JID in MentionedJIDs
	if !CheckTriggerWithMentions("ازيك عامل ايه", false, "", []string{botJID}, botJID) {
		t.Errorf("expected mention by bot JID to match trigger")
	}

	// 2. Mention @nova or @نوفا in text
	if !CheckTriggerWithMentions("@nova مساء الورد", false, "", nil, botJID) {
		t.Errorf("expected @nova to match trigger")
	}
	if !CheckTriggerWithMentions("@نوفا اخبارك", false, "", nil, botJID) {
		t.Errorf("expected @نوفا to match trigger")
	}

	// 3. Standalone "نوفا" without "يا" or "@" should be false
	if CheckTriggerWithMentions("نوفا اسم كويس", false, "", nil, "") {
		t.Errorf("expected standalone 'نوفا' without ya or @ to NOT match trigger")
	}
}

func TestBuildContext(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "novabot_ctx_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	chatLogger, _ := storage.NewChatLogger(tempDir)
	memStore, _ := storage.NewMemoryStore(tempDir)

	chatID := "201000000000@s.whatsapp.net"
	senderID := "201000000000@s.whatsapp.net"

	// Pre-populate chat log and memory
	_ = chatLogger.AppendMessage("private", chatID, storage.LogMessage{
		MessageID:  "M1",
		SenderID:   senderID,
		SenderName: "Mina",
		Text:       "رسالة سابقة 1",
		Timestamp:  "2026-08-15T12:00:00Z",
	})
	_ = memStore.AppendMemoryNote(senderID, "Mina", "بيحب الالعاب")

	payload, err := BuildContext(
		chatLogger,
		memStore,
		"private",
		chatID,
		"",
		senderID,
		"Mina",
		"M2",
		"يا نوفا ازيك",
		false,
		nil,
		nil,
		5,
	)
	if err != nil {
		t.Fatalf("BuildContext failed: %v", err)
	}

	if payload.MessageText != "يا نوفا ازيك" {
		t.Errorf("expected raw message text preserved, got %q", payload.MessageText)
	}
	if !payload.TriggerMatched {
		t.Errorf("expected TriggerMatched to be true")
	}
	if len(payload.RecentContext) != 1 || payload.RecentContext[0].Text != "رسالة سابقة 1" {
		t.Errorf("expected 1 recent context message, got %v", payload.RecentContext)
	}
	if payload.UserMemory == nil || *payload.UserMemory == "" {
		t.Errorf("expected user memory to be populated")
	}
}

func TestChatLimiter(t *testing.T) {
	limiter := NewChatLimiter(1) // 1 second cooldown
	chatID := "chat123"

	if !limiter.Allow(chatID) {
		t.Errorf("first request should be allowed")
	}

	if limiter.Allow(chatID) {
		t.Errorf("immediate second request should be blocked by cooldown")
	}

	time.Sleep(1100 * time.Millisecond)

	if !limiter.Allow(chatID) {
		t.Errorf("request after cooldown should be allowed")
	}
}
