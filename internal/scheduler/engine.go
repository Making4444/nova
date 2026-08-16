package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"novabot/internal/admin"
	"novabot/internal/ai"
	"novabot/internal/storage"
	"novabot/internal/trigger"
)

// ScheduledTask represents an autonomous follow-up requested by the AI model.
type ScheduledTask struct {
	ID         string    `json:"id"`
	ChatID     string    `json:"chat_id"`
	ChatType   string    `json:"chat_type"`
	TargetUser string    `json:"target_user"`
	Reason     string    `json:"reason"`
	DueAt      time.Time `json:"due_at"`
	CreatedAt  time.Time `json:"created_at"`
	Executed   bool      `json:"executed"`
}

// AIResponder interface for generating proactive AI responses.
type AIResponder interface {
	GenerateResponse(ctx context.Context, payload *trigger.RequestPayload) (*ai.ResponsePayload, error)
}

// MessageDispatcher interface for sending messages to WhatsApp.
type MessageDispatcher interface {
	SendMessage(ctx context.Context, chatID string, text string, replyToID string) error
}

// Engine manages scheduled tasks and server automated triggers.
type Engine struct {
	tasksFile          string
	tasks              []ScheduledTask
	state              *admin.State
	chatLogger         *storage.ChatLogger
	memStore           *storage.MemoryStore
	dispatcher         MessageDispatcher
	aiClient           AIResponder
	lastAutoTrigger    map[string]time.Time // chatID -> last trigger time
	recentRushCounts   map[string][]time.Time
	mu                 sync.Mutex
	stopChan           chan struct{}
}

// NewEngine initializes the scheduler and auto triggers engine.
func NewEngine(
	dataDir string,
	state *admin.State,
	chatLogger *storage.ChatLogger,
	memStore *storage.MemoryStore,
	dispatcher MessageDispatcher,
	aiClient AIResponder,
) *Engine {
	tasksPath := filepath.Join(dataDir, "tasks.json")
	eng := &Engine{
		tasksFile:        tasksPath,
		tasks:            make([]ScheduledTask, 0),
		state:            state,
		chatLogger:       chatLogger,
		memStore:         memStore,
		dispatcher:       dispatcher,
		aiClient:         aiClient,
		lastAutoTrigger:  make(map[string]time.Time),
		recentRushCounts: make(map[string][]time.Time),
		stopChan:         make(chan struct{}),
	}
	eng.loadTasks()
	return eng
}

func (e *Engine) loadTasks() {
	if data, err := os.ReadFile(e.tasksFile); err == nil {
		var list []ScheduledTask
		if err := json.Unmarshal(data, &list); err == nil {
			e.tasks = list
		}
	}
}

func (e *Engine) saveTasks() {
	_ = os.MkdirAll(filepath.Dir(e.tasksFile), 0755)
	data, _ := json.MarshalIndent(e.tasks, "", "  ")
	_ = os.WriteFile(e.tasksFile, data, 0644)
}

// ParseDurationFlexible parses durations like "2h", "30m", "1d", "ساعتين", "نص ساعة".
func ParseDurationFlexible(s string) time.Duration {
	s = strings.TrimSpace(strings.ToLower(s))
	if strings.Contains(s, "نص ساعة") || strings.Contains(s, "نصف ساعة") {
		return 30 * time.Minute
	}
	if strings.Contains(s, "ساعتين") {
		return 2 * time.Hour
	}
	if strings.Contains(s, "ساعة") {
		return 1 * time.Hour
	}
	if strings.Contains(s, "يومين") {
		return 48 * time.Hour
	}
	if strings.Contains(s, "يوم") {
		return 24 * time.Hour
	}

	if d, err := time.ParseDuration(s); err == nil {
		return d
	}

	// Default fallback: 1 hour
	return 1 * time.Hour
}

