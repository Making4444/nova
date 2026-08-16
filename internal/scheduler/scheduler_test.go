package scheduler

import (
	"os"
	"testing"
	"time"

	"novabot/internal/admin"
)

func TestParseDurationFlexible(t *testing.T) {
	tests := []struct {
		input    string
		expected time.Duration
	}{
		{"2h", 2 * time.Hour},
		{"30m", 30 * time.Minute},
		{"ساعتين", 2 * time.Hour},
		{"ساعة", 1 * time.Hour},
		{"نص ساعة", 30 * time.Minute},
		{"يوم", 24 * time.Hour},
		{"invalid_string", 1 * time.Hour}, // fallback
	}

	for _, tt := range tests {
		got := ParseDurationFlexible(tt.input)
		if got != tt.expected {
			t.Errorf("ParseDurationFlexible(%q) = %v, expected %v", tt.input, got, tt.expected)
		}
	}
}

func TestSchedulerTaskLifecycle(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "novabot_sched_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	state, _ := admin.NewState(tempDir, "201202172699")
	engine := NewEngine(tempDir, state, nil, nil, nil, nil)

	if engine.GetScheduledTasksCount() != 0 {
		t.Errorf("expected 0 initial tasks")
	}

	taskID := engine.ScheduleTask("chat123", "group", "Makari", "متابعة الدكتور", "2h")
	if taskID == "" {
		t.Errorf("expected non-empty task ID")
	}

	if engine.GetScheduledTasksCount() != 1 {
		t.Errorf("expected 1 task after schedule, got %d", engine.GetScheduledTasksCount())
	}
}
