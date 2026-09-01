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

type silenceTracker struct {
	Stage                int       // 0: normal/active, 1: 3h silence triggered, 2: +6h silence triggered, 3: locked
	LastHumanMessageTime time.Time
	LastSilenceTrigger   time.Time
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
	silenceTrackers    map[string]*silenceTracker
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
		silenceTrackers:  make(map[string]*silenceTracker),
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
	if e == nil {
		return
	}
	if e.state != nil && e.state.GetShutdown() {
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
	if e == nil || e.aiClient == nil || e.dispatcher == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	promptText := fmt.Sprintf("حان موعد المتابعة المجدولة في الشات! (السبب اللي حددتيه: %s). وجهي رسالة مبادرة طبيعية وذكية للجروب بالعامية المصرية بخصوص هذا الموضوع.", task.Reason)

	chatLimit := 0
	if e.state != nil {
		chatLimit = e.state.GetChatLimit(task.ChatID, 0)
	}

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
		chatLimit,
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

// RecordIncomingMessageForRush tracks message rate and resets the Silence Breaker cycle on any human message.
func (e *Engine) RecordIncomingMessageForRush(chatID string, wasTriggerMatched bool) {
	e.mu.Lock()
	defer e.mu.Unlock()

	now := time.Now()

	// 1. Reset Silence Breaker stage on human message!
	tracker, exists := e.silenceTrackers[chatID]
	if !exists {
		tracker = &silenceTracker{}
		e.silenceTrackers[chatID] = tracker
	}
	tracker.Stage = 0
	tracker.LastHumanMessageTime = now

	// 2. Safety: If Nova was called in this message, reset rush count because Nova is now actively involved!
	if wasTriggerMatched {
		e.recentRushCounts[chatID] = nil
		return
	}

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
	if len(valid) >= 12 && e.canTriggerAuto(chatID) {
		// Safety check: ensure none of the last 12 messages were from Nova or called Nova
		if e.chatLogger != nil {
			recentLogs, err := e.chatLogger.GetRecentMessages("group", chatID, 12)
			if err == nil {
				for _, log := range recentLogs {
					if log.IsNova || trigger.ContainsTrigger(log.Text) {
						e.recentRushCounts[chatID] = nil
						return
					}
				}
			}
		}

		e.recentRushCounts[chatID] = nil
		go e.triggerProactive(chatID, "group", "الجروب مولع كلام ورسايل سريعة ورا بعضها في نقاش سخن بين الأعضاء بدون وجودك! ادخلي ارمي تعليق روش وسريع وسط الزحمة يتفاعل مع حماسهم بروحك المصرية.")
	}
}

// TriggerNewMemberJoined fires Trigger 5 when a new member joins.
func (e *Engine) TriggerNewMemberJoined(chatID, newMemberName string) {
	if e == nil || !e.canTriggerAuto(chatID) {
		return
	}
	prompt := fmt.Sprintf("فيه عضو جديد انضم للجروب دلوقتي (اسمه: %s). رحبي بيه ترحيب مصري أصيل وخفة دم وروشي عليه بروقان!", newMemberName)
	go e.triggerProactive(chatID, "group", prompt)
}

func (e *Engine) canTriggerAuto(chatID string) bool {
	if e == nil || e.state == nil || !e.state.IsAutoTriggersEnabled(chatID) || e.state.GetShutdown() {
		return false
	}
	return true
}

func (e *Engine) checkAutomatedTriggers() {
	if e == nil || e.state == nil || e.state.GetShutdown() {
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
		if !e.state.IsAutoTriggersEnabled(chatID) {
			continue
		}

		// 1. Graduated Silence Breaker (3h -> 6h -> auto-stop until human writes)
		e.mu.Lock()
		tracker, exists := e.silenceTrackers[chatID]
		if !exists {
			tracker = &silenceTracker{}
			e.silenceTrackers[chatID] = tracker
		}

		// If LastHumanMessageTime is not yet set, read the last non-Nova message from chat log
		if tracker.LastHumanMessageTime.IsZero() && e.chatLogger != nil {
			recentLogs, err := e.chatLogger.GetRecentMessages("group", chatID, 20)
			if err == nil && len(recentLogs) > 0 {
				for i := len(recentLogs) - 1; i >= 0; i-- {
					if !recentLogs[i].IsNova {
						if t, err := time.Parse(time.RFC3339, recentLogs[i].Timestamp); err == nil {
							tracker.LastHumanMessageTime = t
							break
						}
					}
				}
			}
		}

		lastHuman := tracker.LastHumanMessageTime
		stage := tracker.Stage
		lastTrigger := tracker.LastSilenceTrigger
		e.mu.Unlock()

		// Only trigger during active Egypt daytime (10:00 AM to 23:00 PM)
		if hour >= 10 && hour <= 23 && !lastHuman.IsZero() {
			silenceDuration := now.Sub(lastHuman)

			// Stage 0 -> Stage 1: 3 hours of silence
			if stage == 0 && silenceDuration >= 3*time.Hour && silenceDuration < 24*time.Hour {
				e.mu.Lock()
				tracker.Stage = 1
				tracker.LastSilenceTrigger = now
				e.mu.Unlock()

				fmt.Printf("\n[⏳ Silence Breaker - Stage 1] 3h silence in chat %s -> Triggering Nova...\n", chatID)
				prompt := "الجروب هادي ونايم بقاله 3 ساعات في عز النهار! ادخلي اكسري الصمت بنكشة أو إيفيه أو سؤال روش يصحّي الشات وتكيفي مع جو الجروب بروقان."
				go e.triggerProactive(chatID, "group", prompt)
				continue
			}

			// Stage 1 -> Stage 2: +6 hours additional silence (total >= 9h)
			if stage == 1 && silenceDuration >= 9*time.Hour && now.Sub(lastTrigger) >= 6*time.Hour {
				e.mu.Lock()
				tracker.Stage = 2
				tracker.LastSilenceTrigger = now
				e.mu.Unlock()

				fmt.Printf("\n[⏳ Silence Breaker - Stage 2] +6h additional silence in chat %s -> Triggering Nova final check...\n", chatID)
				prompt := "الجروب مستمر في الهدوء التام بعد مرور 6 ساعات كمان ومحدش اتكلم خالص! ارمي كلمة خفيفة واطمني عليهم بروقان وقفشة خفيفة وقفلي على كدة."
				go e.triggerProactive(chatID, "group", prompt)
				continue
			}

			// Stage 2 -> Stage 3: Auto-shutdown/locked
			if stage == 2 && now.Sub(lastTrigger) >= 1*time.Hour {
				e.mu.Lock()
				tracker.Stage = 3
				e.mu.Unlock()
				fmt.Printf("\n[🔒 Silence Breaker - Locked] Chat %s silence breaker is now locked until a human writes.\n", chatID)
			}
		}

		// Trigger 4: Morning Landmark (e.g. Friday 10:30 AM or Sunday 10:00 AM)
		if (weekday == time.Friday && hour == 10 && now.Minute() >= 30 && now.Minute() <= 35) ||
			(weekday == time.Sunday && hour == 10 && now.Minute() >= 0 && now.Minute() <= 5) {
			if e.canTriggerAuto(chatID) {
				e.mu.Lock()
				tracker.LastSilenceTrigger = now
				e.mu.Unlock()

				prompt := "صباح يوم جديد رايق وجميل على الجروب! ادخلي صبحي على الكل بكلام دافي وخفيف دم بالعامية المصرية وتمنيات بيوم جميل ورايق."
				go e.triggerProactive(chatID, "group", prompt)
				continue
			}
		}
	}
}

func (e *Engine) triggerProactive(chatID, chatType, promptText string) {
	if e == nil || e.aiClient == nil || e.dispatcher == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	chatLimit := 0
	if e.state != nil {
		chatLimit = e.state.GetChatLimit(chatID, 0)
	}

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
		chatLimit,
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
