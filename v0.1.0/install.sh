#!/bin/bash
# Revizor — установочный скрипт (v55: системная установка)
# Устанавливает бинарник в /usr/local/bin/ais_tools/revizor,
# конфиг, README и БД в ~/.config/ais_tools/revizor/.
# После установки удаляет за собой временные файлы.
set -e

SPINNER=('|' '/' '-' '\\')
SPINNER_IDX=0

spinner() {
    local msg="$1"
    local pid="$2"
    while kill -0 "$pid" 2>/dev/null; do
        printf "\r%s %s" "${SPINNER[SPINNER_IDX]}" "$msg"
        SPINNER_IDX=$(( (SPINNER_IDX + 1) % 4 ))
        sleep 0.2
    done
    printf "\r"
}

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
DIST_DIR="$SCRIPT_DIR"

cleanup() {
    # Удаляем распакованный дистрибутив и архив после установки
    echo ""
    echo "→ Cleaning up distribution files..."
    if [ -d "$DIST_DIR" ] && [ "$DIST_DIR" != "/" ]; then
        rm -rf "$DIST_DIR"
        echo "  ✓ Removed $DIST_DIR"
    fi
    PARENT_DIR="$(dirname "$DIST_DIR")"
    ARCHIVE_FILE="$PARENT_DIR/revizor.tar.gz"
    if [ -f "$ARCHIVE_FILE" ]; then
        rm -f "$ARCHIVE_FILE"
        echo "  ✓ Removed $ARCHIVE_FILE"
    fi
}
trap cleanup EXIT

echo "=== Revizor Installer ==="
echo ""

# 0. Остановка запущенного экземпляра Ревизора (если есть)
REVIZOR_PORT="${REVIZOR_PORT:-9876}"
if curl -s --max-time 3 "http://127.0.0.1:${REVIZOR_PORT}/mcp" > /dev/null 2>&1; then
    echo "→ Revizor is running on port ${REVIZOR_PORT} — sending shutdown..."
    SHUTDOWN_RESULT=$(curl -s --max-time 5 -X POST "http://127.0.0.1:${REVIZOR_PORT}/mcp" \
        -H 'Content-Type: application/json' \
        -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"trace_shutdown","arguments":{}}}' 2>/dev/null || true)
    # Ждём завершения процесса (до 5 секунд)
    for i in $(seq 1 10); do
        if ! curl -s --max-time 1 "http://127.0.0.1:${REVIZOR_PORT}/mcp" > /dev/null 2>&1; then
            echo "  ✓ Previous Revizor instance stopped"
            break
        fi
        sleep 0.5
    done
    echo ""
elif pgrep -f "revizor serve" > /dev/null 2>&1; then
    echo "→ Revizor process found (port not responding) — waiting for it to exit..."
    # Ждём 3 секунды — может процесс в процессе остановки
    sleep 3
    echo ""
fi

# 1. Определение ОС и директории установки
OS="$(uname -s)"
if [ "$OS" = "Darwin" ]; then
    if [ -w /usr/local/bin ]; then
        INSTALL_BIN=/usr/local/bin/ais_tools
    else
        INSTALL_BIN="$HOME/Applications/ais_tools"
        echo "No write permission for /usr/local/bin — installing to $INSTALL_BIN"
    fi
else
    if [ -w /usr/local/bin ]; then
        INSTALL_BIN=/usr/local/bin/ais_tools
    else
        INSTALL_BIN="$HOME/.local/bin/ais_tools"
        echo "No write permission for /usr/local/bin — installing to $INSTALL_BIN"
    fi
fi

CONFIG_DIR="$HOME/.config/ais_tools/revizor"

echo "Installing to: $INSTALL_BIN/revizor"
echo "Config: $CONFIG_DIR"
echo ""

# 2. Установка бинарника (со спиннером)
{
    mkdir -p "$INSTALL_BIN"
    cp "$SCRIPT_DIR/revizor" "$INSTALL_BIN/revizor"
    chmod +x "$INSTALL_BIN/revizor"
} &
spinner "Preparing your distribution..." $!
echo "  ✓ revizor installed to $INSTALL_BIN/revizor"

# 3. Symlink для доступности revizor из любого места
SYMLINK_PARENT="$(dirname "$INSTALL_BIN")"
SYMLINK_PATH="$SYMLINK_PARENT/revizor"
if ln -sf "$INSTALL_BIN/revizor" "$SYMLINK_PATH" 2>/dev/null; then
    echo "  ✓ symlink: $SYMLINK_PATH -> $INSTALL_BIN/revizor"
else
    echo "  ⚠ Could not create symlink at $SYMLINK_PATH — use full path: $INSTALL_BIN/revizor"
fi

# 3. Установка конфига
mkdir -p "$CONFIG_DIR"
if [ ! -f "$CONFIG_DIR/revizor.yaml" ]; then
    if [ -f "$SCRIPT_DIR/revizor.yaml.example" ]; then
        cp "$SCRIPT_DIR/revizor.yaml.example" "$CONFIG_DIR/revizor.yaml"
    fi
    echo "  ✓ Config created at $CONFIG_DIR/revizor.yaml"
