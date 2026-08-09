// Пакет webhook — dispatch событий Ревизора внешним системам (REV-W-001).
package webhook

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/ais-platform/ais_products/revizor/internal/config"
	"github.com/ais-platform/ais_products/revizor/internal/store"
)

const (
	defaultTimeout   = 5 * time.Second
	maxRetries       = 3
	retryInterval    = 5 * time.Second
)

// WebhookStatus — статус webhook'а для trace_webhook_list.
type WebhookStatus struct {
	ID           string  `json:"id"`
	URL          string  `json:"url"`
	PathFilter   string  `json:"path_filter"`
	Enabled      bool    `json:"enabled"`
	LastDelivery *string `json:"last_delivery"`
	LastStatus   *int    `json:"last_status"`
}

// TestResult — результат trace_webhook_test.
type TestResult struct {
	WebhookID  string `json:"webhook_id"`
	HTTPStatus int    `json:"http_status"`
	Success    bool   `json:"success"`
	Error      string `json:"error,omitempty"`
}

// WebhookPayload — тело POST-запроса webhook'а (контракт ADD-001).
type WebhookPayload struct {
	WebhookID   string         `json:"webhook_id"`
	EventType   string         `json:"event_type"`
	Timestamp   string         `json:"timestamp"`
	TraceID     string         `json:"trace_id"`
	PayloadData map[string]any `json:"payload_data"`
}

// Dispatcher — диспетчер webhook-уведомлений.
type Dispatcher struct {
	webhooks []config.WebhookConfig
	client   *http.Client
	mu       sync.RWMutex
	logger   *slog.Logger
	statuses map[string]*WebhookStatus
}

// NewDispatcher создаёт новый Dispatcher из конфигурации.
func NewDispatcher(webhooks []config.WebhookConfig, logger *slog.Logger) *Dispatcher {
	if logger == nil {
		logger = slog.Default()
	}
	d := &Dispatcher{
		webhooks: webhooks,
		client: &http.Client{
			Timeout: defaultTimeout,
		},
		logger:   logger,
		statuses: make(map[string]*WebhookStatus, len(webhooks)),
	}
	// Инициализация статусов
	for _, wh := range webhooks {
		d.statuses[wh.ID] = &WebhookStatus{
			ID:         wh.ID,
			URL:        wh.URL,
			PathFilter: wh.PathFilter,
			Enabled:    wh.Enabled,
		}
	}
	return d
}

// SetWebhooks обновляет список webhook'ов (для перезагрузки конфига).
func (d *Dispatcher) SetWebhooks(webhooks []config.WebhookConfig) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.webhooks = webhooks
	d.statuses = make(map[string]*WebhookStatus, len(webhooks))
	for _, wh := range webhooks {
		d.statuses[wh.ID] = &WebhookStatus{
			ID:         wh.ID,
			URL:        wh.URL,
			PathFilter: wh.PathFilter,
			Enabled:    wh.Enabled,
		}
	}
}

// Dispatch проверяет все webhook'и и асинхронно отправляет событие если путь совпадает.
// Вызывается после успешной записи события в БД.
func (d *Dispatcher) Dispatch(entry store.TraceEntry) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	for _, wh := range d.webhooks {
		if !wh.Enabled {
			continue
		}
		if !matchPath(entry.TracePath, wh.PathFilter) {
			continue
		}
		// Асинхронный dispatch
		go d.sendWithRetry(wh, entry)
	}
}

// List возвращает статусы всех зарегистрированных webhook'ов.
func (d *Dispatcher) List() []WebhookStatus {
	d.mu.RLock()
	defer d.mu.RUnlock()

	result := make([]WebhookStatus, 0, len(d.statuses))
	for _, wh := range d.webhooks {
		if s, ok := d.statuses[wh.ID]; ok {
			result = append(result, *s)
		} else {
			result = append(result, WebhookStatus{
				ID:         wh.ID,
				URL:        wh.URL,
				PathFilter: wh.PathFilter,
				Enabled:    wh.Enabled,
			})
		}
	}
	return result
}

// Test отправляет тестовый ping на указанный webhook.
func (d *Dispatcher) Test(webhookID string) *TestResult {
	d.mu.RLock()
	var wh *config.WebhookConfig
	for i := range d.webhooks {
		if d.webhooks[i].ID == webhookID {
			wh = &d.webhooks[i]
			break
		}
	}
	d.mu.RUnlock()

	if wh == nil {
		return &TestResult{
			WebhookID: webhookID,
			Success:   false,
			Error:     fmt.Sprintf("webhook '%s' not found", webhookID),
		}
	}

	payload := WebhookPayload{
		WebhookID: webhookID,
		EventType: "revizor.webhook.test",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		TraceID:   "test-" + time.Now().UTC().Format("20060102-150405"),
		PayloadData: map[string]any{
			"test": true,
		},
	}

	status, success, errStr := d.sendOnce(*wh, payload)
	return &TestResult{
		WebhookID:  webhookID,
		HTTPStatus: status,
		Success:    success,
		Error:      errStr,
	}
}

