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
	AdminsList          []string                    `json:"admins_list"`           // List of additional admin phone numbers / JIDs
	ThinkingEffort      string                      `json:"thinking_effort"`       // "auto", "none", "low", "medium", "high"
	ChatNicknames       map[string]map[string]string `json:"chat_nicknames"`       // chatID -> cleanPhone -> nickname
	StartTime           time.Time                   `json:"start_time"`
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
		AdminsList:          make([]string, 0),
		ThinkingEffort:      "auto",
		ChatNicknames:       make(map[string]map[string]string),
		StartTime:           time.Now(),
		filePath:            settingsPath,
	}

	// Try reading existing settings
	if data, err := os.ReadFile(settingsPath); err == nil {
		var loaded struct {
			IsShutdown          bool                        `json:"is_shutdown"`
			ChatLimits          map[string]int              `json:"chat_limits"`
			AutoTriggersEnabled map[string]bool             `json:"auto_triggers_enabled"`
			ActivePersona       int                         `json:"active_persona"`
			AdminsList          []string                    `json:"admins_list"`
			ThinkingEffort      string                      `json:"thinking_effort"`
			ChatNicknames       map[string]map[string]string `json:"chat_nicknames"`
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
			if loaded.AdminsList != nil {
				st.AdminsList = loaded.AdminsList
			}
			if loaded.ThinkingEffort != "" {
				st.ThinkingEffort = loaded.ThinkingEffort
			}
			if loaded.ChatNicknames != nil {
				st.ChatNicknames = loaded.ChatNicknames
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

// CleanPhoneNumber strips WhatsApp suffixes, prefixes, spaces, and punctuation for consistent matching.
func CleanPhoneNumber(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "@s.whatsapp.net")
	s = strings.TrimSuffix(s, "@c.us")
	s = strings.TrimSuffix(s, "@lid")
	s = strings.TrimPrefix(s, "+")
	s = strings.TrimPrefix(s, "@")
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "-", "")
	return s
}

// MatchPhoneNumber checks if two phone representations match (supporting Egyptian 201... vs 01...).
func MatchPhoneNumber(a, b string) bool {
	ca := CleanPhoneNumber(a)
	cb := CleanPhoneNumber(b)
	if ca == "" || cb == "" {
		return false
	}
	if ca == cb {
		return true
	}
	if len(ca) > 2 && strings.HasPrefix(ca, "20") && cb == "0"+ca[2:] {
		return true
	}
	if len(cb) > 2 && strings.HasPrefix(cb, "20") && ca == "0"+cb[2:] {
		return true
	}
	return false
}

// IsAdmin checks if sender JID/Phone matches the primary owner or any registered admin in AdminsList.
func (s *State) IsAdmin(senderID string, senderName string, isFromMe bool) bool {
	if isFromMe {
		return true
	}
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	cleanSender := CleanPhoneNumber(senderID)

	// 1. Primary Owner check
	if MatchPhoneNumber(cleanSender, s.AdminNumber) {
		return true
	}

	// Match by known owner LID or direct sub-match
	if strings.Contains(senderID, "105012604760193") || (s.AdminNumber != "" && strings.Contains(senderID, CleanPhoneNumber(s.AdminNumber))) {
		return true
	}

	// 2. Added Admins List check
	for _, adminNum := range s.AdminsList {
		if MatchPhoneNumber(cleanSender, adminNum) || (adminNum != "" && strings.Contains(senderID, CleanPhoneNumber(adminNum))) {
			return true
		}
	}

	return false
}

// AddAdmin adds a new admin phone number to the persistent admin list.
func (s *State) AddAdmin(number string) (string, error) {
	if s == nil {
		return "", fmt.Errorf("admin state is nil")
	}
	cleaned := CleanPhoneNumber(number)
	if cleaned == "" {
		return "", fmt.Errorf("رقم هاتف غير صالح")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if already the owner
	if MatchPhoneNumber(cleaned, s.AdminNumber) {
		return cleaned, nil
	}

	// Check if already in list
	for _, existing := range s.AdminsList {
		if MatchPhoneNumber(cleaned, existing) {
			return cleaned, nil
		}
	}

	s.AdminsList = append(s.AdminsList, cleaned)
	return cleaned, s.save()
}

// RemoveAdmin removes an admin phone number from the persistent list.
func (s *State) RemoveAdmin(number string) (bool, error) {
	if s == nil {
		return false, fmt.Errorf("admin state is nil")
	}
	cleaned := CleanPhoneNumber(number)
	if cleaned == "" {
		return false, fmt.Errorf("رقم هاتف غير صالح")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Do not allow removing the primary owner
	if MatchPhoneNumber(cleaned, s.AdminNumber) {
		return false, fmt.Errorf("لا يمكن حذف المالك الأساسي للبوت")
	}

	newList := make([]string, 0, len(s.AdminsList))
	found := false
	for _, existing := range s.AdminsList {
		if MatchPhoneNumber(cleaned, existing) {
			found = true
			continue
		}
		newList = append(newList, existing)
	}

	if found {
		s.AdminsList = newList
		return true, s.save()
	}

	return false, nil
}

// GetAdminsList returns a copy of registered admin numbers.
func (s *State) GetAdminsList() []string {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	res := make([]string, len(s.AdminsList))
	copy(res, s.AdminsList)
	return res
}

// SetThinkingEffort sets the reasoning effort level ("auto", "none", "low", "medium", "high").
func (s *State) SetThinkingEffort(effort string) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	effort = strings.ToLower(strings.TrimSpace(effort))
	switch effort {
	case "none", "off", "0", "disabled":
		s.ThinkingEffort = "none"
	case "low", "1":
		s.ThinkingEffort = "low"
	case "medium", "2":
		s.ThinkingEffort = "medium"
	case "high", "3":
		s.ThinkingEffort = "high"
	default:
		s.ThinkingEffort = "auto"
	}
	return s.save()
}

// GetThinkingEffort returns the current reasoning effort level.
func (s *State) GetThinkingEffort() string {
	if s == nil {
		return "auto"
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.ThinkingEffort == "" {
		return "auto"
	}
	return s.ThinkingEffort
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

// SetChatNickname sets a custom calling nickname for a user specifically inside a chat.
func (s *State) SetChatNickname(chatID, userPhoneOrJID, nickname string) error {
	if s == nil {
		return fmt.Errorf("admin state is nil")
	}
	phone := CleanPhoneNumber(userPhoneOrJID)
	nickname = strings.TrimSpace(nickname)
	if chatID == "" || phone == "" || nickname == "" {
		return fmt.Errorf("بيانات غير مكتملة لتحديد اسم المناداة")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.ChatNicknames == nil {
		s.ChatNicknames = make(map[string]map[string]string)
	}
	if s.ChatNicknames[chatID] == nil {
		s.ChatNicknames[chatID] = make(map[string]string)
	}

	s.ChatNicknames[chatID][phone] = nickname
	return s.save()
}

// GetChatNickname returns custom calling nickname for a user in a specific chat, or empty string if none set.
func (s *State) GetChatNickname(chatID, userPhoneOrJID string) string {
	if s == nil {
		return ""
	}
	phone := CleanPhoneNumber(userPhoneOrJID)
	if chatID == "" || phone == "" {
		return ""
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.ChatNicknames == nil || s.ChatNicknames[chatID] == nil {
		return ""
	}

	// Direct match
	if nick, exists := s.ChatNicknames[chatID][phone]; exists && nick != "" {
		return nick
	}

	// Flexible phone matching (201... vs 01...)
	for p, nick := range s.ChatNicknames[chatID] {
		if MatchPhoneNumber(p, phone) && nick != "" {
			return nick
		}
	}

	return ""
}

// ClearChatNickname removes custom calling nickname for a user in a specific chat.
func (s *State) ClearChatNickname(chatID, userPhoneOrJID string) error {
	if s == nil {
		return nil
	}
	phone := CleanPhoneNumber(userPhoneOrJID)
	if chatID == "" || phone == "" {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.ChatNicknames == nil || s.ChatNicknames[chatID] == nil {
		return nil
	}

	delete(s.ChatNicknames[chatID], phone)
	for p := range s.ChatNicknames[chatID] {
		if MatchPhoneNumber(p, phone) {
			delete(s.ChatNicknames[chatID], p)
		}
	}

	return s.save()
}
