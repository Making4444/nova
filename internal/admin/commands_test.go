package admin

import (
	"os"
	"strings"
	"testing"
)

type dummyStats struct{}

func (d *dummyStats) GetTotalChatsCount() int      { return 5 }
func (d *dummyStats) GetMemoryProfilesCount() int { return 3 }
func (d *dummyStats) GetScheduledTasksCount() int { return 2 }
func (d *dummyStats) GetModelName() string        { return "openai/gpt-4o-mini" }

func TestAdminCommands(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "novabot_admin_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	adminPhone := "201202172699"
	state, err := NewState(tempDir, adminPhone)
	if err != nil {
		t.Fatalf("NewState failed: %v", err)
	}

	stats := &dummyStats{}
	chatID := "120363429594564463@g.us"
	adminSender := "201202172699@s.whatsapp.net"
	nonAdminSender := "201011112222@s.whatsapp.net"

	// 1. Non-admin should be rejected
	res := HandleAdminCommand(state, chatID, nonAdminSender, "/shutdown", stats)
	if res.Handled {
		t.Errorf("non-admin should not be able to execute commands")
	}

	// 2. /shutdown test
	res = HandleAdminCommand(state, chatID, adminSender, "/shutdown", stats)
	if !res.Handled || !state.GetShutdown() {
		t.Errorf("expected /shutdown to set shutdown mode")
	}
	if !strings.Contains(res.ReplyText, "تم إغلاق السيرفرات") {
		t.Errorf("unexpected reply text: %s", res.ReplyText)
	}

	// 3. /start test
	res = HandleAdminCommand(state, chatID, adminSender, "/start", stats)
	if !res.Handled || state.GetShutdown() {
		t.Errorf("expected /start to reset shutdown mode")
	}

	// 4. /set test
	res = HandleAdminCommand(state, chatID, adminSender, "/set 500", stats)
	if !res.Handled || state.GetChatLimit(chatID, 0) != 500 {
		t.Errorf("expected chat limit 500, got %d", state.GetChatLimit(chatID, 0))
	}

	res = HandleAdminCommand(state, chatID, adminSender, "/set all", stats)
	if !res.Handled || state.GetChatLimit(chatID, 100) != 0 {
		t.Errorf("expected chat limit 0 (all), got %d", state.GetChatLimit(chatID, 100))
	}

	// 5. /auto test
	res = HandleAdminCommand(state, chatID, adminSender, "/auto on", stats)
	if !res.Handled || !state.IsAutoTriggersEnabled(chatID) {
		t.Errorf("expected auto triggers enabled")
	}

	res = HandleAdminCommand(state, chatID, adminSender, "/auto off", stats)
	if !res.Handled || state.IsAutoTriggersEnabled(chatID) {
		t.Errorf("expected auto triggers disabled")
	}

	// 6. /status test
	res = HandleAdminCommand(state, chatID, adminSender, "/status", stats)
	if !res.Handled || !strings.Contains(res.ReplyText, "Nova Status") {
		t.Errorf("expected status output, got: %s", res.ReplyText)
	}
}
