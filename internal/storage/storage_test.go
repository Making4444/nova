package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestChatLogger(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "novabot_storage_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	logger, err := NewChatLogger(tempDir)
	if err != nil {
		t.Fatalf("NewChatLogger failed: %v", err)
	}

	privChatID := "201001234567@s.whatsapp.net"
	grpChatID := "120363012345678901@g.us"

	// 1. Append messages to private chat
	msg1 := LogMessage{
		MessageID:  "MSG1",
		SenderID:   privChatID,
		SenderName: "Ahmed",
		Text:       "ازيك يا نوفا عاملة ايه؟",
		IsNova:     false,
	}
	if err := logger.AppendMessage("private", privChatID, msg1); err != nil {
		t.Fatalf("AppendMessage failed: %v", err)
	}

	msg2 := LogMessage{
		MessageID:  "MSG2",
		SenderID:   "bot@s.whatsapp.net",
		SenderName: "Makari", // Should be overwritten to "Nova"
		Text:       "الحمد لله يا غالي تمام",
		IsNova:     true,
	}
	if err := logger.AppendMessage("private", privChatID, msg2); err != nil {
		t.Fatalf("AppendMessage failed: %v", err)
	}

	// 2. Append message to group chat
	grpMsg := LogMessage{
		MessageID:  "GRP1",
		SenderID:   "201111111111@s.whatsapp.net",
		SenderName: "Sara",
		Text:       "مساء الخير للجميع",
		IsNova:     false,
	}
	if err := logger.AppendMessage("group", grpChatID, grpMsg); err != nil {
		t.Fatalf("AppendMessage group failed: %v", err)
	}

	// 3. Verify separation
	privRecent, err := logger.GetRecentMessages("private", privChatID, 10)
	if err != nil {
		t.Fatalf("GetRecentMessages private failed: %v", err)
	}
	if len(privRecent) != 2 {
		t.Fatalf("expected 2 private messages, got %d", len(privRecent))
	}
	if privRecent[1].SenderName != "Nova" {
		t.Errorf("expected Nova sender name, got %s", privRecent[1].SenderName)
	}

	grpRecent, err := logger.GetRecentMessages("group", grpChatID, 10)
	if err != nil {
		t.Fatalf("GetRecentMessages group failed: %v", err)
	}
	if len(grpRecent) != 1 {
		t.Fatalf("expected 1 group message, got %d", len(grpRecent))
	}

	// 4. Verify limit <= 0 returns all messages
	allPriv, err := logger.GetRecentMessages("private", privChatID, 0)
	if err != nil {
		t.Fatalf("GetRecentMessages with 0 limit failed: %v", err)
	}
	if len(allPriv) != 2 {
		t.Fatalf("expected all 2 messages with 0 limit, got %d", len(allPriv))
	}

	// Check file paths exist on disk
	privFile := filepath.Join(tempDir, "private", sanitizeID(privChatID)+".jsonl")
	if _, err := os.Stat(privFile); os.IsNotExist(err) {
		t.Errorf("expected private chat file at %s", privFile)
	}
}

func TestMemoryStore(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "novabot_memory_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	memStore, err := NewMemoryStore(tempDir)
	if err != nil {
		t.Fatalf("NewMemoryStore failed: %v", err)
	}

	userID := "201001234567@s.whatsapp.net"

	// Initial check
	mem, err := memStore.GetUserMemory(userID)
	if err != nil {
		t.Fatalf("GetUserMemory failed: %v", err)
	}
	if mem != "" {
		t.Errorf("expected empty initial memory, got %s", mem)
	}

	// Append note
	if err := memStore.AppendMemoryNote(userID, "Ahmed", "بيحب القهوة السادة وشغال مبرمج"); err != nil {
		t.Fatalf("AppendMemoryNote failed: %v", err)
	}

	memAfter, err := memStore.GetUserMemory(userID)
	if err != nil {
		t.Fatalf("GetUserMemory after failed: %v", err)
	}
	if !strings.Contains(memAfter, "بيحب القهوة السادة") {
		t.Errorf("expected memory to contain note, got: %s", memAfter)
	}

	// Update user profile test
	if err := memStore.UpdateUserProfile(userID, "Ahmed", "- الاهتمامات: برمجة وذكاء اصطناعي\n- الطبع: هادي ورايق"); err != nil {
		t.Fatalf("UpdateUserProfile failed: %v", err)
	}
	profileMem, _ := memStore.GetUserMemory(userID)
	if !strings.Contains(profileMem, "ذكاء اصطناعي") {
		t.Errorf("expected profile to contain new info, got: %s", profileMem)
	}
}

func TestChatLoggerArchiving(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "novabot_archive_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	logger, err := NewChatLogger(tempDir)
	if err != nil {
		t.Fatalf("NewChatLogger failed: %v", err)
	}

	chatID := "120363429594564463@g.us"

	// 1. Populate chat
	_ = logger.AppendMessage("group", chatID, LogMessage{MessageID: "M1", Text: "رسالة 1"})
	_ = logger.AppendMessage("group", chatID, LogMessage{MessageID: "M2", Text: "رسالة 2"})

	transcript, msgs, err := logger.GetAllMessages("group", chatID)
	if err != nil || len(msgs) != 2 || !strings.Contains(transcript, "رسالة 1") {
		t.Fatalf("GetAllMessages failed: %v, msgs=%d", err, len(msgs))
	}

	// 2. Archive chat
	idx, archPath, err := logger.ArchiveCurrentChat("group", chatID)
	if err != nil || idx != 1 {
		t.Fatalf("ArchiveCurrentChat failed: %v, idx=%d", err, idx)
	}
	if _, err := os.Stat(archPath); os.IsNotExist(err) {
		t.Fatalf("archived file not found at %s", archPath)
	}

	// 3. Active chat should now be empty
	activeMsgs, _ := logger.GetRecentMessages("group", chatID, 0)
	if len(activeMsgs) != 0 {
		t.Fatalf("expected empty active chat after archive, got %d", len(activeMsgs))
	}

	// 4. Save Summary
	if err := logger.SaveSummary("group", chatID, idx, "## ملخص المحادثة الأولى\n- اتكلموا في البرمجة."); err != nil {
		t.Fatalf("SaveSummary failed: %v", err)
	}

	// 5. List Archives
	list, err := logger.ListArchivedChats("group", chatID)
	if err != nil || len(list) != 1 || list[0].Index != 1 {
		t.Fatalf("ListArchivedChats failed: %v, len=%d", err, len(list))
	}

	// 6. Restore Archived Chat
	if err := logger.RestoreArchivedChat("group", chatID, 1); err != nil {
		t.Fatalf("RestoreArchivedChat failed: %v", err)
	}
	restoredMsgs, _ := logger.GetRecentMessages("group", chatID, 0)
	if len(restoredMsgs) != 2 {
		t.Fatalf("expected 2 restored messages, got %d", len(restoredMsgs))
	}
}

