package dashboard

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"novabot/internal/admin"
	"novabot/internal/storage"
)

func setupTestEnvironment(t *testing.T, password string) (*Server, string, func()) {
	t.Helper()
	tempDir, err := os.MkdirTemp("", "nova_dashboard_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	chatsDir := filepath.Join(tempDir, "chats")
	usersDir := filepath.Join(tempDir, "users")
	_ = os.MkdirAll(filepath.Join(chatsDir, "groups"), 0755)
	_ = os.MkdirAll(filepath.Join(chatsDir, "private"), 0755)
	_ = os.MkdirAll(usersDir, 0755)

	// Create sample chat log
	sampleGroupLog := filepath.Join(chatsDir, "groups", "12036304_g.us.jsonl")
	msg1 := storage.LogMessage{
		MessageID:  "MSG_1",
		SenderID:   "201202172699",
		SenderName: "Mokary",
		Text:       "ازيك يا نوفا عامل ايه؟",
		IsNova:     false,
		Timestamp:  "2026-09-01T12:00:00Z",
	}
	msg2 := storage.LogMessage{
		MessageID:   "MSG_2",
		SenderID:    "nova_bot",
		SenderName:  "Nova",
		Text:        "يا هلا بيك يا غالي، كلو تمام وفي الروقان!",
		IsNova:      true,
		Timestamp:   "2026-09-01T12:00:05Z",
		IsReply:     true,
		RepliedToID: "MSG_1",
	}
	d1, _ := json.Marshal(msg1)
	d2, _ := json.Marshal(msg2)
	_ = os.WriteFile(sampleGroupLog, []byte(string(d1)+"\n"+string(d2)+"\n"), 0644)

	// Create sample user memory
	sampleUserFile := filepath.Join(usersDir, "201202172699.md")
	userMemoryContent := "# 👤 بطاقة المستخدم: Making (مكاري)\n**آخر تحديث:** 2026-09-01 12:00:00\n\n### [2026-09-01 12:00:00]\n- مبرمج ومطور السيرفر ويحب البرمجة بالـ Go.\n"
	_ = os.WriteFile(sampleUserFile, []byte(userMemoryContent), 0644)

	// Create sample tasks.json
	tasksFile := filepath.Join(tempDir, "tasks.json")
	sampleTasks := `[{"id":"TASK_1","chat_id":"12036304_g.us","reason":"فحص طبي","executed":false}]`
	_ = os.WriteFile(tasksFile, []byte(sampleTasks), 0644)

	// Initialize components
	adminState, _ := admin.NewState(tempDir, "201202172699")
	chatLogger, _ := storage.NewChatLogger(chatsDir)
	memStore, _ := storage.NewMemoryStore(usersDir)
	logHub := NewLogHub(100)
	metrics := NewMetricsTracker()

	metrics.RecordEvent(false, 150, "witty", "🔥", "Mokary", "12036304_g.us", "ازيك يا نوفا")
	metrics.RecordEvent(true, 250, "joking", "😂", "Nova", "12036304_g.us", "يا هلا بيك")

	srv := NewServer(ServerConfig{
		Port:        ":8080",
		Password:    password,
		DataDir:     tempDir,
		AdminState:  adminState,
		ChatLogger:  chatLogger,
		MemoryStore: memStore,
		LogHub:      logHub,
		Metrics:     metrics,
		Models: map[string]string{
			"chat":    "qwen/qwen3-235b-a22b-2507",
			"math":    "z-ai/glm-5.2",
			"vision":  "openai/gpt-5.6-luna",
			"whisper": "whisper-large-v3",
		},
	})

	cleanup := func() {
		_ = os.RemoveAll(tempDir)
	}

	return srv, tempDir, cleanup
}

func TestIndexEndpoint(t *testing.T) {
	srv, _, cleanup := setupTestEnvironment(t, "")
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	srv.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "لوحة تحكم نوفا") || !strings.Contains(body, "Nova Admin Dashboard") {
		t.Errorf("expected HTML body to contain dashboard title, got: %s", body[:200])
	}
}

