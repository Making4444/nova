package games

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// LeaderboardStore handles persistent storage of user scores per chat.
type LeaderboardStore struct {
	dataDir string
	caches  map[string]*Leaderboard
	mu      sync.RWMutex
}

// NewLeaderboardStore initializes the store.
func NewLeaderboardStore(dataDir string) *LeaderboardStore {
	gamesDir := filepath.Join(dataDir, "games")
	_ = os.MkdirAll(gamesDir, 0755)
	return &LeaderboardStore{
		dataDir: gamesDir,
		caches:  make(map[string]*Leaderboard),
	}
}

func sanitizeFilename(name string) string {
	var sb strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			sb.WriteRune(r)
		} else {
			sb.WriteRune('_')
		}
	}
	return sb.String()
}

func (s *LeaderboardStore) getFilePath(chatID string) string {
	safeName := sanitizeFilename(chatID)
	return filepath.Join(s.dataDir, safeName+".json")
}

func (s *LeaderboardStore) load(chatID string) *Leaderboard {
	s.mu.RLock()
	lb, exists := s.caches[chatID]
	s.mu.RUnlock()
	if exists && lb != nil {
		return lb
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Double check
	if lb, exists := s.caches[chatID]; exists && lb != nil {
		return lb
	}

	lb = &Leaderboard{
		ChatID: chatID,
		Scores: make(map[string]*PlayerScore),
	}

	filePath := s.getFilePath(chatID)
	if data, err := os.ReadFile(filePath); err == nil {
		var loaded Leaderboard
		if err := json.Unmarshal(data, &loaded); err == nil {
			if loaded.Scores != nil {
				lb.Scores = loaded.Scores
			}
		}
	}

	s.caches[chatID] = lb
	return lb
}

func (s *LeaderboardStore) save(chatID string, lb *Leaderboard) error {
	filePath := s.getFilePath(chatID)
	data, err := json.MarshalIndent(lb, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filePath, data, 0644)
}

// AddScore adds points to a player in a specific chat.
func (s *LeaderboardStore) AddScore(chatID, userID, userName string, points int) (*PlayerScore, error) {
	if chatID == "" || userID == "" {
		return nil, fmt.Errorf("invalid chatID or userID")
	}

	lb := s.load(chatID)

	s.mu.Lock()
	defer s.mu.Unlock()

	ps, exists := lb.Scores[userID]
	if !exists || ps == nil {
		ps = &PlayerScore{
			UserID:     userID,
			UserName:   userName,
			Points:     0,
			CorrectAns: 0,
		}
		lb.Scores[userID] = ps
	}

	if userName != "" {
		ps.UserName = userName
	}
	ps.Points += points
	ps.CorrectAns++
	ps.LastActive = time.Now()

	_ = s.save(chatID, lb)
	return ps, nil
}

// GetTop retrieves the top N players sorted descending by points.
func (s *LeaderboardStore) GetTop(chatID string, limit int) []*PlayerScore {
	lb := s.load(chatID)

	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(lb.Scores) == 0 {
		return nil
	}

	var list []*PlayerScore
	for _, ps := range lb.Scores {
		if ps != nil && ps.Points > 0 {
			list = append(list, ps)
		}
	}

	sort.Slice(list, func(i, j int) bool {
		if list[i].Points == list[j].Points {
			return list[i].CorrectAns > list[j].CorrectAns
		}
		return list[i].Points > list[j].Points
	})

	if limit > 0 && len(list) > limit {
		list = list[:limit]
	}
	return list
}

// FormatTopMessage returns a nicely formatted leaderboard in Egyptian Arabic.
func (s *LeaderboardStore) FormatTopMessage(chatID string, limit int) string {
	if limit <= 0 {
		limit = 10
	}
	topList := s.GetTop(chatID, limit)
	if len(topList) == 0 {
		return "📭 *لوحة الصدارة فاضية لسه!*\nمحدش كسب أي نقط في الشات ده لغاية دلوقتي.. ابدأوا أول جولة بكتابة `/game` وورونا مين أشطر واحد! 🎮🔥"
	}

	var sb strings.Builder
	sb.WriteString("🏆 *لوحة شرف وصدارة أبطال مسابقات نوفا:*\n\n")

	medals := []string{"🥇", "🥈", "🥉"}
	for idx, player := range topList {
		rankPrefix := fmt.Sprintf("%d.", idx+1)
		if idx < len(medals) {
			rankPrefix = medals[idx]
		}

		name := player.UserName
		if name == "" {
			name = "بطل مجهول"
		}

		sb.WriteString(fmt.Sprintf("%s *%s* ➔ *%d* نقطة (%d إجابة صحيحة)\n",
			rankPrefix, name, player.Points, player.CorrectAns))
	}

	sb.WriteString("\n💡 اكتب `/game` في أي وقت لبدء جولة جديدة وجمع نقط أكتر!")
	return sb.String()
}
