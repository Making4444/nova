package voice

import (
	"context"
	"strings"
	"testing"
)

func TestNewOpenRouterTTS(t *testing.T) {
	tts := NewOpenRouterTTS("test-key", "", "")
	if tts.model != "openai/gpt-audio-mini" {
		t.Errorf("expected model openai/gpt-audio-mini, got %s", tts.model)
	}
	if tts.voice != "onyx" {
		t.Errorf("expected voice onyx, got %s", tts.voice)
	}
}

func TestSynthesizeEmptyText(t *testing.T) {
	tts := NewOpenRouterTTS("test-key", "openai/gpt-audio-mini", "onyx")
	_, _, err := tts.SynthesizeToOggOpus(context.Background(), "   ")
	if err == nil {
		t.Errorf("expected error for empty text, got nil")
	}
}

func TestNewTTSClient_SignorAI(t *testing.T) {
	tts := NewTTSClient("signor-key", "https://api.code.signor.ai/api-2/v1", "gemini-3.8-flash-tts", "Kore")
	if tts.model != "gemini-3.8-flash-tts" {
		t.Errorf("expected model gemini-3.8-flash-tts, got %s", tts.model)
	}
	if tts.voice != "Kore" {
		t.Errorf("expected voice Kore, got %s", tts.voice)
	}
	expectedSpeechURL := "https://api.code.signor.ai/api-2/v1/audio/speech"
	if gotURL := tts.getSpeechURL(); gotURL != expectedSpeechURL {
		t.Errorf("expected speech URL %s, got %s", expectedSpeechURL, gotURL)
	}
	expectedGeminiURL := "https://api.code.signor.ai/api-2/v1beta/models/gemini-3.8-flash-tts:generateContent"
	if gotGemURL := tts.getGeminiGenerateContentURL(); gotGemURL != expectedGeminiURL {
		t.Errorf("expected Gemini URL %s, got %s", expectedGeminiURL, gotGemURL)
	}
}

func TestGeminiVoiceMapping(t *testing.T) {
	if v := getGeminiVoiceName("alloy"); v != "Kore" {
		t.Errorf("expected alloy to map to Kore, got %s", v)
	}
	if v := getGeminiVoiceName("Puck"); v != "Puck" {
		t.Errorf("expected Puck to remain Puck, got %s", v)
	}
	if v := getGeminiVoiceName("charon"); v != "Charon" {
		t.Errorf("expected charon to map to Charon, got %s", v)
	}
}

func TestCleanTextForSpeech(t *testing.T) {
	// Strictly verify audio tags like [chuckles], [pause], [laughs] are PRESERVED for Gemini TTS
	input := "تم البحث\n\n[chuckles] أهلاً بيك يا *معلم* في `نوفا`! [pause] _تمام_؟ [laughs]"
	expected := "[chuckles] أهلاً بيك يا معلم في نوفا! [pause] تمام؟ [laughs]"
	got := CleanTextForSpeech(input)
	if got != expected {
		t.Errorf("CleanTextForSpeech failed:\nexpected: %q\ngot:      %q", expected, got)
	}
}

func TestSplitTextIntoChunks_ExplicitDelimiter(t *testing.T) {
	text := "جملة أولى يا كبير [pause] || جملة تانية اهي || جملة تالتة وتمام"
	chunks := SplitTextIntoChunks(text, 220)
	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks split by ||, got %d: %v", len(chunks), chunks)
	}
	if chunks[0] != "جملة أولى يا كبير [pause]" {
		t.Errorf("expected chunk 0 to keep [pause], got %q", chunks[0])
	}
	if chunks[1] != "جملة تانية اهي" {
		t.Errorf("expected chunk 1 to be exact, got %q", chunks[1])
	}
}

func TestSplitTextIntoChunks_ShortAndEmpty(t *testing.T) {
	if chunks := SplitTextIntoChunks("", 220); chunks != nil {
		t.Errorf("expected nil for empty text, got %v", chunks)
	}

	short := "أهلاً يا باشا، تمام هظبطهالك حالا."
	chunks := SplitTextIntoChunks(short, 220)
	if len(chunks) != 1 || chunks[0] != short {
		t.Errorf("expected 1 chunk matching short text, got %v", chunks)
	}
}

