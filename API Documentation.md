<img src="https://r2cdn.perplexity.ai/pplx-full-logo-primary-dark%402x.png" style="height:64px;margin-right:32px"/>

# API Documentation

## Main App Service (Port 8080)

### Tasks Management

#### 1. Create Task

**Создает новую задачу записи**

```bash
curl.exe -X POST http://192.168.3.55:8080/tasks -H "Content-Type: application/json" -d "{\"name\":\"Test Recording Task\"}"
```


#### 2. Get Task by ID

**Получает информацию о задаче по ID**

```bash
curl.exe -X GET http://192.168.3.55:8080/tasks/{id}
```


#### 3. Update Task Status

**Обновляет статус задачи (stopped, waiting, running)**

```bash
curl.exe -X PUT http://192.168.3.55:8080/tasks/{id} -H "Content-Type: application/json" -d "{\"status\":\"stopped\"}"
```


#### 4. Delete Task

**Удаляет задачу по ID**

```bash
curl.exe -X DELETE http://192.168.3.55:8080/tasks/{id}
```


#### 5. Update Task Status by Stream ID

**Обновляет статус задачи по stream_id**

```bash
curl.exe -X PUT http://192.168.3.55:8080/tasks/update_status_by_stream -H "Content-Type: application/json" -d "{\"stream_id\":\"test-stream-001\",\"status\":\"waiting\"}"
```


#### 6. Get Active Tasks

**Получает список всех активных задач**

```bash
curl.exe -X GET http://192.168.3.55:8080/tasks/active
```


### Stream Management

#### 7. Recovery Handler

**Запускает процедуру восстановления стрима**

```bash
curl.exe -X POST http://192.168.3.55:8080/stream/recovery -H "Content-Type: application/json"
```


***

## Stream App Service (Port 9090)

### Stream Control

#### 1. Get Stream Status

**Получает статус всех стримов**

```bash
curl.exe -X GET http://192.168.3.55:9090/streams
```


#### 2. Create Stream

**Создает новый стрим**

```bash
curl.exe -X POST http://192.168.3.55:9090/streams -H "Content-Type: application/json" -d "{\"stream_id\":\"test-stream-001\",\"name\":\"Test Stream\"}"
```


#### 3. Start Stream

**Запускает стрим**

```bash
curl.exe -X POST http://192.168.3.55:9090/streams/{stream_id}/start -H "Content-Type: application/json"
```


#### 4. Stop Stream

**Останавливает стрим**

```bash
curl.exe -X POST http://192.168.3.55:9090/streams/{stream_id}/stop -H "Content-Type: application/json"
```


#### 5. Get Stream Info

**Получает информацию о конкретном стриме**

```bash
curl.exe -X GET http://192.168.3.55:9090/streams/{stream_id}
```


***

## Recording Service (Kafka Consumer)

### Internal Service

Recording Service работает как Kafka consumer и не имеет HTTP API эндпоинтов.

**Kafka Topics:**

- **stream.status.changed** - получает уведомления об изменении статуса стрима
- **task.created** - получает уведомления о создании новых задач

***

## Health Checks

### Main App Health

```bash
curl.exe -X GET http://192.168.3.55:8080/health
```


### Stream App Health

```bash
curl.exe -X GET http://192.168.3.55:9090/health
```


### Database Connection (Postgres)

```bash
curl.exe -X GET http://192.168.3.55:8080/health/db
```


### MinIO Health

```bash
curl.exe -X GET http://192.168.3.55:9000/minio/health/live
```


***

## Status Values

### Task Status

- `waiting` - задача создана, ожидает стрим
- `running` - запись идет
- `stopped` - запись остановлена
- `completed` - запись завершена


### Stream Status

- `offline` - стрим не активен
- `live` - стрим активен
- `error` - ошибка стрима

***

## Example Responses

### Get Task Response

```json
{
  "id": 1,
  "name": "Test Recording Task",
  "stream_id": "test-stream-001", 
  "status": "waiting",
  "created_at": "2025-09-17T00:00:00Z",
  "updated_at": "2025-09-17T00:00:00Z"
}
```


### Get Active Tasks Response

```json
{
  "tasks": [
    {
      "id": 1,
      "name": "Test Recording Task",
      "stream_id": "test-stream-001",
      "status": "running"
    }
  ],
  "count": 1
}
```


***

## Notes

- Замените `192.168.3.55` на актуальный IP адрес вашего сервера
- Замените `{id}` на реальный ID задачи
- Замените `{stream_id}` на реальный ID стрима
- Все команды адаптированы для Windows Command Prompt
- Для тестирования в production используйте HTTPS и соответствующие порты

