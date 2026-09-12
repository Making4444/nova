package games

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// ConfigStore persists custom game parameters per chat (e.g. round length, question timer).
type ConfigStore struct {
	filePath string
	configs  map[string]ChatGameConfig
	mu       sync.RWMutex
}

// NewConfigStore initializes or loads game configurations from data/games/config.json.
func NewConfigStore(dataDir string) *ConfigStore {
	gamesDir := filepath.Join(dataDir, "games")
	_ = os.MkdirAll(gamesDir, 0755)
	cfgPath := filepath.Join(gamesDir, "config.json")

	store := &ConfigStore{
		filePath: cfgPath,
		configs:  make(map[string]ChatGameConfig),
	}

	if data, err := os.ReadFile(cfgPath); err == nil {
		var loaded map[string]ChatGameConfig
		if err := json.Unmarshal(data, &loaded); err == nil && loaded != nil {
			store.configs = loaded
		}
	}

	return store
}

func (c *ConfigStore) save() error {
	data, err := json.MarshalIndent(c.configs, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(c.filePath, data, 0644)
}

// GetConfig returns the configured settings for a chat or default values if none set.
func (c *ConfigStore) GetConfig(chatID string) ChatGameConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()

	cfg, exists := c.configs[chatID]
	if !exists {
		return DefaultGameConfig()
	}

	// Sanity checks
	if cfg.RoundCount <= 0 {
		cfg.RoundCount = 5
	}
	if cfg.QuestionTimeoutSec <= 0 {
		cfg.QuestionTimeoutSec = 45
	}
	if cfg.SpeedBonusSec <= 0 {
		cfg.SpeedBonusSec = 10
	}
	return cfg
}

// SetRounds updates the number of questions in a game round for this chat.
func (c *ConfigStore) SetRounds(chatID string, count int) error {
	if count < 1 || count > 30 {
		return fmt.Errorf("عدد أسئلة الجولة يجب أن يكون بين 1 و 30 سؤالاً")
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	cfg := DefaultGameConfig()
	if existing, ok := c.configs[chatID]; ok {
		cfg = existing
	}
	cfg.RoundCount = count
	c.configs[chatID] = cfg
	return c.save()
}

// SetTimeout updates the question duration in seconds for this chat.
func (c *ConfigStore) SetTimeout(chatID string, seconds int) error {
	if seconds < 10 || seconds > 180 {
		return fmt.Errorf("مهلة السؤال يجب أن تكون بين 10 ثوانٍ و 180 ثانية")
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	cfg := DefaultGameConfig()
	if existing, ok := c.configs[chatID]; ok {
		cfg = existing
	}
	cfg.QuestionTimeoutSec = seconds
	c.configs[chatID] = cfg
	return c.save()
}

// ResetConfig restores default game settings for this chat.
func (c *ConfigStore) ResetConfig(chatID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.configs, chatID)
	return c.save()
}
