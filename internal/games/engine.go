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
	bank           *BankManager
	historyTracker *HistoryTracker
	configStore    *ConfigStore
	leaderboard    *LeaderboardStore
	broadcaster    BroadcasterFunc
	activeGames    map[string]*ActiveGame
	mu             sync.RWMutex
}

// NewEngine initializes the competition engine with all stores.
func NewEngine(dataDir string, broadcaster BroadcasterFunc) *Engine {
	return &Engine{
		bank:           NewBankManager(dataDir),
		historyTracker: NewHistoryTracker(dataDir),
		configStore:    NewConfigStore(dataDir),
		leaderboard:    NewLeaderboardStore(dataDir),
		broadcaster:    broadcaster,
		activeGames:    make(map[string]*ActiveGame),
	}
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

// GetBankManager returns the underlying bank manager.
func (e *Engine) GetBankManager() *BankManager {
	return e.bank
}

// GetConfigStore returns the underlying config store.
func (e *Engine) GetConfigStore() *ConfigStore {
	return e.configStore
}

// ParseCategory maps user category input in Arabic or English to the canonical Category.
func ParseCategory(input string) Category {
	req := strings.ToLower(strings.TrimSpace(input))
	switch req {
	case "christian", "bible", "مسيحية", "مسيحي", "كتاب", "انجيل", "إنجيل", "كنسي", "دين", "ديني":
		return CategoryChristian
	case "quote", "quotes", "افيه", "إفيه", "افيهات", "إفيهات":
		return CategoryQuote
	case "movie", "movies", "افلام", "أفلام", "سينما":
		return CategoryMovie
	case "football", "ball", "كورة", "كوره", "رياضة", "رياضه":
		return CategoryFootball
	case "proverb", "proverbs", "امثال", "أمثال", "مثل":
		return CategoryProverb
	case "trivia", "عامة", "عامه", "ثقافة", "ثقافه", "معلومات":
		return CategoryTrivia
	case "riddle", "riddles", "فوازير", "فزورة", "فزوره", "لغز", "الغاز", "ألغاز":
		return CategoryRiddle
	case "science", "tech", "علوم", "تكنولوجيا", "فضاء":
		return CategoryScience
	case "history", "تاريخ", "فراعنة", "فراعنه":
		return CategoryHistory
	case "cartoon", "anime", "كرتون", "انمي", "أنمي", "سبيستون", "ديزني":
		return CategoryCartoon
	default:
		return CategoryMixed
	}
}

// StartGame begins a new competition round in the chat with zero repetition.
func (e *Engine) StartGame(chatID string, requestedCategory string) (string, error) {
	if chatID == "" {
		return "", fmt.Errorf("invalid chatID")
	}

	e.mu.Lock()
	if game, exists := e.activeGames[chatID]; exists && game != nil && !game.Stopped {
		e.mu.Unlock()
		return "⚠️ *في مسابقة شغالة بالفعل في الشات ده دلوقتي!*\nجاوبوا على السؤال المعروض أو اكتبوا `/game stop` لإيقافها وبدء غيرها.", nil
	}

	cat := ParseCategory(requestedCategory)
	cfg := e.configStore.GetConfig(chatID)

	// Fetch questions pool
	pool := e.bank.GetQuestions(cat)
	if len(pool) == 0 {
		e.mu.Unlock()
		return "❌ عذراً، لا توجد أسئلة متوفرة حالياً لهذا القسم في بنك الأسئلة.", nil
	}

	// Pick non-repeating questions through rotation history
	questions := e.historyTracker.PickQuestions(chatID, cat, cfg.RoundCount, pool)
	if len(questions) == 0 {
		e.mu.Unlock()
		return "❌ عذراً، تعذر تحميل أسئلة الجولة.", nil
	}

	game := &ActiveGame{
		ChatID:         chatID,
		Category:       cat,
		Questions:      questions,
		CurrentIndex:   0,
		RoundScores:    make(map[string]int),
		RoundUserNames: make(map[string]string),
		Config:         cfg,
		Stopped:        false,
	}
	e.activeGames[chatID] = game
	e.mu.Unlock()

	// Title formatting
	catTitle := "🎮 *تحدي ومسابقات نوفا (كوكتيل منوع)* 🌟"
	switch cat {
	case CategoryChristian:
		catTitle = "✝️ *مسابقة الكتاب المقدس والتاريخ الكنسي مع نوفا* 🕊️"
	case CategoryQuote:
		catTitle = "🎭 *مسابقة الإفيهات والمسرحيات المصرية مع نوفا* 🍿"
	case CategoryMovie:
		catTitle = "🎬 *مسابقة السينما والأفلام والفنون مع نوفا* 🎥"
	case CategoryFootball:
		catTitle = "⚽ *تحدي الكورة والرياضة مع نوفا* 🏆"
	case CategoryProverb:
		catTitle = "📜 *مسابقة كمّل المثل الشعبي المصري مع نوفا* 🪕"
	case CategoryTrivia:
		catTitle = "🧠 *تحدي المعلومات العامة والثقافة مع نوفا* 🌍"
	case CategoryRiddle:
		catTitle = "🧩 *فوازير وألغاز ذكاء مصرية مع نوفا* 💡"
	case CategoryScience:
		catTitle = "🔬 *تحدي العلوم والتكنولوجيا والفضاء مع نوفا* 🚀"
	case CategoryHistory:
		catTitle = "🏛️ *تحدي التاريخ والحضارات والشخصيات مع نوفا* 📜"
	case CategoryCartoon:
		catTitle = "🎨 *مسابقة الكرتون والأنمي وسبيستون وديزني مع نوفا* 📺"
	}

	introMsg := fmt.Sprintf("%s\n"+
		"═══════════════════════\n"+
		"• عدد الجولات: *%d أسئلة*\n"+
		"• مهلة كل سؤال: *%d ثانية*\n"+
		"• الإجابة السريعة (أول %d ثوانٍ) = *نقطتين سرعة بديهة ⚡*\n"+
		"• الإجابة العادية = *نقطة واحدة 🎯*\n"+
		"• الإجابة بتنكتب عادي في الشات بدون منشن ولا أوامر!\n\n"+
		"جاهزين؟ نبدأ مع أول سؤال حالا! 👇\n"+
		"═══════════════════════", catTitle, len(questions), cfg.QuestionTimeoutSec, cfg.SpeedBonusSec)

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

	// Set timeout timer based on chat configuration
	timeoutSec := game.Config.QuestionTimeoutSec
	if timeoutSec <= 0 {
		timeoutSec = 45
	}
	timeoutDuration := time.Duration(timeoutSec) * time.Second

	game.Timer = time.AfterFunc(timeoutDuration, func() {
		e.handleTimeout(chatID, qIndex)
	})
	e.mu.Unlock()

	msg := fmt.Sprintf("❓ *السؤال (%d من %d):*\n\n%s\n\n⏱️ *معاكم %d ثانية تجاوبوا!*",
		qIndex+1, len(game.Questions), q.Prompt, timeoutSec)

	if e.broadcaster != nil {
		_ = e.broadcaster(chatID, msg, "")
	}
}

// handleTimeout is called when question timeout elapses without any correct answer.
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
	speedThreshold := time.Duration(game.Config.SpeedBonusSec) * time.Second
	if speedThreshold <= 0 {
		speedThreshold = 10 * time.Second
	}
	if elapsed <= speedThreshold {
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

// GetConfigText formats current chat game settings.
func (e *Engine) GetConfigText(chatID string) string {
	cfg := e.configStore.GetConfig(chatID)
	return fmt.Sprintf("⚙️ *إعدادات المسابقات في هذا الشات:*\n\n"+
		"• *عدد أسئلة الجولة:* %d أسئلة\n"+
		"• *مهلة الإجابة:* %d ثانية لكل سؤال\n"+
		"• *مهلة بونص السرعة (نقطتين):* أول %d ثوانٍ\n\n"+
		"💡 *لتعديل الإعدادات:*\n"+
		"• `/game config rounds <عدد>` : لتغيير عدد الأسئلة (من 1 إلى 30)\n"+
		"• `/game config time <ثواني>` : لتغيير مهلة السؤال (من 10 إلى 180 ثانية)\n"+
		"• `/game config reset` : لإعادة الإعدادات للوضع الافتراضي (5 أسئلة و 45 ثانية)",
		cfg.RoundCount, cfg.QuestionTimeoutSec, cfg.SpeedBonusSec)
}

// SetRounds updates round questions count.
func (e *Engine) SetRounds(chatID string, count int) (string, error) {
	if err := e.configStore.SetRounds(chatID, count); err != nil {
		return "", err
	}
	return fmt.Sprintf("✅ *تم بنجاح ضبط عدد أسئلة كل جولة في هذا الشات على:* `%d أسئلة`", count), nil
}

// SetTimeout updates question timer duration.
func (e *Engine) SetTimeout(chatID string, seconds int) (string, error) {
	if err := e.configStore.SetTimeout(chatID, seconds); err != nil {
		return "", err
	}
	return fmt.Sprintf("✅ *تم بنجاح ضبط مهلة إجابة السؤال في هذا الشات على:* `%d ثانية`", seconds), nil
}

// ResetConfig restores default settings.
func (e *Engine) ResetConfig(chatID string) string {
	_ = e.configStore.ResetConfig(chatID)
	return "✅ *تمت إعادة إعدادات المسابقات في هذا الشات إلى الوضع الافتراضي:* (5 أسئلة، 45 ثانية لكل سؤال)."
}

// GetHelpText returns the categories and commands list.
func (e *Engine) GetHelpText() string {
	return "🎮 *دليل أقسام مسابقات نوفا التفاعلية (500 سؤال):*\n\n" +
		"• `/game christian` أو `/game مسيحي` : مسابقة الكتاب المقدس والتاريخ الكنسي ✝️\n" +
		"• `/game quote` أو `/game افيهات` : مسابقة أشهر الإفيهات والمسرحيات المصرية 🎭\n" +
		"• `/game movie` أو `/game سينما` : مسابقة الأفلام والفنون والممثلين 🎬\n" +
		"• `/game ball` أو `/game كورة` : مسابقة كورة القدم المحلية والعالمية ⚽\n" +
		"• `/game amthal` أو `/game امثال` : مسابقة كمّل المثل الشعبي المصري 📜\n" +
		"• `/game trivia` أو `/game عامة` : مسابقة ثقافة عامة، جغرافيا، وعواصم 🧠\n" +
		"• `/game riddle` أو `/game فوازير` : مسابقة فوازير وألغاز ذكاء 🧩\n" +
		"• `/game science` أو `/game علوم` : مسابقة علوم وتكنولوجيا وفضاء 🔬\n" +
		"• `/game history` أو `/game تاريخ` : مسابقة تاريخ وحضارات وفراعنة 🏛️\n" +
		"• `/game cartoon` أو `/game كرتون` : مسابقة كرتون وأنمي وسبيستون وديزني 🎨\n" +
		"• `/game` أو `/game كوكتيل` : تشكيلة عشوائية منوعة من كل الأقسام! 🌟\n\n" +
		"⚙️ *التحكم والإعدادات:*\n" +
		"• `/game stop` : إيقاف المسابقة الحالية 🛑\n" +
		"• `/top` : عرض لوحة الصدارة وترتيب أبطال الشات 🏆\n" +
		"• `/game config` : عرض وتعديل عدد الأسئلة ووقت الإجابة ⚙️"
}
