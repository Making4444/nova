package curriculum

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"unicode"
)

// Lesson defines the metadata of a single lesson in the curriculum.
type Lesson struct {
	Chapter     string `json:"chapter"`
	LessonTitle string `json:"lesson_title"`
	StartPage   int    `json:"start_page"`
	EndPage     int    `json:"end_page"`
	Summary     string `json:"summary"`
	PDFFile     string `json:"pdf_file,omitempty"`
}

// SubjectIndex represents the contents of an index.json file for a single book/subject.
type SubjectIndex struct {
	BookTitle       string   `json:"book_title"`
	Grade           string   `json:"grade"`
	SubjectCategory string   `json:"subject_category"`
	Part            string   `json:"part"`
	TotalLessons    int      `json:"total_lessons"`
	Lessons         []Lesson `json:"lessons"`
}

// Service manages access to the curriculum files, indices, and lesson PDF text extraction.
type Service struct {
	dataDir   string
	configDir string
	mu        sync.RWMutex
	cache     map[string]*SubjectIndex
}

// NewService creates a new Curriculum Service.
func NewService(dataDir, configDir string) *Service {
	if dataDir == "" {
		dataDir = "data/curriculum"
	}
	if configDir == "" {
		configDir = "config/curriculum"
	}
	return &Service{
		dataDir:   dataDir,
		configDir: configDir,
		cache:     make(map[string]*SubjectIndex),
	}
}

// NormalizeArabic cleans and standardizes Arabic strings for flexible fuzzy matching.
func NormalizeArabic(text string) string {
	s := strings.ToLower(strings.TrimSpace(text))

	// Remove diacritics (tashkeel)
	var b strings.Builder
	for _, r := range s {
		if !unicode.In(r, unicode.Mn) {
			b.WriteRune(r)
		}
	}
	cleaned := b.String()

	// Normalize alif variants to ا
	alifRegex := regexp.MustCompile(`[أإآٱ]`)
	cleaned = alifRegex.ReplaceAllString(cleaned, "ا")

	// Normalize yaa variants
	cleaned = strings.ReplaceAll(cleaned, "ى", "ي")

	// Normalize taa marbuta
	cleaned = strings.ReplaceAll(cleaned, "ة", "ه")

	// Replace underscores and dashes with spaces
	cleaned = strings.ReplaceAll(cleaned, "_", " ")
	cleaned = strings.ReplaceAll(cleaned, "-", " ")

	// Collapse multiple spaces
	spaceRegex := regexp.MustCompile(`\s+`)
	cleaned = spaceRegex.ReplaceAllString(cleaned, " ")

	return strings.TrimSpace(cleaned)
}

// ResolveGrade maps various grade queries (e.g. "اولى ثانوي", "الصف الأول", "1ث") to canonical folder names.
func ResolveGrade(query string) string {
	norm := NormalizeArabic(query)

	if strings.Contains(norm, "ثاني") || strings.Contains(norm, "تاني") || strings.Contains(norm, "2") {
		return "الصف_الثاني_الثانوي"
	}

	if strings.Contains(norm, "اول") || strings.Contains(norm, "1") {
		return "الصف_الأول_الثانوي"
	}

	return "الصف_الأول_الثانوي"
}

