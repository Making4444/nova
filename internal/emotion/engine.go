package emotion

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
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

	// Aliases for convenience
	MoodJoyful     MoodType = Joyful
	MoodHyped      MoodType = Hyped
	MoodAnnoyed    MoodType = Annoyed
	MoodEmpathetic MoodType = Empathetic
	MoodCalm       MoodType = Calm
	MoodSarcastic  MoodType = Sarcastic
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
	CurrentMood     MoodType           `json:"current_mood"`
	EnergyLevel     int                `json:"energy_level"`    // 1 to 10
	UserAffinities  map[string]int     `json:"user_affinities"` // User JID -> affinity score (0-100; Maker is locked at 100)
	Pleasure        float64            `json:"pleasure"`        // Continuous Pleasure in [-1.0, 1.0]
	Arousal         float64            `json:"arousal"`         // Continuous Arousal in [-1.0, 1.0]
	Dominance       float64            `json:"dominance"`       // Continuous Dominance in [-1.0, 1.0]
	UserAffinity    map[string]float64 `json:"user_affinity"`   // User JID -> continuous affinity in [-1.0, 1.0]
	LastInteraction time.Time          `json:"last_interaction"`
	RecentTriggers  []string           `json:"recent_triggers"`
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

func clampUnit(v float64) float64 {
	if v > 1.0 {
		return 1.0
	}
	if v < -1.0 {
		return -1.0
	}
	return v
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

func (e *Engine) getNow() time.Time {
	e.mu.RLock()
	fn := e.nowFunc
	e.mu.RUnlock()
	if fn != nil {
		return fn()
	}
	return time.Now()
}

func cloneState(st *EmotionalState) *EmotionalState {
	if st == nil {
		return nil
	}
	cp := &EmotionalState{
		CurrentMood:     st.CurrentMood,
		EnergyLevel:     st.EnergyLevel,
		UserAffinities:  make(map[string]int, len(st.UserAffinities)),
		Pleasure:        st.Pleasure,
		Arousal:         st.Arousal,
		Dominance:       st.Dominance,
		UserAffinity:    make(map[string]float64, len(st.UserAffinity)),
		LastInteraction: st.LastInteraction,
		RecentTriggers:  make([]string, len(st.RecentTriggers)),
	}
	for k, v := range st.UserAffinities {
		cp.UserAffinities[k] = v
	}
	for k, v := range st.UserAffinity {
		cp.UserAffinity[k] = v
	}
	copy(cp.RecentTriggers, st.RecentTriggers)
	return cp
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
			Pleasure:        0.2,
			Arousal:         0.1,
			Dominance:       0.2,
			UserAffinity:    make(map[string]float64),
			LastInteraction: time.Now(),
			RecentTriggers:  make([]string, 0),
		}
	}
	cmu := e.getChatMutex(chatID)
	cmu.Lock()
	defer cmu.Unlock()

	return cloneState(e.getStateLocked(chatID))
}

