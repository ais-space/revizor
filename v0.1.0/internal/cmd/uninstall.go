package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Uninstall удаляет файлы Ревизора в зависимости от ОС.
// Возвращает список удалённых путей.
func Uninstall(dryRun bool, keepDB bool) ([]string, error) {
	var paths []string

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("get home dir: %w", err)
	}

	switch runtime.GOOS {
	case "linux":
		// Бинарник + symlink
		paths = append(paths, "/usr/local/bin/ais_tools/revizor")
		paths = append(paths, "/usr/local/bin/revizor") // symlink
		// Конфиг
		configDir := filepath.Join(homeDir, ".config", "ais_tools", "revizor")
		if keepDB {
			paths = append(paths, filepath.Join(configDir, "revizor.yaml"))
			paths = append(paths, filepath.Join(configDir, "license.key"))
		} else {
			paths = append(paths, configDir)
		}
		// Env-переменная
		paths = append(paths, "/etc/profile.d/ais_tools.sh")

	case "darwin":
		// Бинарник
		binPath := "/usr/local/bin/ais_tools/revizor"
		if _, err := os.Stat(binPath); os.IsNotExist(err) {
			binPath = filepath.Join(homeDir, "Applications", "ais_tools", "revizor")
		}
		paths = append(paths, binPath)
		// Конфиг
		configDir := filepath.Join(homeDir, ".config", "ais_tools", "revizor")
		if keepDB {
			paths = append(paths, filepath.Join(configDir, "revizor.yaml"))
			paths = append(paths, filepath.Join(configDir, "license.key"))
		} else {
			paths = append(paths, configDir)
		}
		// Env-переменная
		paths = append(paths, "/etc/paths.d/ais_tools")

	case "windows":
		// Бинарник
		paths = append(paths, `C:\Program Files\AIS_Tools\Revizor\revizor.exe`)
		// Конфиг
		appData := os.Getenv("APPDATA")
		if appData != "" {
			configDir := filepath.Join(appData, "AIS_Tools", "Revizor")
			if keepDB {
				paths = append(paths, filepath.Join(configDir, "revizor.yaml"))
				paths = append(paths, filepath.Join(configDir, "license.key"))
			} else {
				paths = append(paths, configDir)
			}
		}

	default:
		return nil, fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}

	if dryRun {
		fmt.Println("Would remove:")
		for _, p := range paths {
			fmt.Printf("  %s\n", p)
		}
		return paths, nil
	}

	var removed []string
	for _, p := range paths {
		// Lstat вместо Stat: битый symlink тоже нужно удалить
		if _, err := os.Lstat(p); err == nil {
			if err := os.RemoveAll(p); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to remove %s: %v\n", p, err)
			} else {
				removed = append(removed, p)
				fmt.Printf("Removed: %s\n", p)
			}
		}
	}

	// Удаляем REVIZOR_CONFIG_HOME из ~/.profile
	profilePath := filepath.Join(homeDir, ".profile")
	if data, err := os.ReadFile(profilePath); err == nil {
		var newLines []string
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.Contains(line, "REVIZOR_CONFIG_HOME") {
				newLines = append(newLines, line)
			}
		}
		newContent := strings.Join(newLines, "\n") + "\n"
		if string(data) != newContent {
			if err := os.WriteFile(profilePath, []byte(newContent), 0644); err == nil {
				removed = append(removed, "~/.profile (REVIZOR_CONFIG_HOME line)")
				fmt.Println("Removed: REVIZOR_CONFIG_HOME from ~/.profile")
			}
		}
	}

	return removed, nil
}
