package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// -------------------------------------------------------------
// 1. Registry Unit Tests
// -------------------------------------------------------------

type dummyTool struct {
	name       string
	desc       string
	permission PermissionLevel
	result     string
}

func (d *dummyTool) Name() string { return d.name }
func (d *dummyTool) Description() string { return d.desc }
func (d *dummyTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"input": map[string]interface{}{"type": "string"},
		},
	}
}
func (d *dummyTool) Permission() PermissionLevel { return d.permission }
func (d *dummyTool) Execute(ctx context.Context, args json.RawMessage, execCtx ExecutionContext) (string, error) {
	return d.result, nil
}

func TestRegistry_RegisterAndDiscovery(t *testing.T) {
	reg := NewRegistry()

	publicTool := &dummyTool{name: "public_tool", desc: "For everyone", permission: PermissionEveryone, result: "public_ok"}
	adminTool := &dummyTool{name: "admin_tool", desc: "Admin only", permission: PermissionAdminOnly, result: "admin_ok"}

	if err := reg.Register(publicTool); err != nil {
		t.Fatalf("failed to register public tool: %v", err)
	}
	if err := reg.Register(adminTool); err != nil {
		t.Fatalf("failed to register admin tool: %v", err)
	}

	if reg.Count() != 2 {
		t.Fatalf("expected 2 tools, got %d", reg.Count())
	}

	// Test Get
	tool, ok := reg.Get("public_tool")
	if !ok || tool.Name() != "public_tool" {
		t.Errorf("expected to find public_tool")
	}

	_, ok = reg.Get("non_existent")
	if ok {
		t.Errorf("expected non_existent tool to return false")
	}

	// Test List permissions
	everyoneTools := reg.List(false)
	if len(everyoneTools) != 1 || everyoneTools[0].Name() != "public_tool" {
		t.Errorf("expected 1 tool for non-admin, got %d", len(everyoneTools))
	}

	adminTools := reg.List(true)
	if len(adminTools) != 2 {
		t.Errorf("expected 2 tools for admin, got %d", len(adminTools))
	}

	// Test Tool Definitions
	defs := reg.ToToolDefinitions(false)
	if len(defs) != 1 || defs[0].Function.Name != "public_tool" {
		t.Errorf("expected 1 tool definition for non-admin")
	}

	adminDefs := reg.ToToolDefinitions(true)
	if len(adminDefs) != 2 {
		t.Errorf("expected 2 tool definitions for admin")
	}

	// Test Unregister
	reg.Unregister("admin_tool")
	if reg.Count() != 1 {
		t.Errorf("expected 1 tool after unregistering, got %d", reg.Count())
	}
}

func TestRegistry_ExecutionAndPermissions(t *testing.T) {
	reg := NewRegistry()

	publicTool := &dummyTool{name: "ping", desc: "ping", permission: PermissionEveryone, result: "pong"}
	adminTool := &dummyTool{name: "reboot", desc: "reboot", permission: PermissionAdminOnly, result: "rebooting"}

	_ = reg.Register(publicTool)
	_ = reg.Register(adminTool)

	ctx := context.Background()

	// 1. Non-admin calls public tool -> OK
	out, err := reg.Execute(ctx, "ping", []byte(`{}`), ExecutionContext{IsAdmin: false})
	if err != nil || out != "pong" {
		t.Errorf("expected 'pong', got out=%q, err=%v", out, err)
	}

	// 2. Non-admin calls admin tool -> Permission Denied Error
	_, err = reg.Execute(ctx, "reboot", []byte(`{}`), ExecutionContext{IsAdmin: false})
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("expected permission denied error for non-admin, got %v", err)
	}

	// 3. Admin calls admin tool -> OK
	out, err = reg.Execute(ctx, "reboot", []byte(`{}`), ExecutionContext{IsAdmin: true})
	if err != nil || out != "rebooting" {
		t.Errorf("expected 'rebooting', got out=%q, err=%v", out, err)
	}

	// 4. Unknown tool -> Not Found Error
	_, err = reg.Execute(ctx, "unknown", []byte(`{}`), ExecutionContext{IsAdmin: true})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected tool not found error, got %v", err)
	}
}

// -------------------------------------------------------------
// 2. Calculator Unit Tests
// -------------------------------------------------------------