// FindSubjectFolders locates all directories matching a given grade and subject on disk, ranked by relevance.
func (s *Service) FindSubjectFolders(gradeCanonical, subjectQuery string) ([]string, error) {
	normSub := NormalizeArabic(subjectQuery)

	// List grade directory under dataDir
	gradeDir := filepath.Join(s.dataDir, gradeCanonical)
	entries, err := os.ReadDir(gradeDir)
	if err != nil {
		// Try fallback to check if dataDir is in parent dirs
		possiblePaths := []string{
			gradeDir,
			filepath.Join("..", gradeDir),
			filepath.Join("..", "..", gradeDir),
		}
		found := false
		for _, p := range possiblePaths {
			if e, eErr := os.ReadDir(p); eErr == nil {
				gradeDir = p
				entries = e
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("تعذر العثور على مجلد الصف الدراسي: %s", gradeCanonical)
		}
	}

	// Subject keyword dictionary
	subjectKeywords := map[string][]string{
		"الرياضيات":                                    {"ماث", "رياضه", "رياضة", "جبر", "هندسه", "هندسة", "تفاضل", "مثلثات", "حساب"},
		"البرمجة والذكاء الاصطناعي":                    {"برمجه", "برمجة", "ذكاء", "كمبيوتر", "حاسب"},
		"التاريخ":                                      {"تاريخ", "تاريخ مصر"},
		"العلوم المتكاملة":                             {"علوم", "فيزياء", "كيمياء", "احياء", "علوم متكامله"},
		"الفلسفة والمنطق":                              {"فلسفه", "منطق"},
		"اللغة الإنجليزية":                             {"انجليزي", "انجلش", "english"},
		"اللغة الفرنسية":                              {"فرنساوي", "فرنساوى", "french"},
		"اللغة العربية":                                {"عربي", "لغه عربيه", "لغة عربية"},
		"اللغة العربية - القواعد الأساسية للنحو والصرف": {"نحو", "صرف", "قواعد"},
		"اللغة العربية - القصة":                        {"قصه", "قصة", "عنترة", "قطز", "واسلاماه"},
		"علم النفس":                                    {"نفس", "علم نفس", "اجتماع", "علم اجتماع", "سوسيولوجي"},
	}

	// Specific part intents
	isSociology := strings.Contains(normSub, "اجتماع")
	isPart1 := strings.Contains(normSub, "اول") || strings.Contains(normSub, "1")
	isPart2 := strings.Contains(normSub, "ثاني") || strings.Contains(normSub, "تاني") || strings.Contains(normSub, "2")
	isStory := strings.Contains(normSub, "قصه") || strings.Contains(normSub, "قصة")

	var targetSubjects []string
	for officialName, keywords := range subjectKeywords {
		for _, kw := range keywords {
			if strings.Contains(normSub, kw) {
				targetSubjects = append(targetSubjects, officialName)
				break
			}
		}
	}

	type folderMatch struct {
		path  string
		score int
	}
	var scoredMatches []folderMatch

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		normName := NormalizeArabic(name)
		score := 0

		// Direct substring match
		if strings.Contains(normName, normSub) || strings.Contains(normSub, normName) {
			score += 50
		}

		// Official subject match
		for _, ts := range targetSubjects {
			normTs := NormalizeArabic(ts)
			if strings.Contains(normName, normTs) {
				score += 40
			}
		}

		// Part adjustments
		if isSociology && (strings.Contains(normName, "ثاني") || strings.Contains(normName, "علم الاجتماع")) {
			score += 70 // prioritize Part 2 for sociology
		}
		if isPart2 && (strings.Contains(normName, "ثاني") || strings.Contains(normName, "2")) {
			score += 30
		}
		if isPart1 && (strings.Contains(normName, "اول") || strings.Contains(normName, "1")) {
			score += 30
		}
		if isStory && strings.Contains(normName, "قصة") {
			score += 30
		}

		if score > 0 {
			scoredMatches = append(scoredMatches, folderMatch{path: filepath.Join(gradeDir, name), score: score})
		}
	}

	// Sort by score descending
	for i := 0; i < len(scoredMatches); i++ {
		for j := i + 1; j < len(scoredMatches); j++ {
			if scoredMatches[j].score > scoredMatches[i].score {
				scoredMatches[i], scoredMatches[j] = scoredMatches[j], scoredMatches[i]
			}
		}
	}

	var matched []string
	for _, m := range scoredMatches {
		matched = append(matched, m.path)
	}

	if len(matched) == 0 {
		return nil, fmt.Errorf("لم يتم العثور على مادة تطابق %q في %s", subjectQuery, gradeCanonical)
	}

	return matched, nil
}

// FindSubjectFolder locates the best matching directory for a given grade and subject on disk.
func (s *Service) FindSubjectFolder(gradeCanonical, subjectQuery string) (string, error) {
	folders, err := s.FindSubjectFolders(gradeCanonical, subjectQuery)
	if err != nil {
		return "", err
	}
	return folders[0], nil
}

// LoadSubjectIndex reads the index.json for a subject folder or from configDir.
func (s *Service) LoadSubjectIndex(folderPath, gradeCanonical, subjectName string) (*SubjectIndex, error) {
	s.mu.RLock()
	cached, ok := s.cache[folderPath]
	s.mu.RUnlock()
	if ok {
		return cached, nil
	}

	// 1. Try reading index.json in the subject folder
	indexPath := filepath.Join(folderPath, "index.json")
	data, err := os.ReadFile(indexPath)
	if err != nil {
		// 2. Try looking in configDir
		configCandidates := []string{
			filepath.Join(s.configDir, gradeCanonical, filepath.Base(folderPath)+"_index.json"),
			filepath.Join(s.configDir, gradeCanonical, strings.ReplaceAll(filepath.Base(folderPath), " ", "_")+"_index.json"),
		}
		for _, cand := range configCandidates {
			if d, cErr := os.ReadFile(cand); cErr == nil {
				data = d
				err = nil
				break
			}
		}
	}

	if err != nil || len(data) == 0 {
		return nil, fmt.Errorf("تعذر قراءة فهرس المادة من %s: %w", indexPath, err)
	}

	var idx SubjectIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("فشل فك تشفير فهرس المادة json: %w", err)
	}

	s.mu.Lock()
	s.cache[folderPath] = &idx
	s.mu.Unlock()

	return &idx, nil
}

