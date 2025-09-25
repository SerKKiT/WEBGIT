Вот тестовые curl команды для проверки всех endpoints вашего веб-сервиса:

## Тестирование основных endpoints для задач

### 1. Получить все задачи
```bash
curl -X GET http://localhost:8080/tasks
```

### 2. Создать новую задачу
```bash
curl -X POST http://localhost:8080/tasks -H "Content-Type: application/json" -d "{\"name\":\"Test Task 1\"}"
```

```bash
curl -X POST http://localhost:8080/tasks -H "Content-Type: application/json" -d "{\"name\":\"Test Task 2\"}"
```

### 3. Обновить статус задачи (замените {id} на реальный ID)
```bash
curl -X PUT "http://localhost:8080/tasks?id=1" -H "Content-Type: application/json" -d "{\"status\":\"waiting\"}"
```

```bash
curl -X PUT "http://localhost:8080/tasks?id=1" -H "Content-Type: application/json" -d "{\"status\":\"running\"}"        //используется только приложением внутри!!!!
```

```bash
curl -X PUT "http://localhost:8080/tasks?id=1" -H "Content-Type: application/json" -d "{\"status\":\"stopped\"}"
```

```bash
curl -X PUT "http://localhost:8080/tasks?id=1" -H "Content-Type: application/json" -d "{\"status\":\"error\"}"        //используется только приложением внутри!!!!
```

### 4. Удалить задачу
```bash
curl -X DELETE "http://localhost:8080/tasks?id=1"
```

## Тестирование специальных endpoints

### 5. Получить активные задачи
```bash
curl -X GET http://localhost:8080/tasks/active
```

### 6. Обновить статус по stream_id (эмуляция уведомления от stream-app)
```bash
curl -X PUT http://localhost:8080/tasks/update_status_by_stream -H "Content-Type: application/json" -d "{\"stream_id\":\"abc-def-ghi-jkl\",\"status\":\"running\"}"             //используется только приложением внутри!!!!
```

### 7. Проверить статус миграций
```bash
curl -X GET http://localhost:8080/debug/migrations
```

## Тестирование ошибочных сценариев

### Попытка создать задачу без имени
```bash
curl -X POST http://localhost:8080/tasks -H "Content-Type: application/json" -d "{}"
```

### Попытка обновить задачу с невалидным статусом
```bash
curl -X PUT "http://localhost:8080/tasks?id=1" -H "Content-Type: application/json" -d "{\"status\":\"invalid_status\"}"
```

### Попытка обновить несуществующую задачу
```bash
curl -X PUT "http://localhost:8080/tasks?id=9999" -H "Content-Type: application/json" -d "{\"status\":\"stopped\"}"
```

### Попытка удалить несуществующую задачу
```bash
curl -X DELETE "http://localhost:8080/tasks?id=9999"
```

### Попытка использовать неподдерживаемый HTTP метод
```bash
curl -X PATCH http://localhost:8080/tasks
```

## Полный сценарий тестирования

```bash
echo "=== Создаем тестовые задачи ===" && curl -X POST http://localhost:8080/tasks -H "Content-Type: application/json" -d "{\"name\":\"Task 1\"}" && echo "" && curl -X POST http://localhost:8080/tasks -H "Content-Type: application/json" -d "{\"name\":\"Task 2\"}" && echo ""
```

```bash
echo "=== Получаем все задачи ===" && curl -X GET http://localhost:8080/tasks && echo ""
```

```bash
echo "=== Обновляем статус первой задачи ===" && curl -X PUT "http://localhost:8080/tasks?id=1" -H "Content-Type: application/json" -d "{\"status\":\"waiting\"}" && echo ""
```

```bash
echo "=== Получаем активные задачи ===" && curl -X GET http://localhost:8080/tasks/active && echo ""
```

```bash
echo "=== Проверяем миграции ===" && curl -X GET http://localhost:8080/debug/migrations && echo ""
```

## Примечания

- Замените `localhost:8080` на актуальный адрес вашего сервера
- ID задач в командах обновления и удаления замените на реальные ID из ответов создания задач
- Все команды форматированы для Windows и являются однострочными
- Stream_id в тестах указан как пример - используйте реальные значения из созданных задач
- Команды тестируют как успешные сценарии, так и обработку ошибок