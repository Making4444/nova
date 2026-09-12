package games

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// BankManager loads and manages questions across all categories from data/games/questions/*.json
type BankManager struct {
	questionsDir string
	categories   map[Category][]Question
	allQuestions []Question
	mu           sync.RWMutex
}

// NewBankManager initializes the question bank manager and loads all category files.
func NewBankManager(dataDir string) *BankManager {
	qDir := filepath.Join(dataDir, "games", "questions")
	_ = os.MkdirAll(qDir, 0755)

	bm := &BankManager{
		questionsDir: qDir,
		categories:   make(map[Category][]Question),
		allQuestions: make([]Question, 0),
	}

	bm.LoadAll()
	return bm
}

// LoadAll scans the questions directory and populates the question bank.
func (bm *BankManager) LoadAll() {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	bm.categories = make(map[Category][]Question)
	bm.allQuestions = make([]Question, 0)

	entries, err := os.ReadDir(bm.questionsDir)
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
				continue
			}

			filePath := filepath.Join(bm.questionsDir, entry.Name())
			data, err := os.ReadFile(filePath)
			if err != nil {
				continue
			}

			var qs []Question
			if err := json.Unmarshal(data, &qs); err != nil {
				continue
			}

			for _, q := range qs {
				bm.categories[q.Category] = append(bm.categories[q.Category], q)
				bm.allQuestions = append(bm.allQuestions, q)
			}
		}
	}

	// Fallback to built-in questions if no questions could be loaded from disk
	if len(bm.allQuestions) == 0 {
		for _, q := range FallbackQuestions {
			bm.categories[q.Category] = append(bm.categories[q.Category], q)
			bm.allQuestions = append(bm.allQuestions, q)
		}
	}
}

// GetQuestions returns all available questions for a specific category or all if CategoryMixed/empty.
func (bm *BankManager) GetQuestions(cat Category) []Question {
	bm.mu.RLock()
	defer bm.mu.RUnlock()

	if cat == "" || cat == CategoryMixed {
		res := make([]Question, len(bm.allQuestions))
		copy(res, bm.allQuestions)
		return res
	}

	pool := bm.categories[cat]
	res := make([]Question, len(pool))
	copy(res, pool)
	return res
}

// TotalCount returns the count of loaded questions.
func (bm *BankManager) TotalCount() int {
	bm.mu.RLock()
	defer bm.mu.RUnlock()
	return len(bm.allQuestions)
}

// CategoryCount returns question count for a category.
func (bm *BankManager) CategoryCount(cat Category) int {
	bm.mu.RLock()
	defer bm.mu.RUnlock()
	return len(bm.categories[cat])
}

// Summary returns a formatted summary of questions per category.
func (bm *BankManager) Summary() string {
	bm.mu.RLock()
	defer bm.mu.RUnlock()

	return fmt.Sprintf("الإجمالي: %d سؤال (مسيحية: %d، إفيهات: %d، سينما: %d، كورة: %d، أمثال: %d، ثقافة: %d، ألغاز: %d، علوم: %d، تاريخ: %d، كرتون: %d)",
		len(bm.allQuestions),
		len(bm.categories[CategoryChristian]),
		len(bm.categories[CategoryQuote]),
		len(bm.categories[CategoryMovie]),
		len(bm.categories[CategoryFootball]),
		len(bm.categories[CategoryProverb]),
		len(bm.categories[CategoryTrivia]),
		len(bm.categories[CategoryRiddle]),
		len(bm.categories[CategoryScience]),
		len(bm.categories[CategoryHistory]),
		len(bm.categories[CategoryCartoon]),
	)
}

