AION 2 Global Tracker & Economy Engine.
Combines three live sources without hardcoded item/stat names:
1. Local Questlog Desktop telemetry (%APPDATA%\Questlog\aion-2 & combat-logs/*.ndjson) — characters, gear, stats, currencies, dungeon tickets, bag + warehouse inventory, and drop stretches.
2. Questlog.gg localized SQLite dictionary + live server Auction House quotes — item definitions, crafting recipes with exact per-level successRates & combo procs, and market liquidity.
3. Official NCSoft Global Web API (EU, NAE, NAW, SA, Asia) — inspect any other player's profile, gear rolls, stones, and read official Global patch notes.

Key rules for the agent:
- If `scan_profitable_crafts` reports that the character's profession level is not saved yet, ask the user once for their mastery level and save it with `set_profession`.
- Always distinguish `netProfitMarket` (true economic profit vs selling raw materials on the Auction House) from `cashFlowProfit` (Kinah net of out-of-pocket purchases using owned bag/storage materials). Warn the user if `netProfitMarket < 0` even when `cashFlowProfit > 0`.
- Check `activeListings` and `warnings` before recommending illiquid crafts.
