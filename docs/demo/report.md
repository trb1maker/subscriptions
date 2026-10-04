# Прогон демо-стенда

Стенд: `task demo:up` (`deploy/docker-compose-dev.yml`).
Нагрузка: `task demo -- -duration 90s -concurrency 8`.
Дата: 5 октября 2026, около 01:12–01:25 (+05).

Сценарий сам зарегистрировал участников, создал тарифы B2C и B2B, оплатил подписки, сменил тариф одному пользователю и вызывал `POST /api/v1/generate`. Задержка генерации на стенде — 50–200 мс, доля ошибки эмуляции — 5%.

## Ответы Gateway

| Показатель                 | Значение                                        |
| -------------------------- | ----------------------------------------------- |
| Участники                  | 6: 4 пользователя B2C и 2 участника организации |
| Остаток после смены тарифа | 2500 сообщений                                  |
| Запросы                    | 5443                                            |
| `processed`                | 5146                                            |
| `failed`                   | 287                                             |
| Отклонены                  | 10                                              |
| Ошибки транспорта          | 0                                               |
| p50                        | 131 мс                                          |
| p95                        | 200 мс                                          |

Доля `failed` — около 5%, как задано эмулятору.

## ClickHouse

Снимок после того, как потребитель Usage разобрал поток `USAGE` (в очереди не осталось необработанных сообщений).

| Таблица                             | Значение                    |
| ----------------------------------- | --------------------------- |
| `usage.events`, `message_processed` | 5277                        |
| `usage.events`, `message_failed`    | 294                         |
| `usage.events`, `payment_received`  | 10                          |
| `usage.payments`                    | 10 строк                    |
| `usage.token_usage`, `processed`    | 5277 событий, 342032 токена |
| `usage.token_usage`, `failed`       | 294 события, 17928 токенов  |

Во время прогона потребитель отставал: каждое событие применяется по очереди и пересобирает проекцию владельца. К моменту скриншотов очередь уже разобрана. В логе Usage 2509 записей `idempotency conflict`: повторная доставка с тем же идентификатором и другим телом события отклоняется и не применяется второй раз.

## Prometheus

| Метрика                                        | Значение                                |
| ---------------------------------------------- | --------------------------------------- |
| `messages_consumed_total{type="user"}`         | 3305                                    |
| `messages_consumed_total{type="organization"}` | 1710                                    |
| `tokens_used_total{type="user"}`               | 227079                                  |
| `tokens_used_total{type="organization"}`       | 115055                                  |
| `overdraft_count`                              | 710                                     |
| `payments_total{status="received"}`            | 5                                       |
| `http_requests_total`, gateway, 200            | 5434                                    |
| `http_requests_total`, gateway, 403            | 10                                      |
| `http_requests_total`, gateway, 201            | 16                                      |
| `http_requests_total`, gateway, 202            | 5                                       |
| `service_healthy`                              | 1 у auth, gateway, subscriptions, usage |

Gateway принял 5 вебхуков (HTTP 202). В ClickHouse событий `payment_received` — 10.

Овердрафт на дашборде появился, когда проекция догнала параллельные генерации: проверка лимита пускает запрос, пока остаток больше нуля, а списание приходит событием позже.

## Скриншоты

- `docs/demo/grafana-business.png` — сообщения, токены, овердрафт, платежи. http://127.0.0.1:53000/d/business/business
- `docs/demo/grafana-technical.png` — HTTP, p95, ошибки, здоровье, логи. http://127.0.0.1:53000/d/technical/technical
- `docs/demo/jaeger-trace.png` — трейс `5183b8b9bbd65c40d3f6ee75f65c2cf9`, `POST /api/v1/generate`, 158.92 мс, 4 сервиса, 11 спанов. http://127.0.0.1:56686
