package license

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
)

// ErrLicenseRevoked возвращается когда сервер отзывает лицензию.
// Бинарник немедленно переходит в Community без права восстановления.
var ErrLicenseRevoked = errors.New("license has been revoked")

// DefaultPublicKey — дефолтный публичный Ed25519-ключ для проверки подписи лицензии.
// Используется когда ключ не указан в конфиге/окружении.
// Генерируется утилитой scripts/gen_test_license_key.go и обновляется при продакшен-сборке.
const DefaultPublicKey = "R1J5yyhA/owR++3Q15FTQ6VONKe1auNOgVHVf82Il0U="

// License — структура лицензионного ключа v3.
type License struct {
	Ver              int         `json:"ver"`
	KID              string      `json:"kid"`
	Sub              string      `json:"sub"`
	Tier             string      `json:"tier"`
	Product          string      `json:"product"`           // v3: идентификатор продукта ("revizor", "surveyor", ...)
	Iat              int64       `json:"iat"`
	Exp              int64       `json:"exp"`                // -1 = вечная лицензия
	Lim              Limitations `json:"lim"`
	Mid              *string     `json:"mid"`
	IID              string      `json:"iid"`
	Sig              string      `json:"sig"`
	OfflineDays      int         `json:"offline_days"`       // v3: макс дней без heartbeat (0=always online, -1=unlimited)
	Privileged       bool        `json:"privileged"`         // v3: привилегированная лицензия (индивидуальные параметры)
	MaxBinaryVersion string      `json:"max_ver,omitempty"`  // v3: макс. версия бинарника для perpetual-лицензий
}

// Limitations — лимиты лицензии.
type Limitations struct {
	MaxSessions     int      `json:"max_sessions"`
	MaxEventsPerDay int      `json:"max_events_per_day"`
	MaxMachines     int      `json:"max_machines"`
	Features        []string `json:"features"`
}

// CommunityLimits — лимиты для Community-режима (без лицензии).
var CommunityLimits = Limitations{
	MaxSessions:     1,
	MaxEventsPerDay: 100,
	MaxMachines:     1,
	Features:        []string{"mcp_basic"},
}

// TrialLimits — лимиты для триала (краткосрочный безлимит функций, ограниченные машины).
var TrialLimits = Limitations{
	MaxSessions:     0,  // unlimited
	MaxEventsPerDay: 0,  // unlimited
	MaxMachines:     3,
	Features:        []string{"mcp_basic", "mcp_full"},
}

// IndieLimits — лимиты для Indie-тарифа.
// mcp_full (без postgres), меньше машин/сессий/событий чем Pro.
var IndieLimits = Limitations{
	MaxSessions:     5,
	MaxEventsPerDay: 10_000,
	MaxMachines:     1,
	Features:        []string{"mcp_basic", "mcp_full"},
}

// ProLimits — лимиты для Pro-тарифа.
// mcp_full + postgres.
var ProLimits = Limitations{
	MaxSessions:     10,
	MaxEventsPerDay: 100_000,
	MaxMachines:     3,
	Features:        []string{"mcp_basic", "mcp_full", "postgres"},
}

// EnterpriseLimits — лимиты для Enterprise-тарифа.
// Безлимитные сессии, события, машины. mcp_full + postgres.
var EnterpriseLimits = Limitations{
	MaxSessions:     0,
	MaxEventsPerDay: 0,
	MaxMachines:     0,
	Features:        []string{"mcp_basic", "mcp_full", "postgres"},
}

// PrivilegedLimits — лимиты для привилегированных лицензий.
// Безлимитные сессии и события, машины ограничены (задаются админом).
// mcp_full + postgres.
var PrivilegedLimits = Limitations{
	MaxSessions:     0,
	MaxEventsPerDay: 0,
	MaxMachines:     3,
	Features:        []string{"mcp_basic", "mcp_full", "postgres"},
}

