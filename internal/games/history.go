package games

import (
	"encoding/json"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// HistoryTracker guarantees that questions are never repeated in a chat
// until all questions in that category have been cycled through.
type HistoryTracker struct {
	historyDir string
	caches     map[string]map[string][]string // chatID -> category -> list of seen question IDs
	mu         sync.RWMutex
}

// NewHistoryTracker initializes the rotation history tracker.
func NewHistoryTracker(dataDir string) *HistoryTracker {
	hDir := filepath.Join(dataDir, "games", "history")
	_ = os.MkdirAll(hDir, 0755)
	return &HistoryTracker{
		historyDir: hDir,
		caches:     make(map[string]map[string][]string),
	}
}

func (h *HistoryTracker) getFilePath(chatID string) string {
	safeName := sanitizeFilename(chatID)
	return filepath.Join(h.historyDir, safeName+".json")
}

func (h *HistoryTracker) loadChatHistory(chatID string) map[string][]string {
	h.mu.RLock()
	data, exists := h.caches[chatID]
	h.mu.RUnlock()
	if exists && data != nil {
		return data
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if data, exists := h.caches[chatID]; exists && data != nil {
		return data
	}

	hist := make(map[string][]string)
	fPath := h.getFilePath(chatID)
	if bytes, err := os.ReadFile(fPath); err == nil {
		_ = json.Unmarshal(bytes, &hist)
	}

	h.caches[chatID] = hist
	return hist
}

func (h *HistoryTracker) saveChatHistory(chatID string, hist map[string][]string) error {
	fPath := h.getFilePath(chatID)
	bytes, err := json.MarshalIndent(hist, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(fPath, bytes, 0644)
}

// PickQuestions selects 'count' non-repeating questions from 'pool' for 'chatID'.
// If all questions have been seen, it resets the cycle and selects freshly.
func (h *HistoryTracker) PickQuestions(chatID string, cat Category, count int, pool []Question) []Question {
	if len(pool) == 0 {
		return nil
	}
	if count <= 0 {
		count = 5
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	// Load or get chat history
	hist, exists := h.caches[chatID]
	if !exists || hist == nil {
		hist = make(map[string][]string)
		fPath := h.getFilePath(chatID)
		if bytes, err := os.ReadFile(fPath); err == nil {
			_ = json.Unmarshal(bytes, &hist)
		}
		h.caches[chatID] = hist
	}

	catKey := string(cat)
	seenList := hist[catKey]
	seenMap := make(map[string]bool, len(seenList))
	for _, id := range seenList {
		seenMap[id] = true
	}

	// Filter out already seen questions
	var unseen []Question
	for _, q := range pool {
		if !seenMap[q.ID] {
			unseen = append(unseen, q)
		}
	}

	// If remaining unseen questions are fewer than needed, reset cycle for this category
	if len(unseen) < count {
		unseen = make([]Question, len(pool))
		copy(unseen, pool)
		seenList = nil
		hist[catKey] = nil
	}

	// Shuffle unseen pool
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	r.Shuffle(len(unseen), func(i, j int) {
		unseen[i], unseen[j] = unseen[j], unseen[i]
	})

	if count > len(unseen) {
		count = len(unseen)
	}
	selected := unseen[:count]

	// Mark selected as seen
	for _, q := range selected {
		seenList = append(seenList, q.ID)
	}
	hist[catKey] = seenList

	_ = h.saveChatHistory(chatID, hist)
	return selected
}
