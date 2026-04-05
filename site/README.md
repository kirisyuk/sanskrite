# Sanskrite — внешний сайт

Публичный лендинг/маркетинговый сайт. Хостинг: Firebase Hosting.

## Состав

- `index.html` — главная страница
- `styles.css` — стили
- `script.js` — мобильная навигация
- `images/` — изображения hero и сервисов
- `firebase.json` — конфиг Firebase Hosting

## Локальный запуск

```bash
cd /Users/kirisyuk/cursor/sanskrite/site
python3 -m http.server 4173
```

Открыть: `http://localhost:4173`

## Деплой на Firebase Hosting

```bash
cd /Users/kirisyuk/cursor/sanskrite/site
firebase deploy --only hosting:marketing
```

## Привязка к Firebase project `horde-horizon-test-20260405`

Внешний сайт настроен как отдельный Hosting site внутри того же Firebase project,
что и платформа.

- Firebase project: `horde-horizon-test-20260405`
- Hosting target для внешнего сайта: `marketing`
- Hosting site id для внешнего сайта: `horde-horizon-marketing`

Текущая платформа в `/Users/kirisyuk/cursor/sanskrite/web` продолжает
деплоиться в default Hosting site `horde-horizon-test-20260405`, поэтому конфиг в `site/`
не должен её перезаписывать.

## One-time setup

Если второй Hosting site ещё не создан в проекте:

```bash
firebase login --reauth
firebase hosting:sites:create horde-horizon-marketing --project horde-horizon-test-20260405
```

После этого деплой внешнего сайта:

```bash
cd /Users/kirisyuk/cursor/sanskrite/site
firebase deploy --only hosting:marketing
```

Если site id нужен другой, достаточно поменять его в `.firebaserc`.