// ParseKey декодирует лицензионный ключ:
// base64 → (compressed + signature) → zstd-decompress → JSON-unmarshal → verify Ed25519.
//
// publicKey — base64-encoded Ed25519 публичный ключ. Если пустой — используется DefaultPublicKey.
func ParseKey(encoded string, publicKey string) (*License, error) {
	// Убираем дефисы (группы по 4 символа) и восстанавливаем padding
	encoded = strings.ReplaceAll(encoded, "-", "")
	encoded = strings.ReplaceAll(encoded, " ", "")
	encoded = strings.ReplaceAll(encoded, "\n", "")
	encoded = strings.ReplaceAll(encoded, "\r", "")

	// Восстанавливаем base64 padding
	switch len(encoded) % 4 {
	case 2:
		encoded += "=="
	case 3:
		encoded += "="
	}

	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("base64 decode: %w", err)
	}

	// Сигнатура — последние 64 байта (Ed25519)
	if len(raw) < ed25519.SignatureSize+1 {
		return nil, fmt.Errorf("key too short: %d bytes", len(raw))
	}

	signature := raw[len(raw)-ed25519.SignatureSize:]
	compressed := raw[:len(raw)-ed25519.SignatureSize]

	// Выбор публичного ключа
	pk := publicKey
	if pk == "" {
		pk = DefaultPublicKey
	}

	// Проверка подписи
	pubKeyBytes, err := base64.StdEncoding.DecodeString(pk)
	if err != nil {
		return nil, fmt.Errorf("public key decode: %w", err)
	}

	if !ed25519.Verify(pubKeyBytes, compressed, signature) {
		return nil, fmt.Errorf("invalid license signature")
	}

	// zstd декомпрессия
	decoder, err := zstd.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, fmt.Errorf("zstd decoder: %w", err)
	}
	defer decoder.Close()

	var licJSON bytes.Buffer
	if _, err := decoder.WriteTo(&licJSON); err != nil {
		return nil, fmt.Errorf("zstd decompress: %w", err)
	}

	var lic License
	if err := json.Unmarshal(licJSON.Bytes(), &lic); err != nil {
		return nil, fmt.Errorf("json unmarshal: %w", err)
	}

	// Backward-compatible миграция v2 → v3
	if lic.Ver < 3 {
		lic.Product = "revizor" // все существующие ключи — для Ревизора
		if lic.OfflineDays == 0 {
			if lic.Tier == "enterprise" {
				lic.OfflineDays = -1 // безлимитный offline
			}
			// pro/community: остаётся 0 (grace 72h задано в heartbeat-логике)
		}
		// Privileged остаётся false
	}

	return &lic, nil
}

// Validate проверяет валидность лицензии: срок, tier, machine_id, версию бинарника.
// iat = 0 означает что лицензия ещё не активирована — это допустимо
// (срок начнётся при первой активации).
func Validate(lic *License, machineID string, binaryVersion string) error {
	if lic == nil {
		return fmt.Errorf("license is nil")
	}

	// Exp = -1: вечная лицензия (не истекает никогда)
	if lic.Exp > 0 {
		expTime := time.Unix(lic.Exp, 0)
		if time.Now().After(expTime) {
			return fmt.Errorf("license expired at %s", expTime.Format(time.RFC3339))
		}
	}

	if lic.Tier == "" {
		return fmt.Errorf("empty license tier")
	}

	// Проверка machine_id если задан (опционально — enterprise-лицензии могут быть без mid)
	if lic.Mid != nil && *lic.Mid != "" && machineID != *lic.Mid {
		return fmt.Errorf("machine_id mismatch: license bound to different machine")
	}

	// Проверка версии бинарника для perpetual-лицензий
	if err := ValidateMaxBinaryVersion(lic, binaryVersion); err != nil {
		return err
	}

	return nil
}

// compareVersions сравнивает две semver-строки (MAJOR.MINOR.PATCH).
// Возвращает: -1 если a < b, 0 если a == b, 1 если a > b, -2 если ошибка парсинга.
func compareVersions(a, b string) int {
	segA, err := parseSemver(a)
	if err != nil {
		return -2
	}
	segB, err := parseSemver(b)
	if err != nil {
		return -2
	}

	for i := 0; i < 3; i++ {
		if segA[i] < segB[i] {
			return -1
		}
		if segA[i] > segB[i] {
			return 1
		}
	}
	return 0
}

