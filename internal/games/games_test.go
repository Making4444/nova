package games

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
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

func TestBankManagerLoads500Questions(t *testing.T) {
	// Point to actual data directory in repo
	bm := NewBankManager("../../data")
	total := bm.TotalCount()

	if total < 500 {
		t.Errorf("expected at least 500 questions, got %d", total)
	}

	// Verify key categories exist
	categories := []Category{
		CategoryChristian,
		CategoryQuote,
		CategoryMovie,
		CategoryFootball,
		CategoryTrivia,
		CategoryRiddle,
		CategoryProverb,
		CategoryScience,
		CategoryHistory,
		CategoryCartoon,
	}

	for _, cat := range categories {
		count := bm.CategoryCount(cat)
		if count < 50 {
			t.Errorf("expected category %s to have at least 50 questions, got %d", cat, count)
		}
	}
}

func TestHistoryTrackerNoRepeat(t *testing.T) {
	tempDir := filepath.Join(os.TempDir(), "test_nova_history")
	defer os.RemoveAll(tempDir)

	ht := NewHistoryTracker(tempDir)
	chatID := "chat_no_repeat@g.us"

	// Create a test pool of 10 questions
	var pool []Question
	for i := 1; i <= 10; i++ {
		pool = append(pool, Question{
			ID:       string(rune('A' - 1 + i)),
			Category: CategoryChristian,
			Prompt:   "سؤال تجريبي",
		})
	}

	// Pick 4 questions
	q1 := ht.PickQuestions(chatID, CategoryChristian, 4, pool)
	if len(q1) != 4 {
		t.Fatalf("expected 4 questions, got %d", len(q1))
	}

	// Pick next 4 questions -> MUST NOT overlap with q1
	q2 := ht.PickQuestions(chatID, CategoryChristian, 4, pool)
	if len(q2) != 4 {
		t.Fatalf("expected 4 questions, got %d", len(q2))
	}

	q1Map := make(map[string]bool)
	for _, q := range q1 {
		q1Map[q.ID] = true
	}

	for _, q := range q2 {
		if q1Map[q.ID] {
			t.Errorf("duplicate question %s found in next batch before cycle completed", q.ID)
		}
	}

	// Next batch: remaining is only 2, so requesting 4 triggers a clean cycle reset
	q3 := ht.PickQuestions(chatID, CategoryChristian, 4, pool)
	if len(q3) != 4 {
		t.Fatalf("expected 4 questions after cycle reset, got %d", len(q3))
	}
}

func TestConfigStore(t *testing.T) {
	tempDir := filepath.Join(os.TempDir(), "test_nova_cfg")
	defer os.RemoveAll(tempDir)

	store := NewConfigStore(tempDir)
	chatID := "chat_config@g.us"

	// Defaults
	cfg := store.GetConfig(chatID)
	if cfg.RoundCount != 5 || cfg.QuestionTimeoutSec != 45 {
		t.Errorf("expected default 5 rounds, 45 sec, got %d rounds, %d sec", cfg.RoundCount, cfg.QuestionTimeoutSec)
	}

	// Custom valid configuration
	if err := store.SetRounds(chatID, 10); err != nil {
		t.Errorf("unexpected error setting rounds: %v", err)
	}
	if err := store.SetTimeout(chatID, 30); err != nil {
		t.Errorf("unexpected error setting timeout: %v", err)
	}

	cfg = store.GetConfig(chatID)
	if cfg.RoundCount != 10 || cfg.QuestionTimeoutSec != 30 {
		t.Errorf("expected 10 rounds and 30s timeout, got %d and %d", cfg.RoundCount, cfg.QuestionTimeoutSec)
	}

	// Invalid parameters validation
	if err := store.SetRounds(chatID, 100); err == nil {
		t.Errorf("expected error for rounds > 30")
	}
	if err := store.SetTimeout(chatID, 5); err == nil {
		t.Errorf("expected error for timeout < 10")
	}

	// Reset
	_ = store.ResetConfig(chatID)
	cfg = store.GetConfig(chatID)
	if cfg.RoundCount != 5 || cfg.QuestionTimeoutSec != 45 {
		t.Errorf("expected reset to 5 rounds, got %d", cfg.RoundCount)
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
	_, err := store.AddScore(chatID, "user1", "مينا", 5)
	if err != nil {
		t.Fatalf("AddScore failed: %v", err)
	}

	_, err = store.AddScore(chatID, "user2", "مريم", 8)
	if err != nil {
		t.Fatalf("AddScore failed: %v", err)
	}

	top := store.GetTop(chatID, 5)
	if len(top) != 2 {
		t.Fatalf("expected 2 players, got %d", len(top))
	}
	if top[0].UserName != "مريم" || top[0].Points != 8 {
		t.Errorf("expected top player to be مريم with 8 points, got %s with %d", top[0].UserName, top[0].Points)
	}

	msg := store.FormatTopMessage(chatID, 5)
	if !strings.Contains(msg, "مريم") || !strings.Contains(msg, "مينا") {
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
	chatID := "test_chat_123"

	// Set custom config for fast tests
	_ = engine.GetConfigStore().SetRounds(chatID, 3)
	_ = engine.GetConfigStore().SetTimeout(chatID, 15)

	// 1. Start Christian Game
	_, err := engine.StartGame(chatID, "christian")
	if err != nil {
		t.Fatalf("StartGame failed: %v", err)
	}

	if !engine.HasActiveGame(chatID) {
		t.Errorf("expected HasActiveGame to be true")
	}

	// 2. Try starting another game concurrently -> should notify already active
	msg, _ := engine.StartGame(chatID, "christian")
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
