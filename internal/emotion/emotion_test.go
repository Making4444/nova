package emotion

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDefaultStateAndArabicLabels(t *testing.T) {
	tempDir := t.TempDir()
	eng, err := NewEngine(tempDir)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	chatID := "120363429594564463@g.us"
	st := eng.GetState(chatID)

	if st == nil {
		t.Fatalf("expected non-nil state")
	}
	if st.CurrentMood != Joyful {
		t.Errorf("expected default mood Joyful, got %s", st.CurrentMood)
	}
	if st.EnergyLevel != 7 {
		t.Errorf("expected default energy 7, got %d", st.EnergyLevel)
	}
	if st.UserAffinities == nil {
		t.Errorf("expected initialized UserAffinities map")
	}

	// Check Arabic Labels
	moods := []MoodType{Joyful, Hyped, Annoyed, Empathetic, Calm, Sarcastic}
	for _, m := range moods {
		label := m.ArabicLabel()
		if label == "" {
			t.Errorf("expected non-empty Arabic label for mood %s", m)
		}
	}
}

func TestStatePersistence(t *testing.T) {
	tempDir := t.TempDir()
	eng1, err := NewEngine(tempDir)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	chatID := "201001234567@s.whatsapp.net"
	senderID := "201001234567@s.whatsapp.net"

	// 1. Update mood with sweet message
	st1 := eng1.UpdateMood(chatID, senderID, "Ahmed", "تسلم ايدك يا نوفا يا غالي ربنا يخليك", false, "sweet")
	if st1.CurrentMood != Joyful {
		t.Errorf("expected Joyful, got %s", st1.CurrentMood)
	}
	if st1.EnergyLevel != 8 {
		t.Errorf("expected energy 8, got %d", st1.EnergyLevel)
	}
	if st1.UserAffinities[senderID] != 60 { // Started at 50 + 10 = 60
		t.Errorf("expected affinity 60, got %d", st1.UserAffinities[senderID])
	}

	// Verify file was written to disk
	expectedFile := filepath.Join(tempDir, sanitizeID(chatID)+".json")
	if _, err := os.Stat(expectedFile); os.IsNotExist(err) {
		t.Fatalf("expected state file at %s", expectedFile)
	}

	// 2. Initialize a new Engine instance with the same directory to verify persistence
	eng2, err := NewEngine(tempDir)
	if err != nil {
		t.Fatalf("NewEngine 2 failed: %v", err)
	}

	st2 := eng2.GetState(chatID)
	if st2.CurrentMood != Joyful {
		t.Errorf("persisted mood mismatch: expected Joyful, got %s", st2.CurrentMood)
	}
	if st2.EnergyLevel != 8 {
		t.Errorf("persisted energy mismatch: expected 8, got %d", st2.EnergyLevel)
	}
	if st2.UserAffinities[senderID] != 60 {
		t.Errorf("persisted affinity mismatch: expected 60, got %d", st2.UserAffinities[senderID])
	}
}

func TestMoodShifts(t *testing.T) {
	tempDir := t.TempDir()
	eng, err := NewEngine(tempDir)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	chatID := "group_test_1@g.us"
	userA := "user_a@s.whatsapp.net"
	userB := "user_b@s.whatsapp.net"

	// 1. Sadness / Illness / Trouble -> Empathetic, energy 4
	stSad := eng.UpdateMood(chatID, userA, "Karim", "أنا تعبان جداً ومخنوق وعيان في السرير ادعيلي", false, "sad")
	if stSad.CurrentMood != Empathetic {
		t.Errorf("expected Empathetic mood, got %s", stSad.CurrentMood)
	}
	if stSad.EnergyLevel != 4 {
		t.Errorf("expected energy 4 on sadness, got %d", stSad.EnergyLevel)
	}

	// 2. Fast energetic banter -> Hyped, energy 9
	stHype := eng.UpdateMood(chatID, userB, "Omar", "يلا يا وحش عاااش الماتش ولع نااار هوبا 🔥⚡", false, "hype")
	if stHype.CurrentMood != Hyped {
		t.Errorf("expected Hyped mood, got %s", stHype.CurrentMood)
	}
	if stHype.EnergyLevel != 9 {
		t.Errorf("expected energy 9 on hype, got %d", stHype.EnergyLevel)
	}

	// 3. Rude / Annoying message -> Annoyed / Sarcastic, reduces user affinity
	stRude := eng.UpdateMood(chatID, userA, "Karim", "انت بوت غبي وزفت وتفه ومستفز", false, "rude")
	if stRude.CurrentMood != Annoyed && stRude.CurrentMood != Sarcastic {
		t.Errorf("expected Annoyed or Sarcastic mood, got %s", stRude.CurrentMood)
	}
	if stRude.UserAffinities[userA] != 40 { // 50 - 10 = 40
		t.Errorf("expected userA affinity 40, got %d", stRude.UserAffinities[userA])
	}

	// 4. Repeated rude message switches between Annoyed and Sarcastic
	stRude2 := eng.UpdateMood(chatID, userA, "Karim", "اخرس يا حيوان بطل رغي", false, "rude")
	if stRude2.CurrentMood != Sarcastic && stRude2.CurrentMood != Annoyed {
		t.Errorf("expected Sarcastic or Annoyed mood, got %s", stRude2.CurrentMood)
	}
	if stRude2.UserAffinities[userA] != 30 { // 40 - 10 = 30
		t.Errorf("expected userA affinity 30, got %d", stRude2.UserAffinities[userA])
	}

	// 5. Sweet / Appreciative message -> Joyful, increases user affinity
	stSweet := eng.UpdateMood(chatID, userB, "Omar", "شكرا يا غالي كلك ذوق والله ربنا يسعدك يا احلى نوفا ❤️", false, "sweet")
	if stSweet.CurrentMood != Joyful {
		t.Errorf("expected Joyful mood, got %s", stSweet.CurrentMood)
	}
	if stSweet.UserAffinities[userB] != 60 { // 50 + 10 = 60
		t.Errorf("expected userB affinity 60, got %d", stSweet.UserAffinities[userB])
	}
}