func TestBasicAuth(t *testing.T) {
	srv, _, cleanup := setupTestEnvironment(t, "secret123")
	defer cleanup()

	// 1. Request without auth -> 401
	req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized, got %d", w.Code)
	}

	// 2. Request with invalid password -> 401
	req = httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	req.SetBasicAuth("admin", "wrongpass")
	w = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized for wrong pass, got %d", w.Code)
	}

	// 3. Request with valid Basic Auth -> 200
	req = httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	req.SetBasicAuth("admin", "secret123")
	w = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK with valid credentials, got %d", w.Code)
	}

	// 4. Request with Bearer token header -> 200
	req = httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	req.Header.Set("Authorization", "Bearer secret123")
	w = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK with Bearer token, got %d", w.Code)
	}
}

func TestStatsEndpoint(t *testing.T) {
	srv, _, cleanup := setupTestEnvironment(t, "")
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", w.Code)
	}

	var data map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	if data["active_chats"].(float64) < 1 {
		t.Errorf("expected at least 1 active chat, got %v", data["active_chats"])
	}

	if data["memory_count"].(float64) < 1 {
		t.Errorf("expected at least 1 memory profile, got %v", data["memory_count"])
	}

	if data["scheduled_tasks"].(float64) != 1 {
		t.Errorf("expected 1 scheduled task, got %v", data["scheduled_tasks"])
	}

	if data["current_emotion"] == "" {
		t.Errorf("expected current_emotion to be present, got empty")
	}

	if models, ok := data["models"].(map[string]interface{}); !ok || models["chat"] == "" {
		t.Errorf("expected models.chat to be present, got %v", models)
	}
}

func TestChatsEndpoint(t *testing.T) {
	srv, _, cleanup := setupTestEnvironment(t, "")
	defer cleanup()

	// 1. List all chats
	req := httptest.NewRequest(http.MethodGet, "/api/chats", nil)
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", w.Code)
	}

	var listData struct {
		Total int           `json:"total"`
		Chats []ChatSummary `json:"chats"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &listData); err != nil {
		t.Fatalf("failed to decode chats list: %v", err)
	}

	if listData.Total != 1 || len(listData.Chats) != 1 {
		t.Fatalf("expected 1 chat in list, got total=%d, len=%d", listData.Total, len(listData.Chats))
	}
	if listData.Chats[0].ID != "12036304_g.us" {
		t.Errorf("expected chat id 12036304_g.us, got %s", listData.Chats[0].ID)
	}

	// 2. Query messages of specific chat
	req = httptest.NewRequest(http.MethodGet, "/api/chats?id=12036304_g.us&type=group", nil)
	w = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for chat messages, got %d", w.Code)
	}

	var detailData struct {
		ChatID       string               `json:"chat_id"`
		MessageCount int                  `json:"message_count"`
		Messages     []storage.LogMessage `json:"messages"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &detailData); err != nil {
		t.Fatalf("failed to decode chat detail: %v", err)
	}

	if detailData.MessageCount != 2 {
		t.Errorf("expected 2 messages, got %d", detailData.MessageCount)
	}
	if len(detailData.Messages) != 2 || detailData.Messages[0].Text != "ازيك يا نوفا عامل ايه؟" {
		t.Errorf("unexpected messages content: %+v", detailData.Messages)
	}
}

