AION 2 Global Tracker, Combat Analytics & Economy Engine (v0.2.0).
Combines three live sources without hardcoded item/stat/skill names:
1. Local Questlog Desktop telemetry (%APPDATA%\Questlog\aion-2 & combat-logs/*.ndjson) — full local character builds (equipped skills with localized tripod specializations, Daevanion boards, Abyss season PvP stats, Genus rolled options, Monoliths, Pantheon, wings, titles, pets), currencies, human-readable dungeon/timer tickets with nearCap alerts, bag + warehouse inventory, boss encounters (with full rotation & party breakdown), dungeon speedrun logs, PvP incoming/outgoing skill logs, personal bests, and live session tracker.
2. Questlog.gg localized SQLite dictionary + live server Auction House quotes — item definitions, vendor NPC prices, crafting recipes with exact per-level successRates & combo procs, recursive multi-step "craft vs buy" intermediate material evaluation, quote freshness (`quoteAgeMinutes`), slippage spread alerts, market dip/flip deals, and cross-server price comparisons.
3. Official NCSoft Global Web API (EU, NAE, NAW, SA, Asia) — inspect any external player's profile, gear rolls, breakthrough exceed stats, stones, and read official Global patch notes.
4. Deterministic DPS Pipeline & Build Audit (audit_character_dps) — multi-tier damage formula decomposition, marginal stat elasticity (+% DPS gains per stat), and targeted auditing of:
   - Gear: per-slot enchant thresholds (+12/+15), Exceed breakthrough status, manastone coverage and unique slot affixes.
   - Skills: Arcane & Stigma levels, active tripod specializations, and Daevanion node progress.
   - Misc: Wings, pet collection and resonance, Pantheon Lord relics (1..15), Genus offensive vs defensive rolled stats, and regional Monolith tokens.

Key rules for the agent:
- For your own local characters, always prefer `get_my_character` over `inspect_global_player`: it works offline/instantly and exposes skill specializations (tripods), Genus rolls, Abyss season stats, and `nearCap` ticket alerts. Pass `includeCollections: true` only when the user explicitly asks about all 190+ pets, unequipped skills, or raw Daevanion node IDs.
- If `scan_profitable_crafts` reports that the character's profession level is not saved yet, ask the user once for their mastery grade (`Новичок`/`beginner` 1–50 or `Профи`/`intermediate` 1–50) and level, then save it with `set_profession`.
- Distinguish `netProfitMarket` (flat single-step market profit), `multiStepNetProfit` (profit when crafting profitable sub-materials yourself via `multiStep: true`), and `cashFlowProfit` (Kinah net of out-of-pocket purchases using owned bag/storage materials). Warn if `netProfitMarket < 0` even when `cashFlowProfit > 0`.
- Always check `quoteAgeMinutes`, `staleQuote`, `spreadVsMedianPct`, `activeListings`, and `warnings` before recommending high-budget crafts or flips.