func TestMakerAffinityLocked(t *testing.T) {
	tempDir := t.TempDir()
	eng, err := NewEngine(tempDir)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	chatID := "maker_chat@s.whatsapp.net"
	makerID := "201202172699@s.whatsapp.net"

	// Even if Maker sends rude or banter message, affinity must stay locked at 100
	st := eng.UpdateMood(chatID, makerID, "Makari", "انت يا عم البوت الرخم", true, "rude")
	if st.UserAffinities[makerID] != 100 {
		t.Errorf("expected maker affinity locked at 100, got %d", st.UserAffinities[makerID])
	}

	// Sweet message from maker
	st2 := eng.UpdateMood(chatID, makerID, "Makari", "تسلم يا نوفا يا جامد", true, "sweet")
	if st2.UserAffinities[makerID] != 100 {
		t.Errorf("expected maker affinity locked at 100, got %d", st2.UserAffinities[makerID])
	}
}

func TestAffinityClamping(t *testing.T) {
	tempDir := t.TempDir()
	eng, err := NewEngine(tempDir)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	chatID := "clamp_chat@s.whatsapp.net"
	rudeUser := "toxic_user@s.whatsapp.net"
	sweetUser := "angel_user@s.whatsapp.net"

	// Drop affinity to 0
	for i := 0; i < 10; i++ {
		eng.UpdateMood(chatID, rudeUser, "Toxic", "غبي وزفت وتافه", false, "rude")
	}
	st := eng.GetState(chatID)
	if st.UserAffinities[rudeUser] != 0 {
		t.Errorf("expected min affinity 0, got %d", st.UserAffinities[rudeUser])
	}

	// Boost affinity to 100
	for i := 0; i < 10; i++ {
		eng.UpdateMood(chatID, sweetUser, "Angel", "تسلم يا عسل يا قمر ربنا يحفظك ❤️", false, "sweet")
	}
	st2 := eng.GetState(chatID)
	if st2.UserAffinities[sweetUser] != 100 {
		t.Errorf("expected max affinity 100, got %d", st2.UserAffinities[sweetUser])
	}
}

func TestNightTimeEnergyDrop(t *testing.T) {
	tempDir := t.TempDir()
	eng, err := NewEngine(tempDir)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	// Mock time to 3:30 AM Cairo time
	mockNight := time.Date(2026, 9, 1, 3, 30, 0, 0, time.UTC)
	eng.SetNowFunc(func() time.Time {
		return mockNight
	})

	chatID := "night_chat@s.whatsapp.net"
	st := eng.UpdateMood(chatID, "user1@s.whatsapp.net", "Ali", "الو السلام عليكم", false, "neutral")

	if st.EnergyLevel > 4 {
		t.Errorf("expected energy level <= 4 during late night, got %d", st.EnergyLevel)
	}
}

func TestBuildPromptContext(t *testing.T) {
	tempDir := t.TempDir()
	eng, err := NewEngine(tempDir)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	chatID := "prompt_chat@g.us"
	makerID := "201202172699@s.whatsapp.net"
	regularUser := "user_normal@s.whatsapp.net"

	// 1. Maker prompt context
	eng.UpdateMood(chatID, makerID, "Makari", "حبيبي يا نوفا", true, "sweet")
	promptMaker := eng.BuildPromptContext(chatID, makerID, "Makari", true)

	if !strings.Contains(promptMaker, "الحالة المزاجية والانفعالية") {
		t.Errorf("expected header in prompt, got %s", promptMaker)
	}
	if !strings.Contains(promptMaker, "مكاري") || !strings.Contains(promptMaker, "100/100") {
		t.Errorf("expected Maker mention and 100/100 affinity in prompt, got %s", promptMaker)
	}
	if !strings.Contains(promptMaker, "رايق ومبسوط") {
		t.Errorf("expected joyful mood in prompt, got %s", promptMaker)
	}

	// 2. Regular user prompt context after being rude
	eng.UpdateMood(chatID, regularUser, "Hatem", "انت بوت غبي", false, "rude")
	promptRegular := eng.BuildPromptContext(chatID, regularUser, "Hatem", false)

	if !strings.Contains(promptRegular, "Hatem") {
		t.Errorf("expected user name Hatem in prompt, got %s", promptRegular)
	}
	if !strings.Contains(promptRegular, "درجة الود: 40/100") {
		t.Errorf("expected affinity 40/100 in prompt, got %s", promptRegular)
	}
}

func TestConcurrentAccess(t *testing.T) {
	tempDir := t.TempDir()
	eng, err := NewEngine(tempDir)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	var wg sync.WaitGroup
	chats := []string{"chat1@g.us", "chat2@g.us", "chat3@g.us", "chat4@g.us"}

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			chatID := chats[idx%len(chats)]
			userID := "user@s.whatsapp.net"
			if idx%2 == 0 {
				eng.UpdateMood(chatID, userID, "User", "تسلم ايدك يا نوفا", false, "sweet")
			} else {
				eng.UpdateMood(chatID, userID, "User", "يلا يا وحش عاااش 🔥", false, "hype")
			}
			_ = eng.GetState(chatID)
			_ = eng.BuildPromptContext(chatID, userID, "User", false)
		}(i)
	}

	wg.Wait()
}