func TestSplitTextIntoChunks_LongVocabularyList(t *testing.T) {
	vocabList := `من عينيا يا باشمهندس، اسمعي معايا يا إيريني وركزي في النطق: [pause]

الكلمات الاساسية:
arena.. ساحة او مجال.
authentic.. أصيل او حقيقي.
cognitive.. معرفي او ذهني.
engagement.. اندماج ومشاركة.
flexibility.. مرونة.
rehearsal.. بروفة.

وندخل على باقي الكلمات:
absorb.. يستوعب او يمتص.
absorption.. استيعاب.
additional.. إضافي.
arabesque design.. تصميم زخرفي عربي.
brain region.. منطقة في المخ.
calligraphy.. فن الخط.
collaboration.. تعاون.
condition.. حالة او ظرف.
connect.. يربط.
consistently.. بانتظام.
contemporary.. معاصر.
cortisol.. كورتيزول.
creative.. مبدع.
cultural.. ثقافي.
decade.. عقد، يعني عشر سنين.
drama production.. إنتاج مسرحي.
embrace.. يحتضن او يتقبل.
recovery.. تعافي واستعادة.
employer.. صاحب عمل.
entertainment.. ترفيه.
evidence.. دليل.
exploration.. استكشاف.
failure.. فشل.
heritage.. تراث.
impact.. تأثير.
instrument.. آلة موسيقية.
layer.. طبقة.
leisure.. وقت الفراغ.
literally.. حرفياً.
meaningful.. ذو معنى او هادف.
musician.. عازف او موسيقار.
neural connection.. اتصال عصبي.
neuroscientist.. عالم أعصاب.
non-judgmental.. متفهم من غير ما يحكم على حد.
orchestra.. أوركسترا.
passive.. سلبي.
psychological.. نفسي.
rank.. يرتب او يصنف.
require.. يتطلب.
resilience.. مرونة وقدرة على التحمل.
screen-based.. مرتبط بالشاشات.
self-expression.. تعبير عن النفس.
shared goal.. هدف مشترك.
storytelling.. سرد الحكايات.
strengthen.. يقوي.
stress hormone.. هرمون القلق والضغط النفسي.
valuable.. ذو قيمة.
volunteer.. يتطوع.

[laughs] سجلي ورايا بقى وبالتوفيق يا رب!`

	chunks := SplitTextIntoChunks(vocabList, 220)
	if len(chunks) < 8 {
		t.Fatalf("expected at least 8 chunks of <=220 runes for 2000+ char list, got %d", len(chunks))
	}

	for i, chunk := range chunks {
		runes := len([]rune(chunk))
		if runes > 240 {
			t.Errorf("chunk %d exceeds max length limit: %d chars (content: %q)", i+1, runes, chunk)
		}
	}

	// Verify key vocabulary and audio tags from start, middle, and end are all preserved
	allText := ""
	for _, c := range chunks {
		allText += " " + c
	}

	expectedWords := []string{
		"[pause]",
		"arena",
		"authentic",
		"contemporary",
		"neuroscientist",
		"resilience",
		"volunteer",
		"[laughs]",
		"سجلي ورايا بقى وبالتوفيق يا رب!",
	}

	for _, w := range expectedWords {
		if !strings.Contains(allText, w) {
			t.Errorf("missing word %q in chunked output", w)
		}
	}
}

func TestSplitTextIntoChunks_ContinuousLongParagraph(t *testing.T) {
	longPara := strings.Repeat("هذه جملة تجريبية طويلة جداً لا تحتوي على فواصل أسطر ولكنها تحتوي على نقاط. ", 20)
	chunks := SplitTextIntoChunks(longPara, 220)
	if len(chunks) < 5 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	for i, c := range chunks {
		if len([]rune(c)) > 240 {
			t.Errorf("chunk %d is too large: %d chars", i, len([]rune(c)))
		}
	}
}
