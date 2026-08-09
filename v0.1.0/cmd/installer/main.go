// Windows installer for Revizor — self-extracting console installer.
// Build: GOOS=windows GOARCH=amd64 go build -ldflags "-H windowsgui" -o revizor-installer.exe ./cmd/installer/
//
// The installer embeds revizor.exe, revizor.yaml, license.key, and README.md.
// On launch, it copies everything to the proper Windows paths and configures the environment.

package main

import (
	"bufio"
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

//go:embed revizor.exe
var revizorBinary []byte

//go:embed revizor.yaml
var revizorYAML []byte

//go:embed license.key
var licenseKey []byte

//go:embed README.md
var readmeMD []byte

func init() {
	// Ensure empty defaults if embed files are missing (build-time placeholders)
	if len(readmeMD) == 0 {
		readmeMD = []byte("Revizor — AI-managed debug logging.\nhttps://ais-platform.dev/revizor\n")
	}
}

var (
	programFiles  = os.Getenv("ProgramFiles")
	appData       = os.Getenv("APPDATA")
	installDir    = filepath.Join(programFiles, "AIS_Tools", "Revizor")
	configDir     = filepath.Join(appData, "AIS_Tools", "Revizor")
)

func main() {
	fmt.Println("=== Revizor Installer (Windows) ===")
	fmt.Println()

	// Check admin rights
	admin := isAdmin()
	if !admin {
		fmt.Println("WARNING: Not running as Administrator.")
		fmt.Println("Some features may not work. Right-click -> Run as Administrator for full setup.")
		fmt.Println()
	}

	// 1. Install binary
	fmt.Printf("Installing to: %s\n", installDir)
	fmt.Printf("Config:       %s\n", configDir)
	fmt.Println()

	fmt.Print("Installing...")
	if err := os.MkdirAll(installDir, 0755); err != nil {
		fatal("Failed to create install directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(installDir, "revizor.exe"), revizorBinary, 0755); err != nil {
		fatal("Failed to write revizor.exe: %v", err)
	}
	fmt.Println(" OK")

	// 2. Install config
	if err := os.MkdirAll(configDir, 0755); err != nil {
		fatal("Failed to create config directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "revizor.yaml"), revizorYAML, 0644); err != nil {
		fatal("Failed to write revizor.yaml: %v", err)
	}
	fmt.Println("  -> Config saved")

	// 3. Install license key
	if err := os.WriteFile(filepath.Join(configDir, "license.key"), licenseKey, 0600); err != nil {
		fatal("Failed to write license.key: %v", err)
	}
	fmt.Println("  -> License key saved")

	// 4. Install README.md for agent
	if err := os.WriteFile(filepath.Join(configDir, "README.md"), readmeMD, 0644); err != nil {
		fatal("Failed to write README.md: %v", err)
	}
	fmt.Println("  -> README.md saved")

	// 5. Set REVIZOR_CONFIG_HOME environment variable
	err := setUserEnv("REVIZOR_CONFIG_HOME", configDir)
	if err != nil {
		fmt.Printf("  WARNING: Could not set REVIZOR_CONFIG_HOME: %v\n", err)
		fmt.Println("  Add manually: setx REVIZOR_CONFIG_HOME", configDir)
	} else {
		fmt.Println("  -> REVIZOR_CONFIG_HOME set")
	}

	// 6. Add to user PATH
	if err := addToPath(installDir); err != nil {
		fmt.Printf("  WARNING: Could not add to PATH: %v\n", err)
		fmt.Printf("  Add manually: %s\n", installDir)
	} else {
		fmt.Println("  -> Added to PATH")
	}

	fmt.Println()
	fmt.Println("=== Installation complete ===")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  revizor serve          Start HTTP server on port 9876")
	fmt.Println("  revizor --mcp          Run in MCP stdio mode")
	fmt.Println("  revizor uninstall      Remove Revizor completely")
	fmt.Println()
	fmt.Printf("Documentation: %s\\README.md\n", configDir)
	fmt.Println()
	fmt.Println("Press Enter to exit...")
	bufio.NewReader(os.Stdin).ReadBytes('\n')
}

func fatal(format string, args ...interface{}) {
	fmt.Printf("\nERROR: "+format+"\n", args...)
	fmt.Println("Press Enter to exit...")
	bufio.NewReader(os.Stdin).ReadBytes('\n')
	os.Exit(1)
}

func isAdmin() bool {
	_, err := os.Open("\\\\.\\PHYSICALDRIVE0")
	return err == nil
}

// setUserEnv sets a user environment variable via the Windows registry.
func setUserEnv(name, value string) error {
	// Use setx command — simplest reliable way
	cmd := exec.Command("setx", name, value)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Run()
}

// addToPath adds a directory to the user PATH using setx.
func addToPath(dir string) error {
	currentPath := os.Getenv("PATH")
	if strings.Contains(currentPath, dir) {
		return nil // already in PATH
	}
	newPath := currentPath + ";" + dir
	return setUserEnv("PATH", newPath)
}
