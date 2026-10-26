---
name: aion2-global
description: Domain & economic playbook for AION 2 Global character tracking, crafting profitability, auction house liquidity, dungeon tickets, and player comparison via the aion2-global MCP server. Use whenever the user asks about AION 2 Global crafts, market prices, gear, inventory, drops/Kinah per hour, or inspecting players.
---

# AION 2 Global — Agent Playbook

Use the `aion2-global` MCP tools to inspect real-time character state, inventory, crafting profitability, farm efficiency, and any Global player's gear.

## 1. Crafting Profitability Workflow (e.g. Jewelcrafting / Blacksmithing)
When the user asks *"what is profitable to craft right now for my profession"*:
1. Call `scan_profitable_crafts` directly with `profession` (supports `"jewelcrafting"`, `"blacksmithing"`, `"tailoring"`, `"alchemy"`, `"cooking"` and Russian names like `"ювелирка"`).
2. If the tool returns an error that the profession level is not yet configured in SQLite for this character:
   - Ask the user once for their current numeric mastery level (e.g. `1..100`).
   - Persist it immediately with `set_profession` and re-run `scan_profitable_crafts`.
3. **Economic interpretation rules (CRITICAL)**:
   - **`netProfitMarket`** = expected revenue after 12% AH tax minus the **full market value** of all required materials (accounting for level-based `successRate` and `comboProbability` procs).
   - **`cashFlowProfit`** = expected revenue after tax minus **`outOfPocketCost`** (only the materials missing from the character's bag + storage).
   - **Never recommend crafting an item with `netProfitMarket < 0` just because `cashFlowProfit > 0`!** Explain to the user that their owned materials are worth more sold raw on the Auction House than turned into that item (opportunity cost).
   - Always check `productListings`, `comboListings`, and `warnings`:
     - Many normal craft outputs in AION 2 are untradable (`productSellPrice == 0`), meaning profit relies solely on hitting the shiny `combo` proc (`comboProbability`). Highlight the variance/risk when `comboProbability` is 20–25% on expensive crafts!
     - Warn if ingredient `activeListings` are lower than `missingToBuyQty` (`thinSupply: true`).

## 2. Character, Caps & Farm Tracking
- **Account & Caps overview**: Call `get_my_character` (or `list_my_characters` first for alts) to inspect gear, manastones, currencies, and dungeon/nightmare `tickets` so capped tickets don't go to waste.
- **Bag & Warehouse lookup**: Call `get_my_inventory` — it aggregates both `bagCount` and `storageCount` indexed live from Questlog `.ndjson` logs without overwriting the bag when storage is opened.
- **Kinah/hour & Drop valuation**: Call `analyze_farm_drops` to value looted stretches against live server Auction House prices.

## 3. Comparing Builds with Global Players
- To compare the user's local character with another player on EU/NA:
  1. Call `get_my_character` for the local character.
  2. Call `inspect_global_player` (and `inspect_global_player_slot` for exact sub-stat rolls, magic stones, and god stones on key weapons/accessories).
  3. Compare stats, enchant/exceed levels, stone choices, and Daevanion progress concretely.
