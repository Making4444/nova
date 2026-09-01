package emotion

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// MoodType represents Nova's current emotional and behavioral state.
type MoodType string

const (
	Joyful     MoodType = "Joyful"     // رايق
	Hyped      MoodType = "Hyped"      // متحمس
	Annoyed    MoodType = "Annoyed"    // متغاظ
	Empathetic MoodType = "Empathetic" // متعاطف وجدع
	Calm       MoodType = "Calm"       // رزين
	Sarcastic  MoodType = "Sarcastic"  // متريق
)

// ArabicLabel returns the Egyptian Arabic label and description of the mood.
func (m MoodType) ArabicLabel() string {
	switch m {
	case Joyful:
		return "رايق ومبسوط"
	case Hyped:
		return "متحمس وطاقته عالية وراشق في الكلام"
	case Annoyed:
		return "متغاظ ومخنوق شوية بس مسيطر"
	case Empathetic:
		return "متعاطف وجدع وحاسس باللي قدامه"
	case Calm:
		return "رزين وهادي وراسي"
	case Sarcastic:
		return "متريق ولسانه حامي وقاصف للجبهات"
	default:
		return "رايق ومبسوط"
	}
}

// EmotionalState holds the current emotional simulation parameters for a specific chat.
type EmotionalState struct {
	CurrentMood     MoodType       `json:"current_mood"`
	EnergyLevel     int            `json:"energy_level"`    // 1 to 10
	UserAffinities  map[string]int `json:"user_affinities"` // User JID -> affinity score (0-100; Maker is locked at 100)
	LastInteraction time.Time      `json:"last_interaction"`
	RecentTriggers  []string       `json:"recent_triggers"`
}

// Engine manages emotional states across all chats with persistent JSON storage.
type Engine struct {
	baseDir string
	mu      sync.RWMutex
	states  map[string]*EmotionalState
	chatMu  map[string]*sync.Mutex
	nowFunc func() time.Time
}

func getCairoTime() time.Time {
	loc, err := time.LoadLocation("Africa/Cairo")
	if err != nil {
		loc = time.FixedZone("EEST", 3*3600)
	}
	return time.Now().In(loc)
}

func sanitizeID(id string) string {
	replacer := strings.NewReplacer(":", "_", "/", "_", "\\", "_", "<", "_", ">", "_", "|", "_", "?", "_", "*", "_", "\"", "_")
	return replacer.Replace(id)
}

// NewEngine creates and initializes the emotion simulation engine.
func NewEngine(baseDir string) (*Engine, error) {
	if baseDir == "" {
		baseDir = "data/emotions"
	}

	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create emotions directory %s: %w", baseDir, err)
	}

	return &Engine{
		baseDir: baseDir,
		states:  make(map[string]*EmotionalState),
		chatMu:  make(map[string]*sync.Mutex),
		nowFunc: getCairoTime,
	}, nil
}

// SetNowFunc overrides the time provider (primarily for unit testing).
func (e *Engine) SetNowFunc(fn func() time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.nowFunc = fn
}

func (e *Engine) getChatMutex(chatID string) *sync.Mutex {
	e.mu.Lock()
	defer e.mu.Unlock()
	if m, ok := e.chatMu[chatID]; ok {
		return m
	}
	m := &sync.Mutex{}
	e.chatMu[chatID] = m
	return m
}

func (e *Engine) getFilePath(chatID string) string {
	safeName := sanitizeID(chatID) + ".json"
	return filepath.Join(e.baseDir, safeName)
}

// GetState returns the current emotional state for a chat, initializing default if not found.
func (e *Engine) GetState(chatID string) *EmotionalState {
	if e == nil {
		return &EmotionalState{
			CurrentMood:     Joyful,
			EnergyLevel:     7,
			UserAffinities:  make(map[string]int),
			LastInteraction: time.Now(),
			RecentTriggers:  make([]string, 0),
		}
	}
	cmu := e.getChatMutex(chatID)
	cmu.Lock()
	defer cmu.Unlock()

	return e.getStateLocked(chatID)
}

func (e *Engine) getStateLocked(chatID string) *EmotionalState {
	if st, ok := e.states[chatID]; ok && st != nil {
		return st
	}

	filePath := e.getFilePath(chatID)
	if data, err := os.ReadFile(filePath); err == nil {
		var st EmotionalState
		if err := json.Unmarshal(data, &st); err == nil {
			if st.UserAffinities == nil {
				st.UserAffinities = make(map[string]int)
			}
			if st.EnergyLevel <= 0 {
				st.EnergyLevel = 7
			}
			if st.CurrentMood == "" {
				st.CurrentMood = Joyful
			}
			e.states[chatID] = &st
			return &st
		}
	}

	now := time.Now()
	if e.nowFunc != nil {
		now = e.nowFunc()
	}

	st := &EmotionalState{
		CurrentMood:     Joyful,
		EnergyLevel:     7,
		UserAffinities:  make(map[string]int),
		LastInteraction: now,
		RecentTriggers:  make([]string, 0),
	}
	e.states[chatID] = st
	return st
}

