package storage

import (
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

// GetRecentMessages reads the last N messages from the specific chat's JSONL file.
// If limit <= 0, all messages are returned.
func (l *ChatLogger) GetRecentMessages(chatType, chatID string, limit int) ([]LogMessage, error) {
	filePath := l.getFilePath(chatType, chatID)
	fmu := l.getFileMutex(filePath)
	fmu.Lock()
	defer fmu.Unlock()

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return []LogMessage{}, nil
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read chat log %s: %w", filePath, err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) == 0 || (len(lines) == 1 && lines[0] == "") {
		return []LogMessage{}, nil
	}

	var allMessages []LogMessage
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		var msg LogMessage
		if err := json.Unmarshal([]byte(trimmed), &msg); err == nil {
			allMessages = append(allMessages, msg)
		}
	}

	if limit <= 0 || limit >= len(allMessages) {
		return allMessages, nil
	}

	return allMessages[len(allMessages)-limit:], nil
}

// ArchiveInfo contains details of an archived chat session.
type ArchiveInfo struct {
	Index         int       `json:"index"`
	FilePath      string    `json:"file_path"`
	MessagesCount int       `json:"messages_count"`
	ModTime       time.Time `json:"mod_time"`
}

func (l *ChatLogger) getFolder(chatType, chatID string) string {
	if strings.ToLower(chatType) == "group" || strings.Contains(chatID, "@g.us") {
		return "groups"
	}
	return "private"
}

// GetAllMessages retrieves all messages for a chat and formats them as a readable text transcript for AI.
func (l *ChatLogger) GetAllMessages(chatType, chatID string) (string, []LogMessage, error) {
	msgs, err := l.GetRecentMessages(chatType, chatID, 0)
	if err != nil {
		return "", nil, err
	}

	var b strings.Builder
	for _, m := range msgs {
		sender := m.SenderName
		if m.IsNova {
			sender = "Nova"
		}
		b.WriteString(fmt.Sprintf("[%s] %s (%s): %s\n", m.Timestamp, sender, m.SenderID, m.Text))
	}

	return b.String(), msgs, nil
}

// ArchiveCurrentChat moves the current active chat file to archive/<safeID>_<N>.jsonl and starts a fresh active file.
func (l *ChatLogger) ArchiveCurrentChat(chatType, chatID string) (int, string, error) {
	folder := l.getFolder(chatType, chatID)
	safeName := sanitizeID(chatID)
	activeFile := filepath.Join(l.baseDir, folder, safeName+".jsonl")
	archiveDir := filepath.Join(l.baseDir, folder, "archive")

	if err := os.MkdirAll(archiveDir, 0755); err != nil {
		return 0, "", fmt.Errorf("failed to create archive dir: %w", err)
	}

	fmu := l.getFileMutex(activeFile)
	fmu.Lock()
	defer fmu.Unlock()

	// Check if active file exists and has content
	if _, err := os.Stat(activeFile); os.IsNotExist(err) {
		return 0, "", fmt.Errorf("no active chat log exists to archive for %s", chatID)
	}

	// Find the next available index starting from 1
	nextIndex := 1
	for {
		candidate := filepath.Join(archiveDir, fmt.Sprintf("%s_%d.jsonl", safeName, nextIndex))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			break
		}
		nextIndex++
	}

	archivedPath := filepath.Join(archiveDir, fmt.Sprintf("%s_%d.jsonl", safeName, nextIndex))

	// Move active file to archive
	if err := os.Rename(activeFile, archivedPath); err != nil {
		// Fallback: Copy and truncate
		data, err := os.ReadFile(activeFile)
		if err != nil {
			return 0, "", fmt.Errorf("failed to read active chat for archive: %w", err)
		}
		if err := os.WriteFile(archivedPath, data, 0644); err != nil {
			return 0, "", fmt.Errorf("failed to write archive file: %w", err)
		}
		_ = os.Remove(activeFile)
	}

	// Create fresh new active file
	_ = os.WriteFile(activeFile, []byte(""), 0644)

	return nextIndex, archivedPath, nil
}

