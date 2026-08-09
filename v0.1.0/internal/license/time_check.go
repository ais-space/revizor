package license

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// TimeSource — результат проверки времени из одного источника.
type TimeSource struct {
	Source   string    `json:"source"`   // "local", "worldtimeapi", "timeapi", "server"
	Time     time.Time `json:"time"`
	Weekday  int       `json:"weekday"`  // 0=Sunday
	IsSunday bool      `json:"is_sunday"`
	ErrorMsg string    `json:"error_msg,omitempty"`
}

// TimeCheckResult — результат многоуровневой проверки времени.
type TimeCheckResult struct {
	Sources     []TimeSource `json:"sources"`
	IsSunday    bool         `json:"is_sunday"`    // консенсус: ≥2 источников говорят воскресенье
	Discrepancy bool         `json:"discrepancy"`  // расхождение >24ч между любыми источниками
}

// worldTimeAPIResponse — ответ от worldtimeapi.org.
type worldTimeAPIResponse struct {
	DayOfWeek int    `json:"day_of_week"`
	DateTime  string `json:"datetime"`
}

// timeapiResponse — ответ от timeapi.io.
type timeapiResponse struct {
	Year   int    `json:"year"`
	Month  int    `json:"month"`
	Day    int    `json:"day"`
	Hour   int    `json:"hour"`
	Minute int    `json:"minute"`
	Seconds int  `json:"seconds"`
	DayOfWeek int `json:"dayOfWeek"`
}

// CheckLocalTime возвращает локальное время.
func CheckLocalTime() TimeSource {
	now := time.Now()
	return TimeSource{
		Source:   "local",
		Time:     now,
		Weekday:  int(now.Weekday()),
		IsSunday: now.Weekday() == time.Sunday,
	}
}

// CheckWorldTime запрашивает время из двух независимых HTTP-источников.
func CheckWorldTime(ctx context.Context) []TimeSource {
	var sources []TimeSource
	var mu sync.Mutex
	var wg sync.WaitGroup

	// Источник 1: worldtimeapi.org
	wg.Add(1)
	go func() {
		defer wg.Done()
		src := fetchWorldTimeAPI(ctx)
		mu.Lock()
		sources = append(sources, src)
		mu.Unlock()
	}()

	// Источник 2: timeapi.io
	wg.Add(1)
	go func() {
		defer wg.Done()
		src := fetchTimeAPI(ctx)
		mu.Lock()
		sources = append(sources, src)
		mu.Unlock()
	}()

	wg.Wait()
	return sources
}

func fetchWorldTimeAPI(ctx context.Context) TimeSource {
	req, err := http.NewRequestWithContext(ctx, "GET", "http://worldtimeapi.org/api/timezone/Etc/UTC", nil)
	if err != nil {
		return TimeSource{Source: "worldtimeapi", ErrorMsg: err.Error()}
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return TimeSource{Source: "worldtimeapi", ErrorMsg: err.Error()}
	}
	defer resp.Body.Close()

	var apiResp worldTimeAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return TimeSource{Source: "worldtimeapi", ErrorMsg: err.Error()}
	}

	parsed, err := time.Parse("2006-01-02T15:04:05.999999-07:00", apiResp.DateTime)
	if err != nil {
		return TimeSource{Source: "worldtimeapi", ErrorMsg: err.Error()}
	}

	return TimeSource{
		Source:   "worldtimeapi",
		Time:     parsed,
		Weekday:  apiResp.DayOfWeek,
		IsSunday: apiResp.DayOfWeek == 0,
	}
}

func fetchTimeAPI(ctx context.Context) TimeSource {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://timeapi.io/api/time/current/zone?timeZone=UTC", nil)
	if err != nil {
		return TimeSource{Source: "timeapi", ErrorMsg: err.Error()}
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return TimeSource{Source: "timeapi", ErrorMsg: err.Error()}
	}
	defer resp.Body.Close()

	var apiResp timeapiResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return TimeSource{Source: "timeapi", ErrorMsg: err.Error()}
	}

	t := time.Date(apiResp.Year, time.Month(apiResp.Month), apiResp.Day,
		apiResp.Hour, apiResp.Minute, apiResp.Seconds, 0, time.UTC)

	return TimeSource{
		Source:   "timeapi",
		Time:     t,
		Weekday:  apiResp.DayOfWeek,
		IsSunday: apiResp.DayOfWeek == 0,
	}
}

// EvaluateTimeConsensus анализирует все источники и возвращает консенсус.
// Воскресенье = ≥2 источников из всех (включая локальный и серверный) говорят "воскресенье".
// Discrepancy = расхождение >24 часов между любыми двумя успешными источниками.
func EvaluateTimeConsensus(allSources []TimeSource) TimeCheckResult {
	var result TimeCheckResult
	result.Sources = allSources

	sundayCount := 0
	var successTimes []time.Time

	for _, src := range allSources {
		if src.ErrorMsg == "" {
			successTimes = append(successTimes, src.Time)
			if src.IsSunday {
				sundayCount++
			}
		}
	}

	// Консенсус: ≥2 источников говорят "воскресенье"
	result.IsSunday = sundayCount >= 2

	// Проверка расхождений >24 часов между успешными источниками
	for i := 0; i < len(successTimes); i++ {
		for j := i + 1; j < len(successTimes); j++ {
			diff := successTimes[i].Sub(successTimes[j])
			if diff < 0 {
				diff = -diff
			}
			if diff > 24*time.Hour {
				result.Discrepancy = true
				return result
			}
		}
	}

	return result
}

// Воскресный безлимит: CheckSundayUnlimited и fetchServerTime вынесены в sunday.go
