---
name: aion2-global
description: Domain, combat analytics & economic playbook for AION 2 Global character builds, multi-step crafting profitability, auction house flips/dips, boss rotations, dungeon speedruns, PvP incoming/outgoing logs, and Global player comparison via the aion2 MCP server. Use whenever the user asks about AION 2 Global crafts, market deals, server prices, gear, skill tripods, Daevanion/Genus, DPS/rotations, PvP deaths, drops/Kinah per hour, or inspecting players.
---

# AION 2 Global — Agent Playbook (v0.2.0)

Use the `aion2` MCP tools to inspect real-time local character builds, combat/DPS/PvP telemetry, inventory, multi-step crafting economics, market deals, and any Global player's gear.

## 1. Crafting Profitability & Market Deals Workflow
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
     - `spreadVsMedianPct` (>= ±35%): warn about order-book slippage (a single cheap lot won't cover 14 needed materials, or a single dump lot understates product value).
     - `thinSupply: true`: `activeListings < missingToBuyQty`.
4. **Market Flips & Warehouse Peaks**:
   - Call `scan_market_deals` to spot liquid (`listings >= 15`) `dipBuys` trading >=20% below `median7d` (with `flipNetProfitPerOne` after 12% tax) and `peakSells` near `high30d` (prioritizing items with `ownedQty > 0` in the player's warehouse!).
   - Call `compare_server_prices` to compare item baskets across regional servers.

## 2. Full Local Build, Tickets & Live Session
- **Local Character Build**: Always prefer `get_my_character` over `inspect_global_player` for local alts — it works instantly from `characters.json` and exposes **equipped skills with localized tripod specializations** (`specializationDetails`), Daevanion board node counts, `abyssSeason` PvP stats, `genus` rolled stats, `monoliths`, `pantheon`, active wing & titles, and `tickets` with `timeFormatted` (`3h 14m`) and `nearCap: true` alerts! Pass `includeCollections: true` only when full lists of 190+ pets, unequipped skills, or raw node IDs are needed.
- **Bag & Warehouse**: Call `get_my_inventory` for live `.ndjson`-indexed `bagCount` + `storageCount`.
- **Farm & Live Session**: Use `analyze_farm_drops` for multi-day Kinah/hour per map, and `get_session_tracker` (`map-tracker.json`) for real-time mob kills/hour, gathered nodes, souls gained, and live session loot value.

## 3. Combat Analytics: Boss DPS, Dungeon Speedruns & PvP
- **Boss Rotations & DPS**: Call `get_boss_encounters` to list kills/wipes (`selfDps`, `partyDps`, `personalBest`), then pass `encounterId` to inspect full party contribution, localized skill shares, and `critRatePct` / `backRatePct` / `perfectRatePct` / `hardHitRatePct` / `multiHitRatePct`.
- **Personal Bests**: Call `get_personal_bests` to compare boss DPS records across alts.
- **Dungeon Runs**: Call `get_dungeon_runs` (and pass `runId` for boss split timestamps, party skills, looted items & consumables used).
- **PvP Skirmishes**: Call `get_pvp_logs` (and pass `pvpId` to see each enemy's class/GS/CP and the exact localized skills in `takenSkills` that burst you down vs your `dealtSkills`).

## 4. Comparing with External Global Players
- Call `inspect_global_player` and `inspect_global_player_slot` (always passing both `enchantLevel` and `exceedLevel` so breakthrough stats are returned) to inspect external players on EU/NAE/NAW/SA/Asia.
