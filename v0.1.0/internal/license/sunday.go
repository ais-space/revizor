package license

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// ── Воскресный безлимит (v55, Фаза 3) ───────────────────────────────────

// CheckSundayUnlimited выполняет полную проверку воскресного безлимита.
// Возвращает результат консенсуса по всем источникам времени.
// Применяется ко ВСЕМ tier без исключений (для enterprise/privileged
// это ничего не меняет — у них и так безлимит).
func CheckSundayUnlimited(ctx context.Context, serverURL string) TimeCheckResult {
	// Уровень 1: локальное время
	localSrc := CheckLocalTime()

	// Уровень 2: мировые источники
	worldSources := CheckWorldTime(ctx)

	// Уровень 3: серверное время (если доступен URL)
	var serverSrc TimeSource
	if serverURL != "" {
		serverSrc = fetchServerTime(ctx, serverURL)
	}

	// Собираем все источники
	allSources := []TimeSource{localSrc}
	allSources = append(allSources, worldSources...)
	if serverSrc.Source != "" {
		allSources = append(allSources, serverSrc)
	}

	return EvaluateTimeConsensus(allSources)
}

// ── Серверное время ─────────────────────────────────────────────────────

// fetchServerTime запрашивает время с сервера AIS Platform.
func fetchServerTime(ctx context.Context, serverURL string) TimeSource {
	req, err := http.NewRequestWithContext(ctx, "GET", serverURL+"/api/v1/license/server-time", nil)
	if err != nil {
		return TimeSource{Source: "server", ErrorMsg: err.Error()}
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return TimeSource{Source: "server", ErrorMsg: err.Error()}
	}
	defer resp.Body.Close()

	var srvResp struct {
		Unix     int64 `json:"unix"`
		IsSunday bool  `json:"is_sunday"`
		Weekday  int   `json:"weekday"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&srvResp); err != nil {
		return TimeSource{Source: "server", ErrorMsg: err.Error()}
	}

	return TimeSource{
		Source:   "server",
		Time:     time.Unix(srvResp.Unix, 0),
		Weekday:  srvResp.Weekday,
		IsSunday: srvResp.IsSunday,
	}
}
