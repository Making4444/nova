package admin

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// CommandResult represents the response of an executed admin command.
type CommandResult struct {
	Handled   bool
	ReplyText string
}

// StatsProvider interface allows retrieving metrics for the /status command.
type StatsProvider interface {
	GetTotalChatsCount() int
	GetMemoryProfilesCount() int
	GetScheduledTasksCount() int
	GetModelName() string
}

// CleanInvisibleMarks strips invisible Unicode directional marks (\u200e, \u200f, zero-width spaces, BOM) common in WhatsApp.
func CleanInvisibleMarks(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '\u200e' || r == '\u200f' || r == '\u202a' || r == '\u202b' ||
			r == '\u202c' || r == '\u202d' || r == '\u202e' || r == '\ufeff' ||
			r == '\u200b' || r == '\u200c' || r == '\u200d' || r == '\u00a0' {
			return -1
		}
		return r
	}, s)
	return strings.TrimSpace(s)
}

// ChatArchiver interface allows archiving, summarizing, and restoring chat logs.
type ChatArchiver interface {
	ArchiveChatSession(ctx context.Context, chatType, chatID string) (string, int, error)
	RestoreArchivedChatSession(chatType, chatID string, archiveIndex int) error
	ListChatArchives(chatType, chatID string) ([]string, error)
}

// PersonaSwitcher interface allows switching active bot persona dynamically.
type PersonaSwitcher interface {
	SwitchPersona(mode int) (string, error)
}

// ThinkingController interface allows changing reasoning effort dynamically on the AI client.
type ThinkingController interface {
	SetThinkingEffort(effort string)
}