// ScheduleTask adds a new follow-up task requested by Nova.
func (e *Engine) ScheduleTask(chatID, chatType, targetUser, reason, durationStr string) string {
	e.mu.Lock()
	defer e.mu.Unlock()

	duration := ParseDurationFlexible(durationStr)
	taskID := fmt.Sprintf("TASK_%X", time.Now().UnixNano())
	task := ScheduledTask{
		ID:         taskID,
		ChatID:     chatID,
		ChatType:   chatType,
		TargetUser: targetUser,
		Reason:     reason,
		DueAt:      time.Now().Add(duration),
		CreatedAt:  time.Now(),
		Executed:   false,
	}

	e.tasks = append(e.tasks, task)
	e.saveTasks()
	return taskID
}

// GetScheduledTasksCount returns count of active unexecuted tasks.
func (e *Engine) GetScheduledTasksCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	count := 0
	for _, t := range e.tasks {
		if !t.Executed {
			count++
		}
	}
	return count
}

// Start runs the background ticker for due tasks and automated triggers.
func (e *Engine) Start() {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				e.checkDueTasks()
				e.checkAutomatedTriggers()
			case <-e.stopChan:
				return
			}
		}
	}()
}

// Stop terminates the engine.
func (e *Engine) Stop() {
	close(e.stopChan)
}

func (e *Engine) checkDueTasks() {
	if e.state.GetShutdown() {
		return
	}

	e.mu.Lock()
	now := time.Now()
	var dueTasks []ScheduledTask
	for i := range e.tasks {
		if !e.tasks[i].Executed && now.After(e.tasks[i].DueAt) {
			e.tasks[i].Executed = true
			dueTasks = append(dueTasks, e.tasks[i])
		}
	}
	if len(dueTasks) > 0 {
		e.saveTasks()
	}
	e.mu.Unlock()

	for _, task := range dueTasks {
		e.executeTask(task)
	}
}

func (e *Engine) executeTask(task ScheduledTask) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	promptText := fmt.Sprintf("حان موعد المتابعة المجدولة في الشات! (السبب اللي حددتيه: %s). وجهي رسالة مبادرة طبيعية وذكية للجروب بالعامية المصرية بخصوص هذا الموضوع.", task.Reason)

	payload, err := trigger.BuildContext(
		e.chatLogger,
		e.memStore,
		task.ChatType,
		task.ChatID,
		"",
		task.TargetUser,
		"الجروب",
		task.ID,
		promptText,
		false,
		nil,
		nil,
		e.state.GetChatLimit(task.ChatID, 0),
	)
	if err != nil {
		return
	}

	resp, err := e.aiClient.GenerateResponse(ctx, payload)
	if err != nil || resp == nil || !resp.ShouldReply || resp.ReplyText == nil || *resp.ReplyText == "" {
		return
	}

	_ = e.dispatcher.SendMessage(ctx, task.ChatID, *resp.ReplyText, "")
}

// RecordIncomingMessageForRush tracks message rate for Trigger 3 (Rush burst).
func (e *Engine) RecordIncomingMessageForRush(chatID string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	now := time.Now()
	timestamps := e.recentRushCounts[chatID]
	cutoff := now.Add(-90 * time.Second)

	valid := make([]time.Time, 0, len(timestamps)+1)
	for _, t := range timestamps {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}
	valid = append(valid, now)
	e.recentRushCounts[chatID] = valid

	// Check if rush threshold reached (12 messages in 90s)
	if len(valid) >= 12 && e.canTriggerAuto(chatID, 2*time.Hour) {
		e.lastAutoTrigger[chatID] = now
		go e.triggerProactive(chatID, "group", "الجروب مولع كلام ورسايل سريعة ورا بعضها في نقاش سخن! ادخلي ارمي تعليق روش وسريع وسط الزحمة يتفاعل مع حماسهم بروحك المصرية.")
	}
}