func TestCalculator_Arithmetic(t *testing.T) {
	tests := []struct {
		expr     string
		expected float64
	}{
		{"2 + 2", 4},
		{"10 - 3 * 2", 4},
		{"(10 - 3) * 2", 14},
		{"2 ^ 10", 1024},
		{"100 / 4", 25},
		{"-5 + 10", 5},
		{"2 * -3", -6},
		{"15 % 4", 3},
		{"5!", 120},
		{"sqrt(144)", 12},
		{"abs(-42)", 42},
		{"min(10, 20)", 10},
		{"max(10, 20)", 20},
		{"round(3.7)", 4},
		{"floor(3.7)", 3},
		{"ceil(3.2)", 4},
		{"log10(1000)", 3},
		{"٢ + ٣ * ٤", 14}, // Arabic numerals
	}

	for _, tt := range tests {
		val, err := EvaluateExpression(NormalizeArabicMath(tt.expr))
		if err != nil {
			t.Errorf("EvaluateExpression(%q) returned error: %v", tt.expr, err)
			continue
		}
		if val != tt.expected {
			t.Errorf("EvaluateExpression(%q) = %v; want %v", tt.expr, val, tt.expected)
		}
	}
}

func TestCalculator_Percentages(t *testing.T) {
	calc := NewCalculatorTool()
	ctx := context.Background()

	cases := []struct {
		input       string
		mustContain string
	}{
		{`{"expression": "500 + 15%"}`, "575"},
		{`{"expression": "500 - 20%"}`, "400"},
		{`{"expression": "20% of 500"}`, "100"},
		{`{"expression": "50 as % of 200"}`, "25%"},
		{`{"expression": "٢٠٪ من ٥٠٠"}`, "100"},
		{`{"expression": "٥٠٠ + ١٥٪"}`, "575"},
	}

	for _, c := range cases {
		out, err := calc.Execute(ctx, []byte(c.input), ExecutionContext{})
		if err != nil {
			t.Errorf("Calculator.Execute(%s) error: %v", c.input, err)
			continue
		}
		if !strings.Contains(out, c.mustContain) {
			t.Errorf("Calculator.Execute(%s) = %q; expected to contain %q", c.input, out, c.mustContain)
		}
	}
}

func TestCalculator_Equations(t *testing.T) {
	calc := NewCalculatorTool()
	ctx := context.Background()

	// Linear equation: 2x + 6 = 14 -> x = 4
	out, err := calc.Execute(ctx, []byte(`{"expression": "2x + 6 = 14"}`), ExecutionContext{})
	if err != nil {
		t.Fatalf("Linear equation error: %v", err)
	}
	if !strings.Contains(out, "x = 4") {
		t.Errorf("expected 'x = 4' in output, got: %s", out)
	}

	// Linear equation: 3x + 5 = x + 13 -> 2x = 8 -> x = 4
	out, err = calc.Execute(ctx, []byte(`{"expression": "3x + 5 = x + 13"}`), ExecutionContext{})
	if err != nil {
		t.Fatalf("Linear equation error: %v", err)
	}
	if !strings.Contains(out, "x = 4") {
		t.Errorf("expected 'x = 4' in output, got: %s", out)
	}

	// Quadratic equation: x^2 - 5x + 6 = 0 -> x1 = 3, x2 = 2
	out, err = calc.Execute(ctx, []byte(`{"expression": "x^2 - 5x + 6 = 0"}`), ExecutionContext{})
	if err != nil {
		t.Fatalf("Quadratic equation error: %v", err)
	}
	if !strings.Contains(out, "3") || !strings.Contains(out, "2") {
		t.Errorf("expected roots 3 and 2 in output, got: %s", out)
	}

	// Quadratic equation: x^2 - 16 = 0 -> x1 = 4, x2 = -4
	out, err = calc.Execute(ctx, []byte(`{"expression": "x^2 - 16 = 0"}`), ExecutionContext{})
	if err != nil {
		t.Fatalf("Quadratic equation error: %v", err)
	}
	if !strings.Contains(out, "4") || !strings.Contains(out, "-4") {
		t.Errorf("expected roots 4 and -4 in output, got: %s", out)
	}
}

func TestCalculator_ErrorHandling(t *testing.T) {
	calc := NewCalculatorTool()
	ctx := context.Background()

	// Division by zero
	out, _ := calc.Execute(ctx, []byte(`{"expression": "10 / 0"}`), ExecutionContext{})
	if !strings.Contains(out, "القسمة على صفر") && !strings.Contains(out, "خطأ") {
		t.Errorf("expected division by zero error message, got: %s", out)
	}

	// Unmatched parentheses
	out, _ = calc.Execute(ctx, []byte(`{"expression": "(5 + 2"}`), ExecutionContext{})
	if !strings.Contains(out, "خطأ") {
		t.Errorf("expected syntax error message, got: %s", out)
	}
}

// -------------------------------------------------------------
// 3. Web Reader Unit Tests
// -------------------------------------------------------------

