#!/bin/bash

# Скрипт для проверки статуса агента запуска скаутов в Horizon
# Проверяет:
# 1. Статус Cloud Tasks dispatcher (ENABLE_CLOUD_TASKS_SCOUT)
# 2. Статус Firestore poller (ENABLE_SCOUT_FIRESTORE_POLLER)
# 3. Количество задач в очереди Cloud Tasks
# 4. Последние созданные ScoutingJob

set -e

# Цвета для вывода
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Параметры по умолчанию
PROJECT_ID="${GCP_PROJECT_ID:-}"
REGION="${GCP_REGION:-us-central1}"
SERVICE="${CLOUD_RUN_SERVICE:-api}"
QUEUE="${CLOUD_TASKS_SCOUT_QUEUE:-prevalent-horizon-test-scout}"

# Проверка наличия gcloud
if ! command -v gcloud &> /dev/null; then
    echo -e "${RED}Ошибка: gcloud не установлен${NC}"
    exit 1
fi

# Если PROJECT_ID не задан, пытаемся получить из текущего проекта
if [ -z "$PROJECT_ID" ]; then
    PROJECT_ID=$(gcloud config get-value project 2>/dev/null || echo "")
    if [ -z "$PROJECT_ID" ]; then
        echo -e "${RED}Ошибка: PROJECT_ID не задан. Установите переменную GCP_PROJECT_ID или используйте 'gcloud config set project PROJECT_ID'${NC}"
        exit 1
    fi
fi

echo -e "${BLUE}=== Проверка статуса агента запуска скаутов ===${NC}"
echo -e "Проект: ${GREEN}$PROJECT_ID${NC}"
echo -e "Сервис: ${GREEN}$SERVICE${NC}"
echo -e "Регион: ${GREEN}$REGION${NC}"
echo -e "Очередь: ${GREEN}$QUEUE${NC}"
echo ""

# 1. Проверка переменных окружения в Cloud Run
echo -e "${BLUE}[1] Проверка переменных окружения в Cloud Run...${NC}"
if gcloud run services describe "$SERVICE" --region "$REGION" --project "$PROJECT_ID" >/dev/null 2>&1; then
    ENABLE_CLOUD_TASKS_SCOUT=$(gcloud run services describe "$SERVICE" --region "$REGION" --project "$PROJECT_ID" \
        --format='value(spec.template.spec.containers[0].env[?(@.name=="ENABLE_CLOUD_TASKS_SCOUT")].value)' 2>/dev/null || echo "не найдено")
    
    ENABLE_SCOUT_FIRESTORE_POLLER=$(gcloud run services describe "$SERVICE" --region "$REGION" --project "$PROJECT_ID" \
        --format='value(spec.template.spec.containers[0].env[?(@.name=="ENABLE_SCOUT_FIRESTORE_POLLER")].value)' 2>/dev/null || echo "не найдено")
    
    CLOUD_TASKS_LOCATION=$(gcloud run services describe "$SERVICE" --region "$REGION" --project "$PROJECT_ID" \
        --format='value(spec.template.spec.containers[0].env[?(@.name=="CLOUD_TASKS_LOCATION")].value)' 2>/dev/null || echo "не найдено")
    
    CLOUD_TASKS_SCOUT_QUEUE=$(gcloud run services describe "$SERVICE" --region "$REGION" --project "$PROJECT_ID" \
        --format='value(spec.template.spec.containers[0].env[?(@.name=="CLOUD_TASKS_SCOUT_QUEUE")].value)' 2>/dev/null || echo "не найдено")
    
    echo -e "  ENABLE_CLOUD_TASKS_SCOUT: ${YELLOW}${ENABLE_CLOUD_TASKS_SCOUT:-не задано (по умолчанию: 1 - включен)}${NC}"
    echo -e "  ENABLE_SCOUT_FIRESTORE_POLLER: ${YELLOW}${ENABLE_SCOUT_FIRESTORE_POLLER:-не задано (по умолчанию: 0 - выключен)}${NC}"
    echo -e "  CLOUD_TASKS_LOCATION: ${YELLOW}${CLOUD_TASKS_LOCATION:-не задано}${NC}"
    echo -e "  CLOUD_TASKS_SCOUT_QUEUE: ${YELLOW}${CLOUD_TASKS_SCOUT_QUEUE:-не задано}${NC}"
    
    # Определяем фактический статус
    if [ "$ENABLE_CLOUD_TASKS_SCOUT" = "1" ] || [ "$ENABLE_CLOUD_TASKS_SCOUT" = "true" ] || [ -z "$ENABLE_CLOUD_TASKS_SCOUT" ]; then
        CLOUD_TASKS_STATUS="${GREEN}ВКЛЮЧЕН${NC}"
        CLOUD_TASKS_ACTIVE=true
    else
        CLOUD_TASKS_STATUS="${RED}ВЫКЛЮЧЕН${NC}"
        CLOUD_TASKS_ACTIVE=false
    fi
    
    if [ "$ENABLE_SCOUT_FIRESTORE_POLLER" = "1" ] || [ "$ENABLE_SCOUT_FIRESTORE_POLLER" = "true" ]; then
        FIRESTORE_POLLER_STATUS="${GREEN}ВКЛЮЧЕН${NC}"
        FIRESTORE_POLLER_ACTIVE=true
    else
        FIRESTORE_POLLER_STATUS="${RED}ВЫКЛЮЧЕН${NC}"
        FIRESTORE_POLLER_ACTIVE=false
    fi
    
    echo ""
    echo -e "  Статус Cloud Tasks dispatcher: ${CLOUD_TASKS_STATUS}"
    echo -e "  Статус Firestore poller: ${FIRESTORE_POLLER_STATUS}"
