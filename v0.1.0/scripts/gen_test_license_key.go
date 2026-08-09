// Утилита для генерации тестовой пары Ed25519-ключей и подписанного лицензионного ключа.
// Использование:
//
//	go run ./scripts/gen_test_license_key.go
//
// Выводит:
//  1. Приватный ключ (base64) — для сервера лицензирования
//  2. Публичный ключ (base64) — вшить в бинарник (константа PublicKey)
//  3. Тестовый лицензионный ключ для проверки Pro-режима
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
)

func main() {
	// Генерация Ed25519 пары
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		panic(fmt.Sprintf("ошибка генерации Ed25519: %v", err))
	}

	fmt.Println("=== ТЕСТОВЫЕ Ed25519 КЛЮЧИ ===")
	fmt.Println()
	fmt.Println("# Приватный ключ (сохранить для сервера лицензирования):")
	fmt.Println(base64.StdEncoding.EncodeToString(privKey))
	fmt.Println()
	fmt.Println("# Публичный ключ (указать в revizor.yaml: server.license_public_key или env REVIZOR_LICENSE_PUBLIC_KEY):")
	fmt.Println(base64.StdEncoding.EncodeToString(pubKey))
	fmt.Println()

	// Создание тестовой лицензии
	lic := map[string]any{
		"ver":  2,
		"kid":  "test_key_2026",
		"sub":  "test@ais-platform.dev",
		"tier": "pro",
		"iat":  time.Now().Unix(),
		"exp":  time.Now().Add(365 * 24 * time.Hour).Unix(),
		"lim": map[string]any{
			"max_sessions":      10,
			"max_events_per_day": 100000,
			"features":           []string{"mcp", "postgres", "sse", "inject", "mcp_full", "inject_apply", "analytics"},
		},
		"iid": "inst_test_0000000001",
	}

	licJSON, _ := json.Marshal(lic)

	// zstd сжатие
	encoder, _ := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
	compressed := encoder.EncodeAll(licJSON, nil)
	encoder.Close()

	// Ed25519 подпись
	signature := ed25519.Sign(privKey, compressed)

	// Сборка ключа: compressed + signature
	fullKey := append(compressed, signature...)

	// base64url кодирование (замена символов для URL-safe)
	encoded := base64.StdEncoding.EncodeToString(fullKey)
	encoded = strings.TrimRight(encoded, "=")

	fmt.Println("# Тестовый лицензионный ключ (REVIZOR_LICENSE_KEY):")
	// Группы по 4 символа для читаемости
	var groups []string
	for i := 0; i < len(encoded); i += 4 {
		end := i + 4
		if end > len(encoded) {
			end = len(encoded)
		}
		groups = append(groups, encoded[i:end])
	}
	fmt.Println(strings.Join(groups, "-"))
	fmt.Println()
	fmt.Println("# Для проверки:")
	fmt.Println("#   REVIZOR_LICENSE_KEY=<ключ> ./revizor serve")
	fmt.Println()
}
