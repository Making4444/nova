package dashboard

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"novabot/internal/admin"
	"novabot/internal/storage"
)

// LogEntry represents a single system or bot log record.
type LogEntry struct {
	ID        int64  `json:"id"`
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
	Message   string `json:"message"`
}

// LogHub manages an in-memory ring buffer of logs with real-time SSE broadcasting.
type LogHub struct {
	mu          sync.RWMutex
	logs        []LogEntry
	maxLogs     int
	counter     int64
	subscribers map[chan LogEntry]struct{}
}

// NewLogHub creates a new LogHub instance.
func NewLogHub(maxLogs int) *LogHub {
	if maxLogs <= 0 {
		maxLogs = 1000
	}
	return &LogHub{
		logs:        make([]LogEntry, 0, maxLogs),
		maxLogs:     maxLogs,
		subscribers: make(map[chan LogEntry]struct{}),
	}
}

// AddLog records a new log entry and broadcasts it to connected SSE streams.
func (h *LogHub) AddLog(level, msg string) {
	if h == nil {
		return
	}
	id := atomic.AddInt64(&h.counter, 1)
	entry := LogEntry{
		ID:        id,
		Timestamp: time.Now().Format("2006-01-02 15:04:05"),
		Level:     strings.ToUpper(level),
		Message:   msg,
	}

	h.mu.Lock()
	if len(h.logs) >= h.maxLogs {
		h.logs = h.logs[1:]
	}
	h.logs = append(h.logs, entry)

	// Broadcast to active SSE subscribers
	for ch := range h.subscribers {
		select {
		case ch <- entry:
		default:
			// Drop if subscriber channel is blocked
		}
	}
	h.mu.Unlock()
}

// GetRecentLogs returns the latest N log entries.
func (h *LogHub) GetRecentLogs(limit int) []LogEntry {
	if h == nil {
		return nil
	}
	h.mu.RLock()
	defer h.mu.RUnlock()

	if limit <= 0 || limit >= len(h.logs) {
		res := make([]LogEntry, len(h.logs))
		copy(res, h.logs)
		return res
	}

	start := len(h.logs) - limit
	res := make([]LogEntry, limit)
	copy(res, h.logs[start:])
	return res
}

// Subscribe returns a channel receiving new log entries and an unsubscribe function.
func (h *LogHub) Subscribe() (chan LogEntry, func()) {
	if h == nil {
		ch := make(chan LogEntry, 1)
		close(ch)
		return ch, func() {}
	}
	ch := make(chan LogEntry, 100)
	h.mu.Lock()
	h.subscribers[ch] = struct{}{}
	h.mu.Unlock()

	unsubscribe := func() {
		h.mu.Lock()
		delete(h.subscribers, ch)
		close(ch)
		h.mu.Unlock()
	}

	return ch, unsubscribe
}

// EmotionEvent represents a recorded emotional interaction.
type EmotionEvent struct {
	Timestamp     string `json:"timestamp"`
	User          string `json:"user"`
	ChatID        string `json:"chat_id"`
	Mood          string `json:"mood"`
	ReactionEmoji string `json:"reaction_emoji,omitempty"`
	TextSnippet   string `json:"text_snippet,omitempty"`
}

// TimeSeriesPoint represents a data point for real-time charts.
type TimeSeriesPoint struct {
	Time             string `json:"time"`
	IncomingMessages int    `json:"incoming_messages"`
	NovaMessages     int    `json:"nova_messages"`
	Tokens           int    `json:"tokens"`
}

// MetricsTracker tracks runtime metrics, emotional trends, and chart time-series data.
type MetricsTracker struct {
	mu               sync.RWMutex
	incomingCount    int64
	novaCount        int64
	totalTokens      int64
	currentMood      string
	moodCounts       map[string]int
	recentEmotions   []EmotionEvent
	maxEmotions      int
	timeSeriesPoints []TimeSeriesPoint
}

// NewMetricsTracker creates a new MetricsTracker.
func NewMetricsTracker() *MetricsTracker {
	t := &MetricsTracker{
		currentMood: "witty",
		moodCounts: map[string]int{
			"happy":   5,
			"joking":  8,
			"witty":   12,
			"savage":  4,
			"neutral": 6,
			"angry":   1,
			"sad":     1,
		},
		recentEmotions:   make([]EmotionEvent, 0),
		maxEmotions:      50,
		timeSeriesPoints: make([]TimeSeriesPoint, 0),
	}

	// Seed initial time series points for charts
	now := time.Now()
	for i := 6; i >= 0; i-- {
		ptTime := now.Add(-time.Duration(i*10) * time.Minute).Format("15:04")
		t.timeSeriesPoints = append(t.timeSeriesPoints, TimeSeriesPoint{
			Time:             ptTime,
			IncomingMessages: 2 + (i % 4),
			NovaMessages:     1 + (i % 3),
			Tokens:           350 + (i*120)%800,
		})
	}

	return t
}

