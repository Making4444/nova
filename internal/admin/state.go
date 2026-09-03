package admin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// State holds the persistent configuration and runtime state.
type State struct {
	IsShutdown          bool            `json:"is_shutdown"`
	ChatLimits          map[string]int  `json:"chat_limits"`           // chatID -> history limit (0 = all)
	AutoTriggersEnabled map[string]bool `json:"auto_triggers_enabled"` // chatID -> bool
	ActivePersona       int             `json:"active_persona"`        // 1 = Bro/Default, 2 = Charming/Female
	AdminNumber         string          `json:"admin_number"`          // e.g. "201202172699"
	StartTime           time.Time       `json:"start_time"`
	filePath            string
	mu                  sync.RWMutex
}

// NewState initializes or loads persistent state from data/settings.json.
func NewState(dataDir string, adminNumber string) (*State, error) {
	if adminNumber == "" {
		adminNumber = "201202172699"
	}

	settingsPath := filepath.Join(dataDir, "settings.json")
	st := &State{
		IsShutdown:          false,
		ChatLimits:          make(map[string]int),
		AutoTriggersEnabled: make(map[string]bool),
		ActivePersona:       1,
		AdminNumber:         adminNumber,
		StartTime:           time.Now(),
		filePath:            settingsPath,
	}

	// Try reading existing settings
	if data, err := os.ReadFile(settingsPath); err == nil {
		var loaded struct {
			IsShutdown          bool            `json:"is_shutdown"`
			ChatLimits          map[string]int  `json:"chat_limits"`
			AutoTriggersEnabled map[string]bool `json:"auto_triggers_enabled"`
			ActivePersona       int             `json:"active_persona"`
		}
		if err := json.Unmarshal(data, &loaded); err == nil {
			st.IsShutdown = loaded.IsShutdown
			if loaded.ChatLimits != nil {
				st.ChatLimits = loaded.ChatLimits
			}
			if loaded.AutoTriggersEnabled != nil {
				st.AutoTriggersEnabled = loaded.AutoTriggersEnabled
			}
			if loaded.ActivePersona >= 1 && loaded.ActivePersona <= 2 {
				st.ActivePersona = loaded.ActivePersona
			}
		}
	}

	return st, nil
}

func (s *State) save() error {
	_ = os.MkdirAll(filepath.Dir(s.filePath), 0755)
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.filePath, data, 0644)
}

// SetAdminNumber updates the configured admin phone number thread-safely.
func (s *State) SetAdminNumber(number string) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.AdminNumber = strings.TrimSpace(number)
	return s.save()
}

// GetAdminNumber returns the configured admin number thread-safely.
func (s *State) GetAdminNumber() string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.AdminNumber
}

// SetPersona sets active persona mode (1 = Bro/Default, 2 = Charming/Female) thread-safely.
func (s *State) SetPersona(mode int) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if mode < 1 || mode > 2 {
		mode = 1
	}
	s.ActivePersona = mode
	return s.save()
}

// GetPersona returns current active persona mode (1 or 2) thread-safely.
func (s *State) GetPersona() int {
	if s == nil {
		return 1
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.ActivePersona < 1 || s.ActivePersona > 2 {
		return 1
	}
	return s.ActivePersona
}

// IsAdmin checks if sender JID/Phone matches the admin number or if the message is from the owner.
func (s *State) IsAdmin(senderID string, senderName string, isFromMe bool) bool {
	if isFromMe {
		return true
	}
	if s == nil {
		return false
	}

	adminNum := s.GetAdminNumber()

	cleanSender := strings.TrimSuffix(senderID, "@s.whatsapp.net")
	cleanSender = strings.TrimSuffix(cleanSender, "@c.us")
	cleanSender = strings.TrimSuffix(cleanSender, "@lid")
	cleanSender = strings.TrimPrefix(cleanSender, "+")
	cleanSender = strings.TrimSpace(cleanSender)

	cleanAdmin := strings.TrimPrefix(adminNum, "+")
	cleanAdmin = strings.TrimSpace(cleanAdmin)

	// Direct match or 01... vs 201... match
	if cleanSender == cleanAdmin && cleanAdmin != "" {
		return true
	}
	if len(cleanAdmin) > 2 && strings.HasPrefix(cleanAdmin, "20") && cleanSender == "0"+cleanAdmin[2:] {
		return true
	}
	if len(cleanSender) > 2 && strings.HasPrefix(cleanSender, "20") && cleanAdmin == "0"+cleanSender[2:] {
		return true
	}

	// Match by known admin LID or phone number
	if strings.Contains(senderID, "105012604760193") || (cleanAdmin != "" && strings.Contains(senderID, cleanAdmin)) {
		return true
	}

	return false
}

// SetShutdown toggles or sets the shutdown mode.
func (s *State) SetShutdown(val bool) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.IsShutdown = val
	return s.save()
}

// GetShutdown returns current shutdown status.
func (s *State) GetShutdown() bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.IsShutdown
}

// SetChatLimit sets message history limit for a specific chat.
func (s *State) SetChatLimit(chatID string, limit int) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ChatLimits[chatID] = limit
	return s.save()
}

// GetChatLimit returns limit for chat or fallback.
func (s *State) GetChatLimit(chatID string, defaultLimit int) int {
	if s == nil {
		return defaultLimit
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if limit, exists := s.ChatLimits[chatID]; exists {
		return limit
	}
	return defaultLimit
}

// SetAutoTriggers enables or disables automated triggers for a specific chat.
func (s *State) SetAutoTriggers(chatID string, enabled bool) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.AutoTriggersEnabled[chatID] = enabled
	return s.save()
}

// IsAutoTriggersEnabled checks if automated triggers are on for a chat.
func (s *State) IsAutoTriggersEnabled(chatID string) bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.AutoTriggersEnabled[chatID]
}

// GetActiveAutoChats returns list of chatIDs with auto triggers enabled.
func (s *State) GetActiveAutoChats() []string {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	res := make([]string, 0, len(s.AutoTriggersEnabled))
	for chatID, enabled := range s.AutoTriggersEnabled {
		if enabled {
			res = append(res, chatID)
		}
	}
	return res
}

// GetUptimeString returns human readable uptime.
func (s *State) GetUptimeString() string {
	if s == nil {
		return "0 ثانية"
	}
	d := time.Since(s.StartTime)
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60
	if hours > 24 {
		days := hours / 24
		hours = hours % 24
		return fmt.Sprintf("%d يوم و %d ساعة و %d دقيقة", days, hours, minutes)
	}
	return fmt.Sprintf("%d ساعة و %d دقيقة و %d ثانية", hours, minutes, seconds)
}