// ListPDFFiles returns all .pdf files inside a directory.
func (s *Service) ListPDFFiles(folderPath string) []string {
	var pdfs []string
	entries, err := os.ReadDir(folderPath)
	if err != nil {
		return pdfs
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".pdf") {
			pdfs = append(pdfs, e.Name())
		}
	}
	return pdfs
}

// BrowseIndex inspects the curriculum index for a grade and subject, pairing each lesson with its PDF file.
func (s *Service) BrowseIndex(gradeQuery, subjectQuery, keyword string) (string, error) {
	gradeCanonical := ResolveGrade(gradeQuery)
	folderPaths, err := s.FindSubjectFolders(gradeCanonical, subjectQuery)
	if err != nil {
		return "", err
	}

	gradeDisplay := "الصف الأول الثانوي"
	if gradeCanonical == "الصف_الثاني_الثانوي" {
		gradeDisplay = "الصف الثاني الثانوي"
	}

	normKw := NormalizeArabic(keyword)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📚 *فهرس المناهج الدراسية المصرية الرسمية 2026* (%s)\n", gradeDisplay))
	if keyword != "" {
		sb.WriteString(fmt.Sprintf("🔍 *نتائج البحث عن:* %q\n", keyword))
	}
	sb.WriteString("\n")

	totalMatchedLessons := 0

	for _, folderPath := range folderPaths {
		subjectIndex, err := s.LoadSubjectIndex(folderPath, gradeCanonical, filepath.Base(folderPath))
		if err != nil {
			continue
		}
		pdfFiles := s.ListPDFFiles(folderPath)

		type matchedItem struct {
			lesson     Lesson
			matchedPDF string
		}
		var folderLessons []matchedItem

		for i, lesson := range subjectIndex.Lessons {
			matchedPDF := ""
			prefix := fmt.Sprintf("%02d", i+1)
			for _, pdf := range pdfFiles {
				if strings.HasPrefix(pdf, prefix) {
					matchedPDF = pdf
					break
				}
			}
			if matchedPDF == "" && i < len(pdfFiles) {
				matchedPDF = pdfFiles[i]
			}

			if normKw != "" {
				fullSearchable := NormalizeArabic(lesson.Chapter + " " + lesson.LessonTitle + " " + lesson.Summary)
				if !strings.Contains(fullSearchable, normKw) {
					continue
				}
			}

			folderLessons = append(folderLessons, matchedItem{lesson: lesson, matchedPDF: matchedPDF})
		}

		if len(folderLessons) == 0 && normKw != "" {
			continue
		}

		totalMatchedLessons += len(folderLessons)

		sb.WriteString(fmt.Sprintf("📖 *الكتاب: %s*\n", subjectIndex.BookTitle))
		if subjectIndex.Part != "" {
			sb.WriteString(fmt.Sprintf("📑 *القسم/الجزء:* %s\n", subjectIndex.Part))
		}
		sb.WriteString(fmt.Sprintf("📁 *مجلد المادة:* `%s`\n\n", filepath.Base(folderPath)))

		currentChapter := ""
		for _, item := range folderLessons {
			lesson := item.lesson
			if lesson.Chapter != currentChapter {
				currentChapter = lesson.Chapter
				sb.WriteString(fmt.Sprintf("📌 *%s*\n", currentChapter))
			}
			sb.WriteString(fmt.Sprintf("  • *%s*\n", lesson.LessonTitle))
			if item.matchedPDF != "" {
				sb.WriteString(fmt.Sprintf("    📄 *اسم ملف الـ PDF لقراءة الدرس:* `%s`\n", item.matchedPDF))
			}
			if lesson.StartPage > 0 && lesson.EndPage > 0 {
				sb.WriteString(fmt.Sprintf("    📑 الصفحات: من صفحة %d إلى %d\n", lesson.StartPage, lesson.EndPage))
			}
			if lesson.Summary != "" {
				sb.WriteString(fmt.Sprintf("    💡 ملخص المحتوى: %s\n", lesson.Summary))
			}
			sb.WriteString("\n")
		}
	}

	if totalMatchedLessons == 0 {
		if normKw != "" {
			sb.WriteString(fmt.Sprintf("⚠️ لم يتم العثور على دروس تحتوي على كلمة البحث: %q في هذه المادة.\n", keyword))
			sb.WriteString("💡 جرب استعراض الفهرس كاملاً بدون تحديد كلمة بحث.")
		} else {
			sb.WriteString("⚠️ لم يتم العثور على فهارس مسجلة لهذه المادة.")
		}
	} else {
		sb.WriteString("💡 *لاستخراج النص الكامل لأي درس:* استدعِ أداة `read_curriculum_lesson` ومرر اسم ملف الـ PDF الموضح أعلاه.")
	}

	return sb.String(), nil
}

