package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/ais-platform/ais_products/revizor/internal/license"
)

// SundayChecker — кэшируемая проверка воскресного безлимита (v56, ALERT-002).
// Вызывается перед выполнением каждого MCP-инструмента.
// Алгоритм:
//  1. Если кэш свежий (nextCheckAt ещё не наступил) — возвращаем кэшированный результат.
//  2. Проверяем локальное время. Если не воскресенье — кэшируем до следующего воскресенья.
//  3. Если локально воскресенье — собираем мировые источники и отправляем на сервер.
//  4. Сервер сравнивает, принимает решение (is_sunday, discrepancy).
//  5. Кэшируем до next_check_at из ответа сервера.
type SundayChecker struct {
	mu           sync.Mutex
	serverURL    string
	license      *license.License
	isSunday     bool
	nextCheckAt  int64
}

// NewSundayChecker создаёт новый SundayChecker.
func NewSundayChecker(serverURL string, lic *license.License) *SundayChecker {
	return &SundayChecker{
		serverURL: serverURL,
		license:   lic,
	}
}

// IsSunday возвращает true если воскресный безлимит активен.
// Выполняет проверку только если кэш устарел.
func (sc *SundayChecker) IsSunday(ctx context.Context) bool {
	sc.mu.Lock()
	// Кэш свежий?
	if time.Now().Unix() < sc.nextCheckAt {
		result := sc.isSunday
		sc.mu.Unlock()
		return result
	}
	sc.mu.Unlock()

	// Нужна проверка
	result := sc.check(ctx)

	sc.mu.Lock()
	sc.isSunday = result.isSunday
	sc.nextCheckAt = result.nextCheckAt
	sc.mu.Unlock()

	return result.isSunday
}

type sundayResult struct {
	isSunday    bool
	nextCheckAt int64
}

// check выполняет полную проверку воскресного безлимита.
func (sc *SundayChecker) check(ctx context.Context) sundayResult {
	logger := slog.Default()

	// 1. Локальное время
	localSrc := license.CheckLocalTime()
	if !localSrc.IsSunday {
		// Не воскресенье — следующая проверка в ближайшее воскресенье или +24ч
		now := time.Now().Unix()
		daysUntilSunday := (6 - int64(time.Now().Weekday())) % 7
		if daysUntilSunday == 0 {
			daysUntilSunday = 7
		}
		nextCheck := now + daysUntilSunday*86400
		if nextCheck > now+86400 {
			nextCheck = now + 86400
		}
		return sundayResult{isSunday: false, nextCheckAt: nextCheck}
	}

	// 2. Собираем мировые источники
	worldSources := license.CheckWorldTime(ctx)

	// Формируем запрос к серверу
	worldData := make([]map[string]interface{}, 0, len(worldSources))
	for _, src := range worldSources {
		if src.ErrorMsg != "" {
			continue // пропускаем недоступные источники
		}
		worldData = append(worldData, map[string]interface{}{
			"unix":      src.Time.Unix(),
			"is_sunday": src.IsSunday,
			"source":    src.Source,
		})
	}

	reqBody := map[string]interface{}{
		"local_unix":    localSrc.Time.Unix(),
		"local_weekday": localSrc.Weekday,
		"world_sources": worldData,
	}
	reqJSON, _ := json.Marshal(reqBody)

	// 3. Отправляем на сервер
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(sc.serverURL+"/api/v1/license/check-sunday", "application/json", bytes.NewReader(reqJSON))
	if err != nil {
		logger.Warn("check-sunday request failed", "error", err)
		// Если сервер недоступен — полагаемся на локальное время
		now := time.Now().Unix()
		return sundayResult{isSunday: true, nextCheckAt: now + 3600} // перепроверим через час
	}
	defer resp.Body.Close()

	var result struct {
		IsSunday    bool `json:"is_sunday"`
		Discrepancy bool `json:"discrepancy"`
		NextCheckAt int  `json:"next_check_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		logger.Warn("check-sunday decode failed", "error", err)
		now := time.Now().Unix()
		return sundayResult{isSunday: true, nextCheckAt: now + 3600}
	}

	// 4. Если расхождение — отправляем алерт
	if result.Discrepancy && sc.license != nil {
		alertClient := license.NewHeartbeatClient(sc.serverURL)
		details := map[string]interface{}{
			"local_time": localSrc.Time.Format(time.RFC3339),
			"sources":    worldData,
		}
		alertReq := license.AlertRequest{
			KID:         sc.license.KID,
			IID:         sc.license.IID,
			AlertType:   "time_discrepancy",
			Severity:    "warning",
			DetailsJSON: details,
		}
		if alertErr := alertClient.SendAlert(ctx, alertReq); alertErr != nil {
			logger.Warn("failed to send time_discrepancy alert", "error", alertErr)
		}
	}

	return sundayResult{isSunday: result.IsSunday, nextCheckAt: int64(result.NextCheckAt)}
}

// SetLicense обновляет лицензию (например после активации).
func (sc *SundayChecker) SetLicense(lic *license.License) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.license = lic
}

// SetServerURL обновляет URL сервера.
func (sc *SundayChecker) SetServerURL(serverURL string) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.serverURL = serverURL
}

// ForceRecheck сбрасывает кэш для немедленной перепроверки.
func (sc *SundayChecker) ForceRecheck() {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.nextCheckAt = 0
}

// Ensure import used
var _ = fmt.Sprintf