// sendWithRetry отправляет payload с 3 попытками и интервалом 5 сек (ADD-001).
func (d *Dispatcher) sendWithRetry(wh config.WebhookConfig, entry store.TraceEntry) {
	payload := WebhookPayload{
		WebhookID: wh.ID,
		EventType: entry.TracePath,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		TraceID:   fmt.Sprintf("evt-%d", entry.ID),
		PayloadData: map[string]any{
			"project": "ais-platform",
			"env":     "development",
			"meta":    entry.Data,
		},
	}

	var lastStatus int
	var lastErr string

	for attempt := 0; attempt < maxRetries; attempt++ {
		status, success, errStr := d.sendOnce(wh, payload)

		d.mu.Lock()
		now := time.Now().UTC().Format(time.RFC3339)
		if s, ok := d.statuses[wh.ID]; ok {
			s.LastDelivery = &now
			s.LastStatus = &status
		}
		d.mu.Unlock()

		lastStatus = status
		if success {
			return
		}
		lastErr = errStr

		if attempt < maxRetries-1 {
			time.Sleep(retryInterval)
		}
	}

	d.logger.Error("CRITICAL_DELIVERY_FAILURE",
		"webhook_id", wh.ID,
		"url", wh.URL,
		"event_type", entry.TracePath,
		"last_status", lastStatus,
		"retries", maxRetries,
		"error", lastErr,
	)
}

// sendOnce выполняет один HTTP POST запрос.
func (d *Dispatcher) sendOnce(wh config.WebhookConfig, payload WebhookPayload) (status int, success bool, errStr string) {
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, false, fmt.Sprintf("marshal error: %v", err)
	}

	resp, err := d.client.Post(wh.URL, "application/json", bytes.NewReader(body))
	if err != nil {
		return 0, false, fmt.Sprintf("request error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return resp.StatusCode, true, ""
	}
	return resp.StatusCode, false, fmt.Sprintf("HTTP %d", resp.StatusCode)
}

// matchPath проверяет, соответствует ли trace_path фильтру path_filter.
// Синтаксис: glob с `**` (любая глубина) и `*` (один сегмент), через запятую.
func matchPath(tracePath, filter string) bool {
	for _, part := range splitFilters(filter) {
		if globMatch(tracePath, part) {
			return true
		}
	}
	return false
}

// splitFilters разбивает фильтр по запятым, обрезая пробелы.
func splitFilters(filter string) []string {
	var result []string
	for _, f := range splitRaw(filter, ',') {
		f = trimSpace(f)
		if f != "" {
			result = append(result, f)
		}
	}
	return result
}

// globMatch проверяет соответствие trace_path glob-шаблону.
// `**` — любое количество сегментов (включая ноль).
// `*` — ровно один сегмент (без точек).
func globMatch(path, pattern string) bool {
	pathParts := splitRaw(path, '.')
	patParts := splitRaw(pattern, '.')

	pi, pp := 0, 0
	for pi < len(pathParts) && pp < len(patParts) {
		if patParts[pp] == "**" {
			pp++
			if pp >= len(patParts) {
				return true // ** в конце = всё что угодно дальше
			}
			// Ищем следующий паттерн в оставшихся частях пути
			for pi < len(pathParts) {
				if globMatch(joinParts(pathParts[pi:], '.'), joinParts(patParts[pp:], '.')) {
					return true
				}
				pi++
			}
			return false
		}
		if patParts[pp] != "*" && patParts[pp] != pathParts[pi] {
			return false
		}
		pi++
		pp++
	}

	// Оба исчерпаны = точное совпадение
	if pi == len(pathParts) && pp == len(patParts) {
		return true
	}
	// Паттерн заканчивается на ** — всё что осталось в пути допустимо
	if pp == len(patParts)-1 && patParts[pp] == "**" {
		return true
	}

	return false
}

// Вспомогательные функции для избежания импорта strings (упрощённые).

func splitRaw(s string, sep byte) []string {
	if s == "" {
		return nil
	}
	var result []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			result = append(result, s[start:i])
			start = i + 1
		}
	}
	result = append(result, s[start:])
	return result
}

func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}

func joinParts(parts []string, sep byte) string {
	if len(parts) == 0 {
		return ""
	}
	result := parts[0]
	for i := 1; i < len(parts); i++ {
		result += string(sep) + parts[i]
	}
	return result
}
