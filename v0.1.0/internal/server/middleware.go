package server

import (
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// apiKeyMiddleware проверяет API-ключ. Если ключ не задан — пропускает всех.
func apiKeyMiddleware(apiKey string, next http.HandlerFunc) http.HandlerFunc {
	if apiKey == "" {
		return next
	}
	return func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") || strings.TrimPrefix(auth, "Bearer ") != apiKey {
			writeJSON(w, http.StatusUnauthorized, map[string]string{
				"status": "error",
				"detail": "Невалидный API-ключ",
			})
			return
		}
		next(w, r)
	}
}

// RateLimiter — скользящее окно для rate limiting.
type RateLimiter struct {
	mu      sync.Mutex
	buckets map[string][]time.Time // key -> timestamps
	limits  map[string]int         // routeKey -> max_per_sec
}

// NewRateLimiter создаёт новый RateLimiter.
func NewRateLimiter() *RateLimiter {
	return &RateLimiter{
		buckets: make(map[string][]time.Time),
		limits:  make(map[string]int),
	}
}

// SetLimit устанавливает лимит для ключа маршрута.
func (rl *RateLimiter) SetLimit(routeKey string, maxPerSec int) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.limits[routeKey] = maxPerSec
}

// Allow проверяет, разрешён ли запрос. key = routeKey + ":" + clientKey.
func (rl *RateLimiter) Allow(routeKey, clientKey string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	limit, ok := rl.limits[routeKey]
	if !ok {
		return true
	}

	fullKey := routeKey + ":" + clientKey
	now := time.Now()
	windowStart := now.Add(-1 * time.Second)

	timestamps := rl.buckets[fullKey]

	// Очистка старых записей
	valid := make([]time.Time, 0, len(timestamps))
	for _, ts := range timestamps {
		if ts.After(windowStart) {
			valid = append(valid, ts)
		}
	}

	if len(valid) >= limit {
		rl.buckets[fullKey] = valid
		return false
	}

	valid = append(valid, now)
	rl.buckets[fullKey] = valid
	return true
}

// rateLimitMiddleware применяет rate limiting к обработчику.
func rateLimitMiddleware(limiter *RateLimiter, routeKey string, logger *slog.Logger, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Ключ клиента: session_id из тела/query или IP
		clientKey := r.RemoteAddr
		if sid := r.URL.Query().Get("session_id"); sid != "" {
			clientKey = sid
		}

		if !limiter.Allow(routeKey, clientKey) {
			logger.Warn("rate limit exceeded", "route", routeKey, "client", clientKey)
			writeJSON(w, http.StatusTooManyRequests, map[string]any{
				"status":         "error",
				"detail":         "Rate limit exceeded",
				"retry_after_ms": 1000,
			})
			return
		}
		next(w, r)
	}
}

// requestLogger логирует каждый HTTP-запрос.
func requestLogger(logger *slog.Logger, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next(w, r)
		logger.Debug("request",
			"method", r.Method,
			"path", r.URL.Path,
			"remote", r.RemoteAddr,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	}
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	// Безопасно игнорируем ошибку записи — клиент мог отключиться
	_ = encodeJSON(w, data)
}
