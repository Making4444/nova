package pacing

import (
	"strings"
	"testing"
)

func TestAnalyzeAssistantHistory_TinaPattern(t *testing.T) {
	filter := NewStyleDiversityFilter()

	messages := []string{
		"\u200Fيا تينا يا قمر، عاملة إيه النهاردة؟ أصل مكاري عمللي فورمات امبارح وعليكي عزومة حلويات 🦋🌸😂",
		"\u200Fيا تينا يا قمر! انتي برنسيسة الجروب والله ومكاري عمللي فورمات الصبح 🦋🌸😂",
		"\u200Fيا تينا يا قمر منورة الدنيا كلها يا برنسيسة الجروب وفين عزومة حلويات بقى 🦋🌸😂",
		"أحلى مسا عليك يا غالي منور الدنيا كلها",
		"أحلى مسا عليك يا صاحبي اخبارك ايه",
	}

	report := filter.AnalyzeAssistantHistory(messages)

	// Verify OverusedOpenings
	foundTinaOpening := false
	foundAhlaMasa := false
	for _, op := range report.OverusedOpenings {
		if strings.Contains(op, "يا تينا يا قمر") {
			foundTinaOpening = true
		}
		if strings.Contains(op, "أحلى مسا عليك") {
			foundAhlaMasa = true
		}
	}
	if !foundTinaOpening {
		t.Errorf("expected 'يا تينا يا قمر' in OverusedOpenings, got %v", report.OverusedOpenings)
	}
	if !foundAhlaMasa {
		t.Errorf("expected 'أحلى مسا عليك' in OverusedOpenings, got %v", report.OverusedOpenings)
	}

	// Verify OverusedEmojis
	foundCluster := false
	foundButterfly := false
	foundBlossom := false
	for _, em := range report.OverusedEmojis {
		if em == "🦋🌸😂" {
			foundCluster = true
		}
		if em == "🦋" {
			foundButterfly = true
		}
		if em == "🌸" {
			foundBlossom = true
		}
	}
	if !foundCluster {
		t.Errorf("expected emoji cluster '🦋🌸😂' in OverusedEmojis, got %v", report.OverusedEmojis)
	}
	if !foundButterfly || !foundBlossom {
		t.Errorf("expected '🦋' and '🌸' in OverusedEmojis, got %v", report.OverusedEmojis)
	}

	// Verify OverusedPhrases
	foundFormat := false
	foundHalawiyat := false
	foundPrincess := false
	for _, ph := range report.OverusedPhrases {
		if strings.Contains(ph, "مكاري عمللي فورمات") {
			foundFormat = true
		}
		if strings.Contains(ph, "عزومة حلويات") {
			foundHalawiyat = true
		}
		if strings.Contains(ph, "برنسيسة الجروب") {
			foundPrincess = true
		}
	}
	if !foundFormat {
		t.Errorf("expected 'مكاري عمللي فورمات' in OverusedPhrases, got %v", report.OverusedPhrases)
	}
	if !foundHalawiyat {
		t.Errorf("expected 'عزومة حلويات' in OverusedPhrases, got %v", report.OverusedPhrases)
	}
	if !foundPrincess {
		t.Errorf("expected 'برنسيسة الجروب' in OverusedPhrases, got %v", report.OverusedPhrases)
	}

	// Verify BuildPromptDirective
	directive := filter.BuildPromptDirective(messages)
	if !strings.Contains(directive, "[STYLE DIVERSITY & ANTI-REPETITION DIRECTIVE]") {
		t.Errorf("expected directive header, got %q", directive)
	}
	if !strings.Contains(directive, "يا تينا يا قمر") || !strings.Contains(directive, "🦋🌸😂") || !strings.Contains(directive, "مكاري عمللي فورمات") {
		t.Errorf("expected banned items in directive, got %q", directive)
	}
}

func TestBuildPromptDirective_NoRepetitionReturnsEmpty(t *testing.T) {
	messages := []string{
		"تمام يا معلم هبص على الكود دلوقتي",
		"المشكلة كانت في تعريف المتغير جوه اللوب",
		"جرب تشغل السيرفر تاني وقولي النتيجة",
	}

	directive := BuildPromptDirective(messages)
	if directive != "" {
		t.Errorf("expected empty directive when no repetition exists, got %q", directive)
	}
}

func TestSplitIntoBubbles(t *testing.T) {
	// 1. Explicit || delimiter
	explicit := "أهلاً بيك يا غالي || ثواني أشوفلك الموضوع ده || تمام لقيت الحل خلاص"
	bubbles := SplitIntoBubbles(explicit, 3)
	if len(bubbles) != 3 {
		t.Fatalf("expected 3 bubbles from || delimiter, got %d: %v", len(bubbles), bubbles)
	}
	if bubbles[0] != "أهلاً بيك يا غالي" || bubbles[2] != "تمام لقيت الحل خلاص" {
		t.Errorf("unexpected bubble contents: %v", bubbles)
	}

	// 2. Cap at maxBubbles when || has more parts
	fourParts := "واحد || اتنين || تلاتة || أربعة"
	capped := SplitIntoBubbles(fourParts, 3)
	if len(capped) != 3 {
		t.Fatalf("expected capped 3 bubbles, got %d", len(capped))
	}
	if !strings.Contains(capped[2], "أربعة") {
		t.Errorf("expected 4th part merged into 3rd bubble, got %q", capped[2])
	}

	// 3. Conversational multi-paragraph between 140 and 450 runes
	para1 := "والله يا صاحبي الموضوع ده حصل معايا الأسبوع اللي فات وكنت محتار زيك بالظبط ومش عارف أبدأ منين ولا أعمل إيه في القصة دي."
	para2 := "بس لما قعدت ورتبت الأفكار واحدة واحدة الدنيا ظبطت خالص وبقت سهلة جداً، فمتقلقش خالص وابدأ خطوة خطوة وهتلاقي كله تمام."
	conversational := para1 + "\n\n" + para2
	paraBubbles := SplitIntoBubbles(conversational, 3)
	if len(paraBubbles) != 2 {
		t.Fatalf("expected 2 bubbles for conversational paragraphs, got %d", len(paraBubbles))
	}

	// 4. Code blocks should not be split on \n\n
	codeBlock := para1 + "\n\n```go\nfunc main() {\n\n    println(\"hello\")\n}\n```"
	codeBubbles := SplitIntoBubbles(codeBlock, 3)
	if len(codeBubbles) != 1 {
		t.Errorf("expected code block message to stay in 1 bubble, got %d", len(codeBubbles))
	}
}
