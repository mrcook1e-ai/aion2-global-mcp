---
name: aion2-global
description: Domain, combat analytics, DPS pipeline optimization & economic playbook for AION 2 Global character builds, gear/skills/genus audits, multi-step crafting profitability, auction house flips/dips, boss rotations, dungeon speedruns, PvP incoming/outgoing logs, and Global player comparison via the aion2 MCP server. Use whenever the user asks about AION 2 Global DPS optimization, gear audits, skill tripods/Daevanion, Genus rolls, crafts, market deals, server prices, gear, boss encounters, PvP deaths, drops/Kinah per hour, or inspecting players.
---

# AION 2 Global — Agent Playbook (v0.3.0)

Use the `aion2` MCP tools to inspect real-time local character builds, audit DPS bottlenecks across gear/skills/genus, review combat/DPS/PvP telemetry, optimize multi-step crafting economics, track market deals, and inspect any Global player.

---

## 1. Точечный аудит DPS и поиск просадок персонажа (КЛЮЧЕВОЙ ВОРКФЛОУ)

Когда пользователь спрашивает:
- *"Как поднять мой DPS?"*
- *"Где у меня просадки в уроне / билде?"*
- *"Посмотри мое снаряжение, скиллы или генус"*
- *"Какой стат мне качать в первую очередь?"*

### Порядок действий агента:
1. Вызови инструмент **`audit_character_dps`** (передай имя персонажа или оставь пустым для активного).
   Опционально укажи `mode: "pve"` (по умолчанию), `"pvp"` или `"abyss"` и защиту целевого босса `targetDef` (по умолчанию 4500).
2. Разбери полученный ответ по 3 обязательным блокам просадок:

---

### Блок 1: Снаряжение (Gear Audit)
- **Уровень заточки (EnchantLevel)**:
  - Любой слот с заточкой **ниже +10** помечается как критическая просадка (прямая потеря 10–15% характеристик слота).
  - Снаряжение от **+10 до +11** помечается как незавершенное: базовый кап тира требует **минимум +12**, а для капового урона — **+15** (открывает скрытые аффиксы атаки).
- **Прорыв Exceed (Breakthrough)**:
  - На всех героических (Grade 41) и реликварных (Grade 51) предметах должен быть открыт Exceed (1..5/5). Если `exceedLevel == 0`, укажи пользователю на необходимость прорыва дубликатами для открытия дополнительных субстатов.
- **Магические камни (Manastones)**:
  - Проверь `manastones` каждого слота: отсутствие камней — грубая ошибка.
  - Рекомендуй вставлять профильные камни: для физ. классов — *Атака* и *Крит. удар*, для маг. классов — *Сила магии* и *Маг. крит*, а также *Пробивание* при нехватке на боссах.
- **Уникальные статы по слотам**:
  - *Оружие*: Pure Attack, Weapon Damage Boost (каждые +1% дают чистый множитель на всю цепочку!), Max Attack.
  - *Перчатки*: Combat Speed (скорость боя сокращает анимации и разгоняет автоатаки/weaving).
  - *Бижутерия (Ожерелье, Кольца, Серьги)*: Damage Boost %, Double Chance, Critical Damage Boost.
  - *Броня (Нагрудник, Поножи, Плечи, Сапоги)*: Пробивание (Penetration) и основные атрибуты (Might).

---

### Блок 2: Скиллы (Арканы + Стигмы + Девенеон)
- **Уровни умений**:
  - Все экипированные умения должны быть прокачаны **минимум до Ур. 10** (открывает 2-й тир специализации) и стремиться к **Ур. 20**. Низкий уровень умения дает слабую плоскую прибавку (`flat_dmg`).
- **Специализации и триподы (Tripods)**:
  - Проверь `specializationDetails`: у каждого активного умения должны быть выбраны триподы.
  - Фокус в PvE: триподы на гарантированный крит, сокращение отката (CD reduction) и мультиудар/множители по боссам.
- **Девенеон (Daevanion Boards)**:
  - Проверь общее число открытых узлов (`totalNodes`) и прогресс по доскам (31..36).
  - Если открыто менее 50 узлов — направь пользователя фармить осколки памяти Даэваниона, фокусируясь на узлах *Pure Attack*, *Weapon Damage Boost* и *Critical Damage Boost*.

---

### Блок 3: Остатки (Крылья, Петы, Пантеон, Титулы, Генус, Монолиты)
- **Крылья (Wings)**:
  - Должны быть обязательно экипированы. Топовые крылья дают прибавку к атаке, скорости и PvP/PvE урону.
- **Питомцы (Pets & Pet Boards)**:
  - Наличие призванного боевого питомца и его уровень (стремиться к каповому уровню 50). Прокачка питомца активирует пассивный резонанс команды.
- **Генус (Genus Branches)**:
  - Изучи ветки `Intellect`, `Feral`, `Nature`, `Trans`, `Special`.
  - **Выявляй мусорные статы**: если в атакующей ветке выпали защитные роллы (*Регенерация HP*, *Сопротивление магии*, *Защита*), направь игрока на **переролл слотов** в пользу *Attack*, *Damage Boost*, *Double Chance* или *Penetration*.
- **Пантеон (Pantheon Relics)**:
  - Проверь количество установленных реликвий Владык (1..15, минимум 4 активных слота).
  - Приоритет для урона: Реликвия Джикела (*Destruction*), Реликвия Байзела (*Authority*), Реликвия Неджакана, Реликвия Кайсинеля (*Illusion CDR*).
- **Титулы (Titles)**:
  - Убедись, что активирован боевой титул с максимальным бонусом к *Атаке* или *Криту*.