// SaveSummary saves an AI generated summary to data/chats/<folder>/summaries/<safeID>_summary_<N>.md.
func (l *ChatLogger) SaveSummary(chatType, chatID string, archiveIndex int, summaryContent string) error {
	folder := l.getFolder(chatType, chatID)
	safeName := sanitizeID(chatID)
	summaryDir := filepath.Join(l.baseDir, folder, "summaries")

	if err := os.MkdirAll(summaryDir, 0755); err != nil {
		return fmt.Errorf("failed to create summaries dir: %w", err)
	}

	summaryPath := filepath.Join(summaryDir, fmt.Sprintf("%s_summary_%d.md", safeName, archiveIndex))
	header := fmt.Sprintf("# 📋 ملخص المحادثة المؤرشفة رقم %d\n**تاريخ الأرشفة:** %s\n**الشات:** %s\n\n---\n\n",
		archiveIndex, time.Now().Format("2006-01-02 15:04:05"), chatID)

	return os.WriteFile(summaryPath, []byte(header+summaryContent), 0644)
}

// RestoreArchivedChat restores an archived chat file by index back into the active log.
func (l *ChatLogger) RestoreArchivedChat(chatType, chatID string, archiveIndex int) error {
	folder := l.getFolder(chatType, chatID)
	safeName := sanitizeID(chatID)
	activeFile := filepath.Join(l.baseDir, folder, safeName+".jsonl")
	archivedPath := filepath.Join(l.baseDir, folder, "archive", fmt.Sprintf("%s_%d.jsonl", safeName, archiveIndex))

	if _, err := os.Stat(archivedPath); os.IsNotExist(err) {
		return fmt.Errorf("archived chat file #%d does not exist for %s", archiveIndex, chatID)
	}

	archiveData, err := os.ReadFile(archivedPath)
	if err != nil {
		return fmt.Errorf("failed to read archive file: %w", err)
	}

	fmu := l.getFileMutex(activeFile)
	fmu.Lock()
	defer fmu.Unlock()

	// Write archive data into active file
	if err := os.WriteFile(activeFile, archiveData, 0644); err != nil {
		return fmt.Errorf("failed to restore archive to active chat: %w", err)
	}

	return nil
}

// ListArchivedChats returns all existing archives for a chat.
func (l *ChatLogger) ListArchivedChats(chatType, chatID string) ([]ArchiveInfo, error) {
	folder := l.getFolder(chatType, chatID)
	safeName := sanitizeID(chatID)
	archiveDir := filepath.Join(l.baseDir, folder, "archive")

	if _, err := os.Stat(archiveDir); os.IsNotExist(err) {
		return []ArchiveInfo{}, nil
	}

	files, err := os.ReadDir(archiveDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read archive dir: %w", err)
	}

	var list []ArchiveInfo
	for _, f := range files {
		if f.IsDir() || !strings.HasPrefix(f.Name(), safeName+"_") || !strings.HasSuffix(f.Name(), ".jsonl") {
			continue
		}

		// Extract index
		nameWithoutExt := strings.TrimSuffix(f.Name(), ".jsonl")
		idxStr := strings.TrimPrefix(nameWithoutExt, safeName+"_")
		var idx int
		if _, err := fmt.Sscanf(idxStr, "%d", &idx); err == nil {
			info, _ := f.Info()
			fullPath := filepath.Join(archiveDir, f.Name())

			// Count lines
			count := 0
			if fileBytes, err := os.ReadFile(fullPath); err == nil {
				count = len(strings.Split(strings.TrimSpace(string(fileBytes)), "\n"))
			}

			modTime := time.Now()
			if info != nil {
				modTime = info.ModTime()
			}

			list = append(list, ArchiveInfo{
				Index:         idx,
				FilePath:      fullPath,
				MessagesCount: count,
				ModTime:       modTime,
			})
		}
	}

	return list, nil
}
