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

// HandleAdminCommand parses and executes admin and archive commands.
func HandleAdminCommand(
	state *State,
	chatID string,
	senderID string,
	senderName string,
	isFromMe bool,
	text string,
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
	case "shutdown", "suhtdown", "قفل", "اغلاق", "إغلاق":
		if !isAdmin {
			return CommandResult{Handled: true, ReplyText: "⚠️ هذا الأمر مخصص فقط لمشرف البوت (Admin)."}
		}
		if state != nil {
			_ = state.SetShutdown(true)
		}
		return CommandResult{
			Handled:   true,
			ReplyText: "🔴 *تم إغلاق السيرفرات مؤقتاً بنجاح!*\nعند مناداة نوفا، سيتم إرسال رسالة التوقف التلقائية فوراً وبدون استهلاك أي رصيد.",
		}

	case "start", "resume", "restart", "تشغيل", "فتح":
		if !isAdmin {
			return CommandResult{Handled: true, ReplyText: "⚠️ هذا الأمر مخصص فقط لمشرف البوت (Admin)."}
		}
		if state != nil {
			_ = state.SetShutdown(false)
		}
		return CommandResult{
			Handled:   true,
			ReplyText: "🟢 *تم إعادة فتح وتشغيل السيرفرات بنجاح!*\nنوفا جاهزة الآن وتستقبل الرسائل وترد بشكل طبيعي.",
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
	}

	return CommandResult{Handled: false}
}
