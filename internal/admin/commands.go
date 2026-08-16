package admin

import (
	"fmt"
	"strconv"
	"strings"
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

// HandleAdminCommand parses and executes any admin command sent by the verified admin.
func HandleAdminCommand(
	state *State,
	chatID string,
	senderID string,
	text string,
	stats StatsProvider,
) CommandResult {
	cleanText := strings.TrimSpace(text)
	if !strings.HasPrefix(cleanText, "/") {
		return CommandResult{Handled: false}
	}

	// Verify admin authority
	if !state.IsAdmin(senderID) {
		return CommandResult{Handled: false}
	}

	parts := strings.Fields(cleanText)
	if len(parts) == 0 {
		return CommandResult{Handled: false}
	}

	cmd := strings.ToLower(parts[0])

	switch cmd {
	case "/shutdown", "/suhtdown":
		_ = state.SetShutdown(true)
		return CommandResult{
			Handled:   true,
			ReplyText: "🔴 *تم إغلاق السيرفرات مؤقتاً بنجاح!*\nعند مناداة نوفا، سيتم إرسال رسالة التوقف التلقائية فوراً وبدون استهلاك أي رصيد.",
		}

	case "/start", "/resume", "/restart":
		_ = state.SetShutdown(false)
		return CommandResult{
			Handled:   true,
			ReplyText: "🟢 *تم إعادة فتح وتشغيل السيرفرات بنجاح!*\nنوفا جاهزة الآن وتستقبل الرسائل وترد بشكل طبيعي.",
		}

	case "/set":
		if len(parts) < 2 {
			currentLimit := state.GetChatLimit(chatID, 0)
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

		_ = state.SetChatLimit(chatID, limit)
		displayStr := fmt.Sprintf("%d رسالة", limit)
		if limit == 0 {
			displayStr = "all (كامل السجل بدون حد)"
		}

		return CommandResult{
			Handled:   true,
			ReplyText: fmt.Sprintf("✅ *تم بنجاح ضبط حد سياق الرسائل في هذا الشات على:* `%s`", displayStr),
		}

	case "/auto", "/triggers", "/autotriggers":
		if len(parts) < 2 {
			isAuto := state.IsAutoTriggersEnabled(chatID)
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
			_ = state.SetAutoTriggers(chatID, true)
			return CommandResult{
				Handled:   true,
				ReplyText: "🤖 *تم تفعيل الـ 5 مشغلات التلقائية في هذا الجروب بنجاح!*\n(كسر الصمت بعد 3 ساعات، السؤال عن الغايبين، التفاعل مع فوران الرسائل، تحية الصباح، والترحيب بالأعضاء الجدد).",
			}
		} else if action == "off" || action == "0" || action == "ايقاف" || action == "تعطيل" {
			_ = state.SetAutoTriggers(chatID, false)
			return CommandResult{
				Handled:   true,
				ReplyText: "🛑 *تم إيقاف المشغلات التلقائية في هذا الجروب بنجاح.*",
			}
		}

		return CommandResult{
			Handled:   true,
			ReplyText: "⚠️ اكتب `/auto on` للتفعيل أو `/auto off` للإيقاف.",
		}

	case "/status", "/statue":
		statusIcon := "🟢 أونلاين (شغال)"
		if state.GetShutdown() {
			statusIcon = "🔴 مغلق مؤقتاً (Maintenance)"
		}

		autoStatus := "❌ معطلة"
		if state.IsAutoTriggersEnabled(chatID) {
			autoStatus = "✅ مفعلة"
		}

		currentLimit := state.GetChatLimit(chatID, 0)
		limitStr := fmt.Sprintf("%d رسالة", currentLimit)
		if currentLimit <= 0 {
			limitStr = "all (كامل السجل)"
		}

		uptime := state.GetUptimeString()

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
			"• *النموذج النشط:* `%s`\n"+
			"• *حد سياق هذا الشات:* `%s`\n"+
			"• *المشغلات التلقائية في الجروب:* %s\n"+
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
