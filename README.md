# Task Manager API

REST API сервис управления задачами внутри команд: ролевая модель (owner / admin / member), история изменений,
комментарии, кеширование списка задач в Redis и SQL-отчёт по команде.

**Стек:** Go 1.22 (stdlib `net/http`), MySQL 8, Redis 7, Docker Compose, OpenAPI 3.

## Быстрый старт

```bash
docker compose up --build
```

Сервис: http://localhost:8080 · Swagger UI: http://localhost:8080/swagger/ · спецификация: http://localhost:8080/openapi.yaml · health: `/healthz`.

Миграции применяются автоматически при старте приложения. Для первого запуска ничего настраивать не нужно:
у всех переменных есть дефолты в `docker-compose.yml`. Для переопределения скопируйте `.env.example` в `.env`
(**обязательно смените `JWT_SECRET` вне локальной разработки**).

> После первой сборки зафиксируйте `go.sum`: `go mod tidy && git add go.sum` (Dockerfile выполняет `go mod tidy` сам).

## Конфигурация (переменные окружения)

| Переменная | По умолчанию | Описание |
|---|---|---|
| `MYSQL_DSN` | — (обязательна) | DSN, например `app:app@tcp(mysql:3306)/taskmanager` |
| `JWT_SECRET` | — (обязательна) | Секрет подписи JWT (HS256) |
| `REDIS_ADDR` / `REDIS_PASSWORD` / `REDIS_DB` | `localhost:6379` / пусто / `0` | Подключение к Redis |
| `HTTP_ADDR` | `:8080` | Адрес HTTP-сервера |
| `JWT_TTL` | `24h` | Время жизни токена |
| `CACHE_TTL` | `5m` | TTL кеша списка задач |
| `RATE_LIMIT_RPS` / `RATE_LIMIT_BURST` | `20` / `40` | Лимит запросов на IP (`0` — выключено) |

Параметры для compose: `APP_PORT`, `MYSQL_PORT`, `MYSQL_DATABASE`, `MYSQL_USER`, `MYSQL_PASSWORD`, `MYSQL_ROOT_PASSWORD`.

## Миграции

SQL-файлы лежат в `migrations/` и встроены в бинарник (`go:embed`). При старте применяются по порядку имён;
применённые версии хранятся в таблице `schema_migrations`, запуск защищён `GET_LOCK` от гонок нескольких экземпляров.
Новая миграция — новый файл `migrations/0002_*.sql`.

## Примеры запросов

```bash
B=http://localhost:8080/api/v1
H='Content-Type: application/json'

# регистрация и вход
curl -s -X POST $B/register -H "$H" -d '{"email":"alice@example.com","name":"Alice","password":"password1"}'
curl -s -X POST $B/register -H "$H" -d '{"email":"bob@example.com","name":"Bob","password":"password1"}'
TOKEN=$(curl -s -X POST $B/login -H "$H" -d '{"email":"alice@example.com","password":"password1"}' | sed 's/.*"token":"\([^"]*\)".*/\1/')
AUTH="Authorization: Bearer $TOKEN"

# команда и приглашение (роль по умолчанию member; owner может выдать admin)
curl -s -X POST $B/teams -H "$H" -H "$AUTH" -d '{"name":"Backend"}'
curl -s -X POST $B/teams/1/invite -H "$H" -H "$AUTH" -d '{"email":"bob@example.com","role":"admin"}'
curl -s $B/teams -H "$AUTH"

# задачи
curl -s -X POST $B/tasks -H "$H" -H "$AUTH" -d '{"team_id":1,"title":"Сделать API","assignee_id":2}'
curl -s "$B/tasks?team_id=1&status=todo&assignee_id=2&limit=20&offset=0" -H "$AUTH"
curl -s -X PUT $B/tasks/1 -H "$H" -H "$AUTH" -d '{"status":"done","version":1}'
curl -s $B/tasks/1/history -H "$AUTH"

# комментарии
curl -s -X POST $B/tasks/1/comments -H "$H" -H "$AUTH" -d '{"content":"Готово"}'
curl -s $B/tasks/1/comments -H "$AUTH"

# аналитика (owner/admin)
curl -s $B/teams/1/stats -H "$AUTH"
```

Все ошибки возвращаются в формате `{"error": "...", "code": "validation_error|unauthorized|forbidden|not_found|conflict|rate_limited|internal_error"}`.

## Эндпоинты

| Метод | Путь | Описание |
|---|---|---|
| POST | `/api/v1/register`, `/api/v1/login` | Регистрация, JWT |
| POST/GET | `/api/v1/teams` | Создать команду / список моих команд |
| POST | `/api/v1/teams/{id}/invite` | Добавить участника (owner/admin) |
| PUT | `/api/v1/teams/{id}/members/{user_id}/role` | Сменить роль (только owner) — дополнение к ТЗ, закрывает «менять роли» |
| POST/GET | `/api/v1/tasks` | Создать задачу / список с фильтрами `team_id`, `status`, `assignee_id`, `limit`, `offset` |
| PUT | `/api/v1/tasks/{id}` | Обновить задачу (права + история) |
| GET | `/api/v1/tasks/{id}/history` | История изменений |
| POST/GET | `/api/v1/tasks/{id}/comments` | Комментарии |
| GET | `/api/v1/teams/{team_id}/stats` | SQL-отчёт (owner/admin) |