func TestEmotionsEndpoint(t *testing.T) {
	srv, _, cleanup := setupTestEnvironment(t, "")
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/emotions", nil)
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", w.Code)
	}

	var data struct {
		CurrentMood    string             `json:"current_mood"`
		MoodBreakdown  map[string]int     `json:"mood_breakdown"`
		RecentEmotions []EmotionEvent     `json:"recent_emotions"`
		UserAffinities []UserAffinityCard `json:"user_affinities"`
		TotalUsers     int                `json:"total_users"`
	}

	if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil {
		t.Fatalf("failed to decode emotions response: %v", err)
	}

	if data.CurrentMood == "" {
		t.Errorf("expected non-empty current mood")
	}

	if data.TotalUsers != 1 || len(data.UserAffinities) != 1 {
		t.Errorf("expected 1 user affinity card, got %d", data.TotalUsers)
	}

	if len(data.RecentEmotions) < 1 {
		t.Errorf("expected at least 1 recent emotion event, got %d", len(data.RecentEmotions))
	}
}

func TestSettingsEndpoint(t *testing.T) {
	srv, _, cleanup := setupTestEnvironment(t, "")
	defer cleanup()

	// 1. GET settings
	req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", w.Code)
	}

	// 2. POST update settings
	shutdownVal := true
	limitVal := 50
	autoVal := true
	adminNum := "201202172699"

	payload := map[string]interface{}{
		"is_shutdown":   shutdownVal,
		"chat_id":       "12036304_g.us",
		"chat_limit":    limitVal,
		"auto_triggers": autoVal,
		"admin_number":  adminNum,
	}
	bodyBytes, _ := json.Marshal(payload)

	req = httptest.NewRequest(http.MethodPost, "/api/settings", bytes.NewReader(bodyBytes))
	w = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on settings POST, got %d", w.Code)
	}

	// Verify settings updated in adminState
	if !srv.adminState.GetShutdown() {
		t.Errorf("expected is_shutdown to be true")
	}
	if srv.adminState.GetChatLimit("12036304_g.us", 0) != 50 {
		t.Errorf("expected chat limit 50, got %d", srv.adminState.GetChatLimit("12036304_g.us", 0))
	}
	if !srv.adminState.IsAutoTriggersEnabled("12036304_g.us") {
		t.Errorf("expected auto triggers to be true")
	}
}

func TestLogsEndpoint(t *testing.T) {
	srv, _, cleanup := setupTestEnvironment(t, "")
	defer cleanup()

	srv.logHub.AddLog("INFO", "Test log message 1")
	srv.logHub.AddLog("ERROR", "Test log message 2")

	req := httptest.NewRequest(http.MethodGet, "/api/logs?limit=10", nil)
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on logs GET, got %d", w.Code)
	}

	var data struct {
		Logs  []LogEntry `json:"logs"`
		Count int        `json:"count"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil {
		t.Fatalf("failed to decode logs JSON: %v", err)
	}

	if data.Count < 2 {
		t.Errorf("expected at least 2 logs, got %d", data.Count)
	}
}

func TestQuickActions(t *testing.T) {
	srv, tempDir, cleanup := setupTestEnvironment(t, "")
	defer cleanup()

	// 1. Toggle Shutdown
	req := httptest.NewRequest(http.MethodPost, "/api/actions/shutdown", nil)
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on action shutdown, got %d", w.Code)
	}

	// 2. Restart
	req = httptest.NewRequest(http.MethodPost, "/api/actions/restart", nil)
	w = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on action restart, got %d", w.Code)
	}

	// 3. Auto Triggers Action
	autoBody := []byte(`{"chat_id":"12036304_g.us","enabled":true}`)
	req = httptest.NewRequest(http.MethodPost, "/api/actions/auto-triggers", bytes.NewReader(autoBody))
	w = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on action auto-triggers, got %d", w.Code)
	}

	// 4. Clear User Memory
	userFile := filepath.Join(tempDir, "users", "201202172699.md")
	if _, err := os.Stat(userFile); os.IsNotExist(err) {
		t.Fatalf("expected user memory file to exist before delete")
	}

	clearBody := []byte(`{"user_id":"201202172699"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/actions/clear-memory", bytes.NewReader(clearBody))
	w = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on clear-memory, got %d", w.Code)
	}

	if _, err := os.Stat(userFile); !os.IsNotExist(err) {
		t.Errorf("expected user memory file to be deleted")
	}
}
