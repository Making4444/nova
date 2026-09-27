package pacing

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// StyleReport summarizes repetitive linguistic and emoji patterns detected
// across Nova's recent assistant messages in a chat.
type StyleReport struct {
	OverusedOpenings []string
	OverusedEmojis   []string
	OverusedPhrases  []string
}

// StyleDiversityFilter analyzes recent assistant messages to prevent repetitive
// greetings, catchphrases, and emoji clusters, and splits long replies into natural bubbles.
type StyleDiversityFilter struct {
	windowSize int
}

// NewStyleDiversityFilter creates a new StyleDiversityFilter inspecting up to the last 10 turns.
func NewStyleDiversityFilter() *StyleDiversityFilter {
	return &StyleDiversityFilter{
		windowSize: 10,
	}
}

var arabicStopwords = map[string]bool{
	"يا": true, "في": true, "من": true, "على": true, "علي": true,
	"ان": true, "إن": true, "أن": true, "ده": true, "دي": true,
	"اللي": true, "مش": true, "ما": true, "هو": true, "هي": true,
	"انا": true, "أنا": true, "انت": true, "إنت": true, "انتي": true,
	"احنا": true, "إحنا": true, "بس": true, "كده": true, "كدة": true,
	"عشان": true, "لو": true, "مع": true, "عن": true, "كل": true,
	"اي": true, "أي": true, "ولا": true, "او": true, "أو": true,
	"ثم": true, "حتى": true, "ام": true, "كان": true, "كانت": true,
}

func isDirectionalOrFormatRune(r rune) bool {
	return r == '\u200E' || r == '\u200F' || r == '\u200B' || r == '\u200C' ||
		r == '\uFEFF' || (r >= 0x202A && r <= 0x202E) || (r >= 0x2066 && r <= 0x2069)
}

func isEmojiBaseRune(r rune) bool {
	if r >= 0x1F300 && r <= 0x1FAFF {
		return true
	}
	if r >= 0x2600 && r <= 0x27BF {
		return true
	}
	if r >= 0x1F1E6 && r <= 0x1F1FF {
		return true
	}
	if r == 0x2B50 || r == 0x231A || r == 0x231B || (r >= 0x23E9 && r <= 0x23F3) {
		return true
	}
	return false
}

func isEmojiModifierOrJoiner(r rune) bool {
	return r == 0xFE0F || r == 0xFE0E || r == 0x200D || (r >= 0x1F3FB && r <= 0x1F3FF) || r == 0x20E3
}

// extractEmojisAndClusters returns:
// - allEmojis: all individual emoji tokens in the message
// - clusters: all contiguous sequences of 2+ emojis in the message
// - trailingEmojis: individual emojis appearing in the final emoji sequence at the end of the message
// - trailingCluster: the contiguous emoji cluster at the end of the message (if 2+ emojis)
func extractEmojisAndClusters(msg string) (allEmojis []string, clusters []string, trailingEmojis []string, trailingCluster string) {
	runes := []rune(msg)
	n := len(runes)

	type emojiSpan struct {
		token string
		start int
		end   int
	}

	var spans []emojiSpan
	for i := 0; i < n; {
		r := runes[i]
		if !isEmojiBaseRune(r) {
			i++
			continue
		}
		start := i
		i++
		for i < n {
			if isEmojiModifierOrJoiner(runes[i]) {
				if runes[i] == 0x200D && i+1 < n && isEmojiBaseRune(runes[i+1]) {
					i += 2
					continue
				}
				i++
				continue
			}
			break
		}
		spans = append(spans, emojiSpan{
			token: string(runes[start:i]),
			start: start,
			end:   i,
		})
	}

	if len(spans) == 0 {
		return nil, nil, nil, ""
	}

	for _, sp := range spans {
		allEmojis = append(allEmojis, sp.token)
	}

	// Group contiguous emoji spans (allowing only whitespace or directional marks between them)
	type clusterGroup struct {
		tokens []string
		end    int
	}
	var groups []clusterGroup
	curr := clusterGroup{tokens: []string{spans[0].token}, end: spans[0].end}

	for idx := 1; idx < len(spans); idx++ {
		prevEnd := curr.end
		nextStart := spans[idx].start
		onlySpaceBetween := true
		for k := prevEnd; k < nextStart; k++ {
			rk := runes[k]
			if !unicode.IsSpace(rk) && !isDirectionalOrFormatRune(rk) {
				onlySpaceBetween = false
				break
			}
		}
		if onlySpaceBetween {
			curr.tokens = append(curr.tokens, spans[idx].token)
			curr.end = spans[idx].end
		} else {
			groups = append(groups, curr)
			curr = clusterGroup{tokens: []string{spans[idx].token}, end: spans[idx].end}
		}
	}
	groups = append(groups, curr)

	for _, g := range groups {
		if len(g.tokens) >= 2 {
			clusters = append(clusters, strings.Join(g.tokens, ""))
		}
	}

	// Check if the last group is at the end of the message (ignoring trailing spaces, punctuation, directional marks)
	lastGroup := groups[len(groups)-1]
	isAtEnd := true
	for k := lastGroup.end; k < n; k++ {
		rk := runes[k]
		if !unicode.IsSpace(rk) && !unicode.IsPunct(rk) && !isDirectionalOrFormatRune(rk) {
			isAtEnd = false
			break
		}
	}
	if isAtEnd {
		trailingEmojis = append(trailingEmojis, lastGroup.tokens...)
		if len(lastGroup.tokens) >= 2 {
			trailingCluster = strings.Join(lastGroup.tokens, "")
		}
	}

	return allEmojis, clusters, trailingEmojis, trailingCluster
}

