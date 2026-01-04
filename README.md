# Task Manager API

## Быстрый старт (Docker Compose)

1. Поднять всё:

   ```bash
   docker compose up --build
   ```

2. Проверить, что сервис живой:

   ```bash
   curl http://localhost:8080/healthz
   ```

База поднимается и инициализируется на первом старте из `migrations/0001_init.sql`.
Для сброса локальных данных — `docker compose down -v`.

## Переменные окружения (.env.example)

- `APP_ENV` — окружение, например `dev` или `prod`.
- `HTTP_ADDR` — на каком адресе слушаем HTTP, например `:8080`.
- `STORAGE` — `memory` или `sql`.
- `DB_DRIVER` — драйвер БД, для Postgres используем `pgx`.
- `DB_DSN` — строка подключения к БД.
- `SHUTDOWN_TIMEOUT` — таймаут на graceful shutdown, например `5s`.

## Про апдейты Telegram

Решение такое:

- Для MVP — long polling (публичный HTTPS не нужен).
- Для прода — webhook (нужен публичный HTTPS).

## Запуск без Docker

```bash
make run
```

Если нужна БД:

```bash
make run-sql
```