// RecordEvent records an interaction with mood, tokens, and messages.
func (t *MetricsTracker) RecordEvent(isNova bool, tokens int, mood, emoji, sender, chatID, snippet string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	if isNova {
		t.novaCount++
	} else {
		t.incomingCount++
	}

	if tokens > 0 {
		t.totalTokens += int64(tokens)
	}

	if mood != "" {
		mood = strings.ToLower(strings.TrimSpace(mood))
		t.currentMood = mood
		t.moodCounts[mood]++

		event := EmotionEvent{
			Timestamp:     time.Now().Format("2006-01-02 15:04:05"),
			User:          sender,
			ChatID:        chatID,
			Mood:          mood,
			ReactionEmoji: emoji,
			TextSnippet:   snippet,
		}

		if len(t.recentEmotions) >= t.maxEmotions {
			t.recentEmotions = t.recentEmotions[1:]
		}
		t.recentEmotions = append(t.recentEmotions, event)
	}

	// Update or append current time series point
	currentSlot := time.Now().Format("15:04")
	n := len(t.timeSeriesPoints)
	if n > 0 && t.timeSeriesPoints[n-1].Time == currentSlot {
		if isNova {
			t.timeSeriesPoints[n-1].NovaMessages++
		} else {
			t.timeSeriesPoints[n-1].IncomingMessages++
		}
		t.timeSeriesPoints[n-1].Tokens += tokens
	} else {
		if n >= 12 {
			t.timeSeriesPoints = t.timeSeriesPoints[1:]
		}
		inMsg := 0
		novaMsg := 0
		if isNova {
			novaMsg = 1
		} else {
			inMsg = 1
		}
		t.timeSeriesPoints = append(t.timeSeriesPoints, TimeSeriesPoint{
			Time:             currentSlot,
			IncomingMessages: inMsg,
			NovaMessages:     novaMsg,
			Tokens:           tokens,
		})
	}
}

// GetMoodStats returns copy of mood distribution and percentages.
func (t *MetricsTracker) GetMoodStats() (string, map[string]int, map[string]float64, []EmotionEvent) {
	if t == nil {
		return "witty", nil, nil, nil
	}
	t.mu.RLock()
	defer t.mu.RUnlock()

	counts := make(map[string]int)
	total := 0
	for k, v := range t.moodCounts {
		counts[k] = v
		total += v
	}

	percentages := make(map[string]float64)
	if total > 0 {
		for k, v := range counts {
			percentages[k] = float64(v*1000/total) / 10.0
		}
	}

	recent := make([]EmotionEvent, len(t.recentEmotions))
	copy(recent, t.recentEmotions)

	return t.currentMood, counts, percentages, recent
}

// GetCounts returns totals.
func (t *MetricsTracker) GetCounts() (incoming, nova, tokens int64, points []TimeSeriesPoint) {
	if t == nil {
		return 0, 0, 0, nil
	}
	t.mu.RLock()
	defer t.mu.RUnlock()

	pts := make([]TimeSeriesPoint, len(t.timeSeriesPoints))
	copy(pts, t.timeSeriesPoints)
	return t.incomingCount, t.novaCount, t.totalTokens, pts
}

// ChatSummary represents metadata of a discovered chat.
type ChatSummary struct {
	ID              string `json:"id"`
	DisplayName     string `json:"display_name"`
	Type            string `json:"type"` // "group" | "private"
	MessageCount    int    `json:"message_count"`
	LastMessage     string `json:"last_message"`
	LastMessageTime string `json:"last_message_time"`
	AutoTriggers    bool   `json:"auto_triggers"`
	ChatLimit       int    `json:"chat_limit"`
	ArchivesCount   int    `json:"archives_count"`
}

// UserAffinityCard represents affinity and memory info for a user.
type UserAffinityCard struct {
	UserID         string   `json:"user_id"`
	UserName       string   `json:"user_name"`
	NotesCount     int      `json:"notes_count"`
	LastUpdated    string   `json:"last_updated"`
	ProfileSummary string   `json:"profile_summary"`
	FullProfile    string   `json:"full_profile"`
	AffinityLevel  string   `json:"affinity_level"` // "Best Friend", "Close Friend", "Regular", "Neutral"
	Topics         []string `json:"topics"`
}