else
    echo -e "${RED}  Ошибка: сервис $SERVICE не найден в регионе $REGION${NC}"
    exit 1
fi

echo ""

# 2. Проверка очереди Cloud Tasks
TASK_COUNT=0
if [ "$CLOUD_TASKS_ACTIVE" = true ]; then
    echo -e "${BLUE}[2] Проверка очереди Cloud Tasks...${NC}"
    QUEUE_LOCATION="${CLOUD_TASKS_LOCATION:-$REGION}"
    QUEUE_NAME="${CLOUD_TASKS_SCOUT_QUEUE:-$QUEUE}"
    
    QUEUE_PATH="projects/$PROJECT_ID/locations/$QUEUE_LOCATION/queues/$QUEUE_NAME"
    
    if gcloud tasks queues describe "$QUEUE_NAME" --location="$QUEUE_LOCATION" --project="$PROJECT_ID" >/dev/null 2>&1; then
        # Получаем информацию об очереди
        QUEUE_STATE=$(gcloud tasks queues describe "$QUEUE_NAME" --location="$QUEUE_LOCATION" --project="$PROJECT_ID" \
            --format='value(state)' 2>/dev/null || echo "UNKNOWN")
        
        echo -e "  Очередь найдена: ${GREEN}$QUEUE_NAME${NC}"
        echo -e "  Состояние очереди: ${YELLOW}$QUEUE_STATE${NC}"
        
        # Подсчет задач в очереди (это может быть медленно для больших очередей)
        echo -e "  Подсчет задач в очереди..."
        TASK_COUNT=$(gcloud tasks list --queue="$QUEUE_NAME" --location="$QUEUE_LOCATION" --project="$PROJECT_ID" \
            --format='value(name)' 2>/dev/null | wc -l || echo "0")
        
        echo -e "  Количество задач в очереди: ${YELLOW}$TASK_COUNT${NC}"
        
        if [ "$TASK_COUNT" -gt 0 ]; then
            echo -e "  ${YELLOW}⚠️  В очереди есть задачи! Это может тратить деньги.${NC}"
            
            # Показываем первые несколько задач
            echo -e "  Первые задачи в очереди:"
            gcloud tasks list --queue="$QUEUE_NAME" --location="$QUEUE_LOCATION" --project="$PROJECT_ID" \
                --limit=5 --format='table(name,createTime,scheduleTime)' 2>/dev/null || echo "    Не удалось получить список задач"
        else
            echo -e "  ${GREEN}✓ Очередь пуста${NC}"
        fi
    else
        echo -e "  ${YELLOW}⚠️  Очередь $QUEUE_NAME не найдена в регионе $QUEUE_LOCATION${NC}"
        echo -e "  ${YELLOW}Это может означать, что очередь еще не создана или используется другое имя${NC}"
        echo -e "  ${YELLOW}Агент пытается ставить задачи в несуществующую очередь, что создает ошибки в логах${NC}"
    fi
else
    echo -e "${BLUE}[2] Пропуск проверки очереди (Cloud Tasks dispatcher выключен)${NC}"
fi

echo ""

# 3. Резюме и рекомендации
echo -e "${BLUE}[3] Резюме:${NC}"
if [ "$CLOUD_TASKS_ACTIVE" = true ]; then
    echo -e "  ${YELLOW}⚠️  Cloud Tasks dispatcher ВКЛЮЧЕН${NC}"
    echo -e "  ${YELLOW}   Каждый новый ScoutingJob автоматически попадает в очередь Cloud Tasks${NC}"
    if [ "$TASK_COUNT" -gt 0 ]; then
        echo -e "  ${RED}   ⚠️  В очереди $TASK_COUNT задач - это может тратить деньги!${NC}"
    fi
else
    echo -e "  ${GREEN}✓ Cloud Tasks dispatcher выключен${NC}"
fi

if [ "$FIRESTORE_POLLER_ACTIVE" = true ]; then
    echo -e "  ${YELLOW}⚠️  Firestore poller ВКЛЮЧЕН (legacy механизм)${NC}"
else
    echo -e "  ${GREEN}✓ Firestore poller выключен${NC}"
fi

echo ""
echo -e "${BLUE}Для отключения Cloud Tasks dispatcher выполните:${NC}"
echo -e "  ${YELLOW}gcloud run services update $SERVICE --region=$REGION --project=$PROJECT_ID --update-env-vars ENABLE_CLOUD_TASKS_SCOUT=0${NC}"
echo ""
echo -e "${BLUE}Для очистки очереди (удаление всех задач):${NC}"
echo -e "  ${YELLOW}gcloud tasks queues purge $QUEUE_NAME --location=$QUEUE_LOCATION --project=$PROJECT_ID${NC}"
echo ""