## Архитектура

```
cmd/api            точка входа, wiring, graceful shutdown
internal/config    конфигурация из окружения
internal/domain    сущности, DTO, категории ошибок
internal/repository  MySQL (database/sql), транзакции (InTx), SQL-отчёт, миграции
internal/cache     Redis-кеш списков + circuit breaker
internal/service   бизнес-логика и права (access.go — чистые функции проверки прав)
internal/httpapi   HTTP-хендлеры, JWT-middleware, rate limit, логи, recover
migrations, api    SQL-миграции и openapi.yaml (embed)
```

### Права доступа (на уровне бизнес-логики, `internal/service/access.go`)

- **Видимость:** команды — только свои; `GET /tasks` требует `team_id` и членство (иначе 403);
  задачи, история и комментарии чужой команды отдают **404**, чтобы не раскрывать само существование задачи.
- **Приглашения:** owner → admin/member, admin → только member; роль owner через приглашение выдать нельзя; роль owner изменить нельзя.
- **Редактирование:** owner/admin — любые задачи; создатель — название, описание, статус, исполнитель;
  исполнитель — только статус; прочие участники — нельзя. Исполнитель должен быть участником той же команды.
- **Аналитика:** только owner/admin и только по запрошенной команде (все CTE фильтруются по `team_id`).

### Транзакции и конкурентность

- Создание команды + добавление owner-участника — одна транзакция. Создание/обновление задачи + запись истории — одна транзакция.
- `PUT /tasks/{id}`: внутри транзакции строка читается `SELECT ... FOR UPDATE`, права проверяются и diff «старое → новое» строится
  по **актуальному** состоянию, затем `UPDATE ... WHERE id=? AND version=?` с `version = version + 1`. Параллельные обновления
  сериализуются, потерянных изменений нет (проверено: 20 параллельных PUT → версия +20 и 20 записей истории).
- Необязательное поле `version` в теле запроса даёт клиенту оптимистичную блокировку: при расхождении — `409 Conflict`.
- Обновление без реальных изменений не меняет версию и не пишет историю.

### Кеш (Redis)

- Кешируется результат `GET /tasks` (элементы + `total`); TTL 5 минут; ключ включает команду, версию и фильтры
  (`tasks:list:{team}:{ver}:{status}:{assignee}:{limit}:{offset}`). Кеш читается только **после** проверки членства.
- Инвалидация после коммита создания/обновления задачи: записывается новая версия `tasks:ver:{team}`, и все ключи команды
  разом становятся недостижимыми (без `KEYS`/`SCAN`). Гонка «читатель с устаревшей версией» безопасна: он пишет в ключ старой версии.
- Если Redis недоступен — сервис продолжает работать с MySQL; **circuit breaker** отключает кеш на 30 с после 5 подряд ошибок.

### SQL-отчёт

`GET /teams/{team_id}/stats` — один запрос (CTE + `UNION ALL`, см. `internal/repository/stats.go`): `JOIN`, `GROUP BY`,
агрегаты, оконная функция `ROW_NUMBER`, даты (`UTC_TIMESTAMP() - INTERVAL 30 DAY`, `TIMESTAMPDIFF`). Возвращает задачи по статусам,
топ-3 исполнителя по закрытым за 30 дней задачам, среднее время закрытия (сек), число комментариев (всего и топ-10 задач).

## Тесты

```bash
# unit-тесты прав доступа и построения diff (без БД)
go test ./...

# интеграционный тест SQL-отчёта (нужен MySQL; используйте тестовую БД)
docker compose up -d mysql
TEST_MYSQL_DSN="app:app@tcp(localhost:3306)/taskmanager" go test -tags=integration -count=1 ./internal/service
```

Интеграционный тест создаёт команды, пользователей, задачи и комментарии, проверяет все метрики отчёта (включая границу 30 дней
и отсутствие данных другой команды) и права доступа к отчёту; за собой данные удаляет.

## Дополнительно (раздел «Будет плюсом»)

Unit-тесты прав доступа · ограничение частоты запросов (token bucket на IP) · структурированные JSON-логи (`log/slog`) ·
circuit breaker для Redis. Курсорная пагинация не реализована (используется limit/offset по ТЗ).

## Допущения

- Приглашать можно только уже зарегистрированных пользователей (по email); неизвестный email → 404.
- Комментировать может любой участник команды задачи.
- Статусы: `todo`, `in_progress`, `done`; при переходе в `done` проставляется `closed_at`, при выходе из `done` сбрасывается.
- Лимит rate limiter считается по `RemoteAddr`; за reverse proxy его нужно настроить на уровне прокси.
