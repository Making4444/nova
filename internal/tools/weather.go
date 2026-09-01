package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// WeatherTool fetches live weather and forecasts from wttr.in or Open-Meteo.
type WeatherTool struct {
	httpClient        *http.Client
	wttrBaseURL       string
	openMeteoGeoURL   string
	openMeteoForeURL  string
}

// NewWeatherTool creates a new weather tool instance.
func NewWeatherTool() *WeatherTool {
	return &WeatherTool{
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		wttrBaseURL:      "https://wttr.in",
		openMeteoGeoURL:  "https://geocoding-api.open-meteo.com/v1/search",
		openMeteoForeURL: "https://api.open-meteo.com/v1/forecast",
	}
}

// SetBaseURLs allows mocking API endpoints in tests.
func (w *WeatherTool) SetBaseURLs(wttrURL, geoURL, forecastURL string) {
	if wttrURL != "" {
		w.wttrBaseURL = wttrURL
	}
	if geoURL != "" {
		w.openMeteoGeoURL = geoURL
	}
	if forecastURL != "" {
		w.openMeteoForeURL = forecastURL
	}
}

func (w *WeatherTool) Name() string {
	return "weather"
}

func (w *WeatherTool) Description() string {
	return "معرفة حالة الطقس المباشرة، درجات الحرارة الحالية والمحسوسة، الرطوبة، سرعة الرياح، وتوقعات الأيام القادمة لأي مدينة في العالم"
}

func (w *WeatherTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"location": map[string]interface{}{
				"type":        "string",
				"description": "اسم المدينة أو البلد المطلوب معرفة طقسها (مثال: 'القاهرة', 'Alexandria', 'دبي', 'الرياض', 'London')",
			},
		},
		"required": []string{"location"},
	}
}

func (w *WeatherTool) Permission() PermissionLevel {
	return PermissionEveryone
}

type weatherArgs struct {
	Location string `json:"location"`
}

func (w *WeatherTool) Execute(ctx context.Context, args json.RawMessage, execCtx ExecutionContext) (string, error) {
	var input weatherArgs
	if err := json.Unmarshal(args, &input); err != nil {
		input.Location = strings.Trim(string(args), `"{}`)
	}

	loc := strings.TrimSpace(input.Location)
	if loc == "" {
		loc = "Cairo"
	}

	// 1. Try wttr.in
	res, err := w.fetchFromWttr(ctx, loc)
	if err == nil && res != "" {
		return res, nil
	}

	// 2. Fallback to Open-Meteo
	res, err = w.fetchFromOpenMeteo(ctx, loc)
	if err == nil && res != "" {
		return res, nil
	}

	return fmt.Sprintf("❌ تعذر جلب بيانات الطقس للمدينة %q حالياً. يرجى التأكد من اسم المدينة والمحاولة لاحقاً.", loc), nil
}

// -------------------------------------------------------------
// wttr.in Parser
// -------------------------------------------------------------

type wttrCondition struct {
	TempC        string `json:"temp_C"`
	TempF        string `json:"temp_F"`
	FeelsLikeC   string `json:"FeelsLikeC"`
	FeelsLikeF   string `json:"FeelsLikeF"`
	Humidity     string `json:"humidity"`
	Windspeed    string `json:"windspeedKmph"`
	WindDir      string `json:"winddir16Point"`
	UVIndex      string `json:"uvIndex"`
	Visibility   string `json:"visibility"`
	Pressure     string `json:"pressure"`
	WeatherDesc  []struct {
		Value string `json:"value"`
	} `json:"weatherDesc"`
	LangAr []struct {
		Value string `json:"value"`
	} `json:"lang_ar"`
}

type wttrDayForecast struct {
	Date     string `json:"date"`
	MaxTempC string `json:"maxtempC"`
	MinTempC string `json:"mintempC"`
	Hourly   []struct {
		WeatherDesc []struct {
			Value string `json:"value"`
		} `json:"weatherDesc"`
		LangAr []struct {
			Value string `json:"value"`
		} `json:"lang_ar"`
	} `json:"hourly"`
}

