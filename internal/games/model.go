package games

import (
	"sync"
	"time"
)

// Category represents a competition question category.
type Category string

const (
	CategoryMovie  Category = "movie"  // سينما وإفيهات مصرية
	CategoryTrivia Category = "trivia" // معلومات عامة، كورة، ثقافة
	CategoryRiddle Category = "riddle" // فوازير وألغاز ذكاء
	CategoryMixed  Category = "mixed"  // تشكيلة كوكتيل منوعة
)

// Question represents a single competition question with acceptable answer variants.
type Question struct {
	ID              string   `json:"id"`
	Category        Category `json:"category"`
	Prompt          string   `json:"prompt"`
	AcceptedAnswers []string `json:"accepted_answers"`
	Hint            string   `json:"hint,omitempty"`
	FunFact         string   `json:"fun_fact,omitempty"`
}

// PlayerScore records leaderboard performance for a player in a specific chat.
type PlayerScore struct {
	UserID     string    `json:"user_id"`
	UserName   string    `json:"user_name"`
	Points     int       `json:"points"`
	CorrectAns int       `json:"correct_ans"`
	LastActive time.Time `json:"last_active"`
}

// Leaderboard contains all player scores within a specific chat.
type Leaderboard struct {
	ChatID string                  `json:"chat_id"`
	Scores map[string]*PlayerScore `json:"scores"`
}

// ActiveGame represents an ongoing competition session in a chat.
type ActiveGame struct {
	ChatID            string
	Category          Category
	Questions         []Question
	CurrentIndex      int
	QuestionStartTime time.Time
	Timer             *time.Timer
	RoundScores       map[string]int    // userID -> points gained in current session
	RoundUserNames    map[string]string // userID -> last known display name
	Stopped           bool
	Mu                sync.Mutex
}
