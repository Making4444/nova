package admin

import (
	"context"
	"os"
	"strings"
	"testing"
)

type dummyStats struct{}

func (d *dummyStats) GetTotalChatsCount() int      { return 5 }
func (d *dummyStats) GetMemoryProfilesCount() int { return 3 }
func (d *dummyStats) GetScheduledTasksCount() int { return 2 }
func (d *dummyStats) GetModelName() string        { return "qwen/qwen3-235b-a22b-2507" }

type dummyArchiver struct {
	archivedCalled  bool
	restoreIndex    int
	personaSwitched int
}

func (d *dummyArchiver) SwitchPersona(mode int) (string, error) {
	d.personaSwitched = mode
	return "ok", nil
}

func (d *dummyArchiver) ArchiveChatSession(ctx context.Context, chatType, chatID string) (string, int, error) {
	d.archivedCalled = true
	return "data/chats/groups/summaries/test_summary_1.md", 1, nil
}

func (d *dummyArchiver) RestoreArchivedChatSession(chatType, chatID string, archiveIndex int) error {
	d.restoreIndex = archiveIndex
	return nil
}

func (d *dummyArchiver) ListChatArchives(chatType, chatID string) ([]string, error) {
	return []string{"رقم 1 — 50 رسالة (2026-08-31 18:00)"}, nil
}

func TestCleanInvisibleMarks(t *testing.T) {
	inputWithMarks := "\u200e/status\u200f"
	cleaned := CleanInvisibleMarks(inputWithMarks)
	if cleaned != "/status" {
		t.Errorf("CleanInvisibleMarks(%q) = %q, expected %q", inputWithMarks, cleaned, "/status")
	}
}

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
	archiver := &dummyArchiver{}
	chatID := "120363429594564463@g.us"
	adminSender := "201202172699@s.whatsapp.net"
	nonAdminSender := "201099999999@s.whatsapp.net"

	// 0. Non-admin security rejection check
	resNonAdmin := HandleAdminCommand(state, chatID, nonAdminSender, "Attacker", false, "/shutdown", stats, archiver)
	if !resNonAdmin.Handled || !strings.Contains(resNonAdmin.ReplyText, "مخصص فقط لمشرف") {
		t.Errorf("expected non-admin to be blocked, got: %s", resNonAdmin.ReplyText)
	}
	if state.GetShutdown() {
		t.Errorf("shutdown should not have been triggered by non-admin")
	}

	// 1. /shutdown test by admin
	res := HandleAdminCommand(state, chatID, adminSender, "Making", true, "\u200e/shutdown", stats, archiver)
	if !res.Handled || !state.GetShutdown() {
		t.Errorf("expected /shutdown to set shutdown mode")
	}
	if !strings.Contains(res.ReplyText, "تم إغلاق السيرفرات") {
		t.Errorf("unexpected reply text: %s", res.ReplyText)
	}

	// 2. /start test by admin
	res = HandleAdminCommand(state, chatID, adminSender, "Making", true, "/start", stats, archiver)
	if !res.Handled || state.GetShutdown() {
		t.Errorf("expected /start to reset shutdown mode")
	}

	// 3. /set test
	res = HandleAdminCommand(state, chatID, adminSender, "Making", true, "/set 500", stats, archiver)
	if !res.Handled || state.GetChatLimit(chatID, 0) != 500 {
		t.Errorf("expected chat limit 500, got %d", state.GetChatLimit(chatID, 0))
	}

	res = HandleAdminCommand(state, chatID, adminSender, "Making", true, "/set all", stats, archiver)
	if !res.Handled || state.GetChatLimit(chatID, 100) != 0 {
		t.Errorf("expected chat limit 0 (all), got %d", state.GetChatLimit(chatID, 100))
	}

	// 4. /auto test
	res = HandleAdminCommand(state, chatID, adminSender, "Making", true, "/auto on", stats, archiver)
	if !res.Handled || !state.IsAutoTriggersEnabled(chatID) {
		t.Errorf("expected auto triggers enabled")
	}

	res = HandleAdminCommand(state, chatID, adminSender, "Making", true, "/auto off", stats, archiver)
	if !res.Handled || state.IsAutoTriggersEnabled(chatID) {
		t.Errorf("expected auto triggers disabled")
	}

	// 5. /archive test
	res = HandleAdminCommand(state, chatID, adminSender, "Making", true, "/archive", stats, archiver)
	if !res.Handled || !archiver.archivedCalled || !strings.Contains(res.ReplyText, "تمت أرشفة وتلخيص") {
		t.Errorf("expected archive command success, got: %s", res.ReplyText)
	}

	// 6. /archive list test
	res = HandleAdminCommand(state, chatID, adminSender, "Making", true, "/archive list", stats, archiver)
	if !res.Handled || !strings.Contains(res.ReplyText, "رقم 1") {
		t.Errorf("expected archive list output, got: %s", res.ReplyText)
	}

	// 7. /restore test
	res = HandleAdminCommand(state, chatID, adminSender, "Making", true, "/restore 1", stats, archiver)
	if !res.Handled || archiver.restoreIndex != 1 || !strings.Contains(res.ReplyText, "استرجاع المحادثة") {
		t.Errorf("expected restore success, got: %s", res.ReplyText)
	}

	// 8. /status test (with invisible LTR mark \u200e)
	res = HandleAdminCommand(state, chatID, adminSender, "Making", true, "\u200e/status", stats, archiver)
	if !res.Handled || !strings.Contains(res.ReplyText, "Nova Status") {
		t.Errorf("expected status output, got: %s", res.ReplyText)
	}

	// 9. /persona info test
	res = HandleAdminCommand(state, chatID, adminSender, "Making", true, "/persona", stats, archiver)
	if !res.Handled || !strings.Contains(res.ReplyText, "أنماط شخصية نوفا") {
		t.Errorf("expected persona help text, got: %s", res.ReplyText)
	}

	// 10. /persona 2 switch test (Charming/Female)
	res = HandleAdminCommand(state, chatID, adminSender, "Making", true, "/persona 2", stats, archiver)
	if !res.Handled || state.GetPersona() != 2 || archiver.personaSwitched != 2 || !strings.Contains(res.ReplyText, "النمط 2") {
		t.Errorf("expected switch to persona 2, got: %s", res.ReplyText)
	}

	// 11. /persona 1 switch back test (Bro/Default)
	res = HandleAdminCommand(state, chatID, adminSender, "Making", true, "/persona 1", stats, archiver)
	if !res.Handled || state.GetPersona() != 1 || archiver.personaSwitched != 1 || !strings.Contains(res.ReplyText, "النمط 1") {
		t.Errorf("expected switch to persona 1, got: %s", res.ReplyText)
	}
}

