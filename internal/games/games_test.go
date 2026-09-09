package games

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNormalizeArabic(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"أحمد", "احمد"},
		{"إبراهيم", "ابراهيم"},
		{"آلاء", "الاء"},
		{"مدرسة", "مدرسه"},
		{"علي", "علي"},
		{"عَليّ", "علي"},
		{"اللمبيييييي", "اللمبي"},
		{"فيلم 123", "فيلم 123"},
		{"فيلم ١٢٣", "فيلم 123"},
	}

	for _, c := range cases {
		res := NormalizeArabic(c.input)
		if res != c.expected {
			t.Errorf("NormalizeArabic(%q) = %q; want %q", c.input, res, c.expected)
		}
	}
}

func TestCheckAnswer(t *testing.T) {
	accepted := []string{"الناظر", "فيلم الناظر"}

	positives := []string{
		"الناظر",
		"الناظر صلاح الدين",
		"فيلم الناظر",
		"النااااظر",
		"هو فيلم الناظر",
		"الناطر", // 1 typo fuzzy match
	}

	for _, p := range positives {
		if !CheckAnswer(p, accepted) {
			t.Errorf("CheckAnswer(%q, %v) expected true, got false", p, accepted)
		}
	}

	negatives := []string{
		"اللمبي",
		"مفيش فيلم",
		"أبو علي",
	}

	for _, n := range negatives {
		if CheckAnswer(n, accepted) {
			t.Errorf("CheckAnswer(%q, %v) expected false, got true", n, accepted)
		}
	}
}

func TestLeaderboardStore(t *testing.T) {
	tempDir := filepath.Join(os.TempDir(), "test_nova_games")
	defer os.RemoveAll(tempDir)

	store := NewLeaderboardStore(tempDir)
	chatID := "120363000000000000@g.us"

	// Initially empty
	emptyMsg := store.FormatTopMessage(chatID, 5)
	if !strings.Contains(emptyMsg, "فاضية") {
		t.Errorf("expected empty message, got: %s", emptyMsg)
	}

	// Add scores
	_, err := store.AddScore(chatID, "user1", "أحمد", 5)
	if err != nil {
		t.Fatalf("AddScore failed: %v", err)
	}

	_, err = store.AddScore(chatID, "user2", "سارة", 8)
	if err != nil {
		t.Fatalf("AddScore failed: %v", err)
	}

	top := store.GetTop(chatID, 5)
	if len(top) != 2 {
		t.Fatalf("expected 2 players, got %d", len(top))
	}
	if top[0].UserName != "سارة" || top[0].Points != 8 {
		t.Errorf("expected top player to be سارة with 8 points, got %s with %d", top[0].UserName, top[0].Points)
	}

	msg := store.FormatTopMessage(chatID, 5)
	if !strings.Contains(msg, "سارة") || !strings.Contains(msg, "أحمد") {
		t.Errorf("formatted message missing players: %s", msg)
	}
}

func TestGameLifecycle(t *testing.T) {
	tempDir := filepath.Join(os.TempDir(), "test_nova_lifecycle")
	defer os.RemoveAll(tempDir)

	var mu sync.Mutex
	var sentMessages []string

	broadcaster := func(chatID, text, replyToID string) error {
		mu.Lock()
		sentMessages = append(sentMessages, text)
		mu.Unlock()
		return nil
	}

	engine := NewEngine(tempDir, broadcaster)
	engine.SetDurations(100*time.Millisecond, 50*time.Millisecond) // fast for tests
	chatID := "test_chat_123"

	// 1. Start Game
	_, err := engine.StartGame(chatID, "movie")
	if err != nil {
		t.Fatalf("StartGame failed: %v", err)
	}

	if !engine.HasActiveGame(chatID) {
		t.Errorf("expected HasActiveGame to be true")
	}

	// 2. Try starting another game concurrently -> should notify already active
	msg, _ := engine.StartGame(chatID, "movie")
	if !strings.Contains(msg, "شغالة بالفعل") {
		t.Errorf("expected already active message, got: %s", msg)
	}

	// 3. Stop Game
	stopMsg, stopped := engine.StopGame(chatID)
	if !stopped || !strings.Contains(stopMsg, "تم إيقاف") {
		t.Errorf("expected game to be stopped, got: %s", stopMsg)
	}

	if engine.HasActiveGame(chatID) {
		t.Errorf("expected HasActiveGame to be false after stop")
	}
}