- **Монолиты (Monoliths)**:
  - Прогресс жетонов региональных монолитов (Альтгард, Морхейм, Келькмарос и др.), дающих постоянные пассивные статы.

---

### Блок 4: Математические приоритеты статов (Stat Weights)
- Всегда обращай внимание на таблицу `statWeights` из ответа инструмента.
- Показывай пользователю **предельную полезность (+% DPS)** каждого стата:
  - `Double Chance` — независимая мультипликативная корзина (дает наивысший прирост DPS на единицу рейтинга).
  - `Weapon Damage Boost (WDB)` — множитель базы Pure Attack перед всеми процентами.
  - `Might / Zikel` — множитель всей цепочки атаки (+0.1% за очко).
  - `Critical Damage Boost` — критический мультипликатор.
  - `Penetration` — срезает защиту босса до входа в процентные корзины.
- Опирайся на готовый список `bottlenecks` из ответа: там проблемы уже отсортированы по силе влияния на урон!

---

## 2. Crafting Profitability & Market Deals Workflow
When the user asks *"what is profitable to craft"* or *"what to buy/flip on the market"*:
1. Call `scan_profitable_crafts` with `profession` (supports `"jewelcrafting"`, `"blacksmithing"`, `"tailoring"`, `"alchemy"`, `"cooking"` and Russian aliases like `"ювелирка"`). Pass `multiStep: true` (or `sortBy: "multi_step"`) when evaluating multi-tier chains where intermediate sub-materials can be crafted cheaper than bought!
2. If the tool reports that the profession level is not yet configured in SQLite:
   - Ask the user once for their mastery grade (`Новичок`/`beginner` 1–50 or `Профи`/`intermediate` 1–50) and level.
   - Persist it immediately with `set_profession` (`Профи 26` normalizes to internal `76` automatically) and re-run `scan_profitable_crafts`.
3. **Economic interpretation rules (CRITICAL)**:
   - **`netProfitMarket`** = expected revenue after 12% AH tax minus the **full market value** of all required materials (accounting for level-based `successRate` and `comboProbability`).
   - **`multiStepNetProfit`** = `netProfitMarket + multiStepSavings` when crafting profitable intermediate materials (`subCraft.recommendation == "craft"`) yourself instead of buying them on AH. In `evaluate_recipe`, sub-craft comparison is always included automatically.
   - **`cashFlowProfit`** = expected revenue after tax minus **`outOfPocketCost`** (only materials missing from bag + storage, counting bound `(привяз.)` equivalents).
   - **Never recommend crafting an item with `netProfitMarket < 0` (and `multiStepNetProfit < 0`) just because `cashFlowProfit > 0`!** Warn that tradable raw materials are worth more sold directly on AH.
   - Always check **quote freshness & slippage**:
     - `quoteAgeMinutes` / `staleQuote: true` (>150 min): warn that rare gear quotes may be stale and pass `forceRefresh: true` if needed.
     - `spreadVsMedianPct` (>= ±35%): warn about order-book slippage.
     - `thinSupply: true`: `activeListings < missingToBuyQty`.
4. **Market Flips & Warehouse Peaks**:
   - Call `scan_market_deals` to spot liquid (`listings >= 15`) `dipBuys` trading >=20% below `median7d` (with `flipNetProfitPerOne` after 12% tax) and `peakSells` near `high30d` (prioritizing items with `ownedQty > 0` in the player's warehouse!).
   - Call `compare_server_prices` to compare item baskets across regional servers.

---

## 3. Full Local Build, Tickets & Live Session
- **Local Character Build**: Always prefer `get_my_character` over `inspect_global_player` for local alts — it works instantly from `characters.json` and exposes **equipped skills with localized tripod specializations** (`specializationDetails`), Daevanion board node counts, `abyssSeason` PvP stats, `genus` rolled stats, `monoliths`, `pantheon`, active wing & titles, and `tickets` with `timeFormatted` (`3h 14m`) and `nearCap: true` alerts! Pass `includeCollections: true` only when full lists of 190+ pets, unequipped skills, or raw node IDs are needed.
- **Bag & Warehouse**: Call `get_my_inventory` for live `.ndjson`-indexed `bagCount` + `storageCount`.
- **Farm & Live Session**: Use `analyze_farm_drops` for multi-day Kinah/hour per map, and `get_session_tracker` (`map-tracker.json`) for real-time mob kills/hour, gathered nodes, souls gained, and live session loot value.

---

## 4. Combat Analytics: Boss DPS, Dungeon Speedruns & PvP
- **Boss Rotations & DPS**: Call `get_boss_encounters` to list kills/wipes (`selfDps`, `partyDps`, `personalBest`), then pass `encounterId` to inspect full party contribution, localized skill shares, and `critRatePct` / `backRatePct` / `perfectRatePct` / `hardHitRatePct` / `multiHitRatePct`.
- **Personal Bests**: Call `get_personal_bests` to compare boss DPS records across alts.
- **Dungeon Runs**: Call `get_dungeon_runs` (and pass `runId` for boss split timestamps, party skills, looted items & consumables used).
- **PvP Skirmishes**: Call `get_pvp_logs` (and pass `pvpId` to see each enemy's class/GS/CP and the exact localized skills in `takenSkills` that burst you down vs your `dealtSkills`).

---

## 5. Comparing with External Global Players
- Call `inspect_global_player` and `inspect_global_player_slot` (always passing both `enchantLevel` and `exceedLevel` so breakthrough stats are returned) to inspect external players on EU/NAE/NAW/SA/Asia.
