# Сервисы AIS / суда в реальном времени

Краткий обзор сервисов, которые дают позиции судов в онлайне (AIS), и как их подключать к карте.

---

## 1. **AISStream** (рекомендуется, через бэкенд)

- **Сайт:** https://aisstream.io/
- **Документация:** https://aisstream.io/documentation
- **Модель:** WebSocket API; **прямое подключение из браузера не поддерживается** (ключ не должен светиться на фронте). В проекте используется **прокси на бэкенде**.
- **Настройка:**
  1. Получить API key: https://aisstream.io/apikeys (вход через GitHub и т.п.).
  2. В **бэкенде** ключ задать одним из способов:
     - переменная окружения: `AISSTREAM_API_KEY=ваш_ключ`;
     - либо GCP Secret Manager: секрет `aisstream_api_key` (в Cloud Run привязать к env `AISSTREAM_API_KEY`).
  3. Фронт **ничего** не настраивает: по умолчанию дергает `VITE_API_BASE_URL/api/v1/vessels/bbox?minLat=...&maxLat=...&minLon=...&maxLon=...`. Ключ хранится только на сервере.
- Эндпоинт бэкенда: `GET /api/v1/vessels/bbox` (без авторизации). Ответ: `{ "data": [ { "lat", "lng", "mmsi", "name?", "sog", "cog" } ] }`.

---

## 2. **Marinesia** (проще всего стартовать с ключом на фронте)

- **Сайт:** https://marinesia.com/
- **Регистрация:** бесплатный аккаунт → в кабинете выдаётся Free API Key.
- **Документация:** https://docs.marinesia.com/
- **Free tier:** 1 запрос / 20 минут; поиск «рядом» — до 2 судов. Для карты по bbox обычно нужен платный тариф (до 2000 судов в области).
- **Переменные окружения (пример):**  
  `VITE_MARINESIA_API_KEY=your_key`  
  Включение слоя судов в панели карты опционально (см. ниже).

---

## 3. **AISHub**

- **Сайт:** https://www.aishub.net/
- **API (bbox):**  
  `https://data.aishub.net/ws.php?username=USER&format=1&output=json&latmin=...&latmax=...&lonmin=...&lonmax=...`
- **Ключ:** бесплатный доступ в обмен на **свой AIS-фид** (UDP-стрим на их сервер). Подходит, если есть приёмник AIS или другой источник данных.
- **Документация:** https://www.aishub.net/api

---

## 4. **VesselFinder API**

- **Сайт:** https://www.vesselfinder.com/  
- **API:** https://api.vesselfinder.com/docs/
- **Модель:** кредиты (pay-per-use) или подписка. Есть LiveData по прямоугольнику (bbox), удобно для карты.
- **Для продакшена:** покупка пакетов кредитов или подписка.

---

## 5. **MarineTraffic**

- **Сайт:** https://www.marinetraffic.com/
- **API:** https://support.marinetraffic.com/ (раздел Using API's).
- Коммерческий доступ к AIS, есть разные планы.

---

## 6. **OpenSeaMap**

- **Сайт:** https://openseamap.org/
- Открытые карты и данные; треки судов частично завязаны на свои станции и партнёров. Готового публичного API «все суда в bbox» с бесплатным ключом может не быть — уточнять на сайте.

---

## Подключение в проекте

Слой судов на карте реализован в хуке **`useVesselLayer`**.

- **Включение:** переключатель **«Vessels (AIS)»** в панели фильтров карты. Кнопка показывается, если доступен бэкенд-прокси (AISStream) или задан свой API (Marinesia и т.д.).
- **Вариант AISStream (рекомендуется):** ключ только на бэкенде. Либо env на сервере `AISSTREAM_API_KEY=...`, либо в GCP Secret Manager создать секрет `aisstream_api_key` и в Cloud Run пробросить: `AISSTREAM_API_KEY=aisstream_api_key:latest`. Фронт использует `VITE_API_BASE_URL` и вызывает `GET /api/v1/vessels/bbox` — отдельный ключ на фронте не нужен.
- **Вариант Marinesia / свой прокси:** в корне фронта (`web/`) в `.env.local`:
  ```bash
  VITE_MARINESIA_API_KEY=your_key
  # или
  VITE_VESSEL_API_KEY=your_key
  VITE_VESSEL_API_URL=https://your-proxy.com/vessels
  ```
- **Логика:** при движении карты (и раз в 60 с) запрашиваются суда в видимой области; точки отображаются кружками; по клику — попап (название, MMSI, скорость, курс).
- **Marinesia Free:** до 2 судов, 1 запрос / 20–30 мин — только для проверки. Для нормальной карты — AISStream через бэкенд, Marinesia Premium или свой прокси к AISHub/VesselFinder.

Итог: **суда в онлайне подключаются** через бэкенд-прокси AISStream (ключ на сервере) или через Marinesia/AISHub/VesselFinder/MarineTraffic с ключом или прокси на фронте.