// parseSemver разбирает semver-строку "MAJOR.MINOR.PATCH" на [3]int.
func parseSemver(v string) ([3]int, error) {
	var seg [3]int
	_, err := fmt.Sscanf(strings.TrimSpace(v), "%d.%d.%d", &seg[0], &seg[1], &seg[2])
	return seg, err
}

// ValidateMaxBinaryVersion проверяет что версия бинарника не превышает
// максимальную разрешённую для perpetual-лицензии.
// Возвращает nil если проверка пройдена или не применима
// (лицензия не perpetual, поле не задано, или lic == nil).
func ValidateMaxBinaryVersion(lic *License, binaryVersion string) error {
	if lic == nil {
		return nil
	}
	if lic.Exp != -1 {
		return nil // проверка только для perpetual
	}
	if lic.MaxBinaryVersion == "" {
		return nil // не задано — без ограничений
	}
	if binaryVersion == "" || binaryVersion == "dev" {
		return nil // dev-сборка — без ограничений
	}

	cmp := compareVersions(binaryVersion, lic.MaxBinaryVersion)
	if cmp > 0 {
		return fmt.Errorf("binary version %s exceeds licensed max version %s — upgrade license to use this version",
			binaryVersion, lic.MaxBinaryVersion)
	}
	return nil
}

// IsExpired проверяет истекла ли лицензия.
// Exp = -1 (вечная) никогда не истекает.
// Iat = 0 (не активирована) — не истекла (срок начнётся при активации).
func IsExpired(lic *License) bool {
	if lic == nil {
		return true
	}
	if lic.Exp == -1 {
		return false // вечная лицензия
	}
	if lic.Exp <= 0 {
		return true
	}
	return time.Now().After(time.Unix(lic.Exp, 0))
}

// HasFeature проверяет доступна ли фича по лицензии.
// Для Community-режима (lic == nil) проверяет CommunityLimits.
func HasFeature(lic *License, feature string) bool {
	return hasFeatureInList(lic, feature)
}

// hasFeatureInList — внутренняя функция проверки фич.
func hasFeatureInList(lic *License, feature string) bool {
	var features []string
	if lic == nil {
		features = CommunityLimits.Features
	} else {
		features = lic.Lim.Features
	}

	for _, f := range features {
		if f == feature {
			return true
		}
		// "mcp_full" включает все инструменты КРОМЕ postgres
		if f == "mcp_full" && feature != "postgres" {
			return true
		}
	}
	return false
}

// GetTierLimits возвращает лимиты для указанного tier.
func GetTierLimits(tier string) Limitations {
	switch tier {
	case "trial":
		return TrialLimits
	case "indie":
		return IndieLimits
	case "pro":
		return ProLimits
	case "enterprise":
		return EnterpriseLimits
	case "privileged":
		return PrivilegedLimits
	default:
		return CommunityLimits
	}
}

// EffectiveLimits возвращает актуальные лимиты: из лицензии или Community.
// Если isSunday = true — воскресный безлимит: MaxEventsPerDay и MaxSessions = 0
// для ВСЕХ tier (для enterprise/privileged это ничего не меняет — у них и так 0).
func EffectiveLimits(lic *License, isSunday bool) Limitations {
	if lic == nil {
		lim := CommunityLimits
		if isSunday {
			lim.MaxEventsPerDay = 0
			lim.MaxSessions = 0
		}
		return lim
	}
	if lic.Lim.MaxSessions == 0 && lic.Lim.MaxEventsPerDay == 0 {
		return GetTierLimits(lic.Tier)
	}
	lim := lic.Lim
	if isSunday {
		lim.MaxEventsPerDay = 0
		lim.MaxSessions = 0
	}
	return lim
}

// SaveLicenseKey сохраняет лицензию в JSON-файл для восстановления при перезапуске.
// Используется после активации для сохранения обновлённых iat/exp/iid.
func SaveLicenseKey(path string, lic *License) error {
	data, err := json.MarshalIndent(lic, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal license: %w", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("write license file: %w", err)
	}
	return nil
}
