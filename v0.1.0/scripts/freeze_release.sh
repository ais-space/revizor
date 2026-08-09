#!/usr/bin/env bash
# freeze_release.sh — заморозка текущей версии Ревизора в distrib/
#
# Использование:
#   ./scripts/freeze_release.sh 0.1.0
#   ./scripts/freeze_release.sh 0.2.0 "Первый стабильный релиз"
#
# Что делает:
#   1. Создаёт distrib/X_Y_Z/ с копией исходников
#   2. Записывает RELEASE_VERSION и RELEASE_DATE в distrib/X_Y_Z/.release
#   3. Выводит SQL для вставки в platform_settings
#
# Требования: запускать из корня ais_products/revizor/

set -euo pipefail

VERSION="${1:-}"
NOTE="${2:-}"

if [ -z "$VERSION" ]; then
    echo "Usage: $0 <version> [note]"
    echo "Example: $0 0.2.0 'First stable release'"
    exit 1
fi

# Нормализация версии: 0.1.0 -> 0_1_0
VERSION_DIR="$(echo "$VERSION" | tr '.' '_')"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
DISTRIB_DIR="$REPO_DIR/distrib"
RELEASE_DIR="$DISTRIB_DIR/$VERSION_DIR"

echo "=== Заморозка Ревизора v$VERSION ==="

# Проверка что не в distrib уже
if [[ "$REPO_DIR" == */distrib/* ]]; then
    echo "ОШИБКА: Нельзя запускать freeze изнутри distrib/"
    exit 1
fi

if [ -d "$RELEASE_DIR" ]; then
    echo "ОШИБКА: Версия $VERSION_DIR уже существует в distrib/"
    echo "  Удалите $RELEASE_DIR если хотите пересоздать."
    exit 1
fi

# Создание структуры
mkdir -p "$RELEASE_DIR"

# Копирование исходников (исключая бинарники, БД, distrib, build, .git)
echo "Копирование исходников..."
rsync -av --quiet \
    --exclude='/revizor' \
    --exclude='/revizor.exe' \
    --exclude='/revizor_*_bin' \
    --exclude='/revizor.db' \
    --exclude='/distrib/' \
    --exclude='/build/' \
    --exclude='/.git/' \
    --exclude='*.bak' \
    --exclude='__pycache__/' \
    "$REPO_DIR/" "$RELEASE_DIR/"

# Запись метаданных релиза
RELEASE_DATE="$(date -u +%Y-%m-%d)"
mkdir -p "$RELEASE_DIR/.release"
cat > "$RELEASE_DIR/.release/meta.json" << EOF
{
    "product": "revizor",
    "version": "$VERSION",
    "version_dir": "$VERSION_DIR",
    "release_date": "$RELEASE_DATE",
    "note": "$NOTE",
    "frozen_from": "$(cd "$REPO_DIR" && git rev-parse HEAD 2>/dev/null || echo 'unknown')"
}
EOF

# .gitignore чтобы distrib/ не попадал в git-трекинг родительской папки
touch "$RELEASE_DIR/.gitkeep"

echo ""
echo "✅ Релиз v$VERSION заморожен в distrib/$VERSION_DIR/"
echo "   Дата релиза: $RELEASE_DATE"
echo "   Файлов: $(find "$RELEASE_DIR" -type f | wc -l)"

echo ""
echo "=== SQL для platform_settings ==="
cat << EOF
INSERT INTO platform_settings (key, value, description) VALUES
  ('revizor_latest_version', '$VERSION', 'Последняя доступная версия Ревизора (semver)'),
  ('revizor_release_date_${VERSION_DIR}', '$RELEASE_DATE', 'Дата выпуска версии $VERSION')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;
EOF

echo ""
echo "=== Готово ==="
echo "Для сборки этой версии: REVIZOR_RELEASE_VERSION=$VERSION REVIZOR_SOURCE_DIR=$RELEASE_DIR python3 ..."
