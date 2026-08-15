package storage

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// LogMessage represents a single message recorded in the chat log.
type LogMessage struct {
	MessageID   string `json:"message_id"`
	SenderID    string `json:"sender_id"`
	SenderName  string `json:"sender_name"`
	Text        string `json:"text"`
	IsNova      bool   `json:"is_nova"`
	Timestamp   string `json:"timestamp"`
	IsReply     bool   `json:"is_reply,omitempty"`
	RepliedToID string `json:"replied_to_id,omitempty"`
}

// ChatLogger manages appending and reading JSONL logs segregated per chat.
type ChatLogger struct {
	baseDir string
	mu      sync.RWMutex
	fileMu  map[string]*sync.Mutex
}

// NewChatLogger creates a new ChatLogger instance.
func NewChatLogger(baseDir string) (*ChatLogger, error) {
	if baseDir == "" {
		baseDir = "data/chats"
	}

	privDir := filepath.Join(baseDir, "private")
	grpDir := filepath.Join(baseDir, "groups")

	if err := os.MkdirAll(privDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create private chats directory: %w", err)
	}
	if err := os.MkdirAll(grpDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create groups directory: %w", err)
	}

	return &ChatLogger{
		baseDir: baseDir,
		fileMu:  make(map[string]*sync.Mutex),
	}, nil
}

func sanitizeID(id string) string {
	// Replace colons, slashes, backslashes for filesystem safety on Windows/Linux
	replacer := strings.NewReplacer(":", "_", "/", "_", "\\", "_", "<", "_", ">", "_", "|", "_", "?", "_", "*", "_", "\"", "_")
	return replacer.Replace(id)
}

func (l *ChatLogger) getFilePath(chatType, chatID string) string {
	folder := "private"
	if strings.ToLower(chatType) == "group" || strings.Contains(chatID, "@g.us") {
		folder = "groups"
	}
	safeName := sanitizeID(chatID) + ".jsonl"
	return filepath.Join(l.baseDir, folder, safeName)
}

func (l *ChatLogger) getFileMutex(path string) *sync.Mutex {
	l.mu.Lock()
	defer l.mu.Unlock()
	if m, ok := l.fileMu[path]; ok {
		return m
	}
	m := &sync.Mutex{}
	l.fileMu[path] = m
	return m
}

// AppendMessage appends a message to the specific chat's JSONL file.
func (l *ChatLogger) AppendMessage(chatType, chatID string, msg LogMessage) error {
	if msg.Timestamp == "" {
		msg.Timestamp = time.Now().Format(time.RFC3339)
	}

	// Always ensure Nova is identified correctly
	if msg.IsNova {
		msg.SenderName = "Nova"
	}

	filePath := l.getFilePath(chatType, chatID)
	fmu := l.getFileMutex(filePath)
	fmu.Lock()
	defer fmu.Unlock()

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal log message: %w", err)
	}

	f, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open chat log file %s: %w", filePath, err)
	}
	defer f.Close()

	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("failed to write to chat log %s: %w", filePath, err)
	}

	return nil
}

// GetRecentMessages retrieves the last N messages for a chat. If limit <= 0, returns all messages.
func (l *ChatLogger) GetRecentMessages(chatType, chatID string, limit int) ([]LogMessage, error) {
	filePath := l.getFilePath(chatType, chatID)
	fmu := l.getFileMutex(filePath)
	fmu.Lock()
	defer fmu.Unlock()

	f, err := os.Open(filePath)
	if os.IsNotExist(err) {
		return []LogMessage{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to open chat log %s: %w", filePath, err)
	}
	defer f.Close()

	var allMessages []LogMessage
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var msg LogMessage
		if err := json.Unmarshal([]byte(line), &msg); err == nil {
			allMessages = append(allMessages, msg)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading chat log %s: %w", filePath, err)
	}

	// If limit <= 0, return the complete history (all messages)
	if limit <= 0 || len(allMessages) <= limit {
		return allMessages, nil
	}

	return allMessages[len(allMessages)-limit:], nil
}
