#!/bin/bash

# Скрипт для отключения агента запуска скаутов в Horizon
# Отключает Cloud Tasks dispatcher, чтобы предотвратить попытки постановки задач в несуществующую очередь

set -e

# Цвета для вывода
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Параметры по умолчанию
PROJECT_ID="${GCP_PROJECT_ID:-sanskrite}"
REGION="${GCP_REGION:-us-central1}"
SERVICE="${CLOUD_RUN_SERVICE:-sanskrite-api}"

echo -e "${BLUE}=== Отключение агента запуска скаутов ===${NC}"
echo -e "Проект: ${GREEN}$PROJECT_ID${NC}"
echo -e "Сервис: ${GREEN}$SERVICE${NC}"
echo -e "Регион: ${GREEN}$REGION${NC}"
echo ""

# Проверка наличия gcloud
if ! command -v gcloud &> /dev/null; then
    echo -e "${RED}Ошибка: gcloud не установлен${NC}"
    exit 1
fi

# Проверка существования сервиса
if ! gcloud run services describe "$SERVICE" --region "$REGION" --project "$PROJECT_ID" >/dev/null 2>&1; then
    echo -e "${RED}Ошибка: сервис $SERVICE не найден в регионе $REGION${NC}"
    exit 1
fi

echo -e "${YELLOW}Внимание: Это отключит Cloud Tasks dispatcher для скаутов.${NC}"
echo -e "${YELLOW}ScoutingJob будут создаваться, но не будут автоматически ставиться в очередь Cloud Tasks.${NC}"
echo ""
read -p "Продолжить? (y/N): " -n 1 -r
echo ""
if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    echo -e "${YELLOW}Отменено${NC}"
    exit 0
fi

echo -e "${BLUE}Отключение Cloud Tasks dispatcher...${NC}"
gcloud run services update "$SERVICE" \
    --region="$REGION" \
    --project="$PROJECT_ID" \
    --update-env-vars ENABLE_CLOUD_TASKS_SCOUT=0

if [ $? -eq 0 ]; then
    echo -e "${GREEN}✓ Cloud Tasks dispatcher успешно отключен${NC}"
    echo ""
    echo -e "${BLUE}Для повторного включения выполните:${NC}"
    echo -e "  ${YELLOW}gcloud run services update $SERVICE --region=$REGION --project=$PROJECT_ID --update-env-vars ENABLE_CLOUD_TASKS_SCOUT=1${NC}"
else
    echo -e "${RED}✗ Ошибка при отключении агента${NC}"
    exit 1
fi