type wttrResponse struct {
	CurrentCondition []wttrCondition `json:"current_condition"`
	NearestArea      []struct {
		AreaName []struct {
			Value string `json:"value"`
		} `json:"areaName"`
		Country []struct {
			Value string `json:"value"`
		} `json:"country"`
	} `json:"nearest_area"`
	Weather []wttrDayForecast `json:"weather"`
}

func (w *WeatherTool) fetchFromWttr(ctx context.Context, loc string) (string, error) {
	reqURL := fmt.Sprintf("%s/%s?format=j1&lang=ar", w.wttrBaseURL, url.PathEscape(loc))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "curl/8.0.0")

	resp, err := w.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("wttr.in status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var data wttrResponse
	if err := json.Unmarshal(body, &data); err != nil {
		return "", err
	}

	if len(data.CurrentCondition) == 0 {
		return "", errors.New("empty wttr.in condition")
	}

	curr := data.CurrentCondition[0]
	desc := "معتدل"
	if len(curr.LangAr) > 0 && curr.LangAr[0].Value != "" {
		desc = curr.LangAr[0].Value
	} else if len(curr.WeatherDesc) > 0 && curr.WeatherDesc[0].Value != "" {
		desc = translateWeatherCondition(curr.WeatherDesc[0].Value)
	}

	areaName := loc
	countryName := ""
	if len(data.NearestArea) > 0 {
		if len(data.NearestArea[0].AreaName) > 0 {
			areaName = data.NearestArea[0].AreaName[0].Value
		}
		if len(data.NearestArea[0].Country) > 0 {
			countryName = data.NearestArea[0].Country[0].Value
		}
	}

	emoji := getWeatherEmoji(desc)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("🌤️ **حالة الطقس في %s**", areaName))
	if countryName != "" {
		sb.WriteString(fmt.Sprintf(" (%s)", countryName))
	}
	sb.WriteString("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	sb.WriteString(fmt.Sprintf("🌡️ **درجة الحرارة:** `%s°C` (المحسوسة: `%s°C`)\n", curr.TempC, curr.FeelsLikeC))
	sb.WriteString(fmt.Sprintf("⛅ **الحالة الجوية:** %s %s\n", desc, emoji))
	sb.WriteString(fmt.Sprintf("💧 **نسبة الرطوبة:** `%s%%`\n", curr.Humidity))
	sb.WriteString(fmt.Sprintf("💨 **سرعة الرياح:** `%s كم/س` (الاتجاه: %s)\n", curr.Windspeed, translateWindDir(curr.WindDir)))

	if curr.UVIndex != "" && curr.UVIndex != "0" {
		sb.WriteString(fmt.Sprintf("☀️ **مؤشر الأشعة (UV):** `%s`\n", curr.UVIndex))
	}
	if curr.Visibility != "" {
		sb.WriteString(fmt.Sprintf("👁️ **مدى الرؤية:** `%s كم`\n", curr.Visibility))
	}

	if len(data.Weather) > 1 {
		sb.WriteString("\n🔮 **توقعات الأيام القادمة:**\n")
		days := []string{"غداً", "بعد غد", "اليوم الثالث"}
		for i := 1; i < len(data.Weather) && i <= 3; i++ {
			day := data.Weather[i]
			dayDesc := "معتدل"
			if len(day.Hourly) > 4 {
				if len(day.Hourly[4].LangAr) > 0 && day.Hourly[4].LangAr[0].Value != "" {
					dayDesc = day.Hourly[4].LangAr[0].Value
				} else if len(day.Hourly[4].WeatherDesc) > 0 {
					dayDesc = translateWeatherCondition(day.Hourly[4].WeatherDesc[0].Value)
				}
			}
			dayEmoji := getWeatherEmoji(dayDesc)
			label := days[i-1]
			sb.WriteString(fmt.Sprintf("• **%s (%s):** %s `%s°C / %s°C` (%s)\n", label, day.Date, dayEmoji, day.MinTempC, day.MaxTempC, dayDesc))
		}
	}

	return sb.String(), nil
}

