// Пакет license — проверка и валидация лицензионных ключей Ревизора.
package license

import (
	"crypto/sha256"
	"fmt"
	"net"
	"os"
	"strings"
)

// MachineID генерирует уникальный идентификатор машины на основе:
// - /etc/machine-id (Linux) или /var/lib/dbus/machine-id
// - hostname
// - MAC-адрес первого не-loopback сетевого интерфейса
//
// Используется для привязки лицензии к конкретной машине (анти-копирование).
func MachineID() (string, error) {
	var parts []string

	// machine-id
	if data, err := os.ReadFile("/etc/machine-id"); err == nil {
		parts = append(parts, strings.TrimSpace(string(data)))
	} else if data, err := os.ReadFile("/var/lib/dbus/machine-id"); err == nil {
		parts = append(parts, strings.TrimSpace(string(data)))
	}

	// hostname
	if host, err := os.Hostname(); err == nil {
		parts = append(parts, host)
	}

	// MAC-адрес первого не-loopback интерфейса
	if mac := getFirstMAC(); mac != "" {
		parts = append(parts, mac)
	}

	if len(parts) == 0 {
		return "", fmt.Errorf("не удалось получить ни одного идентификатора машины")
	}

	hash := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return fmt.Sprintf("%x", hash), nil
}

// getFirstMAC возвращает MAC-адрес первого не-loopback интерфейса.
func getFirstMAC() string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		if mac := iface.HardwareAddr.String(); mac != "" {
			return mac
		}
	}
	return ""
}