func TestWebReader_Extraction(t *testing.T) {
	// Mock HTTP Server
	htmlContent := `
	<!DOCTYPE html>
	<html>
	<head>
		<title>مقال تجريبي عن الذكاء الاصطناعي</title>
		<meta name="description" content="هذا المقال يشرح تطور نماذج الذكاء الاصطناعي.">
		<meta name="author" content="د. أحمد">
		<style>body { font-size: 14px; }</style>
		<script>console.log("ignore me");</script>
	</head>
	<body>
		<header><nav><a href="/home">الرئيسية</a></nav></header>
		<main>
			<h1>مقدمة في الذكاء الاصطناعي</h1>
			<p>يعتبر الذكاء الاصطناعي من أهم الثورات التقنية في العصر الحديث.</p>
			<h2>أهم التطبيقات</h2>
			<ul>
				<li>معالجة اللغات الطبيعية</li>
				<li>الرؤية الحاسوبية</li>
			</ul>
			<blockquote>العلم يبني بيوتا لا عماد لها.</blockquote>
		</main>
		<footer>حقوق النشر 2026</footer>
	</body>
	</html>
	`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(htmlContent))
	}))
	defer server.Close()

	reader := NewWebReaderTool()
	reader.SetAllowPrivateIPs(true) // Allow test server localhost

	ctx := context.Background()
	args, _ := json.Marshal(map[string]interface{}{
		"url": server.URL,
	})

	out, err := reader.Execute(ctx, args, ExecutionContext{})
	if err != nil {
		t.Fatalf("WebReader execution failed: %v", err)
	}

	if !strings.Contains(out, "مقال تجريبي عن الذكاء الاصطناعي") {
		t.Errorf("expected title in output, got: %s", out)
	}
	if !strings.Contains(out, "معالجة اللغات الطبيعية") {
		t.Errorf("expected list content in output, got: %s", out)
	}
	if strings.Contains(out, "console.log") || strings.Contains(out, "font-size") {
		t.Errorf("script/style tags were not stripped properly: %s", out)
	}
}

func TestWebReader_SSRFProtection(t *testing.T) {
	reader := NewWebReaderTool()
	reader.SetAllowPrivateIPs(false) // Strict SSRF check

	ctx := context.Background()
	args, _ := json.Marshal(map[string]interface{}{
		"url": "http://127.0.0.1:8080/secret",
	})

	out, err := reader.Execute(ctx, args, ExecutionContext{})
	if err != nil {
		t.Fatalf("unexpected execution error: %v", err)
	}

	if !strings.Contains(out, "forbidden") && !strings.Contains(out, "خطأ") {
		t.Errorf("expected SSRF block message for 127.0.0.1, got: %s", out)
	}
}

// -------------------------------------------------------------
// 4. Weather Tool Unit Tests
// -------------------------------------------------------------

