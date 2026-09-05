package curriculum

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeArabic(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"الرِّيَاضِيَاتُ", "الرياضيات"},
		{"الصف_الأول_الثانوي", "الصف الاول الثانوي"},
		{"لغة عربية", "لغه عربيه"},
		{"إنجليزي", "انجليزي"},
	}

	for _, c := range cases {
		res := NormalizeArabic(c.input)
		if res != c.expected {
			t.Errorf("NormalizeArabic(%q) = %q, expected %q", c.input, res, c.expected)
		}
	}
}

func TestResolveGrade(t *testing.T) {
	if ResolveGrade("اولى ثانوي") != "الصف_الأول_الثانوي" {
		t.Errorf("expected الصف_الأول_الثانوي")
	}
	if ResolveGrade("تانية ثانوي") != "الصف_الثاني_الثانوي" {
		t.Errorf("expected الصف_الثاني_الثانوي")
	}
	if ResolveGrade("الصف الثاني") != "الصف_الثاني_الثانوي" {
		t.Errorf("expected الصف_الثاني_الثانوي")
	}
}

func TestCurriculumService(t *testing.T) {
	// Create temporary mock curriculum folder
	tempDir, err := os.MkdirTemp("", "nova_curriculum_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	gradeDir := filepath.Join(tempDir, "الصف_الأول_الثانوي", "الرياضيات")
	if err := os.MkdirAll(gradeDir, 0755); err != nil {
		t.Fatalf("failed to create subject dir: %v", err)
	}

	// Create sample index.json
	mockIndex := SubjectIndex{
		BookTitle:       "الرياضيات العامة",
		Grade:           "الصف الأول الثانوي",
		SubjectCategory: "general_subjects",
		Part:            "الجزء الأول",
		TotalLessons:    2,
		Lessons: []Lesson{
			{
				Chapter:     "الوحدة الأولى: الجبر",
				LessonTitle: "الدرس الأول: حل معادلات الدرجة الثانية",
				StartPage:   6,
				EndPage:     12,
				Summary:     "حل المعادلة التربيعية بيانياً وجبرياً.",
			},
			{
				Chapter:     "الوحدة الأولى: الجبر",
				LessonTitle: "الدرس الثاني: مقدمة في الأعداد المركبة",
				StartPage:   13,
				EndPage:     18,
				Summary:     "العدد التخيلي ت، قوى ت، تعريف العدد المركب.",
			},
		},
	}
	indexBytes, _ := json.MarshalIndent(mockIndex, "", "  ")
	_ = os.WriteFile(filepath.Join(gradeDir, "index.json"), indexBytes, 0644)

	// Create sample dummy PDF files and text cache
	pdf1 := filepath.Join(gradeDir, "01_الجزء_1.pdf")
	_ = os.WriteFile(pdf1, []byte("%PDF-1.4 dummy content"), 0644)
	_ = os.WriteFile(filepath.Join(gradeDir, "01_الجزء_1.txt"), []byte("محتوى الدرس الأول من كتاب الوزارة: حل معادلات الدرجة الثانية بالمميز."), 0644)

	pdf2 := filepath.Join(gradeDir, "02_الجزء_2.pdf")
	_ = os.WriteFile(pdf2, []byte("%PDF-1.4 dummy content"), 0644)
	_ = os.WriteFile(filepath.Join(gradeDir, "02_الجزء_2.txt"), []byte("محتوى الدرس الثاني: الأعداد المركبة والعدد التخيلي ت."), 0644)

	svc := NewService(tempDir, tempDir)

	// 1. Test BrowseIndex
	output, err := svc.BrowseIndex("اولى ثانوي", "رياضيات", "")
	if err != nil {
		t.Fatalf("BrowseIndex failed: %v", err)
	}
	if !strings.Contains(output, "الرياضيات العامة") || !strings.Contains(output, "01_الجزء_1.pdf") {
		t.Errorf("unexpected BrowseIndex output:\n%s", output)
	}

	// 2. Test BrowseIndex with keyword filter
	filtered, err := svc.BrowseIndex("الصف الأول الثانوي", "جبر", "مركبة")
	if err != nil {
		t.Fatalf("BrowseIndex with filter failed: %v", err)
	}
	if !strings.Contains(filtered, "الدرس الثاني") || strings.Contains(filtered, "الدرس الأول") {
		t.Errorf("expected only Lesson 2 in filtered output, got:\n%s", filtered)
	}

	// 3. Test ReadLesson
	lessonContent, err := svc.ReadLesson("اولى ثانوي", "رياضة", "01_الجزء_1.pdf")
	if err != nil {
		t.Fatalf("ReadLesson failed: %v", err)
	}
	if !strings.Contains(lessonContent, "حل معادلات الدرجة الثانية بالمميز") {
		t.Errorf("unexpected ReadLesson content:\n%s", lessonContent)
	}

	// 4. Test ReadLesson by number
	lesson2Content, err := svc.ReadLesson("الصف_الأول_الثانوي", "الرياضيات", "2")
	if err != nil {
		t.Fatalf("ReadLesson by number failed: %v", err)
	}
	if !strings.Contains(lesson2Content, "الأعداد المركبة") {
		t.Errorf("unexpected ReadLesson 2 content:\n%s", lesson2Content)
	}

	// 5. Test FindSubjectFolders ranking for sociology / Part 2
	p1Dir := filepath.Join(tempDir, "الصف_الثاني_الثانوي", "علم النفس - كتاب الطالب - الجزء الأول")
	p2Dir := filepath.Join(tempDir, "الصف_الثاني_الثانوي", "علم النفس - كتاب الطالب - الجزء الثاني")
	_ = os.MkdirAll(p1Dir, 0755)
	_ = os.MkdirAll(p2Dir, 0755)

	socFolders, sErr := svc.FindSubjectFolders("الصف_الثاني_الثانوي", "علم الاجتماع")
	if sErr != nil {
		t.Fatalf("FindSubjectFolders for sociology failed: %v", sErr)
	}
	if len(socFolders) == 0 || !strings.Contains(socFolders[0], "الجزء الثاني") {
		t.Errorf("expected Part 2 to rank first for sociology, got: %v", socFolders)
	}
}