// -------------------------------------------------------------
// Open-Meteo Fallback Parser
// -------------------------------------------------------------

type openMeteoGeoResult struct {
	Results []struct {
		Name      string  `json:"name"`
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
		Country   string  `json:"country"`
	} `json:"results"`
}

type openMeteoForecastResponse struct {
	Current struct {
		Temperature2M       float64 `json:"temperature_2m"`
		RelativeHumidity2M  float64 `json:"relative_humidity_2m"`
		ApparentTemperature float64 `json:"apparent_temperature"`
		WeatherCode         int     `json:"weather_code"`
		WindSpeed10M        float64 `json:"wind_speed_10m"`
	} `json:"current"`
	Daily struct {
		Time             []string  `json:"time"`
		WeatherCode      []int     `json:"weather_code"`
		Temperature2MMax []float64 `json:"temperature_2m_max"`
		Temperature2MMin []float64 `json:"temperature_2m_min"`
	} `json:"daily"`
}

func (w *WeatherTool) fetchFromOpenMeteo(ctx context.Context, loc string) (string, error) {
	// 1. Geocode location name
	geoURL := fmt.Sprintf("%s?name=%s&count=1&language=ar", w.openMeteoGeoURL, url.QueryEscape(loc))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, geoURL, nil)
	if err != nil {
		return "", err
	}

	resp, err := w.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var geoData openMeteoGeoResult
	if err := json.NewDecoder(resp.Body).Decode(&geoData); err != nil {
		return "", err
	}

	if len(geoData.Results) == 0 {
		return "", fmt.Errorf("location %q not found in geocoding", loc)
	}

	target := geoData.Results[0]

	// 2. Fetch forecast
	foreURL := fmt.Sprintf("%s?latitude=%.4f&longitude=%.4f&current=temperature_2m,relative_humidity_2m,apparent_temperature,weather_code,wind_speed_10m&daily=weather_code,temperature_2m_max,temperature_2m_min&timezone=auto",
		w.openMeteoForeURL, target.Latitude, target.Longitude)

	reqFore, err := http.NewRequestWithContext(ctx, http.MethodGet, foreURL, nil)
	if err != nil {
		return "", err
	}

	respFore, err := w.httpClient.Do(reqFore)
	if err != nil {
		return "", err
	}
	defer respFore.Body.Close()

	var forecast openMeteoForecastResponse
	if err := json.NewDecoder(respFore.Body).Decode(&forecast); err != nil {
		return "", err
	}

	curr := forecast.Current
	desc, emoji := wmoCodeToDesc(curr.WeatherCode)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("🌤️ **حالة الطقس في %s**", target.Name))
	if target.Country != "" {
		sb.WriteString(fmt.Sprintf(" (%s)", target.Country))
	}
	sb.WriteString("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	sb.WriteString(fmt.Sprintf("🌡️ **درجة الحرارة:** `%.1f°C` (المحسوسة: `%.1f°C`)\n", curr.Temperature2M, curr.ApparentTemperature))
	sb.WriteString(fmt.Sprintf("⛅ **الحالة الجوية:** %s %s\n", desc, emoji))
	sb.WriteString(fmt.Sprintf("💧 **نسبة الرطوبة:** `%.0f%%`\n", curr.RelativeHumidity2M))
	sb.WriteString(fmt.Sprintf("💨 **سرعة الرياح:** `%.1f كم/س`\n", curr.WindSpeed10M))

	if len(forecast.Daily.Time) > 1 {
		sb.WriteString("\n🔮 **توقعات الأيام القادمة:**\n")
		days := []string{"غداً", "بعد غد", "اليوم الثالث"}
		for i := 1; i < len(forecast.Daily.Time) && i <= 3; i++ {
			dCode := forecast.Daily.WeatherCode[i]
			dDesc, dEmoji := wmoCodeToDesc(dCode)
			label := days[i-1]
			sb.WriteString(fmt.Sprintf("• **%s (%s):** %s `%.1f°C / %.1f°C` (%s)\n",
				label, forecast.Daily.Time[i], dEmoji, forecast.Daily.Temperature2MMin[i], forecast.Daily.Temperature2MMax[i], dDesc))
		}
	}

	return sb.String(), nil
}