func TestWeather_WttrParsing(t *testing.T) {
	mockWttrJSON := `{
		"current_condition": [{
			"temp_C": "26",
			"temp_F": "79",
			"FeelsLikeC": "27",
			"FeelsLikeF": "81",
			"humidity": "50",
			"windspeedKmph": "15",
			"winddir16Point": "NE",
			"uvIndex": "5",
			"visibility": "10",
			"weatherDesc": [{"value": "Sunny"}],
			"lang_ar": [{"value": "مشمس وصافٍ"}]
		}],
		"nearest_area": [{
			"areaName": [{"value": "Cairo"}],
			"country": [{"value": "Egypt"}]
		}],
		"weather": [
			{"date": "2026-09-01", "maxtempC": "30", "mintempC": "20", "hourly": []},
			{"date": "2026-09-02", "maxtempC": "31", "mintempC": "21", "hourly": [{"lang_ar": [{"value": "مشمس"}]}]}
		]
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(mockWttrJSON))
	}))
	defer server.Close()

	weather := NewWeatherTool()
	weather.SetBaseURLs(server.URL, "", "")

	ctx := context.Background()
	args, _ := json.Marshal(map[string]interface{}{
		"location": "Cairo",
	})

	out, err := weather.Execute(ctx, args, ExecutionContext{})
	if err != nil {
		t.Fatalf("Weather.Execute error: %v", err)
	}

	if !strings.Contains(out, "Cairo") || !strings.Contains(out, "26°C") {
		t.Errorf("expected Cairo temperature in output, got: %s", out)
	}
	if !strings.Contains(out, "50%") {
		t.Errorf("expected humidity in output, got: %s", out)
	}
	if !strings.Contains(out, "توقعات الأيام القادمة") {
		t.Errorf("expected forecast section in output, got: %s", out)
	}
}

func TestWeather_OpenMeteoFallback(t *testing.T) {
	// Geocoding server
	geoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"results": [{
				"name": "Alexandria",
				"latitude": 31.2,
				"longitude": 29.9,
				"country": "Egypt"
			}]
		}`))
	}))
	defer geoServer.Close()

	// Forecast server
	foreServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"current": {
				"temperature_2m": 24.5,
				"relative_humidity_2m": 60,
				"apparent_temperature": 25.0,
				"weather_code": 0,
				"wind_speed_10m": 12.0
			},
			"daily": {
				"time": ["2026-09-01", "2026-09-02"],
				"weather_code": [0, 1],
				"temperature_2m_max": [27.0, 28.0],
				"temperature_2m_min": [19.0, 20.0]
			}
		}`))
	}))
	defer foreServer.Close()

	weather := NewWeatherTool()
	// Point wttr to invalid server to trigger OpenMeteo fallback
	weather.SetBaseURLs("http://invalid.wttr.test", geoServer.URL, foreServer.URL)

	ctx := context.Background()
	args, _ := json.Marshal(map[string]interface{}{
		"location": "Alexandria",
	})

	out, err := weather.Execute(ctx, args, ExecutionContext{})
	if err != nil {
		t.Fatalf("Weather fallback error: %v", err)
	}

	if !strings.Contains(out, "Alexandria") || !strings.Contains(out, "24.5°C") {
		t.Errorf("expected Alexandria fallback weather in output, got: %s", out)
	}
}

// -------------------------------------------------------------
// 5. Web Search Unit Tests
// -------------------------------------------------------------

type mockSearchProvider struct {
	response string
}

func (m *mockSearchProvider) Search(ctx context.Context, query string) (string, error) {
	return fmt.Sprintf("نتائج البحث عن %s من موقع wikipedia.org و reuters.com بتاريخ 2026: تم تأكيد البيانات بنسبة 95%%.", query), nil
}

func TestWebSearch_AgenticMultiStepAndCredibility(t *testing.T) {
	mockProv := &mockSearchProvider{}
	searchTool := NewWebSearchTool(mockProv)

	ctx := context.Background()
	args, _ := json.Marshal(map[string]interface{}{
		"query":     "أحدث نتائج اكتشافات الفضاء",
		"mode":      "fact_check",
		"max_steps": 2,
	})

	out, err := searchTool.Execute(ctx, args, ExecutionContext{})
	if err != nil {
		t.Fatalf("WebSearch.Execute failed: %v", err)
	}

	if !strings.Contains(out, "مؤشر الموثوقية") {
		t.Errorf("expected credibility indicator in output, got: %s", out)
	}
	if !strings.Contains(out, "wikipedia.org") || !strings.Contains(out, "reuters.com") {
		t.Errorf("expected authoritative source citations in output, got: %s", out)
	}
}

func TestWebSearch_QueryExpansion(t *testing.T) {
	queries := ExpandQuery("سعر الذهب اليوم", "news", 3)
	if len(queries) != 3 {
		t.Fatalf("expected 3 expanded queries, got %d", len(queries))
	}
	if queries[0] != "سعر الذهب اليوم" {
		t.Errorf("first query should be original")
	}
}

// -------------------------------------------------------------
// 6. Reminder Tool Unit Tests
// -------------------------------------------------------------

type mockScheduler struct {
	tasks []string
}

func (m *mockScheduler) ScheduleTask(chatID, chatType, targetUser, reason, durationStr string) string {
	taskID := fmt.Sprintf("TASK_%d", len(m.tasks)+1)
	m.tasks = append(m.tasks, taskID)
	return taskID
}

func (m *mockScheduler) GetScheduledTasksCount() int {
	return len(m.tasks)
}

func TestReminder_CreateAndList(t *testing.T) {
	mockSch := &mockScheduler{}
	tool := NewReminderTool(mockSch)

	ctx := context.Background()
	execCtx := ExecutionContext{
		ChatID:     "chat_123",
		ChatType:   "group",
		SenderName: "أحمد",
	}

	// 1. Create reminder
	args, _ := json.Marshal(map[string]interface{}{
		"action":   "create",
		"task":     "اسأل مكاري عن نتيجة الفحص",
		"duration": "ساعتين",
	})

	out, err := tool.Execute(ctx, args, execCtx)
	if err != nil {
		t.Fatalf("Reminder.Execute create error: %v", err)
	}

	if !strings.Contains(out, "تمت جدولة التذكير بنجاح") || !strings.Contains(out, "TASK_1") {
		t.Errorf("expected task confirmation in output, got: %s", out)
	}

	// 2. List reminders
	listArgs, _ := json.Marshal(map[string]interface{}{
		"action": "list",
	})
	listOut, err := tool.Execute(ctx, listArgs, execCtx)
	if err != nil {
		t.Fatalf("Reminder.Execute list error: %v", err)
	}

	if !strings.Contains(listOut, "1") {
		t.Errorf("expected 1 task in status list, got: %s", listOut)
	}
}