// TriggerNewMemberJoined fires Trigger 5 when a new member joins.
func (e *Engine) TriggerNewMemberJoined(chatID, newMemberName string) {
	if !e.state.IsAutoTriggersEnabled(chatID) || e.state.GetShutdown() {
		return
	}
	prompt := fmt.Sprintf("فيه عضو جديد انضم للجروب دلوقتي (اسمه: %s). رحبي بيه ترحيب مصري أصيل وخفة دم وروشي عليه بروقان!", newMemberName)
	go e.triggerProactive(chatID, "group", prompt)
}

func (e *Engine) canTriggerAuto(chatID string, minGap time.Duration) bool {
	if !e.state.IsAutoTriggersEnabled(chatID) || e.state.GetShutdown() {
		return false
	}
	last, exists := e.lastAutoTrigger[chatID]
	if !exists {
		return true
	}
	return time.Since(last) >= minGap
}

func (e *Engine) checkAutomatedTriggers() {
	if e.state.GetShutdown() {
		return
	}

	activeChats := e.state.GetActiveAutoChats()
	if len(activeChats) == 0 {
		return
	}

	loc, _ := time.LoadLocation("Africa/Cairo")
	if loc == nil {
		loc = time.FixedZone("EEST", 3*3600)
	}
	now := time.Now().In(loc)
	hour := now.Hour()
	weekday := now.Weekday()

	for _, chatID := range activeChats {
		// Trigger 1: Silence Breaker (3 hours without talking between 10:00 AM and 23:00 PM)
		if hour >= 10 && hour <= 23 && e.canTriggerAuto(chatID, 4*time.Hour) {
			logs, err := e.chatLogger.GetRecentMessages("group", chatID, 1)
			if err == nil && len(logs) > 0 {
				if lastMsgTime, err := time.Parse(time.RFC3339, logs[len(logs)-1].Timestamp); err == nil {
					if now.Sub(lastMsgTime) >= 3*time.Hour {
						e.mu.Lock()
						e.lastAutoTrigger[chatID] = now
						e.mu.Unlock()

						prompt := "الجروب هادي ونايم بقاله 3 ساعات في عز النهار! ادخلي اكسري الصمت بنكشة أو إيفيه أو سؤال روش يصحّي الشات وتكيفي مع جو الجروب بروقان."
						go e.triggerProactive(chatID, "group", prompt)
						continue
					}
				}
			}
		}

		// Trigger 4: Morning Landmark (e.g. Friday 10:30 AM or Sunday 10:00 AM)
		if (weekday == time.Friday && hour == 10 && now.Minute() >= 30 && now.Minute() <= 35) ||
			(weekday == time.Sunday && hour == 10 && now.Minute() >= 0 && now.Minute() <= 5) {
			if e.canTriggerAuto(chatID, 12*time.Hour) {
				e.mu.Lock()
				e.lastAutoTrigger[chatID] = now
				e.mu.Unlock()

				prompt := "صباح يوم جديد رايق وجميل على الجروب! ادخلي صبحي على الكل بكلام دافي وخفيف دم بالعامية المصرية وتمنيات بيوم جميل ورايق."
				go e.triggerProactive(chatID, "group", prompt)
				continue
			}
		}
	}
}

func (e *Engine) triggerProactive(chatID, chatType, promptText string) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	payload, err := trigger.BuildContext(
		e.chatLogger,
		e.memStore,
		chatType,
		chatID,
		"",
		"nova_proactive",
		"الجروب",
		fmt.Sprintf("AUTO_%X", time.Now().UnixNano()),
		promptText,
		false,
		nil,
		nil,
		e.state.GetChatLimit(chatID, 0),
	)
	if err != nil {
		return
	}

	resp, err := e.aiClient.GenerateResponse(ctx, payload)
	if err != nil || resp == nil || !resp.ShouldReply || resp.ReplyText == nil || *resp.ReplyText == "" {
		return
	}

	_ = e.dispatcher.SendMessage(ctx, chatID, *resp.ReplyText, "")
}