else
    echo "  • Config already exists at $CONFIG_DIR/revizor.yaml (skipped)"
fi

# 4. Установка README.md для агента
if [ -f "$SCRIPT_DIR/README.md" ]; then
    cp "$SCRIPT_DIR/README.md" "$CONFIG_DIR/README.md"
    echo "  ✓ README.md installed to $CONFIG_DIR/README.md"
fi

# 5. Установка env-переменной REVIZOR_CONFIG_HOME
if [ "$OS" = "Darwin" ]; then
    if [ -w /etc/paths.d ]; then
        echo "export REVIZOR_CONFIG_HOME=\"$CONFIG_DIR\"" | sudo tee /etc/paths.d/ais_tools > /dev/null
        echo "  ✓ REVIZOR_CONFIG_HOME set in /etc/paths.d/ais_tools"
    fi
else
    PROFILE_D="/etc/profile.d/ais_tools.sh"
    if [ -w /etc/profile.d ]; then
        echo "export REVIZOR_CONFIG_HOME=\"$CONFIG_DIR\"" | sudo tee "$PROFILE_D" > /dev/null
        echo "  ✓ REVIZOR_CONFIG_HOME set in $PROFILE_D"
    elif [ -w "$HOME/.profile" ]; then
        grep -q "REVIZOR_CONFIG_HOME" "$HOME/.profile" 2>/dev/null || \
            echo "export REVIZOR_CONFIG_HOME=\"$CONFIG_DIR\"" >> "$HOME/.profile"
        echo "  ✓ REVIZOR_CONFIG_HOME added to ~/.profile"
    fi
fi

# 7. Лицензия — запись ключа, публичного ключа и URL сервера в revizor.yaml
{{REVIZOR_LICENSE_KEY}}
{{REVIZOR_LICENSE_PUBLIC_KEY}}
{{REVIZOR_LICENSE_SERVER_URL}}
if [ -n "$REVIZOR_LICENSE_PUBLIC_KEY" ]; then
    if grep -q "^  license_public_key:" "$CONFIG_DIR/revizor.yaml" 2>/dev/null; then
        sed -i "s|^  license_public_key:.*|  license_public_key: \"$REVIZOR_LICENSE_PUBLIC_KEY\"|" "$CONFIG_DIR/revizor.yaml"
    else
        sed -i "/^  port:/a\\  license_public_key: \"$REVIZOR_LICENSE_PUBLIC_KEY\"" "$CONFIG_DIR/revizor.yaml"
    fi
    echo "  ✓ License public key written to $CONFIG_DIR/revizor.yaml"
fi
if [ -n "$REVIZOR_LICENSE_KEY" ]; then
    if grep -q "^license_key:" "$CONFIG_DIR/revizor.yaml" 2>/dev/null; then
        sed -i "s|^license_key:.*|license_key: \"$REVIZOR_LICENSE_KEY\"|" "$CONFIG_DIR/revizor.yaml"
    else
        echo "" >> "$CONFIG_DIR/revizor.yaml"
        echo "license_key: \"$REVIZOR_LICENSE_KEY\"" >> "$CONFIG_DIR/revizor.yaml"
    fi
    echo "  ✓ License key written to $CONFIG_DIR/revizor.yaml"
fi
if [ -n "$REVIZOR_LICENSE_SERVER_URL" ]; then
    if grep -q "^  license_server_url:" "$CONFIG_DIR/revizor.yaml" 2>/dev/null; then
        sed -i "s|^  license_server_url:.*|  license_server_url: \"$REVIZOR_LICENSE_SERVER_URL\"|" "$CONFIG_DIR/revizor.yaml"
    else
        sed -i "/^  port:/a\\  license_server_url: \"$REVIZOR_LICENSE_SERVER_URL\"" "$CONFIG_DIR/revizor.yaml"
    fi
    echo "  ✓ License server URL set to $REVIZOR_LICENSE_SERVER_URL"
fi

echo ""
echo "=== Installation complete ==="
echo ""
echo "Usage:"
echo "  revizor serve --shift-iat  # Start HTTP server on port 9876 (auto-shift license)"
echo "  revizor --mcp          # Run in MCP stdio mode"
echo "  revizor uninstall      # Remove Revizor completely"
echo ""
echo "Documentation: $CONFIG_DIR/README.md"
echo ""
echo "To start using Revizor with your AI agent, tell it:"
echo '  "Revizor is running on port 9876. Read' "$CONFIG_DIR/README.md" 'for available tools."'
echo ""

# Проверка что бинарник в PATH
INSTALL_PARENT="$(dirname "$INSTALL_BIN")"
if [ "$INSTALL_PARENT" = "/usr/local/bin" ] || [ "$INSTALL_PARENT" = "$HOME/.local/bin" ]; then
    if ! command -v revizor &>/dev/null; then
        echo "⚠️  $INSTALL_PARENT is not in PATH."
        echo "   Add this to your ~/.bashrc or ~/.zshrc:"
        echo "   export PATH=\"$INSTALL_PARENT:\$PATH\""
    fi
fi

echo "Done!"