// cleanWords strips directional marks, emojis, and punctuation from a message and returns clean surface words.
func cleanWords(msg string) []string {
	var b strings.Builder
	b.Grow(len(msg))
	for _, r := range msg {
		if isDirectionalOrFormatRune(r) || isEmojiBaseRune(r) || isEmojiModifierOrJoiner(r) {
			b.WriteRune(' ')
			continue
		}
		// Strip Arabic tatweel and diacritics
		if (r >= 0x064B && r <= 0x065F) || r == 0x0670 || r == 0x0640 {
			continue
		}
		if unicode.IsPunct(r) || unicode.IsSymbol(r) {
			b.WriteRune(' ')
			continue
		}
		b.WriteRune(r)
	}
	return strings.Fields(b.String())
}

// normalizeWordKey normalizes an Arabic/Latin word for case- andorthography-insensitive grouping.
func normalizeWordKey(w string) string {
	w = strings.ToLower(strings.TrimSpace(w))
	var b strings.Builder
	b.Grow(len(w))
	for _, r := range w {
		switch r {
		case 'أ', 'إ', 'آ', 'ٱ':
			b.WriteRune('ا')
		case 'ة':
			b.WriteRune('ه')
		case 'ى':
			b.WriteRune('ي')
		case 'ؤ', 'ئ':
			b.WriteRune('ء')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func normalizePhraseKey(words []string) string {
	keys := make([]string, len(words))
	for i, w := range words {
		keys[i] = normalizeWordKey(w)
	}
	return strings.Join(keys, " ")
}

// AnalyzeAssistantHistory inspects up to the last 10 Nova messages and returns overused openings, emojis, and phrases.
func (f *StyleDiversityFilter) AnalyzeAssistantHistory(novaMessages []string) StyleReport {
	return AnalyzeAssistantHistory(novaMessages)
}

// AnalyzeAssistantHistory inspects up to the last 10 Nova messages and returns overused openings, emojis, and phrases.
func AnalyzeAssistantHistory(novaMessages []string) StyleReport {
	report := StyleReport{
		OverusedOpenings: []string{},
		OverusedEmojis:   []string{},
		OverusedPhrases:  []string{},
	}

	// Filter non-empty messages and bound to last 10
	var msgs []string
	for _, m := range novaMessages {
		if strings.TrimSpace(m) != "" {
			msgs = append(msgs, m)
		}
	}
	if len(msgs) > 10 {
		msgs = msgs[len(msgs)-10:]
	}
	if len(msgs) == 0 {
		return report
	}

	// 1. Analyze OverusedOpenings (2-to-4 word opening prefixes appearing >= 2 times)
	type phraseStat struct {
		surface string
		count   int
		length  int
	}
	openingCounts := make(map[string]*phraseStat)
	var openingOrder []string

	msgWordsList := make([][]string, len(msgs))
	baseVocab := make(map[string]string)
	for i, m := range msgs {
		words := cleanWords(m)
		msgWordsList[i] = words
		for _, w := range words {
			nk := normalizeWordKey(w)
			if utf8.RuneCountInString(nk) >= 3 {
				if _, exists := baseVocab[nk]; !exists {
					baseVocab[nk] = w
				}
			}
		}
	}

	// Normalize words with attached leading waw conjunction ("ومكاري" -> "مكاري") if the base word appears in the window
	for i, words := range msgWordsList {
		for j, w := range words {
			nk := normalizeWordKey(w)
			if strings.HasPrefix(nk, "و") && utf8.RuneCountInString(nk) >= 4 {
				withoutWaw := strings.TrimPrefix(nk, "و")
				if surfaceBase, exists := baseVocab[withoutWaw]; exists {
					msgWordsList[i][j] = surfaceBase
				}
			}
		}
	}

	for i := range msgs {
		words := msgWordsList[i]

		maxK := 4
		if len(words) < maxK {
			maxK = len(words)
		}
		for k := maxK; k >= 2; k-- {
			prefixWords := words[:k]
			// Avoid prefixes ending on a dangling vocative particle "يا"
			lastNorm := normalizeWordKey(prefixWords[len(prefixWords)-1])
			if lastNorm == "يا" {
				continue
			}
			key := normalizePhraseKey(prefixWords)
			if st, exists := openingCounts[key]; exists {
				st.count++
			} else {
				openingCounts[key] = &phraseStat{
					surface: strings.Join(prefixWords, " "),
					count:   1,
					length:  k,
				}
				openingOrder = append(openingOrder, key)
			}
		}
	}

	// Select openings with count >= 2, preferring longer prefixes when a shorter prefix only occurs inside a longer one
	selectedOpeningKeys := make(map[string]int)
	for length := 4; length >= 2; length-- {
		for _, key := range openingOrder {
			st := openingCounts[key]
			if st.length != length || st.count < 2 {
				continue
			}
			subsumed := false
			for selKey, selCount := range selectedOpeningKeys {
				if strings.HasPrefix(selKey, key+" ") && selCount >= st.count {
					subsumed = true
					break
				}
			}
			if !subsumed {
				selectedOpeningKeys[key] = st.count
				report.OverusedOpenings = append(report.OverusedOpenings, st.surface)
			}
		}
	}

	// 2. Analyze OverusedEmojis (individual emojis >= 3 times or trailing in >= 2 consecutive msgs;
	// emoji clusters >= 2 times or trailing in >= 2 consecutive msgs)
	emojiTotalCount := make(map[string]int)
	var emojiOrder []string
	clusterTotalCount := make(map[string]int)
	var clusterOrder []string

	msgTrailingEmojis := make([][]string, len(msgs))
	msgTrailingCluster := make([]string, len(msgs))

	for i, m := range msgs {
		allEmojis, clusters, trailingEmojis, trailingCluster := extractEmojisAndClusters(m)
		msgTrailingEmojis[i] = trailingEmojis
		msgTrailingCluster[i] = trailingCluster

		for _, em := range allEmojis {
			if _, exists := emojiTotalCount[em]; !exists {
				emojiOrder = append(emojiOrder, em)
			}
			emojiTotalCount[em]++
		}
		seenClusterInMsg := make(map[string]bool)
		for _, cl := range clusters {
			if seenClusterInMsg[cl] {
				continue
			}
			seenClusterInMsg[cl] = true
			if _, exists := clusterTotalCount[cl]; !exists {
				clusterOrder = append(clusterOrder, cl)
			}
			clusterTotalCount[cl]++
		}
	}

	// Check consecutive trailing emojis & clusters
	consecutiveTrailingEmoji := make(map[string]bool)
	consecutiveTrailingCluster := make(map[string]bool)
	for i := 1; i < len(msgs); i++ {
		if msgTrailingCluster[i] != "" && msgTrailingCluster[i] == msgTrailingCluster[i-1] {
			consecutiveTrailingCluster[msgTrailingCluster[i]] = true
		}
		if len(msgTrailingEmojis[i]) > 0 && len(msgTrailingEmojis[i-1]) > 0 {
			prevSet := make(map[string]bool)
			for _, e := range msgTrailingEmojis[i-1] {
				prevSet[e] = true
			}
			for _, e := range msgTrailingEmojis[i] {
				if prevSet[e] {
					consecutiveTrailingEmoji[e] = true
				}
			}
		}
	}

	addedEmojiItem := make(map[string]bool)
	for _, cl := range clusterOrder {
		if clusterTotalCount[cl] >= 2 || consecutiveTrailingCluster[cl] {
			if !addedEmojiItem[cl] {
				addedEmojiItem[cl] = true
				report.OverusedEmojis = append(report.OverusedEmojis, cl)
			}
		}
	}
	for _, em := range emojiOrder {
		if emojiTotalCount[em] >= 3 || consecutiveTrailingEmoji[em] {
			if !addedEmojiItem[em] {
				addedEmojiItem[em] = true
				report.OverusedEmojis = append(report.OverusedEmojis, em)
			}
		}
	}

	// 3. Analyze OverusedPhrases (body n-grams appearing >= 2 times across messages)
	phraseCounts := make(map[string]*phraseStat)
	var phraseOrder []string

	for _, words := range msgWordsList {
		if len(words) < 2 {
			continue
		}
		seenInMsg := make(map[string]bool)
		maxN := 5
		if len(words) < maxN {
			maxN = len(words)
		}
		for n := maxN; n >= 2; n-- {
			for start := 0; start+n <= len(words); start++ {
				slice := words[start : start+n]
				firstNorm := normalizeWordKey(slice[0])
				lastNorm := normalizeWordKey(slice[len(slice)-1])
				// Skip n-grams starting or ending with stopwords
				if arabicStopwords[firstNorm] || arabicStopwords[lastNorm] {
					continue
				}
				// For 2-word phrases, require both words to be substantive content words (>= 3 runes)
				if n == 2 {
					if utf8.RuneCountInString(firstNorm) < 3 || utf8.RuneCountInString(lastNorm) < 3 {
						continue
					}
				}
				key := normalizePhraseKey(slice)
				if seenInMsg[key] {
					continue
				}
				seenInMsg[key] = true
				if st, exists := phraseCounts[key]; exists {
					st.count++
				} else {
					phraseCounts[key] = &phraseStat{
						surface: strings.Join(slice, " "),
						count:   1,
						length:  n,
					}
					phraseOrder = append(phraseOrder, key)
				}
			}
		}
	}

	selectedPhraseKeys := make(map[string]int)
	for length := 5; length >= 2; length-- {
		for _, key := range phraseOrder {
			st := phraseCounts[key]
			if st.length != length || st.count < 2 {
				continue
			}
			// Skip if already reported as an opening or strictly subsumed by an opening with >= count
			subsumedByOpening := false
			for openKey, openCount := range selectedOpeningKeys {
				if (openKey == key || strings.Contains(openKey, key)) && openCount >= st.count {
					subsumedByOpening = true
					break
				}
			}
			if subsumedByOpening {
				continue
			}

			subsumedByPhrase := false
			for selKey, selCount := range selectedPhraseKeys {
				if strings.Contains(selKey, key) && selCount >= st.count {
					subsumedByPhrase = true
					break
				}
			}
			if !subsumedByPhrase {
				selectedPhraseKeys[key] = st.count
				report.OverusedPhrases = append(report.OverusedPhrases, st.surface)
			}
		}
	}

	return report
}

// BuildPromptDirective returns a dynamic system directive banning overused openings, phrases, and emojis,
// or "" if no repetition is detected.
func (f *StyleDiversityFilter) BuildPromptDirective(novaMessages []string) string {
	return BuildPromptDirective(novaMessages)
}

// BuildPromptDirective returns a dynamic system directive banning overused openings, phrases, and emojis,
// or "" if no repetition is detected.
func BuildPromptDirective(novaMessages []string) string {
	report := AnalyzeAssistantHistory(novaMessages)
	if len(report.OverusedOpenings) == 0 && len(report.OverusedEmojis) == 0 && len(report.OverusedPhrases) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("[STYLE DIVERSITY & ANTI-REPETITION DIRECTIVE]\n")
	b.WriteString("⚠️ تحذير هام لمنع التكرار (CRITICAL ANTI-REPETITION): لقد كررت نفس الافتتاحيات أو العبارات أو الإيموجيز في ردودك الأخيرة. يُمنع تماماً استخدام أي مما يلي في هذا الرد:\n")

	if len(report.OverusedOpenings) > 0 {
		b.WriteString(fmt.Sprintf("- Banned Openings (افتتاحيات ممنوعة في بداية الرد): %s\n", strings.Join(report.OverusedOpenings, " ، ")))
	}
	if len(report.OverusedPhrases) > 0 {
		b.WriteString(fmt.Sprintf("- Banned Phrases (عبارات مكررة ممنوعة): %s\n", strings.Join(report.OverusedPhrases, " ، ")))
	}
	if len(report.OverusedEmojis) > 0 {
		b.WriteString(fmt.Sprintf("- Banned Emojis (إيموجيز مستهلكة ممنوعة في هذا الدور): %s\n", strings.Join(report.OverusedEmojis, " ")))
	}

	b.WriteString("غيّر أسلوبك وادخل في الموضوع مباشرةً بمفردات جديدة وطبيعية بدون أي لزمات محفوظة أو إيموجيز مكررة.")
	return b.String()
}

func isFormattedListOrCode(text string) bool {
	if strings.Contains(text, "```") {
		return true
	}
	lines := strings.Split(text, "\n")
	numberedLines := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if len(trimmed) >= 2 && trimmed[0] >= '1' && trimmed[0] <= '9' && (trimmed[1] == '.' || trimmed[1] == ')' || trimmed[1] == '-') {
			numberedLines++
		}
	}
	return numberedLines >= 2
}

// SplitIntoBubbles splits a response into up to maxBubbles natural sequential WhatsApp bubbles.
func (f *StyleDiversityFilter) SplitIntoBubbles(replyText string, maxBubbles int) []string {
	return SplitIntoBubbles(replyText, maxBubbles)
}

// SplitIntoBubbles splits a response into up to maxBubbles natural sequential WhatsApp bubbles.
func SplitIntoBubbles(replyText string, maxBubbles int) []string {
	if maxBubbles <= 0 {
		maxBubbles = 3
	}

	trimmed := strings.TrimSpace(replyText)
	if trimmed == "" {
		return nil
	}

	// 1. Explicit "||" delimiter takes priority
	if strings.Contains(trimmed, "||") {
		rawParts := strings.Split(trimmed, "||")
		var parts []string
		for _, p := range rawParts {
			clean := strings.TrimSpace(p)
			if clean != "" {
				parts = append(parts, clean)
			}
		}
		if len(parts) == 0 {
			return nil
		}
		if len(parts) > maxBubbles {
			mergedTail := strings.Join(parts[maxBubbles-1:], "\n")
			parts = append(parts[:maxBubbles-1], mergedTail)
		}
		return parts
	}

	// 2. Conversational multi-paragraph heuristic (140..450 runes, contains "\n\n", not code or numbered list)
	runeCount := utf8.RuneCountInString(trimmed)
	if runeCount >= 140 && runeCount <= 450 && strings.Contains(trimmed, "\n\n") && !isFormattedListOrCode(trimmed) {
		rawParas := strings.Split(trimmed, "\n\n")
		var paras []string
		for _, p := range rawParas {
			clean := strings.TrimSpace(p)
			if clean != "" {
				paras = append(paras, clean)
			}
		}
		if len(paras) > 1 {
			if len(paras) > maxBubbles {
				mergedTail := strings.Join(paras[maxBubbles-1:], "\n\n")
				paras = append(paras[:maxBubbles-1], mergedTail)
			}
			return paras
		}
	}

	return []string{trimmed}
}