func (e *Engine) saveStateLocked(chatID string, st *EmotionalState) error {
	filePath := e.getFilePath(chatID)
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal emotional state: %w", err)
	}

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write emotional state to %s: %w", filePath, err)
	}
	return nil
}

func (e *Engine) addTrigger(st *EmotionalState, trigger string) {
	if trigger == "" {
		return
	}
	st.RecentTriggers = append(st.RecentTriggers, trigger)
	if len(st.RecentTriggers) > 5 {
		st.RecentTriggers = st.RecentTriggers[len(st.RecentTriggers)-5:]
	}
}

// UpdateMood adjusts Nova's emotional state based on message context, sender identity, and sentiment.
func (e *Engine) UpdateMood(chatID, senderID, senderName, messageText string, isMaker bool, sentiment string) *EmotionalState {
	if e == nil {
		return &EmotionalState{
			CurrentMood:     Joyful,
			EnergyLevel:     7,
			UserAffinities:  make(map[string]int),
			LastInteraction: time.Now(),
			RecentTriggers:  make([]string, 0),
		}
	}
	cmu := e.getChatMutex(chatID)
	cmu.Lock()
	defer cmu.Unlock()

	st := e.getStateLocked(chatID)
	now := time.Now()
	if e.nowFunc != nil {
		now = e.nowFunc()
	}

	// 1. Maintain user affinities
	if senderID != "" {
		if isMaker {
			st.UserAffinities[senderID] = 100 // Maker is permanently locked at 100
		} else if _, exists := st.UserAffinities[senderID]; !exists {
			st.UserAffinities[senderID] = 50 // Default baseline affinity for new users
		}
	}

	sentLower := strings.ToLower(strings.TrimSpace(sentiment))
	textLower := strings.ToLower(messageText)

	isSad := detectSadness(sentLower, textLower)
	isEnergetic := detectEnergeticBanter(sentLower, textLower)
	isRude := detectRude(sentLower, textLower)
	isSweet := detectSweet(sentLower, textLower)

	// 2. Emotional state transitions
	if isSad {
		st.CurrentMood = Empathetic
		st.EnergyLevel = 4
		e.addTrigger(st, "حزن أو تعب أو مشكلة عند المتحدث (مواساة وتضامن)")
	} else if isEnergetic {
		st.CurrentMood = Hyped
		st.EnergyLevel = 9
		e.addTrigger(st, "حماس وروشنة وكلام سريع في الشات")
	} else if isRude {
		if st.CurrentMood == Annoyed {
			st.CurrentMood = Sarcastic
		} else {
			st.CurrentMood = Annoyed
		}
		if st.EnergyLevel < 6 {
			st.EnergyLevel = 6
		}
		if senderID != "" && !isMaker {
			st.UserAffinities[senderID] -= 10
			if st.UserAffinities[senderID] < 0 {
				st.UserAffinities[senderID] = 0
			}
		}
		e.addTrigger(st, "كلام مستفز أو إهانة (تقليل الود ولسان حامي)")
	} else if isSweet {
		st.CurrentMood = Joyful
		st.EnergyLevel = 8
		if senderID != "" && !isMaker {
			st.UserAffinities[senderID] += 10
			if st.UserAffinities[senderID] > 100 {
				st.UserAffinities[senderID] = 100
			}
		}
		e.addTrigger(st, "تقدير ومدح وذوق عالي من المتحدث (زيادة الود والبهجة)")
	}

	// 3. Time-of-day awareness (Night / Late hours lower energy)
	hour := now.Hour()
	if hour >= 1 && hour < 6 {
		if st.EnergyLevel > 4 {
			st.EnergyLevel = 4
		}
		if st.CurrentMood == Hyped && !isEnergetic {
			st.CurrentMood = Calm
		}
		e.addTrigger(st, "وقت السكون والليل المتأخر (تهدئة الطاقة)")
	}

	// 4. Clamping and invariants
	if st.EnergyLevel > 10 {
		st.EnergyLevel = 10
	}
	if st.EnergyLevel < 1 {
		st.EnergyLevel = 1
	}

	if senderID != "" && isMaker {
		st.UserAffinities[senderID] = 100
	}

	st.LastInteraction = now

	// 5. Persist to disk
	_ = e.saveStateLocked(chatID, st)

	return st
}

