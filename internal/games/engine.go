package games

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// BroadcasterFunc is a callback to send messages or quote replies back to WhatsApp.
type BroadcasterFunc func(chatID string, text string, replyToMsgID string) error

// Engine coordinates real-time interactive game sessions across WhatsApp chats.
type Engine struct {
	leaderboard         *LeaderboardStore
	broadcaster         BroadcasterFunc
	activeGames         map[string]*ActiveGame
	questionDuration    time.Duration
	speedBonusThreshold time.Duration
	mu                  sync.RWMutex
}

// NewEngine initializes the competition engine.
func NewEngine(dataDir string, broadcaster BroadcasterFunc) *Engine {
	return &Engine{
		leaderboard:         NewLeaderboardStore(dataDir),
		broadcaster:         broadcaster,
		activeGames:         make(map[string]*ActiveGame),
		questionDuration:    45 * time.Second,
		speedBonusThreshold: 10 * time.Second,
	}
}

// SetDurations allows customizing question timeout and speed bonus threshold (useful for testing).
func (e *Engine) SetDurations(timeout, speedBonus time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.questionDuration = timeout
	e.speedBonusThreshold = speedBonus
}

// HasActiveGame checks if a game is currently ongoing in the specified chat.
func (e *Engine) HasActiveGame(chatID string) bool {
	if chatID == "" {
		return false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	game, exists := e.activeGames[chatID]
	return exists && game != nil && !game.Stopped
}

// GetLeaderboardStore returns the underlying score store.
func (e *Engine) GetLeaderboardStore() *LeaderboardStore {
	return e.leaderboard
}

// StartGame begins a new 5-question competition round in the chat.
func (e *Engine) StartGame(chatID string, requestedCategory string) (string, error) {
	if chatID == "" {
		return "", fmt.Errorf("invalid chatID")
	}

	e.mu.Lock()
	if game, exists := e.activeGames[chatID]; exists && game != nil && !game.Stopped {
		e.mu.Unlock()
		return "⚠️ *في مسابقة شغالة بالفعل في الشات ده دلوقتي!*\nجاوبوا على السؤال المعروض أو اكتبوا `/game stop` لإيقافها وبدء غيرها.", nil
	}

	cat := CategoryMixed
	req := strings.ToLower(strings.TrimSpace(requestedCategory))
	switch req {
	case "movie", "movies", "افلام", "أفلام", "سينما", "افيه", "إفيه":
		cat = CategoryMovie
	case "trivia", "ثقافة", "ثقافه", "معلومات", "اسئلة", "أسئلة", "كورة", "رياضة":
		cat = CategoryTrivia
	case "riddle", "riddles", "فوازير", "فزورة", "فزوره", "لغز", "الغاز":
		cat = CategoryRiddle
	}

	questions := GetRandomQuestions(cat, 5)
	if len(questions) == 0 {
		e.mu.Unlock()
		return "❌ عذراً، لا توجد أسئلة متوفرة حالياً في بنك الأسئلة.", nil
	}

	game := &ActiveGame{
		ChatID:         chatID,
		Category:       cat,
		Questions:      questions,
		CurrentIndex:   0,
		RoundScores:    make(map[string]int),
		RoundUserNames: make(map[string]string),
		Stopped:        false,
	}
	e.activeGames[chatID] = game
	e.mu.Unlock()

	// Announce game start and display first question
	catTitle := "🎮 *تحدي ومسابقات نوفا السريعة (كوكتيل منوع)* 🌟"
	switch cat {
	case CategoryMovie:
		catTitle = "🎬 *مسابقة السينما والإفيهات المصرية مع نوفا* 🍿"
	case CategoryTrivia:
		catTitle = "🧠 *تحدي المعلومات العامة والكورة مع نوفا* ⚽"
	case CategoryRiddle:
		catTitle = "🧩 *فوازير وألغاز ذكاء مع نوفا* 💡"
	}

	introMsg := fmt.Sprintf("%s\n"+
		"═══════════════════════\n"+
		"• عدد الجولات: *5 أسئلة*\n"+
		"• مهلة كل سؤال: *45 ثانية*\n"+
		"• الإجابة السريعة (أول 10 ثواني) = *نقطتين سرعة بديهة ⚡*\n"+
		"• الإجابة العادية = *نقطة واحدة 🎯*\n"+
		"• الإجابة بتنكتب عادي في الشات بدون منشن ولا أوامر!\n\n"+
		"جاهزين؟ نبدأ مع أول سؤال حالا! 👇\n"+
		"═══════════════════════", catTitle)

	if e.broadcaster != nil {
		_ = e.broadcaster(chatID, introMsg, "")
	}

	// Post question 1 after a short pause
	time.AfterFunc(1200*time.Millisecond, func() {
		e.postQuestion(chatID, 0)
	})

	return "", nil
}

// postQuestion displays the current question and starts the countdown timer.
func (e *Engine) postQuestion(chatID string, qIndex int) {
	e.mu.Lock()
	game, exists := e.activeGames[chatID]
	if !exists || game == nil || game.Stopped {
		e.mu.Unlock()
		return
	}
	if qIndex != game.CurrentIndex || qIndex >= len(game.Questions) {
		e.mu.Unlock()
		return
	}

	q := game.Questions[qIndex]
	game.QuestionStartTime = time.Now()

	// Set timeout timer
	timeout := e.questionDuration
	game.Timer = time.AfterFunc(timeout, func() {
		e.handleTimeout(chatID, qIndex)
	})
	e.mu.Unlock()

	msg := fmt.Sprintf("❓ *السؤال (%d من %d):*\n\n%s\n\n⏱️ *معاكم 45 ثانية تجاوبوا!*",
		qIndex+1, len(game.Questions), q.Prompt)

	if e.broadcaster != nil {
		_ = e.broadcaster(chatID, msg, "")
	}
}

// handleTimeout is called when 45s elapse without any correct answer.
func (e *Engine) handleTimeout(chatID string, qIndex int) {
	e.mu.Lock()
	game, exists := e.activeGames[chatID]
	if !exists || game == nil || game.Stopped {
		e.mu.Unlock()
		return
	}
	if qIndex != game.CurrentIndex {
		e.mu.Unlock()
		return
	}

	currentQ := game.Questions[qIndex]
	correctAnswer := currentQ.AcceptedAnswers[0]

	msg := fmt.Sprintf("⏰ *الوقت خلص ومحدش عرف يحلها يا شباب!*\n"+
		"💡 الإجابة الصح كانت: *%s*\n"+
		"ركزوا في السؤال اللي جاي! 😉🔥", correctAnswer)

	if e.broadcaster != nil {
		_ = e.broadcaster(chatID, msg, "")
	}

	game.CurrentIndex++
	nextIndex := game.CurrentIndex
	isFinished := nextIndex >= len(game.Questions)
	e.mu.Unlock()

	if isFinished {
		time.AfterFunc(2*time.Second, func() {
			e.finishGame(chatID)
		})
	} else {
		time.AfterFunc(3*time.Second, func() {
			e.postQuestion(chatID, nextIndex)
		})
	}
}

// StopGame cancels any ongoing game in the specified chat.
func (e *Engine) StopGame(chatID string) (string, bool) {
	if chatID == "" {
		return "مفيش مسابقة شغالة حالياً.", false
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	game, exists := e.activeGames[chatID]
	if !exists || game == nil || game.Stopped {
		return "⚠️ مفيش أي مسابقة نشطة حالياً في الشات ده.", false
	}

	if game.Timer != nil {
		game.Timer.Stop()
	}
	game.Stopped = true
	delete(e.activeGames, chatID)

	return "🛑 *تم إيقاف المسابقة الحالية بنجاح!*\nتقدروا تبدأوا جولة جديدة في أي وقت بكتابة `/game`.", true
}

// ProcessAnswer evaluates an incoming chat message against the active question.
// Returns true if the message matched the correct answer.
func (e *Engine) ProcessAnswer(chatID, senderID, senderName, text, messageID string) (bool, string) {
	if chatID == "" || text == "" {
		return false, ""
	}

	e.mu.Lock()
	game, exists := e.activeGames[chatID]
	if !exists || game == nil || game.Stopped {
		e.mu.Unlock()
		return false, ""
	}

	if game.CurrentIndex >= len(game.Questions) {
		e.mu.Unlock()
		return false, ""
	}

	currentQ := game.Questions[game.CurrentIndex]
	if !CheckAnswer(text, currentQ.AcceptedAnswers) {
		e.mu.Unlock()
		return false, ""
	}

	// Correct Answer!
	if game.Timer != nil {
		game.Timer.Stop()
	}

	elapsed := time.Since(game.QuestionStartTime)
	points := 1
	speedBonus := false
	if elapsed <= e.speedBonusThreshold {
		points = 2
		speedBonus = true
	}

	// Update round stats
	game.RoundScores[senderID] += points
	game.RoundUserNames[senderID] = senderName

	// Update persistent leaderboard
	if e.leaderboard != nil {
		_, _ = e.leaderboard.AddScore(chatID, senderID, senderName, points)
	}

	// Congratulatory message
	bonusTag := "نقطة واحدة 🎯"
	speedPraise := ""
	if speedBonus {
		bonusTag = "نقطتين (+2) سرعة بديهة صاروخية! ⚡🔥"
		speedPraise = "\n🚀 *سرعة الرد:* جاوبتها في ثواني معدودة، عاش جداً!"
	}

	dispAnswer := currentQ.AcceptedAnswers[0]
	replyMsg := fmt.Sprintf("🎯 *الله عليك يا %s! إجابة صحيحة في مقتل!*\n\n"+
		"🏆 *الإجابة الصح:* *%s*\n"+
		"🎁 *النقاط:* %s%s\n"+
		"═══════════════════════", senderName, dispAnswer, bonusTag, speedPraise)

	if e.broadcaster != nil {
		_ = e.broadcaster(chatID, replyMsg, messageID)
	}

	game.CurrentIndex++
	nextIndex := game.CurrentIndex
	isFinished := nextIndex >= len(game.Questions)
	e.mu.Unlock()

	if isFinished {
		time.AfterFunc(2*time.Second, func() {
			e.finishGame(chatID)
		})
	} else {
		time.AfterFunc(3*time.Second, func() {
			e.postQuestion(chatID, nextIndex)
		})
	}

	return true, replyMsg
}

// finishGame summarizes the ended round and prints the chat's top scorers.
func (e *Engine) finishGame(chatID string) {
	e.mu.Lock()
	game, exists := e.activeGames[chatID]
	if !exists || game == nil {
		e.mu.Unlock()
		return
	}
	delete(e.activeGames, chatID)
	roundScores := game.RoundScores
	roundNames := game.RoundUserNames
	e.mu.Unlock()

	var sb strings.Builder
	sb.WriteString("🏁 *انتهت مسابقة نوفا لهذا الدور! عاش يا أبطال!* 👏🎉\n\n")

	if len(roundScores) == 0 {
		sb.WriteString("😅 مفيش حد عرف يجاوب أي سؤال صح في الجولة دي! محتاجين تركيز أكتر المرة الجاية!\n\n")
	} else {
		type roundWinner struct {
			Name   string
			Points int
		}
		var winners []roundWinner
		for uID, pts := range roundScores {
			name := roundNames[uID]
			if name == "" {
				name = "بطل"
			}
			winners = append(winners, roundWinner{Name: name, Points: pts})
		}
		sort.Slice(winners, func(i, j int) bool {
			return winners[i].Points > winners[j].Points
		})

		sb.WriteString("🌟 *ترتيب نجوم الجولة الحالية:*\n")
		medals := []string{"🥇", "🥈", "🥉"}
		for i, w := range winners {
			icon := "•"
			if i < len(medals) {
				icon = medals[i]
			}
			sb.WriteString(fmt.Sprintf("%s *%s*: %d نقطة\n", icon, w.Name, w.Points))
		}
		sb.WriteString("\n")
	}

	// Append chat overall leaderboard
	if e.leaderboard != nil {
		sb.WriteString("═══════════════════════\n")
		sb.WriteString(e.leaderboard.FormatTopMessage(chatID, 5))
	}

	if e.broadcaster != nil {
		_ = e.broadcaster(chatID, sb.String(), "")
	}
}

// GetLeaderboardText returns the current top list formatted for a chat.
func (e *Engine) GetLeaderboardText(chatID string) string {
	if e.leaderboard == nil {
		return "لوحة الصدارة غير متصلة حالياً."
	}
	return e.leaderboard.FormatTopMessage(chatID, 10)
}