// Helper: ScanAllChats reads data/chats to list all chats and summary info.
func ScanAllChats(baseDir string, state *admin.State) ([]ChatSummary, int, int, int) {
	if baseDir == "" {
		baseDir = "data/chats"
	}

	var chats []ChatSummary
	totalMessages := 0
	groupCount := 0
	privateCount := 0

	scanDir := func(folder, chatType string) {
		dirPath := filepath.Join(baseDir, folder)
		files, err := os.ReadDir(dirPath)
		if err != nil {
			return
		}

		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".jsonl") {
				continue
			}

			filePath := filepath.Join(dirPath, f.Name())
			rawID := strings.TrimSuffix(f.Name(), ".jsonl")
			chatID := strings.ReplaceAll(rawID, "_", ":") // approximate reverse or keep raw
			if strings.Contains(rawID, "@g.us") || strings.Contains(rawID, "@s.whatsapp.net") {
				chatID = rawID
			}

			// Read file lines to count messages and get last message
			msgCount := 0
			var lastMsg storage.LogMessage
			file, err := os.Open(filePath)
			if err == nil {
				scanner := bufio.NewScanner(file)
				for scanner.Scan() {
					line := strings.TrimSpace(scanner.Text())
					if line != "" {
						msgCount++
						var m storage.LogMessage
						if jsonErr := json.Unmarshal([]byte(line), &m); jsonErr == nil {
							lastMsg = m
						}
					}
				}
				_ = file.Close()
			}

			totalMessages += msgCount
			if chatType == "group" {
				groupCount++
			} else {
				privateCount++
			}

			// Count archives
			archCount := 0
			archDir := filepath.Join(dirPath, "archive")
			if aFiles, aErr := os.ReadDir(archDir); aErr == nil {
				for _, af := range aFiles {
					if strings.HasPrefix(af.Name(), rawID+"_") && strings.HasSuffix(af.Name(), ".jsonl") {
						archCount++
					}
				}
			}

			displayName := chatID
			if lastMsg.SenderName != "" && chatType == "private" {
				displayName = fmt.Sprintf("%s (%s)", lastMsg.SenderName, chatID)
			}

			autoEnabled := false
			limit := 0
			if state != nil {
				autoEnabled = state.IsAutoTriggersEnabled(chatID)
				limit = state.GetChatLimit(chatID, 0)
			}

			chats = append(chats, ChatSummary{
				ID:              rawID,
				DisplayName:     displayName,
				Type:            chatType,
				MessageCount:    msgCount,
				LastMessage:     lastMsg.Text,
				LastMessageTime: lastMsg.Timestamp,
				AutoTriggers:    autoEnabled,
				ChatLimit:       limit,
				ArchivesCount:   archCount,
			})
		}
	}

	scanDir("groups", "group")
	scanDir("private", "private")

	return chats, len(chats), groupCount, privateCount
}

// Helper: ScanUserAffinities parses all user memory markdown cards in data/users.
func ScanUserAffinities(baseDir string) []UserAffinityCard {
	if baseDir == "" {
		baseDir = "data/users"
	}

	files, err := os.ReadDir(baseDir)
	if err != nil {
		return []UserAffinityCard{}
	}

	var results []UserAffinityCard
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".md") {
			continue
		}

		filePath := filepath.Join(baseDir, f.Name())
		rawID := strings.TrimSuffix(f.Name(), ".md")
		data, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}

		content := string(data)
		lines := strings.Split(content, "\n")

		userName := rawID
		lastUpdated := time.Now().Format("2006-01-02 15:04:05")
		notesCount := strings.Count(content, "### [")
		if notesCount == 0 {
			notesCount = strings.Count(content, "- ")
		}

		// Parse name if present in title
		for _, line := range lines {
			if strings.HasPrefix(line, "# ") {
				trimmed := strings.TrimPrefix(line, "# ")
				trimmed = strings.TrimPrefix(trimmed, "👤 بطاقة المستخدم: ")
				trimmed = strings.TrimPrefix(trimmed, "ذاكرة المستخدم: ")
				if trimmed != "" {
					userName = trimmed
				}
			} else if strings.Contains(line, "آخر تحديث:") {
				parts := strings.Split(line, "آخر تحديث:")
				if len(parts) > 1 {
					lastUpdated = strings.Trim(parts[1], " *")
				}
			}
		}

		// Determine affinity level
		affinity := "Regular"
		if notesCount >= 10 || strings.Contains(strings.ToLower(userName), "making") || strings.Contains(strings.ToLower(userName), "مكاري") {
			affinity = "Best Friend (الانتيم)"
		} else if notesCount >= 5 {
			affinity = "Close Friend (صاحب قديم)"
		} else if notesCount >= 2 {
			affinity = "Active (متفاعل)"
		} else {
			affinity = "Acquaintance (معرفة جديدة)"
		}

		summary := content
		if len(summary) > 240 {
			summary = summary[:240] + "..."
		}

		results = append(results, UserAffinityCard{
			UserID:         rawID,
			UserName:       userName,
			NotesCount:     notesCount,
			LastUpdated:    lastUpdated,
			ProfileSummary: summary,
			FullProfile:    content,
			AffinityLevel:  affinity,
			Topics:         extractTopics(content),
		})
	}

	return results
}

func extractTopics(content string) []string {
	var topics []string
	keywords := []string{"كورة", "برمجة", "طب", "امتحانات", "شغل", "سفر", "رياضة", "سيارات", "جيم", "ألعاب"}
	for _, kw := range keywords {
		if strings.Contains(content, kw) {
			topics = append(topics, kw)
		}
	}
	if len(topics) == 0 {
		topics = append(topics, "عام")
	}
	return topics
}