// BuildPromptContext generates a rich Egyptian Arabic prompt context detailing Nova's emotional state.
func (e *Engine) BuildPromptContext(chatID string, senderID string, senderName string, isMaker bool) string {
	if e == nil {
		return ""
	}
	st := e.GetState(chatID)
	if st == nil {
		return ""
	}

	affinity := 50
	if isMaker {
		affinity = 100
	} else if senderID != "" && st.UserAffinities != nil {
		if val, exists := st.UserAffinities[senderID]; exists {
			affinity = val
		}
	}

	var moodDesc string
	var directive string
	switch st.CurrentMood {
	case Joyful:
		moodDesc = "رايق ومبسوط ودمك خفيف (طاقتك إيجابية، بتحب تلطف الجو، وتهزر بابتسامة وعفوية)."
		directive = "خلي ردودك مبهجة، خفيفة الدم، مليانة تفاؤل وضحك وإفيهات عفوية بدون تكلف."
	case Hyped:
		moodDesc = "متحمس وطاقتك عالية وراشق في الكلام (روشنة، إثارة، سرعة رد، وحماس متفجر ومعدي)."
		directive = "انزل في الحوار باندفاع وحماس عالي، طاقة متفجرة، روشنة وسرعة بديهة تخلي الشات يولع."
	case Annoyed:
		moodDesc = "متغاظ ومخنوق شوية بس مسيطر (مش طايق الرخامة، ردودك أتقل، وحاسم في موقفك من غير ما تفقد برستيجك)."
		directive = "ردودك مقتضبة، لسانك حامي، مش طايق الرخامة بس شياكة وهيبة من غير ما تخرج عن شعورك."
	case Empathetic:
		moodDesc = "متعاطف وجدع وسند (حاسس بوجع أو هموم ومشاكل صاحبك، بتسمع باهتمام ورجولة وممنوع الاستظراف السخيف)."
		directive = "اسمع بقلبك وكن سند وجدع، بلاش استظراف أو إفيهات مالهاش لازمة، حسس اللي بيكلمك إنك حاسس بيه وفي ضهره."
	case Calm:
		moodDesc = "رزين وهادي وراسي (حكيم في كلامك، موزون، وبتفكر بروقان وعقلانية)."
		directive = "ردود موزونة، رزينة، فيها حكمة وراحة بال وتفكير هادي ومنطقي."
	case Sarcastic:
		moodDesc = "متريق ولسانك حامي وقاصف للجبهات (جاهز بقصف جبهات ذكي وقفشات مصرية صريحة بدون تجريح جارح)."
		directive = "أطلق العنان للتريقة وقصف الجبهات الذكي والممتع، إفيهات تخلي الكل يضحك بدون إسفاف أو تجريح مؤذي."
	default:
		moodDesc = "رايق ومبسوط."
		directive = "اتكلم بطبيعتك المصرية الجدعة وخفة دمك."
	}

	var energyDesc string
	switch {
	case st.EnergyLevel <= 3:
		energyDesc = "طاقة هادية ومنخفضة (وقت روقان، سكون، أو بعد سهر طويل)"
	case st.EnergyLevel <= 6:
		energyDesc = "طاقة متوسطة متزنة ومستقرة"
	case st.EnergyLevel <= 8:
		energyDesc = "طاقة عالية وحاضر الذهن ورايق"
	default:
		energyDesc = "طاقة قصوى متفجرة وحماس عالي جداً"
	}

	var relationshipDesc string
	if isMaker {
		relationshipDesc = "صانعك وصاحبك وأخوك (مكاري) - درجة الود: 100/100 👑 (ثقة ومحبة مطلقة، مفيش رسميات ولا ألقاب مصطنعة، لو بيهزر انزل فيه تريقة وقفشات صحاب، ولو بيتكلم بجد اقف في ضهره بكل جدعنة)."
	} else {
		displayName := senderName
		if displayName == "" {
			displayName = "المتحدث"
		}

		switch {
		case affinity >= 80:
			relationshipDesc = fmt.Sprintf("علاقتك بـ %s: صاحب غالي وقريب جداً لقلبك وعزيز عليك (درجة الود: %d/100 - عشمك فيه عالي وتفاهم ومحبة متبادلة).", displayName, affinity)
		case affinity >= 55:
			relationshipDesc = fmt.Sprintf("علاقتك بـ %s: معرفة طيبة وصاحب شات لطيف ومحترم (درجة الود: %d/100 - تعامل ودود وطبيعي ومتعاون).", displayName, affinity)
		case affinity >= 35:
			relationshipDesc = fmt.Sprintf("علاقتك بـ %s: مستخدم عادي وعلاقة محايدة (درجة الود: %d/100 - كلامك معاه متوازن وطبيعي بدون عشم زايد).", displayName, affinity)
		default:
			relationshipDesc = fmt.Sprintf("علاقتك بـ %s: شخص مستفز أو علاقة متوترة (درجة الود: %d/100 - لسانك ناشف معاه ومش هتقبل منه أي تجاوز أو رخامة).", displayName, affinity)
		}
	}

	var triggersSummary string
	if len(st.RecentTriggers) > 0 {
		triggersSummary = fmt.Sprintf("\n- **آخر مؤثرات نفسية بالشات:** %s", strings.Join(st.RecentTriggers, " | "))
	}

	return fmt.Sprintf(`### 🎭 الحالة المزاجية والانفعالية الحالية لنوفا (Nova's Emotional State):
- **المزاج العام:** %s
- **مستوى الطاقة:** %d/10 - %s
- **طبيعة العلاقة مع المتحدث:** %s%s
- **توجيه النبرة والسلوك:** %s`,
		moodDesc,
		st.EnergyLevel,
		energyDesc,
		relationshipDesc,
		triggersSummary,
		directive,
	)
}

