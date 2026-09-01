package trigger

import (
	"sync"
	"time"
)

// ChatLimiter manages rate limiting (cooldown) and per-chat concurrency control.
type ChatLimiter struct {
	mu          sync.Mutex
	lastTrigger map[string]time.Time
	chatLocks   map[string]*sync.Mutex
	cooldown    time.Duration
}

// NewChatLimiter creates a new ChatLimiter with specified cooldown duration.
func NewChatLimiter(cooldownSeconds int) *ChatLimiter {
	return &ChatLimiter{
		lastTrigger: make(map[string]time.Time),
		chatLocks:   make(map[string]*sync.Mutex),
		cooldown:    time.Duration(cooldownSeconds) * time.Second,
	}
}

// Allow checks if the cooldown period has elapsed for the given chatID and updates timestamp if allowed.
func (l *ChatLimiter) Allow(chatID string) bool {
	if l == nil || l.cooldown <= 0 {
		return true
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	last, exists := l.lastTrigger[chatID]
	now := time.Now()
	if exists && now.Sub(last) < l.cooldown {
		return false
	}

	l.lastTrigger[chatID] = now
	return true
}

// GetChatLock returns the specific mutex for a chat to ensure orderly processing.
func (l *ChatLimiter) GetChatLock(chatID string) *sync.Mutex {
	if l == nil {
		return &sync.Mutex{}
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	lock, exists := l.chatLocks[chatID]
	if !exists {
		lock = &sync.Mutex{}
		l.chatLocks[chatID] = lock
	}
	return lock
}