// HandleAdminCommand parses and executes admin and archive commands.
func HandleAdminCommand(
	state *State,
	chatID string,
	senderID string,
	senderName string,
	isFromMe bool,
	text string,
	repliedSender string,
	stats StatsProvider,
	archiver ChatArchiver,
) CommandResult {
	cleanText := CleanInvisibleMarks(text)
	if cleanText == "" {
		return CommandResult{Handled: false}
	}

	// Check if this looks like a command (starts with /, !, ., or matches command keyword)
	hasPrefix := strings.HasPrefix(cleanText, "/") || strings.HasPrefix(cleanText, "!") || strings.HasPrefix(cleanText, ".")
	rawCmd := cleanText
	if hasPrefix {
		rawCmd = cleanText[1:]
	}

	parts := strings.Fields(rawCmd)
	if len(parts) == 0 {
		return CommandResult{Handled: false}
	}

	cmd := strings.ToLower(parts[0])
	isAdmin := false
	if state != nil {
		isAdmin = state.IsAdmin(senderID, senderName, isFromMe)
	}

	switch cmd {
	case "shutdown", "suhtdown", "stop":
		if !isAdmin {
			return CommandResult{Handled: true, ReplyText: "⚠️ هذا الأمر مخصص فقط لمشرف البوت (Admin)."}
		}
		if state != nil {
			_ = state.SetShutdown(true)
		}
		return CommandResult{
			Handled:   true,
			ReplyText: "🔴 *تم إغلاق السيرفرات مؤقتاً بنجاح (Shutdown)!*\nعند مناداة نوفا، سيتم إرسال رسالة التوقف التلقائية فوراً وبدون استهلاك أي رصيد.",
		}

	case "start", "resume", "restart":
		if !isAdmin {
			return CommandResult{Handled: true, ReplyText: "⚠️ هذا الأمر مخصص فقط لمشرف البوت (Admin)."}
		}
		if state != nil {
			_ = state.SetShutdown(false)
		}
		return CommandResult{
			Handled:   true,
			ReplyText: "🟢 *تم إعادة فتح وتشغيل السيرفرات بنجاح (Start)!*\nنوفا جاهزة الآن وتستقبل الرسائل وترد بشكل طبيعي.",
		}

	case "admin":
		if !isAdmin {
			return CommandResult{Handled: true, ReplyText: "⚠️ هذا الأمر مخصص فقط لمشرف البوت (Admin)."}
		}
		if len(parts) < 2 {
			return CommandResult{
				Handled: true,
				ReplyText: "👑 *أوامر إدارة المشرفين (Admin Management):*\n\n" +
					"• `/admin add` : لإضافة مشرف جديد (بالرد على رسالته أو كتابة رقمه مثل: `/admin add 2010xxxxxxxx`)\n" +
					"• `/admin remove 2010xxxxxxxx` : لحذف مشرف من القائمة\n" +
					"• `/admin list` : لعرض قائمة المشرفين المسجلين حالياً",
			}
		}

		action := strings.ToLower(parts[1])
		switch action {
		case "add", "اضافة", "إضافة":
			target := ""
			if len(parts) >= 3 {
				target = parts[2]
			} else if repliedSender != "" {
				target = repliedSender
			}

			if target == "" {
				return CommandResult{
					Handled: true,
					ReplyText: "⚠️ يرجى تحديد المستخدم المراد إضافته:\n• إما بالرد (Reply) على رسالته وكتابة `/admin add`\n• أو بكتابة رقمه مباشرة: `/admin add 2010xxxxxxxx`",
				}
			}

			addedNum, err := state.AddAdmin(target)
			if err != nil {
				return CommandResult{
					Handled: true,
					ReplyText: fmt.Sprintf("❌ فشل إضافة المشرف: %v", err),
				}
			}

			return CommandResult{
				Handled: true,
				ReplyText: fmt.Sprintf("✅ *تم بنجاح إضافة المشرف الجديد!*\n📱 الرقم: `%s`\nأصبح لديه الآن كامل صلاحيات إدارة وتشغيل وإيقاف البوت.", addedNum),
			}

		case "remove", "delete", "del", "حذف":
			target := ""
			if len(parts) >= 3 {
				target = parts[2]
			} else if repliedSender != "" {
				target = repliedSender
			}

			if target == "" {
				return CommandResult{
					Handled: true,
					ReplyText: "⚠️ اكتب رقم المشرف المراد حذفه، مثلاً: `/admin remove 2010xxxxxxxx` أو بالرد على رسالته.",
				}
			}

			removed, err := state.RemoveAdmin(target)
			if err != nil {
				return CommandResult{
					Handled: true,
					ReplyText: fmt.Sprintf("❌ فشل حذف المشرف: %v", err),
				}
			}
			if !removed {
				return CommandResult{
					Handled: true,
					ReplyText: "⚠️ هذا الرقم غير موجود في قائمة المشرفين المضافين.",
				}
			}

			return CommandResult{
				Handled: true,
				ReplyText: fmt.Sprintf("🗑️ *تم حذف المشرف بنجاح من قائمة الإدارة.*"),
			}

		case "list", "قائمة", "عرض":
			admins := state.GetAdminsList()
			owner := state.GetAdminNumber()
			var sb strings.Builder
			sb.WriteString("👑 *قائمة مشرفي نوفا (Nova Admins):*\n\n")
			sb.WriteString(fmt.Sprintf("⭐ *المالك الأساسي (Owner):* `%s`\n", owner))
			if len(admins) == 0 {
				sb.WriteString("\nلا يوجد مشرفين إضافيين مضافين حالياً.")
			} else {
				sb.WriteString("\n👥 *المشرفون الإضافيون (Admins):*\n")
				for i, adm := range admins {
					sb.WriteString(fmt.Sprintf("%d. `%s`\n", i+1, adm))
				}
			}
			return CommandResult{
				Handled: true,
				ReplyText: sb.String(),
			}

		default:
			return CommandResult{
				Handled: true,
				ReplyText: "⚠️ أمر غير معروف. اكتب `/admin` لعرض خيارات إدارة المشرفين.",
			}
		}

	case "thinking", "reasoning", "تفكير":
		if !isAdmin {
			return CommandResult{Handled: true, ReplyText: "⚠️ هذا الأمر مخصص فقط لمشرف البوت (Admin)."}
		}

		currentEffort := "auto"
		if state != nil {
			currentEffort = state.GetThinkingEffort()
		}

		if len(parts) < 2 {
			desc := "تلقائي وديناميكي (يفكر في المسائل والصور الصعبة فقط)"
			switch currentEffort {
			case "none":
				desc = "ملغي تماماً (سرعة قصوى ورد فوري في ثانية)"
			case "low":
				desc = "خفيف وسريع (تفكير محدود)"
			case "medium":
				desc = "متوسط"
			case "high":
				desc = "عالي ومكثف (تحليل عميق)"
			}

			msg := fmt.Sprintf("🧠 *التحكم في تفكير الذكاء الاصطناعي (Reasoning Effort):*\n\n"+
				"• *الوضع الحالي:* `%s` (%s)\n\n"+
				"💡 *للتحكم في نمط التفكير اكتب:*\n"+
				"• `/thinking auto` : تفكير تلقائي ذكي (حسب الحاجة والصعوبة)\n"+
				"• `/thinking off` : إيقاف التفكير نهائياً (سرعة خارقة ورد فوري في ثانية)\n"+
				"• `/thinking low` : تفكير خفيف وسريع\n"+
				"• `/thinking high` : تفكير عميق جداً للمسائل الرياضية المعقدة", currentEffort, desc)

			return CommandResult{Handled: true, ReplyText: msg}
		}

		targetEffort := parts[1]
		if state != nil {
			_ = state.SetThinkingEffort(targetEffort)
		}
		newEffort := state.GetThinkingEffort()
		if tc, ok := archiver.(ThinkingController); ok {
			tc.SetThinkingEffort(newEffort)
		}

		desc := "تلقائي وديناميكي"
		if newEffort == "none" {
			desc = "تم إلغاء التفكير تماماً (سرعة قصوى)"
		} else if newEffort == "low" {
			desc = "تفكير خفيف وسريع"
		} else if newEffort == "high" {
			desc = "تفكير عميق ومكثف"
		}

		return CommandResult{
			Handled:   true,
			ReplyText: fmt.Sprintf("✅ *تم بنجاح ضبط وضع تفكير النموذج على:* `%s`\n(%s)", newEffort, desc),
		}

	case "name", "callme", "لقب", "ناديني":
		if !isAdmin {
			return CommandResult{Handled: true, ReplyText: "⚠️ هذا الأمر مخصص فقط لمشرف البوت (Admin)."}
		}
		if len(parts) < 2 {
			currentNick := ""
			if state != nil {
				currentNick = state.GetChatNickname(chatID, senderID)
			}
			statusText := "غير محدد (يناديك باسمك المسجل)"
			if currentNick != "" {
				statusText = fmt.Sprintf("👑 *%s*", currentNick)
			}
			msg := fmt.Sprintf("🏷️ *تخصيص اللقب في هذا الشات فقط (Chat Nickname):*\n\n"+
				"• *اللقب الحالي لك هنا:* %s\n\n"+
				"💡 *طريقة الاستخدام:*\n"+
				"• `/name <اللقب الجديد>` (مثلاً: `/name الباشمهندس` أو `/name كيمو` أو `/name يا برنس`)\n"+
				"• لإلغاء اللقب والرجوع لاسمك العادي: `/name reset` أو `/name clear`\n\n"+
				"📌 *ملاحظة:* هذا اللقب يتم استخدامه داخل هذا الشات فقط ولن يؤثر على أي شات أو جروب تاني!", statusText)
			return CommandResult{Handled: true, ReplyText: msg}
		}

		arg := strings.Join(parts[1:], " ")
		arg = strings.TrimSpace(arg)
		lowerArg := strings.ToLower(arg)

		if lowerArg == "reset" || lowerArg == "clear" || lowerArg == "افتراضي" || lowerArg == "مسح" || lowerArg == "الغاء" || lowerArg == "إلغاء" {
			if state != nil {
				_ = state.ClearChatNickname(chatID, senderID)
			}
			return CommandResult{
				Handled:   true,
				ReplyText: "✅ *تم إلغاء اللقب الخاص في هذا الشات بنجاح!*\nنوفا هيناديك دلوقتي باسمك الطبيعي المسجل.",
			}
		}

		if len(arg) > 40 {
			return CommandResult{
				Handled:   true,
				ReplyText: "⚠️ اللقب طويل جداً، يرجى اختيار اسم أو لقب لا يتجاوز 40 حرفاً.",
			}
		}

		if state != nil {
			if err := state.SetChatNickname(chatID, senderID, arg); err != nil {
				return CommandResult{
					Handled:   true,
					ReplyText: fmt.Sprintf("❌ فشل حفظ اللقب: %v", err),
				}
			}
		}

		return CommandResult{
			Handled:   true,
			ReplyText: fmt.Sprintf("✅ *تمام يا %s! عُلم ويُنفذ.*\nمن اللحظة دي في الشات ده بس، نوفا هيناديك بـ *\"%s\"* دايماً 🫡✨", arg, arg),
		}

	case "set", "ضبط":
		if !isAdmin {
			return CommandResult{Handled: true, ReplyText: "⚠️ هذا الأمر مخصص فقط لمشرف البوت (Admin)."}
		}
		if len(parts) < 2 {
			currentLimit := 0
			if state != nil {
				currentLimit = state.GetChatLimit(chatID, 0)
			}
			limitStr := fmt.Sprintf("%d", currentLimit)
			if currentLimit <= 0 {
				limitStr = "all (كل الرسائل)"
			}
			return CommandResult{
				Handled:   true,
				ReplyText: fmt.Sprintf("ℹ️ حد السياق الحالي في هذا الشات: *%s*\nلتغييره اكتب مثلاً: `/set 500` أو `/set all`", limitStr),
			}
		}

		val := strings.ToLower(parts[1])
		var limit int
		if val == "all" || val == "كل" || val == "0" {
			limit = 0
		} else {
			n, err := strconv.Atoi(val)
			if err != nil || n < 0 {
				return CommandResult{
					Handled:   true,
					ReplyText: "⚠️ قيمة غير صالحة. اكتب رقم صحيح مثل: `/set 500` أو `/set all`",
				}
			}
			limit = n
		}

		if state != nil {
			_ = state.SetChatLimit(chatID, limit)
		}
		displayStr := fmt.Sprintf("%d رسالة", limit)
		if limit == 0 {
			displayStr = "all (كامل السجل بدون حد)"
		}

		return CommandResult{
			Handled:   true,
			ReplyText: fmt.Sprintf("✅ *تم بنجاح ضبط حد سياق الرسائل في هذا الشات على:* `%s`", displayStr),
		}

	case "auto", "triggers", "autotriggers", "تلقائي":
		if !isAdmin {
			return CommandResult{Handled: true, ReplyText: "⚠️ هذا الأمر مخصص فقط لمشرف البوت (Admin)."}
		}
		if len(parts) < 2 {
			isAuto := false
			if state != nil {
				isAuto = state.IsAutoTriggersEnabled(chatID)
			}
			statusStr := "❌ معطلة"
			if isAuto {
				statusStr = "✅ مفعلة"
			}
			return CommandResult{
				Handled:   true,
				ReplyText: fmt.Sprintf("ℹ️ حالة المشغلات الأوتوماتيكية في هذا الشات: *%s*\nلتفعيلها اكتب: `/auto on`\nلإيقافها اكتب: `/auto off`", statusStr),
			}
		}

		action := strings.ToLower(parts[1])
		if action == "on" || action == "1" || action == "تفعيل" || action == "شغال" {
			if state != nil {
				_ = state.SetAutoTriggers(chatID, true)
			}
			return CommandResult{
				Handled:   true,
				ReplyText: "🤖 *تم تفعيل المشغلات التلقائية المتدرجة في هذا الجروب بنجاح!*\n(كسر الصمت المتدرج بعد 3 ساعات ثم 6 ساعات ثم القفل التلقائي، التفاعل مع فوران الرسائل، تحية الصباح، والترحيب بالأعضاء الجدد).",
			}
		} else if action == "off" || action == "0" || action == "ايقاف" || action == "تعطيل" {
			if state != nil {
				_ = state.SetAutoTriggers(chatID, false)
			}
			return CommandResult{
				Handled:   true,
				ReplyText: "🛑 *تم إيقاف المشغلات التلقائية في هذا الجروب بنجاح.*",
			}
		}

		return CommandResult{
			Handled:   true,
			ReplyText: "⚠️ اكتب `/auto on` للتفعيل أو `/auto off` للإيقاف.",
		}

	case "archive", "ارشيف", "أرشيف", "summarize", "تلخيص":
		if !isAdmin {
			return CommandResult{Handled: true, ReplyText: "⚠️ هذا الأمر مخصص فقط لمشرف البوت (Admin)."}
		}
		if archiver == nil {
			return CommandResult{Handled: true, ReplyText: "⚠️ خدمة الأرشفة والتلخيص غير متصلة حالياً."}
		}

		chatType := "private"
		if strings.Contains(chatID, "@g.us") {
			chatType = "group"
		}

		subCmd := "new"
		if len(parts) > 1 {
			subCmd = strings.ToLower(parts[1])
		}

		if subCmd == "list" || subCmd == "عرض" || subCmd == "قائمة" {
			list, err := archiver.ListChatArchives(chatType, chatID)
			if err != nil || len(list) == 0 {
				return CommandResult{
					Handled:   true,
					ReplyText: "📭 لا توجد محادثات سابقة مؤرشفة لهذا الشات حتى الآن.",
				}
			}
			var b strings.Builder
			b.WriteString("📂 *قائمة المحادثات المؤرشفة لهذا الشات:*\n\n")
			for _, item := range list {
				b.WriteString(fmt.Sprintf("• %s\n", item))
			}
			b.WriteString("\n💡 لاسترجاع أي محادثة اكتب: `/archive load <رقم المحادثة>`")
			return CommandResult{
				Handled:   true,
				ReplyText: b.String(),
			}
		}

		if subCmd == "load" || subCmd == "restore" || subCmd == "استرجاع" {
			if len(parts) < 3 {
				return CommandResult{
					Handled:   true,
					ReplyText: "⚠️ يرجى تحديد رقم المحادثة، مثلاً: `/archive load 1`",
				}
			}
			idx, err := strconv.Atoi(parts[2])
			if err != nil || idx < 1 {
				return CommandResult{
					Handled:   true,
					ReplyText: "⚠️ رقم محادثة غير صالح.",
				}
			}
			if err := archiver.RestoreArchivedChatSession(chatType, chatID, idx); err != nil {
				return CommandResult{
					Handled:   true,
					ReplyText: fmt.Sprintf("❌ فشل استرجاع المحادثة رقم %d: %v", idx, err),
				}
			}
			return CommandResult{
				Handled:   true,
				ReplyText: fmt.Sprintf("✅ *تم بنجاح استرجاع المحادثة المؤرشفة رقم %d وتفعيلها كشات نشط!*", idx),
			}
		}

		// Default: Archive Current Chat and start fresh
		ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
		defer cancel()

		summaryPath, archiveIdx, err := archiver.ArchiveChatSession(ctx, chatType, chatID)
		if err != nil {
			return CommandResult{
				Handled:   true,
				ReplyText: fmt.Sprintf("❌ فشل أرشفة المحادثة: %v", err),
			}
		}

		return CommandResult{
			Handled:   true,
			ReplyText: fmt.Sprintf("🎉 *تمت أرشفة وتلخيص المحادثة الحالية بنجاح!*\n\n• *رقم الأرشيف:* `%d`\n• *ملف الملخص وبطاقات المستخدمين:* `%s`\n• *الحالة:* تم بدء ملف محادثة جديد ونظيف تماماً 🚀\n\n💡 يمكنك استرجاع المحادثة القديمة في أي وقت بكتابة: `/archive load %d`", archiveIdx, summaryPath, archiveIdx),
		}

	case "restore", "استرجاع":
		if !isAdmin {
			return CommandResult{Handled: true, ReplyText: "⚠️ هذا الأمر مخصص فقط لمشرف البوت (Admin)."}
		}
		if archiver == nil {
			return CommandResult{Handled: true, ReplyText: "⚠️ خدمة الأرشفة والتلخيص غير متصلة حالياً."}
		}
		if len(parts) < 2 {
			return CommandResult{
				Handled:   true,
				ReplyText: "⚠️ اكتب رقم المحادثة المراد استرجاعها، مثلاً: `/restore 1`",
			}
		}
		idx, err := strconv.Atoi(parts[1])
		if err != nil || idx < 1 {
			return CommandResult{
				Handled:   true,
				ReplyText: "⚠️ رقم محادثة غير صالح.",
			}
		}
		chatType := "private"
		if strings.Contains(chatID, "@g.us") {
			chatType = "group"
		}
		if err := archiver.RestoreArchivedChatSession(chatType, chatID, idx); err != nil {
			return CommandResult{
				Handled:   true,
				ReplyText: fmt.Sprintf("❌ فشل استرجاع المحادثة رقم %d: %v", idx, err),
			}
		}
		return CommandResult{
			Handled:   true,
			ReplyText: fmt.Sprintf("✅ *تم بنجاح استرجاع المحادثة المؤرشفة رقم %d وتفعيلها كشات نشط!*", idx),
		}

	case "persona", "mode", "شخصية", "شخصيه", "نمط":
		if !isAdmin {
			return CommandResult{Handled: true, ReplyText: "⚠️ هذا الأمر مخصص فقط لمشرف البوت (Admin)."}
		}

		currentPersona := 1
		if state != nil {
			currentPersona = state.GetPersona()
		}

		if len(parts) < 2 {
			currentName := "النمط 1 (الشباب والقهوة ☕)"
			if currentPersona == 2 {
				currentName = "النمط 2 (الجنتلمان مع البنات 🌸)"
			}
			helpMsg := fmt.Sprintf("🎭 *أنماط شخصية نوفا:*\n\n"+
				"1️⃣ *النمط 1 (الشباب والقهوة ☕):*\nالصاحب القاهري الجدع، تريقة ودية، قفشات سريعة، ونقاشات صريحة.\n\n"+
				"2️⃣ *النمط 2 (الجنتلمان والكاريزما مع البنات 🌸):*\nالذوق العالي، الاحتواء العاطفي، مناداة الاسم والدلع بذكاء، ونبرة دافئة ومهذبة.\n\n"+
				"📌 *النمط المفعّل حالياً:* *%s*\n\n"+
				"💡 *للتبديل اكتب:*\n• `/persona 1` (لتفعيل نمط الشباب)\n• `/persona 2` (لتفعيل نمط البنات)", currentName)
			return CommandResult{Handled: true, ReplyText: helpMsg}
		}

		targetMode := 1
		sub := strings.ToLower(parts[1])
		switch sub {
		case "1", "bro", "شباب", "ولاد", "ولد", "قهوة", "قهوه":
			targetMode = 1
		case "2", "girl", "girls", "بنات", "بنت", "جنتلمان", "دلع":
			targetMode = 2
		default:
			return CommandResult{
				Handled:   true,
				ReplyText: "⚠️ نمط غير معروف! اكتب:\n• `/persona 1` (لنمط الشباب)\n• `/persona 2` (لنمط البنات)",
			}
		}

		if state != nil {
			_ = state.SetPersona(targetMode)
		}

		if switcher, ok := archiver.(PersonaSwitcher); ok {
			_, _ = switcher.SwitchPersona(targetMode)
		}

		if targetMode == 1 {
			return CommandResult{
				Handled:   true,
				ReplyText: "☕ *تم التبديل إلى النمط 1 (الصاحب القاهري الجدع - نمط الشباب)!*\nالأسلوب المفعّل الآن: روح القهوة، الجدعنة، التريقة الودية، وإفيهات الصحاب.",
			}
		}

		return CommandResult{
			Handled:   true,
			ReplyText: "🌸 *تم التبديل إلى النمط 2 (الجنتلمان الكاريزما - نمط التعامل الراقي مع البنات)!*\nالأسلوب المفعّل الآن: الذوق العالي، الاحتواء العاطفي، إدارة الأسماء والدلع بذكاء، والنبرة الدافئة.",
		}

	case "status", "statue", "stats", "حالة", "تقرير":
		statusIcon := "🟢 أونلاين (شغال)"
		if state != nil && state.GetShutdown() {
			statusIcon = "🔴 مغلق مؤقتاً (Maintenance)"
		}

		autoStatus := "❌ معطلة"
		if state != nil && state.IsAutoTriggersEnabled(chatID) {
			autoStatus = "✅ مفعلة"
		}

		currentLimit := 0
		if state != nil {
			currentLimit = state.GetChatLimit(chatID, 0)
		}
		limitStr := fmt.Sprintf("%d رسالة", currentLimit)
		if currentLimit <= 0 {
			limitStr = "all (كامل السجل)"
		}

		uptime := "0 ثانية"
		if state != nil {
			uptime = state.GetUptimeString()
		}

		var totalChats, memProfiles, scheduledTasks int
		modelName := "Default"
		if stats != nil {
			totalChats = stats.GetTotalChatsCount()
			memProfiles = stats.GetMemoryProfilesCount()
			scheduledTasks = stats.GetScheduledTasksCount()
			modelName = stats.GetModelName()
		}

		msg := fmt.Sprintf("📊 *لوحة معلومات وحالة نوفا (Nova Status)*\n\n"+
			"• *حالة السيرفر:* %s\n"+
			"• *مدة التشغيل (Uptime):* %s\n"+
			"• *النماذج النشطة (Multi-Model):*\n"+
			"  - 🗣️ *الحوار والأسلوب:* `%s`\n"+
			"  - 🧮 *الرياضيات:* `z-ai/glm-5.2`\n"+
			"  - 🖼️ *الرؤية والصور:* `openai/gpt-5.6-luna`\n"+
			"  - 🎙️ *الصوت (STT):* `whisper-large-v3` (Groq)\n"+
			"• *حد سياق هذا الشات:* `%s`\n"+
			"• *المشغلات التلقائية:* %s\n"+
			"• *عدد الشاتات المسجلة:* %d\n"+
			"• *ملفات ذاكرة المستخدمين:* %d\n"+
			"• *المواعيد والمتابعات المجدولة:* %d\n",
			statusIcon, uptime, modelName, limitStr, autoStatus, totalChats, memProfiles, scheduledTasks)

		return CommandResult{
			Handled:   true,
			ReplyText: msg,
		}

	case "help", "commands", "اوامر", "أوامر":
		if !isAdmin {
			return CommandResult{
				Handled: true,
				ReplyText: "🤖 *أوامر مسابقات وتفاعل نوفا:*\n\n" +
					"• `/game` : بدء مسابقة كوكتيل عشوائي 🎮\n" +
					"• `/game christian` : مسابقة الكتاب المقدس والتاريخ الكنسي ✝️\n" +
					"• `/game quote` : مسابقة إفيهات السينما والمسرحيات 🎭\n" +
					"• `/game movie` : مسابقة أفلام وفنون ومسلسلات 🎬\n" +
					"• `/game ball` : مسابقة كرة القدم ⚽\n" +
					"• `/game sports` : مسابقة الرياضات والألعاب الأولمبية 🏅\n" +
					"• `/game tech` : مسابقة التكنولوجيا والبرمجة والذكاء الاصطناعي 💻\n" +
					"• `/game science` : مسابقة العلوم والفيزياء والكيمياء 🔬\n" +
					"• `/game space` : مسابقة الفضاء والكواكب 🪐\n" +
					"• `/game geography` : مسابقة الجغرافيا والدول والعواصم 🗺️\n" +
					"• `/game history` : مسابقة التاريخ والحضارات والفراعنة 🏛️\n" +
					"• `/game animals` : مسابقة عالم الحيوان والطبيعة 🦁\n" +
					"• `/game food` : مسابقة الأكلات والمطابخ 🍳\n" +
					"• `/game amthal` : مسابقة كمّل المثل الشعبي 📜\n" +
					"• `/game riddle` : فوازير وألغاز ذكاء 🧩\n" +
					"• `/game trivia` : ثقافة عامة ومعلومات منوعة 🧠\n" +
					"• `/game cartoon` : كرتون وأنمي وسبيستون 🎨\n" +
					"• `/game stop` : إيقاف المسابقة الحالية 🛑\n" +
					"• `/top` : عرض لوحة الصدارة وترتيب المتسابقين 🏆\n" +
					"• `/game config` : عرض وتعديل عدد الأسئلة ووقت الإجابة ⚙️\n\n" +
					"💡 *للتحدث مع نوفا:* ناديه بـ (يا نوفا) أو اعمل له ريبلاي في أي وقت!",
			}
		}

		helpMsg := "👑 *دليل أوامر الإدارة الشامل لبوت نوفا (Nova Admin Commands):*\n\n" +
			"⚙️ *التحكم والتشغيل:*\n" +
			"• `start` أو `/start` : تشغيل السيرفرات واستقبال الرسائل 🟢\n" +
			"• `shutdown` أو `/shutdown` : إيقاف السيرفرات مؤقتاً لوضع الصيانة 🔴\n" +
			"• `/status` : عرض تقرير الحالة، وقت التشغيل، والنماذج 📊\n\n" +
			"👥 *إدارة المشرفين:*\n" +
			"• `/admin add` : إضافة مشرف جديد (بالرد على رسالته أو كتابة رقمه)\n" +
			"• `/admin remove <رقم>` : إزالة مشرف\n" +
			"• `/admin list` : عرض قائمة المشرفين الحاليين\n\n" +
			"🏷️ *تخصيص اللقب والمناداة (خاص بكل شات):*\n" +
			"• `/name <اللقب>` : تحديد الاسم أو اللقب اللي نوفا هيناديك بيه في هذا الشات فقط\n" +
			"• `/name reset` : إلغاء اللقب والرجوع لاسمك الأصلي\n\n" +
			"🧠 *الذكاء والتفكير:*\n" +
			"• `/thinking auto` : تفكير تلقائي ذكي حسب صعوبة السؤال\n" +
			"• `/thinking off` : إيقاف التفكير (سرعة فائقة ورد فوري في ثانية)\n" +
			"• `/thinking low | high` : تفكير خفيف أو عميق جداً\n\n" +
			"🎭 *الشخصية وسياق الشات:*\n" +
			"• `/persona 1` : تفعيل نمط الشباب والقهوة ☕\n" +
			"• `/persona 2` : تفعيل نمط الجنتلمان مع البنات 🌸\n" +
			"• `/set <عدد|all>` : تحديد حد سياق الرسائل في الشات\n" +
			"• `/auto on|off` : تشغيل أو إيقاف المشغلات التلقائية التفاعلية\n\n" +
			"📂 *الأرشفة والتلخيص:*\n" +
			"• `/archive` : أرشفة الشات الحالي وتلخيصه وتوليد ملف جديد نظيف\n" +
			"• `/archive list` : عرض المحادثات المؤرشفة\n" +
			"• `/archive load <رقم>` : استرجاع محادثة مؤرشفة\n\n" +
			"🎮 *الألعاب والمسابقات (أقسام متخصصة + منع التكرار):*\n" +
			"• `/game` : بدء مسابقة كوكتيل عشوائي\n" +
			"• `/game <القسم>` : اختيار القسم (مسيحي، افيهات، سينما، كورة، رياضة، تكنولوجيا، علوم، فضاء، جغرافيا، تاريخ، حيوانات، اكل، امثال، فوازير، عامة، كرتون)\n" +
			"• `/game stop` : إيقاف اللعبة الحالية\n" +
			"• `/top` : عرض لوحة المتصدرين والنقاط 🏆\n" +
			"• `/game config` : عرض إعدادات اللعبة للشات\n" +
			"• `/game config rounds <عدد>` : ضبط عدد أسئلة الجولة (1 إلى 30)\n" +
			"• `/game config time <ثواني>` : ضبط مهلة السؤال (10 إلى 180 ثانية)\n" +
			"• `/game config reset` : استرجاع الإعدادات الافتراضية"

		return CommandResult{Handled: true, ReplyText: helpMsg}
	}

	return CommandResult{Handled: false}
}