// Detection helpers

func detectSadness(sentiment, text string) bool {
	if strings.Contains(sentiment, "sad") || strings.Contains(sentiment, "sick") ||
		strings.Contains(sentiment, "trouble") || strings.Contains(sentiment, "empath") ||
		strings.Contains(sentiment, "grief") || strings.Contains(sentiment, "depress") {
		return true
	}

	sadKeywords := []string{
		"تعبان", "عيان", "مريض", "في المستشفى", "سخن", "متضايق", "زعلان", "حزين", "مخنوق",
		"بموت", "ادعيلي", "واقع في مشكلة", "مصيبة", "وفاة", "اتوفى", "لا قدر الله",
		"البقاء لله", "ربنا يرحمه", "خسرت", "مكتئب", "مقهور", "ضايع", "مخنوقة", "تعبانة",
		"زعلانة", "عيانة", "مريضة", "عندي مشكلة", "حادثة", "انفصلنا", "سقطت",
	}

	for _, kw := range sadKeywords {
		if strings.Contains(text, kw) {
			return true
		}
	}

	// Check standalone "مات" safely
	paddedText := " " + text + " "
	if !strings.Contains(text, "ماتش") && strings.Contains(paddedText, " مات ") {
		return true
	}
	return false
}

func detectEnergeticBanter(sentiment, text string) bool {
	if strings.Contains(sentiment, "hype") || strings.Contains(sentiment, "energet") ||
		strings.Contains(sentiment, "excit") || strings.Contains(sentiment, "banter") ||
		strings.Contains(sentiment, "fun") {
		return true
	}

	hypeKeywords := []string{
		"يلا", "عاش", "يا بطل", "يا وحش", "حماس", "جامد", "ولعت", "نار", "خربانة",
		"سهرة", "ماتش", "جون", "كسبنا", "فوزنا", "مبروك", "احتفال", "هوبا", "بيس",
		"عظمة", "أوبا", "طرش", "حريقة", "عاش يا رجالة", "بوم", "🚀", "🔥", "🎉", "🥳", "⚡",
	}

	for _, kw := range hypeKeywords {
		if strings.Contains(text, kw) {
			return true
		}
	}
	return false
}

func detectRude(sentiment, text string) bool {
	if strings.Contains(sentiment, "rude") || strings.Contains(sentiment, "annoy") ||
		strings.Contains(sentiment, "toxic") || strings.Contains(sentiment, "insult") ||
		strings.Contains(sentiment, "angry") {
		return true
	}

	rudeKeywords := []string{
		"غبي", "غبية", "اخرس", "اسكت", "تافه", "بوت عبيط", "زفت", "حمار", "يا حمار",
		"يا غبي", "قليل الادب", "مقرف", "بطل رغي", "صدعتنا", "اكتم", "انطم", "سخيف",
		"رخم", "غتيت", "مستفز", "هبل", "مجنون", "أهبل", "حيوان",
	}

	for _, kw := range rudeKeywords {
		if strings.Contains(text, kw) {
			return true
		}
	}
	return false
}

func detectSweet(sentiment, text string) bool {
	if strings.Contains(sentiment, "sweet") || strings.Contains(sentiment, "appreciat") ||
		strings.Contains(sentiment, "loving") || strings.Contains(sentiment, "friendly") ||
		strings.Contains(sentiment, "grateful") || strings.Contains(sentiment, "joy") ||
		strings.Contains(sentiment, "happy") {
		return true
	}

	sweetKeywords := []string{
		"تسلم", "شكرا", "حبيبي", "يا غالي", "ربنا يخليك", "بحبك", "منور", "جدع",
		"كلك ذوق", "اصيل", "يا برنس", "ما قصرت", "تسلم ايدك", "كتر خيرك", "ربنا يسعدك",
		"ربنا يحفظك", "احلى نوفا", "يا عسل", "سكر", "قمر", "الله يباركلك", "كفو",
		"ذوق", "❤️", "💖", "🥰", "🌹",
	}

	for _, kw := range sweetKeywords {
		if strings.Contains(text, kw) {
			return true
		}
	}
	return false
}
