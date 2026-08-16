package trigger

import (
	"fmt"
	"time"

	"novabot/internal/storage"
)

// RepliedToInfo contains details about the message being replied to.
type RepliedToInfo struct {
	MessageID  string `json:"message_id"`
	SenderName string `json:"sender_name"`
	Text       string `json:"text"`
}

// ContextMessage represents a single message in the recent conversation context.
type ContextMessage struct {
	SenderName string `json:"sender_name"`
	Text       string `json:"text"`
	IsNova     bool   `json:"is_nova"`
	Timestamp  string `json:"timestamp"`
}

// RequestPayload represents the complete payload sent to the LLM.
type RequestPayload struct {
	ChatType             string           `json:"chat_type"` // "private" | "group"
	ChatID               string           `json:"chat_id"`
	ChatName             *string          `json:"chat_name"`
	SenderID             string           `json:"sender_id"`
	SenderName           string           `json:"sender_name"`
	MessageID            string           `json:"message_id"`
	MessageText          string           `json:"message_text"` // Raw original text
	IsReply              bool             `json:"is_reply"`
	RepliedTo            *RepliedToInfo   `json:"replied_to"`
	TriggerMatched       bool             `json:"trigger_matched"`
	RecentContext        []ContextMessage `json:"recent_context"`
	UserMemory           *string          `json:"user_memory"`
	CurrentTime          string           `json:"current_time"`
	TimeOfDay            string           `json:"time_of_day"`
	TimeSinceLastMessage string           `json:"time_since_last_message"`
	MediaDataURL         *string          `json:"media_data_url,omitempty"`
}

func getCairoTime() time.Time {
	loc, err := time.LoadLocation("Africa/Cairo")
	if err != nil {
		// Fallback to UTC+3 (Egypt Standard Time)
		loc = time.FixedZone("EEST", 3*3600)
	}
	return time.Now().In(loc)
}

func formatArabicTime(t time.Time) (string, string) {
	weekdays := []string{"الأحد", "الإثنين", "الثلاثاء", "الأربعاء", "الخميس", "الجمعة", "السبت"}
	months := []string{"يناير", "فبراير", "مارس", "أبريل", "مايو", "يونيو", "يوليو", "أغسطس", "سبتمبر", "أكتوبر", "نوفمبر", "ديسمبر"}

	weekday := weekdays[t.Weekday()]
	month := months[t.Month()-1]
	hour := t.Hour()
	minute := t.Minute()

	period := "صباحاً"
	displayHour := hour
	if hour >= 12 {
		period = "مساءً"
		if hour > 12 {
			displayHour = hour - 12
		}
	} else if hour == 0 {
		displayHour = 12
	}

	formatted := fmt.Sprintf("%s %d %s %d، %02d:%02d %s (توقيت مصر)",
		weekday, t.Day(), month, t.Year(), displayHour, minute, period)

	var timeOfDay string
	switch {
	case hour >= 5 && hour < 12:
		timeOfDay = "فترة الصباح (Morning)"
	case hour >= 12 && hour < 16:
		timeOfDay = "فترة الظهيرة (Afternoon)"
	case hour >= 16 && hour < 19:
		timeOfDay = "فترة العصر والمغرب (Late Afternoon)"
	case hour >= 19 && hour < 24:
		timeOfDay = "فترة الليل والسهرة (Evening)"
	default:
		timeOfDay = "فترة الفجرية ومنتصف الليل المتأخر (Late Night / Dawn)"
	}

	return formatted, timeOfDay
}

func formatDurationArabic(d time.Duration) string {
	if d < time.Minute {
		return "منذ لحظات (ورا بعض على طول)"
	}
	minutes := int(d.Minutes())
	if minutes < 60 {
		if minutes == 1 {
			return "منذ دقيقة واحدة"
		} else if minutes == 2 {
			return "منذ دقيقتين"
		} else if minutes <= 10 {
			return fmt.Sprintf("منذ %d دقائق", minutes)
		}
		return fmt.Sprintf("منذ %d دقيقة", minutes)
	}

	hours := int(d.Hours())
	if hours < 24 {
		if hours == 1 {
			return "منذ ساعة واحدة"
		} else if hours == 2 {
			return "منذ ساعتين"
		} else if hours <= 10 {
			return fmt.Sprintf("منذ %d ساعات", hours)
		}
		return fmt.Sprintf("منذ %d ساعة", hours)
	}

	days := hours / 24
	if days == 1 {
		return "منذ يوم واحد (أمس)"
	} else if days == 2 {
		return "منذ يومين"
	} else if days <= 10 {
		return fmt.Sprintf("منذ %d أيام", days)
	}
	return fmt.Sprintf("منذ %d يوماً", days)
}

// BuildContext constructs the RequestPayload using recent chat history, user memory, time awareness, and media attachments.
func BuildContext(
	chatLogger *storage.ChatLogger,
	memStore *storage.MemoryStore,
	chatType string,
	chatID string,
	chatName string,
	senderID string,
	senderName string,
	messageID string,
	rawMessageText string,
	isReply bool,
	repliedTo *RepliedToInfo,
	mediaDataURL *string,
	historyLimit int,
) (*RequestPayload, error) {
	// Fetch recent messages
	recentLogs, err := chatLogger.GetRecentMessages(chatType, chatID, historyLimit)
	if err != nil {
		return nil, err
	}

	recentContext := make([]ContextMessage, 0, len(recentLogs))
	var lastMsgTimestamp string

	for _, log := range recentLogs {
		// Avoid duplicating the current incoming message if it was just logged
		if log.MessageID == messageID {
			continue
		}
		recentContext = append(recentContext, ContextMessage{
			SenderName: log.SenderName,
			Text:       log.Text,
			IsNova:     log.IsNova,
			Timestamp:  log.Timestamp,
		})
		lastMsgTimestamp = log.Timestamp
	}

	// Calculate time and elapsed duration
	now := getCairoTime()
	currTimeStr, timeOfDay := formatArabicTime(now)

	timeSinceLast := "محادثة جديدة أو أول رسالة"
	if lastMsgTimestamp != "" {
		if parsedTime, err := time.Parse(time.RFC3339, lastMsgTimestamp); err == nil {
			elapsed := now.Sub(parsedTime)
			if elapsed > 0 {
				timeSinceLast = formatDurationArabic(elapsed)
			}
		}
	}

	// Fetch user memory
	var userMemPtr *string
	if memStore != nil && senderID != "" {
		mem, err := memStore.GetUserMemory(senderID)
		if err == nil && mem != "" {
			userMemPtr = &mem
		}
	}

	var chatNamePtr *string
	if chatName != "" {
		chatNamePtr = &chatName
	}

	return &RequestPayload{
		ChatType:             chatType,
		ChatID:               chatID,
		ChatName:             chatNamePtr,
		SenderID:             senderID,
		SenderName:           senderName,
		MessageID:            messageID,
		MessageText:          rawMessageText,
		IsReply:              isReply,
		RepliedTo:            repliedTo,
		TriggerMatched:       true,
		RecentContext:        recentContext,
		UserMemory:           userMemPtr,
		CurrentTime:          currTimeStr,
		TimeOfDay:            timeOfDay,
		TimeSinceLastMessage: timeSinceLast,
		MediaDataURL:         mediaDataURL,
	}, nil
}