func (e *Engine) getStateLocked(chatID string) *EmotionalState {
	e.mu.RLock()
	st, ok := e.states[chatID]
	e.mu.RUnlock()
	if ok && st != nil {
		if st.UserAffinities == nil {
			st.UserAffinities = make(map[string]int)
		}
		if st.UserAffinity == nil {
			st.UserAffinity = make(map[string]float64)
		}
		return st
	}

	filePath := e.getFilePath(chatID)
	if data, err := os.ReadFile(filePath); err == nil {
		var loaded EmotionalState
		if err := json.Unmarshal(data, &loaded); err == nil {
			if loaded.UserAffinities == nil {
				loaded.UserAffinities = make(map[string]int)
			}
			if loaded.UserAffinity == nil {
				loaded.UserAffinity = make(map[string]float64)
			}
			if loaded.EnergyLevel <= 0 {
				loaded.EnergyLevel = 7
			}
			if loaded.CurrentMood == "" {
				loaded.CurrentMood = Joyful
			}
			e.mu.Lock()
			e.states[chatID] = &loaded
			e.mu.Unlock()
			return &loaded
		}
	}

	now := e.getNow()

	newSt := &EmotionalState{
		CurrentMood:     Joyful,
		EnergyLevel:     7,
		UserAffinities:  make(map[string]int),
		Pleasure:        0.2,
		Arousal:         0.1,
		Dominance:       0.2,
		UserAffinity:    make(map[string]float64),
		LastInteraction: now,
		RecentTriggers:  make([]string, 0),
	}
	e.mu.Lock()
	e.states[chatID] = newSt
	e.mu.Unlock()
	return newSt
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
			Pleasure:        0.2,
			Arousal:         0.1,
			Dominance:       0.2,
			UserAffinity:    make(map[string]float64),
			LastInteraction: time.Now(),
			RecentTriggers:  make([]string, 0),
		}
	}
	cmu := e.getChatMutex(chatID)
	cmu.Lock()
	defer cmu.Unlock()

	st := e.getStateLocked(chatID)
	now := e.getNow()

	// Smooth PAD decay toward baseline (Pleasure: 0.2, Arousal: 0.1, Dominance: 0.2)
	st.Pleasure = clampUnit(st.Pleasure*0.85 + 0.2*0.15)
	st.Arousal = clampUnit(st.Arousal*0.85 + 0.1*0.15)
	st.Dominance = clampUnit(st.Dominance*0.85 + 0.2*0.15)

	// 1. Maintain user affinities (both integer 0-100 and continuous [-1.0, 1.0])
	if senderID != "" {
		if isMaker {
			st.UserAffinities[senderID] = 100 // Maker is permanently locked at 100
			st.UserAffinity[senderID] = 1.0
		} else {
			if _, exists := st.UserAffinities[senderID]; !exists {
				st.UserAffinities[senderID] = 50 // Default baseline affinity for new users
			}
			if _, exists := st.UserAffinity[senderID]; !exists {
				st.UserAffinity[senderID] = 0.0
			}
		}
	}

	sentLower := strings.ToLower(strings.TrimSpace(sentiment))
	textLower := strings.ToLower(messageText)

	isSad := detectSadness(sentLower, textLower)
	isEnergetic := detectEnergeticBanter(sentLower, textLower)
	isRude := detectRude(sentLower, textLower)
	isSweet := detectSweet(sentLower, textLower)

	// 2. Emotional state transitions and PAD shifts
	if isSad {
		st.CurrentMood = Empathetic
		st.EnergyLevel = 4
		st.Pleasure = clampUnit(st.Pleasure - 0.50)
		st.Arousal = clampUnit(st.Arousal - 0.30)
		st.Dominance = clampUnit(0.10)
		if senderID != "" && !isMaker {
			st.UserAffinity[senderID] = clampUnit(st.UserAffinity[senderID] + 0.05)
		}
		e.addTrigger(st, "حزن أو تعب أو مشكلة عند المتحدث (مواساة وتضامن)")
	} else if isEnergetic {
		st.CurrentMood = Hyped
		st.EnergyLevel = 9
		st.Pleasure = clampUnit(st.Pleasure + 0.35)
		st.Arousal = clampUnit(st.Arousal + 0.55)
		st.Dominance = clampUnit(st.Dominance + 0.25)
		if senderID != "" && !isMaker {
			st.UserAffinity[senderID] = clampUnit(st.UserAffinity[senderID] + 0.10)
		}
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
		st.Pleasure = clampUnit(st.Pleasure - 0.45)
		st.Arousal = clampUnit(st.Arousal + 0.35)
		st.Dominance = clampUnit(st.Dominance + 0.35)
		if senderID != "" && !isMaker {
			st.UserAffinities[senderID] -= 10
			if st.UserAffinities[senderID] < 0 {
				st.UserAffinities[senderID] = 0
			}
			st.UserAffinity[senderID] = clampUnit(st.UserAffinity[senderID] - 0.20)
		}
		e.addTrigger(st, "كلام مستفز أو إهانة (تقليل الود ولسان حامي)")
	} else if isSweet {
		st.CurrentMood = Joyful
		st.EnergyLevel = 8
		st.Pleasure = clampUnit(st.Pleasure + 0.40)
		st.Arousal = clampUnit(st.Arousal + 0.20)
		st.Dominance = clampUnit(st.Dominance + 0.15)
		if senderID != "" && !isMaker {
			st.UserAffinities[senderID] += 10
			if st.UserAffinities[senderID] > 100 {
				st.UserAffinities[senderID] = 100
			}
			st.UserAffinity[senderID] = clampUnit(st.UserAffinity[senderID] + 0.20)
		}
		e.addTrigger(st, "تقدير ومدح وذوق عالي من المتحدث (زيادة الود والبهجة)")
	}

	// 3. Time-of-day awareness (Night / Late hours lower energy)
	hour := now.Hour()
	if hour >= 1 && hour < 6 {
		if st.EnergyLevel > 4 {
			st.EnergyLevel = 4
		}
		if st.Arousal > -0.2 {
			st.Arousal = -0.2
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
	st.Pleasure = clampUnit(st.Pleasure)
	st.Arousal = clampUnit(st.Arousal)
	st.Dominance = clampUnit(st.Dominance)

	if senderID != "" && isMaker {
		st.UserAffinities[senderID] = 100
		st.UserAffinity[senderID] = 1.0
	}

	st.LastInteraction = now

	// 5. Persist to disk
	_ = e.saveStateLocked(chatID, st)

	return cloneState(st)
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
	affinityFloat := 0.0
	if isMaker {
		affinity = 100
		affinityFloat = 1.0
	} else if senderID != "" {
		if st.UserAffinities != nil {
			if val, exists := st.UserAffinities[senderID]; exists {
				affinity = val
			}
		}
		if st.UserAffinity != nil {
			if val, exists := st.UserAffinity[senderID]; exists {
				affinityFloat = val
			}
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

	padSummary := fmt.Sprintf("\n- **إحداثيات الانفعال (PAD):** بهجة=%.2f | يقظة=%.2f | هيمنة=%.2f | مؤشر الألفة=%.2f",
		st.Pleasure, st.Arousal, st.Dominance, affinityFloat)

	var triggersSummary string
	if len(st.RecentTriggers) > 0 {
		triggersSummary = fmt.Sprintf("\n- **آخر مؤثرات نفسية بالشات:** %s", strings.Join(st.RecentTriggers, " | "))
	}

	return fmt.Sprintf(`### 🎭 الحالة المزاجية والانفعالية الحالية لنوفا (Nova's Emotional State):
- **المزاج العام:** %s
- **مستوى الطاقة:** %d/10 - %s
- **طبيعة العلاقة مع المتحدث:** %s%s%s
- **توجيه النبرة والسلوك:** %s`,
		moodDesc,
		st.EnergyLevel,
		energyDesc,
		relationshipDesc,
		padSummary,
		triggersSummary,
		directive,
	)
}

// Arabic Negation & Detection helpers

var arabicNegationParticles = map[string]bool{
	"مش":      true,
	"ما":      true,
	"مبقتش":   true,
	"مابقيتش": true,
	"مبقتيش":  true,
	"بطلت":    true,
	"بطلنا":   true,
	"غير":     true,
	"لا":      true,
	"ولا":     true,
	"لم":      true,
	"لن":      true,
	"بدون":    true,
	"منيش":    true,
	"مانيش":   true,
	"معدتش":   true,
}

var arabicContrastParticles = map[string]bool{
	"بس":   true,
	"لكن":  true,
	"ولكن": true,
	"انما": true,
	"إنما": true,
}

func normalizeNegWord(w string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(w)) {
		if (r >= 0x064B && r <= 0x065F) || r == 0x0670 || r == 0x0640 {
			continue
		}
		if unicode.IsPunct(r) || unicode.IsSymbol(r) {
			continue
		}
		switch r {
		case 'أ', 'إ', 'آ', 'ٱ':
			b.WriteRune('ا')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// isNegatedKeyword checks if the occurrences of kw in text are preceded within 1-2 words
// by an Arabic negation particle ("مش ", "ما ", "مبقتش ", "بطلت ", "غير ", "لا ", "ولا ").
func isNegatedKeyword(text, kw string) bool {
	if text == "" || kw == "" {
		return false
	}
	idx := strings.Index(text, kw)
	if idx < 0 {
		return false
	}

	searchFrom := 0
	foundAny := false
	allNegated := true

	for searchFrom < len(text) {
		relIdx := strings.Index(text[searchFrom:], kw)
		if relIdx < 0 {
			break
		}
		absIdx := searchFrom + relIdx
		foundAny = true

		prefix := text[:absIdx]
		// Clean punctuation in prefix into spaces
		var pb strings.Builder
		for _, r := range prefix {
			if unicode.IsPunct(r) || unicode.IsSymbol(r) {
				pb.WriteRune(' ')
			} else {
				pb.WriteRune(r)
			}
		}
		words := strings.Fields(pb.String())

		occNegated := false
		if len(words) >= 1 {
			w1 := normalizeNegWord(words[len(words)-1])
			if arabicNegationParticles[w1] {
				occNegated = true
			} else if len(words) >= 2 && !arabicContrastParticles[w1] {
				w2 := normalizeNegWord(words[len(words)-2])
				if arabicNegationParticles[w2] {
					occNegated = true
				}
			}
		}

		if !occNegated {
			allNegated = false
			break
		}

		searchFrom = absIdx + len(kw)
	}

	return foundAny && allNegated
}

func detectSadness(sentiment, text string) bool {
	sadKeywords := []string{
		"تعبان", "عيان", "مريض", "في المستشفى", "سخن", "متضايق", "زعلان", "حزين", "مخنوق",
		"بموت", "ادعيلي", "واقع في مشكلة", "مصيبة", "وفاة", "اتوفى", "لا قدر الله",
		"البقاء لله", "ربنا يرحمه", "خسرت", "مكتئب", "مقهور", "ضايع", "مخنوقة", "تعبانة",
		"زعلانة", "عيانة", "مريضة", "عندي مشكلة", "حادثة", "انفصلنا", "سقطت",
	}

	hasNegatedSad := false
	for _, kw := range sadKeywords {
		if strings.Contains(text, kw) {
			if !isNegatedKeyword(text, kw) {
				return true
			}
			hasNegatedSad = true
		}
	}

	// Check standalone "مات" safely
	paddedText := " " + text + " "
	if !strings.Contains(text, "ماتش") && strings.Contains(paddedText, " مات ") {
		if !isNegatedKeyword(text, "مات") {
			return true
		}
		hasNegatedSad = true
	}

	if hasNegatedSad {
		return false
	}

	if strings.Contains(sentiment, "sad") || strings.Contains(sentiment, "sick") ||
		strings.Contains(sentiment, "trouble") || strings.Contains(sentiment, "empath") ||
		strings.Contains(sentiment, "grief") || strings.Contains(sentiment, "depress") {
		return true
	}

	return false
}

func detectEnergeticBanter(sentiment, text string) bool {
	hypeKeywords := []string{
		"يلا", "عاش", "يا بطل", "يا وحش", "حماس", "جامد", "ولعت", "نار", "خربانة",
		"سهرة", "ماتش", "جون", "كسبنا", "فوزنا", "مبروك", "احتفال", "هوبا", "بيس",
		"عظمة", "أوبا", "طرش", "حريقة", "عاش يا رجالة", "بوم", "🚀", "🔥", "🎉", "🥳", "⚡",
	}

	hasNegatedHype := false
	for _, kw := range hypeKeywords {
		if strings.Contains(text, kw) {
			if !isNegatedKeyword(text, kw) {
				return true
			}
			hasNegatedHype = true
		}
	}

	if hasNegatedHype {
		return false
	}

	if strings.Contains(sentiment, "hype") || strings.Contains(sentiment, "energet") ||
		strings.Contains(sentiment, "excit") || strings.Contains(sentiment, "banter") ||
		strings.Contains(sentiment, "fun") {
		return true
	}

	return false
}

func detectRude(sentiment, text string) bool {
	rudeKeywords := []string{
		"غبي", "غبية", "اخرس", "اسكت", "تافه", "بوت عبيط", "زفت", "حمار", "يا حمار",
		"يا غبي", "قليل الادب", "مقرف", "بطل رغي", "صدعتنا", "اكتم", "انطم", "سخيف",
		"رخم", "غتيت", "مستفز", "هبل", "مجنون", "أهبل", "حيوان",
	}

	hasNegatedRude := false
	for _, kw := range rudeKeywords {
		if strings.Contains(text, kw) {
			if !isNegatedKeyword(text, kw) {
				return true
			}
			hasNegatedRude = true
		}
	}

	if hasNegatedRude {
		return false
	}

	if strings.Contains(sentiment, "rude") || strings.Contains(sentiment, "annoy") ||
		strings.Contains(sentiment, "toxic") || strings.Contains(sentiment, "insult") ||
		strings.Contains(sentiment, "angry") {
		return true
	}

	return false
}

func detectSweet(sentiment, text string) bool {
	sweetKeywords := []string{
		"تسلم", "شكرا", "حبيبي", "يا غالي", "ربنا يخليك", "بحبك", "منور", "جدع",
		"كلك ذوق", "اصيل", "يا برنس", "ما قصرت", "تسلم ايدك", "كتر خيرك", "ربنا يسعدك",
		"ربنا يحفظك", "احلى نوفا", "يا عسل", "سكر", "قمر", "الله يباركلك", "كفو",
		"ذوق", "❤️", "💖", "🥰", "🌹",
	}

	hasNegatedSweet := false
	for _, kw := range sweetKeywords {
		if strings.Contains(text, kw) {
			if !isNegatedKeyword(text, kw) {
				return true
			}
			hasNegatedSweet = true
		}
	}

	if hasNegatedSweet {
		return false
	}

	if strings.Contains(sentiment, "sweet") || strings.Contains(sentiment, "appreciat") ||
		strings.Contains(sentiment, "loving") || strings.Contains(sentiment, "friendly") ||
		strings.Contains(sentiment, "grateful") || strings.Contains(sentiment, "joy") ||
		strings.Contains(sentiment, "happy") {
		return true
	}

	return false
}
