# Subscription Platform

Финальный проект курса [Микросервисы на Go](https://otus.ru/lessons/microservices-go/). Демонстрирует навыки проектирования приложений в микросервисной архитектуре. Базовое описание задачи см. в [TASK.md](docs/TASK.md). Правила кода, слоёв и контрактов — в [CONVENTIONS.md](docs/CONVENTIONS.md).

Локально платформа поднимается в Docker Compose. Снаружи доступен Gateway, `http://127.0.0.1:8080`. Нужны Docker, Go и [Task](https://taskfile.dev).

## Подготовка

Скопируйте [env.example](env.example) в `.env`. Файл `.env` не коммитится, Compose читает его при `task demo:up` и `task ha:up`.

Сертификаты mTLS генерируются один раз:

```bash
task certs
```

Оба стенда публикуют приложение и наблюдаемость на одних портах, поэтому одновременно работает один из них.

| Что           | Адрес                  |
| ------------- | ---------------------- |
| Gateway       | http://127.0.0.1:8080  |
| Auth          | http://127.0.0.1:8081  |
| Subscriptions | http://127.0.0.1:8082  |
| Usage         | http://127.0.0.1:8083  |
| Grafana       | http://127.0.0.1:53000 |
| Jaeger        | http://127.0.0.1:56686 |
| Prometheus    | http://127.0.0.1:59090 |

Проверка: `GET http://127.0.0.1:8080/health` отвечает `200`. В Grafana включён анонимный вход. Дашборды — Business и Technical.

## Демо-стенд

Один узел каждого хранилища: PostgreSQL, Redis, ClickHouse, NATS. Файл — [deploy/docker-compose-dev.yml](deploy/docker-compose-dev.yml).

```bash
task demo:up
```

Команда собирает образы сервисов и поднимает профиль `demo`. Остановка с удалением томов:

```bash
task demo:down
```

На этом стенде задержка генерации — 50–200 мс, доля ошибки эмуляции — 5%.

## Отказоустойчивый стенд

PostgreSQL через Patroni и etcd, Redis с Sentinel, ClickHouse из двух реплик, NATS из трёх узлов. Файл — [deploy/docker-compose-ha.yml](deploy/docker-compose-ha.yml). Сервисы приложения те же, адреса из таблицы выше те же.

```bash
task ha:up
```

```bash
task ha:down
```

Узлы данных опубликованы на соседних портах: PostgreSQL `55432`–`55434`, Redis `56379`–`56381`, Sentinel `56479`–`56481`, NATS `54222`–`54224`, ClickHouse HTTP `58123` и `58124`.

## Сценарий на поднятом стенде

`task demo` инфраструктуру не запускает. Он регистрирует пользователей, создаёт тарифы B2C и B2B, оплачивает подписки и заданное время вызывает `POST /api/v1/generate`.

```bash
task demo -- -duration 90s -concurrency 8
```

По умолчанию сценарий идёт 2 минуты в 8 потоков на `http://127.0.0.1:8080`. Ключ вебхука на обоих стендах — `webhook-secret`.