func wmoCodeToDesc(code int) (string, string) {
	switch code {
	case 0:
		return "صافٍ تماماً", "☀️"
	case 1:
		return "مشمس في الغالب", "🌤️"
	case 2:
		return "غائم جزئياً", "⛅"
	case 3:
		return "غائم كلياً", "☁️"
	case 45, 48:
		return "ضبابي", "🌫️"
	case 51, 53, 55:
		return "رذاذ مطر خفيف", "🌦️"
	case 61, 63, 65:
		return "أمطار", "🌧️"
	case 71, 73, 75:
		return "تساقط ثلوج", "❄️"
	case 80, 81, 82:
		return "زخات مطرية غزيرة", "🌧️"
	case 95, 96, 99:
		return "عواصف رعدية", "⛈️"
	default:
		return "معتدل", "🌤️"
	}
}

func translateWeatherCondition(desc string) string {
	d := strings.ToLower(desc)
	switch {
	case strings.Contains(d, "sunny"), strings.Contains(d, "clear"):
		return "مشمس وصافٍ"
	case strings.Contains(d, "partly cloudy"):
		return "غائم جزئياً"
	case strings.Contains(d, "cloudy"), strings.Contains(d, "overcast"):
		return "غائم"
	case strings.Contains(d, "thunder"), strings.Contains(d, "storm"):
		return "عاصفة رعدية"
	case strings.Contains(d, "rain"), strings.Contains(d, "shower"), strings.Contains(d, "drizzle"):
		return "ممطر"
	case strings.Contains(d, "snow"), strings.Contains(d, "blizzard"), strings.Contains(d, "ice"):
		return "تساقط ثلوج"
	case strings.Contains(d, "fog"), strings.Contains(d, "mist"), strings.Contains(d, "haze"):
		return "ضبابي"
	default:
		return desc
	}
}

func getWeatherEmoji(desc string) string {
	d := strings.ToLower(desc)
	switch {
	case strings.Contains(d, "مشمس"), strings.Contains(d, "sunny"), strings.Contains(d, "صاف"):
		return "☀️"
	case strings.Contains(d, "جزئي"), strings.Contains(d, "partly"):
		return "⛅"
	case strings.Contains(d, "غائم"), strings.Contains(d, "cloudy"), strings.Contains(d, "overcast"):
		return "☁️"
	case strings.Contains(d, "رعد"), strings.Contains(d, "thunder"), strings.Contains(d, "storm"):
		return "⛈️"
	case strings.Contains(d, "مطر"), strings.Contains(d, "ممطر"), strings.Contains(d, "rain"), strings.Contains(d, "drizzle"):
		return "🌧️"
	case strings.Contains(d, "ثلج"), strings.Contains(d, "snow"), strings.Contains(d, "ice"):
		return "❄️"
	case strings.Contains(d, "ضباب"), strings.Contains(d, "fog"), strings.Contains(d, "mist"):
		return "🌫️"
	default:
		return "🌤️"
	}
}

func translateWindDir(dir string) string {
	d := strings.ToUpper(strings.TrimSpace(dir))
	switch d {
	case "N":
		return "شمالية"
	case "NNE", "NE":
		return "شمالية شرقية"
	case "ENE", "E":
		return "شرقية"
	case "ESE", "SE":
		return "جنوبية شرقية"
	case "SSE", "S":
		return "جنوبية"
	case "SSW", "SW":
		return "جنوبية غربية"
	case "WSW", "W":
		return "غربية"
	case "WNW", "NW":
		return "شمالية غربية"
	case "NNW":
		return "شمالية غربية"
	default:
		return dir
	}
}
