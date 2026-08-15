package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// MemoryStore manages user-specific memory notes saved in markdown format.
type MemoryStore struct {
	baseDir string
	mu      sync.RWMutex
	userMu  map[string]*sync.Mutex
}

// NewMemoryStore initializes the memory store in the specified directory.
func NewMemoryStore(baseDir string) (*MemoryStore, error) {
	if baseDir == "" {
		baseDir = "data/users"
	}

	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create memory directory %s: %w", baseDir, err)
	}

	return &MemoryStore{
		baseDir: baseDir,
		userMu:  make(map[string]*sync.Mutex),
	}, nil
}

func (s *MemoryStore) getUserMutex(userID string) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m, ok := s.userMu[userID]; ok {
		return m
	}
	m := &sync.Mutex{}
	s.userMu[userID] = m
	return m
}

func (s *MemoryStore) getFilePath(userID string) string {
	safeID := sanitizeID(userID)
	return filepath.Join(s.baseDir, safeID+".md")
}

// GetUserMemory returns the full memory text of a user if it exists.
func (s *MemoryStore) GetUserMemory(userID string) (string, error) {
	if userID == "" {
		return "", nil
	}

	filePath := s.getFilePath(userID)
	mu := s.getUserMutex(userID)
	mu.Lock()
	defer mu.Unlock()

	data, err := os.ReadFile(filePath)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("failed to read user memory for %s: %w", userID, err)
	}

	return strings.TrimSpace(string(data)), nil
}

// AppendMemoryNote appends a new memory note for the user with a timestamp.
func (s *MemoryStore) AppendMemoryNote(userID, userName, note string) error {
	if userID == "" || strings.TrimSpace(note) == "" {
		return nil
	}

	filePath := s.getFilePath(userID)
	mu := s.getUserMutex(userID)
	mu.Lock()
	defer mu.Unlock()

	fileExists := true
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		fileExists = false
	}

	f, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open memory file for %s: %w", userID, err)
	}
	defer f.Close()

	timestamp := time.Now().Format("2006-01-02 15:04:05")
	var entry strings.Builder

	if !fileExists {
		entry.WriteString(fmt.Sprintf("# ذاكرة المستخدم: %s (%s)\n\n", userName, userID))
	}

	entry.WriteString(fmt.Sprintf("### [%s]\n- %s\n\n", timestamp, strings.TrimSpace(note)))

	if _, err := f.WriteString(entry.String()); err != nil {
		return fmt.Errorf("failed to write memory note for %s: %w", userID, err)
	}

	return nil
}