// ExtractTextFromPDF extracts text from a PDF file using pdftotext or cached .txt.
func ExtractTextFromPDF(pdfPath string) (string, error) {
	// 1. Check if cached .txt exists
	txtPath := strings.TrimSuffix(pdfPath, filepath.Ext(pdfPath)) + ".txt"
	if data, err := os.ReadFile(txtPath); err == nil && len(data) > 0 {
		return string(data), nil
	}

	// 2. Try pdftotext with -layout
	cmd := exec.Command("pdftotext", "-layout", pdfPath, "-")
	out, err := cmd.Output()
	if err == nil && len(out) > 0 {
		text := cleanExtractedText(string(out))
		_ = os.WriteFile(txtPath, []byte(text), 0644)
		return text, nil
	}

	// 3. Try pdftotext without -layout
	cmd2 := exec.Command("pdftotext", pdfPath, "-")
	out2, err2 := cmd2.Output()
	if err2 == nil && len(out2) > 0 {
		text := cleanExtractedText(string(out2))
		_ = os.WriteFile(txtPath, []byte(text), 0644)
		return text, nil
	}

	return "", fmt.Errorf("تعذر استخراج النص من PDF (تأكد من تثبيت poppler-utils): %v", err)
}

func cleanExtractedText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	// Collapse excessive blank lines
	multiNewline := regexp.MustCompile(`\n{3,}`)
	s = multiNewline.ReplaceAllString(s, "\n\n")

	// Limit to reasonable token size (~25,000 characters)
	if len(s) > 25000 {
		s = s[:25000] + "\n\n... [تم اقتطاع باقي الصفحات للحفاظ على سياق المحادثة]"
	}
	return strings.TrimSpace(s)
}

// ReadLesson retrieves the content of a specific lesson PDF chosen by the model.
func (s *Service) ReadLesson(gradeQuery, subjectQuery, fileNameOrNumber string) (string, error) {
	gradeCanonical := ResolveGrade(gradeQuery)
	folderPaths, err := s.FindSubjectFolders(gradeCanonical, subjectQuery)
	if err != nil {
		return "", err
	}

	cleanTarget := strings.TrimSpace(fileNameOrNumber)

	var targetPDF string
	var foundFolder string

	for _, folderPath := range folderPaths {
		pdfFiles := s.ListPDFFiles(folderPath)

		// 1. Exact or contains match on file name
		for _, pdf := range pdfFiles {
			if strings.EqualFold(pdf, cleanTarget) || strings.Contains(strings.ToLower(pdf), strings.ToLower(cleanTarget)) {
				targetPDF = pdf
				foundFolder = folderPath
				break
			}
		}
		if targetPDF != "" {
			break
		}

		// 2. Number matching (e.g. "1" or "01")
		if num, nErr := strconv.Atoi(cleanTarget); nErr == nil {
			prefix := fmt.Sprintf("%02d", num)
			for _, pdf := range pdfFiles {
				if strings.HasPrefix(pdf, prefix) {
					targetPDF = pdf
					foundFolder = folderPath
					break
				}
			}
		}
		if targetPDF != "" {
			break
		}
	}

	if targetPDF == "" {
		var allAvailable []string
		for _, f := range folderPaths {
			allAvailable = append(allAvailable, s.ListPDFFiles(f)...)
		}
		return "", fmt.Errorf("لم يتم العثور على ملف PDF يطابق %q في مجلدات المادة. الملفات المتوفرة هي: %v", fileNameOrNumber, allAvailable)
	}

	fullPDFPath := filepath.Join(foundFolder, targetPDF)

	// Extract text
	text, extErr := ExtractTextFromPDF(fullPDFPath)
	if extErr != nil {
		// Fallback: provide summary from index.json
		if idx, lErr := s.LoadSubjectIndex(foundFolder, gradeCanonical, filepath.Base(foundFolder)); lErr == nil {
			for _, l := range idx.Lessons {
				return fmt.Sprintf("📄 *ملف الدرس:* %s\n📖 *عنوان الدرس:* %s\n📌 *الوحدة:* %s\n📑 *الصفحات:* من %d إلى %d\n💡 *ملخص الدرس من الفهرس الوزاري:*\n%s\n\n⚠️ ملاحظة: أداة pdftotext غير مثبتة على السيرفر حالياً لاستخراج النص الحرفي. يرجى تشغيل `sudo apt install -y poppler-utils` على السيرفر.",
					targetPDF, l.LessonTitle, l.Chapter, l.StartPage, l.EndPage, l.Summary), nil
			}
		}
		return "", fmt.Errorf("فشل استخراج محتوى الـ PDF: %w", extErr)
	}

	return fmt.Sprintf("📖 *نص الدرس المعتمد من كتاب الوزارة الرسمي 2026*\n📁 المجلد: `%s` | الملف: `%s`\n\n%s", filepath.Base(foundFolder), targetPDF, text), nil
}
