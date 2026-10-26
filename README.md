# aion2-global-mcp

Единый Go-демон (**Clean Architecture**: фоновый индексатор телеметрии + локальный REST API + Streamable HTTP / Stdio MCP-сервер) для **AION 2 Global**.

## Три источника данных (без хардкода ID в коде)
1. **Локальная телеметрия Questlog Desktop** (`%APPDATA%\Questlog\aion-2`):
   - `characters.json`, `self.json`, `currencies.json`, `tickets.json`, `drops/*.json`, `game-data.json`, `npc-display.json`.
   - Потоковый инкрементальный индексатор `combat-logs/*.ndjson` (хранит `last_offset` каждого лога и разделяет сумку `bag` и склад `storage` по пересечению 64-битных `instance` ID).
2. **Локализованный SQLite-каталог + живой аукцион `questlog.gg`**:
   - Read-Through резолвер `ID -> Localized Entity` с кэшем в SQLite (обратно совместим с `aion2_catalog.sqlite`).
   - Честная математика крафта: поуровневые таблицы `successRates[playerLevel - masteryLevel]`, прок `comboProbability`, комиссия аукциона, защита от тролль-лотов через `median7d` и разделение `netProfitMarket` vs `cashFlowProfit`.
3. **Публичный Web API NCSoft Global (`nuriland/aion2-api`)**:
   - Поиск и осмотр любого чужого игрока на `EU`/`NAE`/`NAW`/`SA`/`Asia` (профиль, статы, эквип, роллы сабстатов, камни, Даэванион) и официальные патчноуты Global.

## Сборка и запуск

```bash
# Проверка и тесты
make vet test

# Кросс-сборка нативного .exe для Windows 11
make build-win

# Первичная/полная синхронизация справочников в SQLite на Win11 (или подключение существующей aion2_catalog.sqlite):
aion2d.exe -db C:\Users\mrcook1e\Scripts\.aion2_cache\aion2_catalog.sqlite -sync-catalog

# Запуск фонового сервера на Win11 (MCP на /mcp + REST API на /api/v1/*):
aion2d.exe -db C:\Users\mrcook1e\Scripts\.aion2_cache\aion2_catalog.sqlite -http 0.0.0.0:8090
```
