# AgentHorde Azure

Полный мультиагентный цикл **без Google ADK**: тот же граф состояния, те же инструкции (`agenthorde_common.prompts.instructions`), та же логика супервизора (`agenthorde_common.orchestration.supervisor_core`), что и у Vertex. Отличия только в платформе:

- **LLM**: `openai.AsyncAzureOpenAI` + `beta.chat.completions.parse` (structured output) и **`client.responses.create`** для grounded `web_search` (Microsoft Foundry / Azure OpenAI; не Bing Search v7).
- **web_search**: OpenAI Responses API с tool `{"type": "web_search"}` на том же `AZURE_*` endpoint; тексты и URL цитируются в ответе. При недоступности Responses или пустом тексте — fallback через обычный `chat.completions` без живых URL (см. `groundingMode` в payload).
- **Критик**: сначала Azure с инструментами (как Vertex), затем та же цепочка **OpenRouter**, что в `agenthorde` Vertex (`AGENTHORDE_CRITIC_*` env).

## Инфраструктура Azure (CLI)

Минимум для AgentHorde Azure: ресурс **Azure OpenAI** (`Microsoft.CognitiveServices/accounts`, kind `OpenAI`) в вашей подписке, **custom subdomain** (`https://<name>.openai.azure.com/`), ключи **Key1/Key2**, и **model deployment** (имя деплоймента = значение `AZURE_OPENAI_DEPLOYMENT`).

Если подписка новая, сначала зарегистрируйте провайдер и проверьте **квоты TPM** в регионе (часто по умолчанию `0` до запроса в портале):

```bash
az provider register -n Microsoft.CognitiveServices --wait
az cognitiveservices usage list -l eastus -o table   # или ваш регион
```

После того как квота на выбранную модель (например `gpt-4o` или `gpt-4o-mini`) станет больше нуля:

```bash
az cognitiveservices account deployment create \
  -g <RESOURCE_GROUP> -n <OPENAI_ACCOUNT_NAME> \
  --deployment-name gpt-4o \
  --model-name gpt-4o \
  --model-version "2024-11-20" \
  --model-format OpenAI \
  --sku-name Standard \
  --sku-capacity 50
```

Локально можно положить переменные в **`agents/azure/agenthorde_azure/.env`** (файл в `.gitignore`); шаблон без секретов — [`.env.example`](./.env.example).

После апгрейда ресурса в **Microsoft Foundry** в `.env` имеет смысл держать оба URL: **`AZURE_OPENAI_ENDPOINT`** (`https://<resource>.openai.azure.com`, без суффикса `/openai/v1`) для `AsyncAzureOpenAI`, и опционально **`AZURE_FOUNDRY_PROJECT_ENDPOINT`** (`https://<resource>.services.ai.azure.com/api/projects/<project>`) — читается из `agenthorde_azure.config` для будущих Foundry-only вызовов; ключ обычно тот же, что у OpenAI-ресурса.

Для **эмбеддингов** на том же ресурсе задайте **`AZURE_EMBEDDING_DEPLOYMENT`** (имя деплоймента, например `text-embedding-3-small`).

В корне репозитория **[`.vscode/settings.json`](../../.vscode/settings.json)** продублированы те же переменные для **встроенного терминала** Cursor/VS Code (`terminal.integrated.env.*`) и `python.envFile` → `agents/azure/agenthorde_azure/.env`. Перед публичным пушем удалите ключи из `settings.json` или оставьте только `python.envFile` без секретов в JSON.

## Setup

```bash
cd agents/azure/agenthorde_azure
python3 -m venv .venv && source .venv/bin/activate
pip install -r requirements.txt
set -a && [ -f .env ] && source .env && set +a   # или экспортируйте переменные вручную
# AZURE_OPENAI_ENDPOINT / AZURE_OPENAI_API_KEY / AZURE_OPENAI_DEPLOYMENT — см. .env.example
# Рекомендуется API version с поддержкой Responses + web_search:
# export AZURE_OPENAI_API_VERSION="2025-03-01-preview"
# опционально разнести деплойменты:
# export AZURE_SUPERVISOR_DEPLOYMENT=...
# export AZURE_WORKER_DEPLOYMENT=...
# export AZURE_CRITIC_DEPLOYMENT=...
```

## Commands

```bash
python runner_azure_cli.py --probe
python runner_azure_cli.py --input-json '{"question":"…"}' [--skip-canonicalizer] [--followup-from prior.json]
python runner_azure_cli.py --canonicalize-file path/to/canonicalizer_input.json
```

### Тестовый полный прогон (как на GCP, вопрос из Vertex fixtures)

1. **Venv и зависимости** из каталога `agents/azure/agenthorde_azure`:  
   `python3 -m venv .venv && source .venv/bin/activate` → `pip install -r requirements.txt`  
   (тянет `-e` на `agenthorde_common`).

2. **Переменные Azure** — `source .env` или переменные из [`.vscode/settings.json`](../../.vscode/settings.json) в терминале Cursor.  
   Для **одной модели на супервизор + воркеры + критик** достаточно **`AZURE_OPENAI_DEPLOYMENT=o4-mini`**: остальные роли берут его же, если не заданы `AZURE_SUPERVISOR_DEPLOYMENT` / `AZURE_WORKER_DEPLOYMENT` / `AZURE_CRITIC_DEPLOYMENT`.

3. **`OPENROUTER_API_KEY`** (рекомендуется): критик сначала бьёт в Azure, затем при сбое — цепочка OpenRouter; без ключа fallback не сработает.

4. **Канонизатор**: сейчас фазы LLM в `agenthorde_common` идут через **Vertex или OpenRouter**, не через этот Azure-клиент. Для чисто Azure-smoke теста оркестрации используйте **`--skip-canonicalizer`**. Полный граф без GCP — отдельная доработка (Azure в `LLMPhaseClient`).

5. **Пример** (тот же смысл вопроса, что и fixture Малакки на Vertex):

```bash
cd agents/azure/agenthorde_azure
set -a && source .env && set +a
export OPENROUTER_API_KEY="..."   # опционально, но лучше для критика

python runner_azure_cli.py --probe

python runner_azure_cli.py \
  --input-file ../../vertex/agenthorde/fixtures/malacca_strait_resources.json \
  --skip-canonicalizer
```

Укоротить циклы для быстрого теста: `export AGENTHORDE_MAX_SUPERVISOR_CYCLES=2` (или `AZURE_MAX_SUPERVISOR_CYCLES`).

См. также [docs/agents/agenthorde-runtimes.md](../../../docs/agents/agenthorde-runtimes.md).
