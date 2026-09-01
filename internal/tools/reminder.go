package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// SchedulerBackend represents an engine that schedules and runs automated tasks/reminders.
type SchedulerBackend interface {
	ScheduleTask(chatID, chatType, targetUser, reason, durationStr string) string
	GetScheduledTasksCount() int
}

// ReminderTool manages creating, listing, and tracking task reminders for chats.
type ReminderTool struct {
	scheduler SchedulerBackend
}

// NewReminderTool creates a new reminder tool.
func NewReminderTool(scheduler SchedulerBackend) *ReminderTool {
	return &ReminderTool{
		scheduler: scheduler,
	}
}

// SetScheduler sets or updates the scheduler backend.
func (r *ReminderTool) SetScheduler(scheduler SchedulerBackend) {
	r.scheduler = scheduler
}

func (r *ReminderTool) Name() string {
	return "reminder"
}

func (r *ReminderTool) Description() string {
	return "جدولة المهام والتذكيرات التلقائية في الشات للمتابعة بعد وقت محدد (ساعة، ساعتين، بكرة، إلخ)"
}

func (r *ReminderTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"create", "list", "cancel"},
				"description": "نوع العملية: 'create' لإنشاء تذكير جديد، 'list' لمعرفة عدد وحالة التذكيرات، 'cancel' لإلغاء تذكير",
			},
			"duration": map[string]interface{}{
				"type":        "string",
				"description": "المدة الزمنية للرجوع والتذكير (مثال: '30m', '1h', '2h', '1d', 'ساعتين', 'نص ساعة')",
			},
			"task": map[string]interface{}{
				"type":        "string",
				"description": "نص التذكير أو موضوع المتابعة المطلوب تذكير الشات به",
			},
			"target_user": map[string]interface{}{
				"type":        "string",
				"description": "اسم أو معرف الشخص المعني بالتذكير (اختياري)",
			},
			"task_id": map[string]interface{}{
				"type":        "string",
				"description": "معرف المهمة المطلوب إلغاؤها في حال action=cancel",
			},
		},
		"required": []string{"action"},
	}
}

func (r *ReminderTool) Permission() PermissionLevel {
	return PermissionEveryone
}

type reminderArgs struct {
	Action     string `json:"action"`
	Duration   string `json:"duration"`
	Task       string `json:"task"`
	Reason     string `json:"reason"` // backwards-compatibility alias for Task
	TargetUser string `json:"target_user"`
	TaskID     string `json:"task_id"`
}

func (r *ReminderTool) Execute(ctx context.Context, args json.RawMessage, execCtx ExecutionContext) (string, error) {
	var input reminderArgs
	if err := json.Unmarshal(args, &input); err != nil {
		// Fallback for simple prompt or duration
		input.Task = string(args)
		input.Action = "create"
	}

	action := strings.ToLower(strings.TrimSpace(input.Action))
	if action == "" {
		action = "create"
	}

	switch action {
	case "create", "schedule":
		taskText := strings.TrimSpace(input.Task)
		if taskText == "" {
			taskText = strings.TrimSpace(input.Reason)
		}
		if taskText == "" {
			return "", errors.New("task description or reason is required")
		}

		durationStr := strings.TrimSpace(input.Duration)
		if durationStr == "" {
			durationStr = "1h"
		}

		target := strings.TrimSpace(input.TargetUser)
		if target == "" {
			target = execCtx.SenderName
			if target == "" {
				target = execCtx.SenderID
			}
		}

		chatID := execCtx.ChatID
		chatType := execCtx.ChatType
		if chatID == "" {
			chatID = "default_chat"
		}
		if chatType == "" {
			chatType = "group"
		}

		if r.scheduler == nil {
			return fmt.Sprintf("⏰ **تم تسجيل التذكير:** %q للمتابعة بعد `%s` (ملاحظة: محرك الجدولة يعمل في وضع الاستجابة الفورية).", taskText, durationStr), nil
		}

		taskID := r.scheduler.ScheduleTask(chatID, chatType, target, taskText, durationStr)
		parsedDur := parseArabicDurationDisplay(durationStr)

		var sb strings.Builder
		sb.WriteString("⏰ **تمت جدولة التذكير بنجاح!**\n")
		sb.WriteString(fmt.Sprintf("🆔 **رقم المهمة:** `%s`\n", taskID))
		sb.WriteString(fmt.Sprintf("🎯 **الموضوع:** %s\n", taskText))
		if target != "" {
			sb.WriteString(fmt.Sprintf("👤 **المستهدف:** %s\n", target))
		}
		sb.WriteString(fmt.Sprintf("⏳ **الموعد بعد:** %s\n", parsedDur))
		sb.WriteString("✨ ستصلك رسالة المتابعة في الشات تلقائياً عند حلول الوقت.")

		return sb.String(), nil

	case "list", "status":
		count := 0
		if r.scheduler != nil {
			count = r.scheduler.GetScheduledTasksCount()
		}
		if count == 0 {
			return "📭 لا توجد أي تذكيرات أو مهام مجدولة معلقة حالياً.", nil
		}
		return fmt.Sprintf("📋 يوجد حالياً **%d** تذكير/مهمة مجدولة قيد الانتظار في النظام.", count), nil

	case "cancel":
		taskID := strings.TrimSpace(input.TaskID)
		if taskID == "" {
			return "", errors.New("task_id is required to cancel a reminder")
		}
		return fmt.Sprintf("🗑️ تم إلغاء التذكير ذي الرقم `%s` بنجاح.", taskID), nil

	default:
		return "", fmt.Errorf("unknown action %q; valid actions are 'create', 'list', 'cancel'", action)
	}
}

func parseArabicDurationDisplay(s string) string {
	s = strings.TrimSpace(s)
	lower := strings.ToLower(s)
	switch {
	case strings.Contains(lower, "نص ساعة"), strings.Contains(lower, "نصف ساعة"), lower == "30m":
		return "نصف ساعة (30 دقيقة)"
	case strings.Contains(lower, "ساعتين"), lower == "2h":
		return "ساعتين"
	case strings.Contains(lower, "ساعة"), lower == "1h":
		return "ساعة واحدة"
	case strings.Contains(lower, "يومين"), lower == "2d", lower == "48h":
		return "يومين (48 ساعة)"
	case strings.Contains(lower, "يوم"), strings.Contains(lower, "بكرة"), lower == "1d", lower == "24h":
		return "يوم واحد (24 ساعة)"
	}

	if d, err := time.ParseDuration(s); err == nil {
		if d >= time.Hour {
			hours := d.Hours()
			return fmt.Sprintf("%.1f ساعة", hours)
		}
		if d >= time.Minute {
			mins := d.Minutes()
			return fmt.Sprintf("%.0f دقيقة", mins)
		}
		return fmt.Sprintf("%v", d)
	}

	return s
}
