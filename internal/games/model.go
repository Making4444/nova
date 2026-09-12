package games

import (
	"sync"
	"time"
)

// Category represents a competition question category.
type Category string

const (
	CategoryChristian Category = "christian" // مسيحية، كتاب مقدس، تاريخ كنسي
	CategoryQuote     Category = "quote"     // إفيهات ومسرحيات وسينما مصرية
	CategoryMovie     Category = "movie"     // أفلام ومسلسلات وفنون
	CategoryFootball  Category = "football"  // كورة ورياضة محلية وعالمية
	CategoryTrivia    Category = "trivia"    // معلومات عامة وثقافة وجغرافيا
	CategoryRiddle    Category = "riddle"    // فوازير وألغاز ذكاء مصرية
	CategoryProverb   Category = "proverb"   // أمثال شعبية (كمّل المثل)
	CategoryScience   Category = "science"   // علوم طبيعية وفيزياء وكيمياء وأحياء وطب
	CategoryTech      Category = "tech"      // تكنولوجيا وكمبيوتر وبرمجة وهواتف
	CategorySpace     Category = "space"     // فضاء وفلك وكواكب ونجوم
	CategoryGeography Category = "geography" // جغرافيا ودول وعواصم وتضاريس
	CategoryAnimals   Category = "animals"   // عالم الحيوان والطيور والبحار
	CategoryFood      Category = "food"      // أكلات ومطابخ وتوابل
	CategorySports    Category = "sports"    // رياضات عامة وأولمبياد وتنس وسلة
	CategoryHistory   Category = "history"   // تاريخ وحضارات وشخصيات تاريخية
	CategoryCartoon   Category = "cartoon"   // كرتون وأنمي وسبيستون وديزني
	CategoryMixed     Category = "mixed"     // تشكيلة كوكتيل منوعة من كل الأقسام
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

// ChatGameConfig holds configurable game preferences per chat.
type ChatGameConfig struct {
	RoundCount         int `json:"round_count"`           // Number of questions per round (default 5, min 1, max 30)
	QuestionTimeoutSec int `json:"question_timeout_sec"`  // Duration per question in seconds (default 45, min 10, max 180)
	SpeedBonusSec      int `json:"speed_bonus_sec"`       // Seconds threshold for 2-point speed bonus (default 10)
}

// DefaultGameConfig returns sensible default game parameters.
func DefaultGameConfig() ChatGameConfig {
	return ChatGameConfig{
		RoundCount:         5,
		QuestionTimeoutSec: 45,
		SpeedBonusSec:      10,
	}
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
	Config            ChatGameConfig
	Stopped           bool
	Mu                sync.Mutex
}