// FallbackQuestions guarantees the engine runs smoothly in unit tests or fresh checkouts.
var FallbackQuestions = []Question{
	{
		ID:              "fb_chr_01",
		Category:        CategoryChristian,
		Prompt:          "✝️ *كتاب مقدس:*\nمن هو أول شهيد في المسيحية رُجم بالحجارة؟",
		AcceptedAnswers: []string{"استفانوس", "إستفانوس", "القديس استفانوس"},
		Hint:            "أول الشمامسة في سفر أعمال الرسل.",
	},
	{
		ID:              "fb_chr_02",
		Category:        CategoryChristian,
		Prompt:          "✝️ *تاريخ كنسي:*\nمن هو كاروز الديار المصرية ومؤسس الكنيسة القبطية الأرثوذكسية؟",
		AcceptedAnswers: []string{"مارمرقس", "مرقس", "مار مرقس", "القديس مرقس"},
		Hint:            "كاتب الإنجيل الثاني ورمزه الأسد.",
	},
	{
		ID:              "fb_qot_01",
		Category:        CategoryQuote,
		Prompt:          "🎭 *إفيه مصري:*\n\"الراجل ده هيموت متدلع!\"\nمين الفيلم صاحب الإفيه ده؟",
		AcceptedAnswers: []string{"التجربة الدنماركية", "التجربه الدنماركيه", "الدنماركية"},
		Hint:            "عادل إمام ونيكول سابا.",
	},
	{
		ID:              "fb_mov_01",
		Category:        CategoryMovie,
		Prompt:          "🎬 *سينما:*\nما اسم الفيلم الذي لعب فيه كريم عبد العزيز وأحمد عز معاً كأبطال لثورة 1919؟",
		AcceptedAnswers: []string{"كيرة والجن", "كيره والجن"},
		Hint:            "مأخوذ عن رواية 1919 لأحمد مراد.",
	},
	{
		ID:              "fb_ftb_01",
		Category:        CategoryFootball,
		Prompt:          "⚽ *كورة:*\nمن هو الهداف التاريخي لمنتخب مصر الأول لكرة القدم؟",
		AcceptedAnswers: []string{"حسام حسن", "العميد حسام حسن"},
		Hint:            "العميد ومدرب منتخب مصر الحالي.",
	},
	{
		ID:              "fb_prv_01",
		Category:        CategoryProverb,
		Prompt:          "📜 *كمّل المثل الشعبي:*\n\"ابن الوز ...\"",
		AcceptedAnswers: []string{"عوام", "يطلع عوام"},
		Hint:            "اللي بيورث شطارة أبوه.",
	},
	{
		ID:              "fb_trv_01",
		Category:        CategoryTrivia,
		Prompt:          "🌍 *جغرافيا:*\nما هي أكبر قارة في العالم من حيث المساحة والسكان؟",
		AcceptedAnswers: []string{"اسيا", "آسيا"},
		Hint:            "تضم الصين والهند.",
	},
	{
		ID:              "fb_rid_01",
		Category:        CategoryRiddle,
		Prompt:          "🧩 *فزورة:*\nحاجة كل ما تاخد منها تكبر أكتر.. إيه هي؟",
		AcceptedAnswers: []string{"الحفرة", "الحفره", "حفرة"},
		Hint:            "في الأرض لما تفحر.",
	},
	{
		ID:              "fb_sci_01",
		Category:        CategoryScience,
		Prompt:          "🪐 *فضاء:*\nما هو الكوكب الملقب بـ \"الكوكب الأحمر\" في مجموعتنا الشمسية؟",
		AcceptedAnswers: []string{"المريخ", "كوكب المريخ"},
		Hint:            "رابع كواكب المجموعة الشمسية.",
	},
	{
		ID:              "fb_his_01",
		Category:        CategoryHistory,
		Prompt:          "🏛️ *مصر القديمة:*\nمن هو الملك الفرعوني الذي أمر بنحت تمثال أبو الهول بالجيزة؟",
		AcceptedAnswers: []string{"خفرع", "الملك خفرع"},
		Hint:            "صاحب الهرم الأوسط.",
	},
	{
		ID:              "fb_crt_01",
		Category:        CategoryCartoon,
		Prompt:          "🎨 *كرتون وأنمي:*\nما اسم المحقق الذكي الذي تقلص جسده ليصبح طفلاً صغيراً في مدرسة ابتدائية؟",
		AcceptedAnswers: []string{"كونان", "المحقق كونان", "سينشي كودو"},
		Hint:            "سينشي كودو وران موري.",
	},
}
