package mcpserver

import (
	"context"
	_ "embed"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	aion2 "github.com/nuriland/aion2-api"

	"github.com/mrcook1e/aion2-global-mcp/internal/adapter/ncsoft"
	"github.com/mrcook1e/aion2-global-mcp/internal/domain"
	"github.com/mrcook1e/aion2-global-mcp/internal/usecase"
)

//go:embed instructions.md
var serverInstructions string

type Deps struct {
	Catalog  domain.CatalogRepository
	Resolver *usecase.Resolver
	Sync     *usecase.SyncService
	Char     *usecase.CharacterService
	Craft    *usecase.CraftService
	Market   *usecase.MarketScannerService
	Farm     *usecase.FarmService
	Combat   *usecase.CombatService
	NCSoft   *ncsoft.Client
}

func New(d Deps) *mcp.Server {
	s := mcp.NewServer(
		&mcp.Implementation{
			Name:    "aion2-global",
			Title:   "AION 2 Global Tracker, Combat & Economy MCP",
			Version: "0.2.0",
		},
		&mcp.ServerOptions{Instructions: serverInstructions},
	)

	registerLocalTools(s, d)
	registerEconomyTools(s, d)
	registerCombatTools(s, d)
	registerGlobalArmoryTools(s, d)
	return s
}

func readOnlyTool(name, title, desc string) *mcp.Tool {
	openWorld := true
	return &mcp.Tool{
		Name:        name,
		Description: desc,
		Annotations: &mcp.ToolAnnotations{
			Title:          title,
			ReadOnlyHint:   true,
			IdempotentHint: true,
			OpenWorldHint:  &openWorld,
		},
	}
}

func writeTool(name, title, desc string) *mcp.Tool {
	return &mcp.Tool{
		Name:        name,
		Description: desc,
		Annotations: &mcp.ToolAnnotations{
			Title:          title,
			ReadOnlyHint:   false,
			IdempotentHint: true,
		},
	}
}

// --- 1. Local Character, Build, Inventory, Tickets & Telemetry Tools ---

type emptyArgs struct{}

type charListOut struct {
	Characters []domain.Character `json:"characters"`
}

type charQueryArgs struct {
	Character          string `json:"character,omitempty" jsonschema:"character name or serverId:Name; empty selects the currently active character"`
	IncludeCollections bool   `json:"includeCollections,omitempty" jsonschema:"false (default) returns compact combat build (equipped skills with tripods, active wing/titles, Daevanion counts); true also includes all unequipped skills, all pets, and raw Daevanion node IDs"`
}

type invQueryArgs struct {
	Character string `json:"character,omitempty" jsonschema:"character name or serverId:Name; empty selects active character"`
	Query     string `json:"query,omitempty" jsonschema:"optional substring filter on localized item name"`
	Category  string `json:"category,omitempty" jsonschema:"optional mainCategory filter, e.g. misc, weapon, armor"`
	Limit     int    `json:"limit,omitempty" jsonschema:"max rows to return, default 100"`
}

type invOut struct {
	CharacterKey string                  `json:"characterKey"`
	TotalMatched int                     `json:"totalMatched"`
	Items        []domain.AggregatedItem `json:"items"`
}

type setProfArgs struct {
	Character    string `json:"character,omitempty" jsonschema:"character name or serverId:Name; empty selects active character"`
	Profession   string `json:"profession" jsonschema:"jewelcrafting, blacksmithing, tailoring, alchemy, or cooking (RU aliases supported)"`
	MasteryLevel int    `json:"masteryLevel" jsonschema:"in-game tier level (1..50 when masteryGrade is passed) or continuous internal level (1..100)"`
	MasteryGrade string `json:"masteryGrade,omitempty" jsonschema:"beginner (новичок, 1..50) or intermediate (профи, 1..50 -> normalized to 51..100)"`
}

type syncArgs struct {
	FullCatalog bool `json:"fullCatalog,omitempty" jsonschema:"false (default) quickly syncs local .ndjson inventory logs; true also downloads/refreshes all items, recipes, skills, wings, pets & entities from questlog.gg"`
}

func registerLocalTools(s *mcp.Server, d Deps) {
	mcp.AddTool(s,
		readOnlyTool("list_my_characters", "List my local characters",
			"Lists all local characters tracked by Questlog Desktop with serverId, class, combatPower, Abyss season summary, Daevanion node total, active flag, and saved crafting professions"),
		func(ctx context.Context, _ *mcp.CallToolRequest, _ emptyArgs) (*mcp.CallToolResult, charListOut, error) {
			chars, err := d.Char.ListCharacters(ctx)
			return nil, charListOut{Characters: chars}, err
		},
	)

	mcp.AddTool(s,
		readOnlyTool("get_my_character", "Get my character full build & overview",
			"Returns complete local profile: gear (+enchant/exceed & manastones), equipped skills with localized tripod specializations, Daevanion boards, Abyss season PvP stats, Genus rolled options, Monoliths, Pantheon, wing, titles, currencies, dungeon/timer tickets with nearCap alerts, and inventory counts"),
		func(ctx context.Context, _ *mcp.CallToolRequest, args charQueryArgs) (*mcp.CallToolResult, *usecase.CharacterOverview, error) {
			out, err := d.Char.GetCharacterOverview(ctx, args.Character, args.IncludeCollections)
			return nil, out, err
		},
	)

	mcp.AddTool(s,
		readOnlyTool("get_my_inventory", "Get my bag & storage inventory",
			"Returns live aggregated bag + warehouse item quantities indexed from Questlog .ndjson session logs"),
		func(ctx context.Context, _ *mcp.CallToolRequest, args invQueryArgs) (*mcp.CallToolResult, invOut, error) {
			key, items, err := d.Char.GetInventory(ctx, args.Character, args.Query, args.Category)
			if err != nil {
				return nil, invOut{}, err
			}
			total := len(items)
			limit := args.Limit
			if limit <= 0 || limit > 300 {
				limit = 100
			}
			if len(items) > limit {
				items = items[:limit]
			}
			return nil, invOut{CharacterKey: key, TotalMatched: total, Items: items}, nil
		},
	)

	mcp.AddTool(s,
		writeTool("set_profession", "Save character crafting mastery level",
			"Persists a character's crafting profession level (jewelcrafting, blacksmithing, tailoring, alchemy, cooking) in SQLite for future profit scans"),
		func(ctx context.Context, _ *mcp.CallToolRequest, args setProfArgs) (*mcp.CallToolResult, *domain.ProfessionProgress, error) {
			out, err := d.Char.SetProfession(ctx, args.Character, args.Profession, args.MasteryLevel, args.MasteryGrade)
			return nil, out, err
		},
	)

	mcp.AddTool(s,
		writeTool("sync_catalog", "Sync telemetry or full Questlog catalog",
			"Incrementally indexes .ndjson inventory logs, or refreshes the full localized SQLite item/recipe/skill/entity catalog from questlog.gg when fullCatalog=true"),
		func(ctx context.Context, _ *mcp.CallToolRequest, args syncArgs) (*mcp.CallToolResult, usecase.SyncSummary, error) {
			if args.FullCatalog {
				out, err := d.Sync.SyncFullCatalog(ctx)
				return nil, out, err
			}
			out, err := d.Sync.SyncTelemetry(ctx)
			return nil, out, err
		},
	)
}

// --- 2. Economy, Multi-Step Crafting, Market Deals & Cross-Server Tools ---

type scanCraftsArgs struct {
	Character       string  `json:"character,omitempty" jsonschema:"character name or serverId:Name; empty uses active character"`
	ServerID        int     `json:"serverId,omitempty" jsonschema:"optional serverId override if character is not specified"`
	Profession      string  `json:"profession" jsonschema:"jewelcrafting (ювелирка), blacksmithing (кузнечка), tailoring (портняжка), alchemy (алхимия), cooking (кулинария)"`
	MasteryGrade    string  `json:"masteryGrade,omitempty" jsonschema:"optional tier label when overriding masteryLevel: beginner/новичок or intermediate/профи"`
	MasteryLevel    *int    `json:"masteryLevel,omitempty" jsonschema:"optional mastery level override; omit to use the level saved via set_profession"`
	MinMasteryLevel int     `json:"minMasteryLevel,omitempty" jsonschema:"optional minimum recipe mastery level to skip low-level crafts"`
	SubCategory     string  `json:"subCategory,omitempty" jsonschema:"optional subCategory filter, e.g. ring, necklace, earring, bow, staff"`
	MultiStep       bool    `json:"multiStep,omitempty" jsonschema:"true recursively evaluates intermediate craftable materials (craft vs buy) and computes multiStepNetProfit"`
	SortBy          string  `json:"sortBy,omitempty" jsonschema:"net_profit (default), multi_step (profit with self-crafted intermediates), or cash_flow (profit using owned materials)"`
	TaxRate         float64 `json:"taxRate,omitempty" jsonschema:"auction house fee+tax fraction, default 0.12 (12%)"`
	ForceRefresh    bool    `json:"forceRefresh,omitempty" jsonschema:"true forces live market refresh ignoring 25m cache"`
	TopN            int     `json:"topN,omitempty" jsonschema:"number of top recipes to return, default 15"`
}

type evalRecipeArgs struct {
	RecipeID     int64  `json:"recipeId,omitempty" jsonschema:"exact recipe ID if known"`
	Query        string `json:"query,omitempty" jsonschema:"recipe or product item name when recipeId is omitted"`
	Character    string `json:"character,omitempty" jsonschema:"character name or serverId:Name; empty uses active character"`
	ServerID     int    `json:"serverId,omitempty" jsonschema:"optional serverId override"`
	MasteryGrade string `json:"masteryGrade,omitempty" jsonschema:"optional tier label (beginner/новичок, intermediate/профи) when overriding masteryLevel"`
	MasteryLevel *int   `json:"masteryLevel,omitempty" jsonschema:"optional mastery level override"`
	ForceRefresh bool   `json:"forceRefresh,omitempty" jsonschema:"true forces live market refresh"`
}

type searchMarketArgs struct {
	Query        string `json:"query" jsonschema:"item name words or exact numeric item ID"`
	Category     string `json:"category,omitempty" jsonschema:"optional category filter"`
	Grade        int    `json:"grade,omitempty" jsonschema:"optional numeric grade filter (11, 21, 31, 41, 51)"`
	Character    string `json:"character,omitempty" jsonschema:"character to infer serverId from; empty uses active character"`
	ServerID     int    `json:"serverId,omitempty" jsonschema:"optional explicit serverId for market prices"`
	ForceRefresh bool   `json:"forceRefresh,omitempty" jsonschema:"true forces live market refresh ignoring 25m cache"`
	Limit        int    `json:"limit,omitempty" jsonschema:"max items, default 15"`
}

type marketItemRow struct {
	Item    domain.Item         `json:"item"`
	Market  *domain.MarketQuote `json:"market,omitempty"`
	Recipes []domain.Recipe     `json:"recipes,omitempty"`
}

type searchMarketOut struct {
	ServerID domain.ServerID `json:"serverId,omitempty"`
	Results  []marketItemRow `json:"results"`
}

type scanDealsArgs struct {
	Character      string  `json:"character,omitempty" jsonschema:"character to infer serverId and owned warehouse stock; empty uses active character"`
	ServerID       int     `json:"serverId,omitempty" jsonschema:"optional explicit serverId"`
	Category       string  `json:"category,omitempty" jsonschema:"optional mainCategory or subCategory filter (e.g. craftresource, misc, magicstone)"`
	MinListings    int     `json:"minListings,omitempty" jsonschema:"minimum active listings to filter out illiquid traps, default 15"`
	MinDiscountPct float64 `json:"minDiscountPct,omitempty" jsonschema:"minimum discount % below 7d median for dip_buy signals, default 20"`
	RefreshSample  bool    `json:"refreshSample,omitempty" jsonschema:"true refreshes live quotes for owned tradable items + category sample before scanning"`
	Limit          int     `json:"limit,omitempty" jsonschema:"max deals per bucket (dipBuys & peakSells), default 20"`
}

type compareServersArgs struct {
	Query           string  `json:"query,omitempty" jsonschema:"item name search query"`
	ItemIDs         []int64 `json:"itemIds,omitempty" jsonschema:"optional explicit item IDs to compare"`
	Character       string  `json:"character,omitempty" jsonschema:"character to infer homeServerId; empty uses active character"`
	HomeServerID    int     `json:"homeServerId,omitempty" jsonschema:"optional explicit home serverId (e.g. 2319)"`
	TargetServerIDs []int   `json:"targetServerIds,omitempty" jsonschema:"optional peer serverIds to compare against; omit to compare across all active servers in the same region"`
	ForceRefresh    bool    `json:"forceRefresh,omitempty" jsonschema:"true refreshes live multi-server quotes before comparing"`
	Limit           int     `json:"limit,omitempty" jsonschema:"max items to compare, default 10"`
}

type farmArgs struct {
	Character string `json:"character,omitempty" jsonschema:"character to infer serverId from; empty uses active character"`
	ServerID  int    `json:"serverId,omitempty" jsonschema:"optional explicit serverId"`
	Days      int    `json:"days,omitempty" jsonschema:"number of recent drop log days to analyze (1..30, default 7)"`
}

func registerEconomyTools(s *mcp.Server, d Deps) {
	mcp.AddTool(s,
		readOnlyTool("scan_profitable_crafts", "Scan most profitable crafts for my profession & inventory",
			"Finds the best crafting recipes for the character's mastery level, cross-checks owned bag/storage materials (including bound equivalents), checks quote age & slippage risk, optionally evaluates multi-step intermediate sub-crafts (multiStep=true), and calculates expected net profit"),
		func(ctx context.Context, _ *mcp.CallToolRequest, args scanCraftsArgs) (*mcp.CallToolResult, *usecase.ProfitableCraftsReport, error) {
			out, err := d.Craft.ScanProfitableCrafts(ctx, usecase.ProfitableCraftsQuery{
				Character:       args.Character,
				ServerID:        args.ServerID,
				Profession:      args.Profession,
				MasteryGrade:    args.MasteryGrade,
				MasteryLevel:    args.MasteryLevel,
				MinMasteryLevel: args.MinMasteryLevel,
				SubCategory:     args.SubCategory,
				UseInventory:    true,
				MultiStep:       args.MultiStep,
				SortBy:          args.SortBy,
				TaxRate:         args.TaxRate,
				ForceRefresh:    args.ForceRefresh,
				TopN:            args.TopN,
			})
			return nil, out, err
		},
	)

	mcp.AddTool(s,
		readOnlyTool("evaluate_recipe", "Evaluate single crafting recipe (with recursive craft vs buy)",
			"Detailed material, quote freshness, slippage & profit breakdown of one specific recipe against character inventory (including bound materials), vendor NPC prices, and recursive 'craft vs buy' comparison for intermediate sub-materials"),
		func(ctx context.Context, _ *mcp.CallToolRequest, args evalRecipeArgs) (*mcp.CallToolResult, *domain.CraftEvaluation, error) {
			out, err := d.Craft.EvaluateSingleRecipe(ctx, domain.RecipeID(args.RecipeID), args.Query, args.Character, args.ServerID, args.MasteryGrade, args.MasteryLevel, args.ForceRefresh)
			return nil, out, err
		},
	)

	mcp.AddTool(s,
		readOnlyTool("search_catalog_and_market", "Search items, market prices & recipes",
			"Searches the localized SQLite item catalog (by name or exact numeric ID) and enriches matches with server Auction House quotes (including quoteAgeMinutes) and crafting recipes"),
		func(ctx context.Context, _ *mcp.CallToolRequest, args searchMarketArgs) (*mcp.CallToolResult, searchMarketOut, error) {
			limit := args.Limit
			if limit <= 0 || limit > 40 {
				limit = 15
			}
			var items []domain.Item
			qTrim := strings.TrimSpace(args.Query)
			if exactID, err := strconv.ParseInt(qTrim, 10, 64); err == nil && exactID > 0 {
				resMap, _ := d.Resolver.ResolveItems(ctx, []domain.ItemID{domain.ItemID(exactID)})
				if it, ok := resMap[domain.ItemID(exactID)]; ok {
					items = append(items, it)
				}
			}
			if len(items) == 0 {
				var err error
				items, err = d.Catalog.SearchItems(ctx, args.Query, args.Category, args.Grade, limit)
				if err != nil {
					return nil, searchMarketOut{}, err
				}
			}

			srvID := domain.ServerID(args.ServerID)
			if srvID <= 0 {
				if ch, err := d.Char.FindCharacter(ctx, args.Character); err == nil && ch != nil {
					srvID = ch.ServerID
				}
			}

			var ids []domain.ItemID
			for _, it := range items {
				ids = append(ids, it.ID)
			}
			var quotes map[domain.ItemID]*domain.MarketQuote
			if srvID > 0 && len(ids) > 0 {
				quotes, _ = d.Resolver.EnsureMarketQuotes(ctx, srvID, ids, args.ForceRefresh)
			}

			rows := make([]marketItemRow, 0, len(items))
			for _, it := range items {
				recs, _ := d.Catalog.GetRecipesForItem(ctx, it.ID)
				rows = append(rows, marketItemRow{
					Item:    it,
					Market:  quotes[it.ID],
					Recipes: recs,
				})
			}
			return nil, searchMarketOut{ServerID: srvID, Results: rows}, nil
		},
	)

	mcp.AddTool(s,
		readOnlyTool("scan_market_deals", "Scan Auction House dips (flips) & price peaks",
			"Finds liquid Auction House items trading significantly below their 7-day median (buy/flip opportunities net of 12% tax) as well as items near 30-day highs (signals to sell owned warehouse stock raw)"),
		func(ctx context.Context, _ *mcp.CallToolRequest, args scanDealsArgs) (*mcp.CallToolResult, *usecase.MarketDealsReport, error) {
			out, err := d.Market.ScanMarketDeals(ctx, usecase.MarketDealsQuery{
				Character:      args.Character,
				ServerID:       args.ServerID,
				Category:       args.Category,
				MinListings:    args.MinListings,
				MinDiscountPct: args.MinDiscountPct,
				RefreshSample:  args.RefreshSample,
				Limit:          args.Limit,
			})
			return nil, out, err
		},
	)

	mcp.AddTool(s,
		readOnlyTool("compare_server_prices", "Compare item market prices across servers",
			"Compares an item's or basket's Auction House prices on your home server against peer regional servers cached in SQLite (regional min, max, median, and % spread)"),
		func(ctx context.Context, _ *mcp.CallToolRequest, args compareServersArgs) (*mcp.CallToolResult, *usecase.CompareServersReport, error) {
			out, err := d.Market.CompareServerPrices(ctx, usecase.CompareServersQuery{
				Query:           args.Query,
				ItemIDs:         args.ItemIDs,
				Character:       args.Character,
				HomeServerID:    args.HomeServerID,
				TargetServerIDs: args.TargetServerIDs,
				ForceRefresh:    args.ForceRefresh,
				Limit:           args.Limit,
			})
			return nil, out, err
		},
	)

	mcp.AddTool(s,
		readOnlyTool("analyze_farm_drops", "Analyze farm loot value & Kinah/hour",
			"Values looted items from Questlog's drops/*.json against live server Auction House prices and calculates Kinah/hour per map/dungeon"),
		func(ctx context.Context, _ *mcp.CallToolRequest, args farmArgs) (*mcp.CallToolResult, *usecase.FarmReport, error) {
			out, err := d.Farm.AnalyzeDropsValue(ctx, args.Character, args.ServerID, args.Days)
			return nil, out, err
		},
	)
}

// --- 3. Combat Analytics, Dungeon Runs, PvP Logs, Personal Bests & Session Tracker ---

type bossEncountersArgs struct {
	EncounterID       string `json:"encounterId,omitempty" jsonschema:"optional exact encounter UUID for full skill rotation, crit/back/perfect/multiHit & party breakdown; omit to list encounters"`
	Character         string `json:"character,omitempty" jsonschema:"optional character name filter"`
	BossQuery         string `json:"bossQuery,omitempty" jsonschema:"optional boss name substring filter"`
	NpcID             int64  `json:"npcId,omitempty" jsonschema:"optional exact boss NPC ID filter"`
	OnlyKills         bool   `json:"onlyKills,omitempty" jsonschema:"true filters out wipes/resets"`
	OnlyPersonalBests bool   `json:"onlyPersonalBests,omitempty" jsonschema:"true shows only personal-best encounters"`
	Limit             int    `json:"limit,omitempty" jsonschema:"max encounters to list, default 20"`
}

type dungeonRunsArgs struct {
	RunID        string `json:"runId,omitempty" jsonschema:"optional exact run UUID for full boss splits, party DPS, looted items & consumables used; omit to list runs"`
	Character    string `json:"character,omitempty" jsonschema:"optional character name filter"`
	DungeonQuery string `json:"dungeonQuery,omitempty" jsonschema:"optional dungeon or boss name substring filter"`
	DungeonID    int64  `json:"dungeonId,omitempty" jsonschema:"optional exact dungeon ID filter"`
	Limit        int    `json:"limit,omitempty" jsonschema:"max runs to list, default 20"`
}

type pvpLogsArgs struct {
	PvPID         string `json:"pvpId,omitempty" jsonschema:"optional exact PvP encounter UUID for detailed opponent stats, outgoing skills & exact incoming enemy skills that hit you; omit to list skirmishes"`
	Character     string `json:"character,omitempty" jsonschema:"optional character name filter"`
	OpponentQuery string `json:"opponentQuery,omitempty" jsonschema:"optional enemy player name substring filter"`
	Limit         int    `json:"limit,omitempty" jsonschema:"max PvP skirmishes to list, default 20"`
}

type personalBestsArgs struct {
	Character string `json:"character,omitempty" jsonschema:"optional character name or serverId:Name filter; empty lists across all characters"`
	BossQuery string `json:"bossQuery,omitempty" jsonschema:"optional boss name substring filter"`
	Limit     int    `json:"limit,omitempty" jsonschema:"max records to return, default 25"`
}

func registerCombatTools(s *mcp.Server, d Deps) {
	mcp.AddTool(s,
		readOnlyTool("get_boss_encounters", "List boss encounters or inspect full rotation",
			"Reads local Questlog boss combat logs: lists recent boss kills/wipes with selfDps & partyDps, or inspects one encounterId for full party & skill rotation breakdown (localized skill names, crit/back/perfect/hardHit/multiHit rates)"),
		func(ctx context.Context, _ *mcp.CallToolRequest, args bossEncountersArgs) (*mcp.CallToolResult, *usecase.BossEncountersOut, error) {
			out, err := d.Combat.GetBossEncounters(ctx, usecase.BossEncounterQuery{
				EncounterID:       args.EncounterID,
				Character:         args.Character,
				BossQuery:         args.BossQuery,
				NpcID:             args.NpcID,
				OnlyKills:         args.OnlyKills,
				OnlyPersonalBests: args.OnlyPersonalBests,
				Limit:             args.Limit,
			})
			return nil, out, err
		},
	)

	mcp.AddTool(s,
		readOnlyTool("get_dungeon_runs", "List dungeon speedruns or inspect run details",
			"Reads local Questlog dungeon run history (clear time, timer remaining, selfRunDps, party size), or inspects one runId for boss kill splits, party member skills, looted items & consumables used"),
		func(ctx context.Context, _ *mcp.CallToolRequest, args dungeonRunsArgs) (*mcp.CallToolResult, *usecase.DungeonRunsOut, error) {
			out, err := d.Combat.GetDungeonRuns(ctx, usecase.DungeonRunsQuery{
				RunID:        args.RunID,
				Character:    args.Character,
				DungeonQuery: args.DungeonQuery,
				DungeonID:    args.DungeonID,
				Limit:        args.Limit,
			})
			return nil, out, err
		},
	)

	mcp.AddTool(s,
		readOnlyTool("get_pvp_logs", "List PvP skirmishes or inspect incoming/outgoing skills",
			"Reads local Questlog PvP logs (K/D, damage dealt & taken, opponent names), or inspects one pvpId to see each opponent's gearScore/combatPower/class and the exact localized skills dealt and taken"),
		func(ctx context.Context, _ *mcp.CallToolRequest, args pvpLogsArgs) (*mcp.CallToolResult, *usecase.PvPLogsOut, error) {
			out, err := d.Combat.GetPvPLogs(ctx, usecase.PvPLogsQuery{
				PvPID:         args.PvPID,
				Character:     args.Character,
				OpponentQuery: args.OpponentQuery,
				Limit:         args.Limit,
			})
			return nil, out, err
		},
	)

	mcp.AddTool(s,
		readOnlyTool("get_personal_bests", "Get personal best boss DPS records",
			"Returns personal-best boss DPS & kill duration records per character from Questlog's personal-bests.json"),
		func(ctx context.Context, _ *mcp.CallToolRequest, args personalBestsArgs) (*mcp.CallToolResult, *usecase.PersonalBestsOut, error) {
			out, err := d.Combat.GetPersonalBests(ctx, args.Character, args.BossQuery, args.Limit)
			return nil, out, err
		},
	)

	mcp.AddTool(s,
		readOnlyTool("get_session_tracker", "Get live farming/gathering session tracker",
			"Reads Questlog's live map-tracker.json session summary: total mob kills, kills/hour, top killed NPCs, gathered nodes, souls gained, and estimated Auction House net value of looted items"),
		func(ctx context.Context, _ *mcp.CallToolRequest, _ emptyArgs) (*mcp.CallToolResult, *usecase.SessionTrackerOut, error) {
			out, err := d.Combat.GetSessionTracker(ctx)
			return nil, out, err
		},
	)
}

// --- 4. NCSoft Official Global Web API Tools (via nuriland/aion2-api) ---

type inspectRemoteArgs struct {
	Region string `json:"region,omitempty" jsonschema:"Global shard: eu (default), nae, naw, sa, asia"`
	Locale string `json:"locale,omitempty" jsonschema:"en (default), de, fr, es, ja, pt-BR"`
	Name   string `json:"name" jsonschema:"exact character name to inspect"`
	Server string `json:"server,omitempty" jsonschema:"optional server name, short name, or serverId"`
}

type inspectRemoteSlotArgs struct {
	Region       string `json:"region,omitempty" jsonschema:"Global shard: eu (default), nae, naw, sa, asia"`
	Locale       string `json:"locale,omitempty" jsonschema:"en (default), de, fr, es, ja, pt-BR"`
	ServerID     int    `json:"serverId" jsonschema:"character's serverId from inspect_global_player"`
	CharacterID  string `json:"characterId" jsonschema:"character's characterId from inspect_global_player"`
	ItemID       int    `json:"itemId" jsonschema:"slot's item id"`
	SlotPos      int    `json:"slotPos" jsonschema:"slot's slotPos"`
	EnchantLevel int    `json:"enchantLevel,omitempty" jsonschema:"slot's enchantLevel (0..15) from inspect_global_player"`
	ExceedLevel  int    `json:"exceedLevel,omitempty" jsonschema:"slot's exceedLevel breakthrough (0..5) from inspect_global_player so breakthrough stats are included"`
}

type patchNotesArgs struct {
	Region string `json:"region,omitempty" jsonschema:"eu (default), nae, naw, sa, asia"`
	Locale string `json:"locale,omitempty" jsonschema:"en (default), de, fr, es, ja, pt-BR"`
	PostID string `json:"postId,omitempty" jsonschema:"optional post ID to fetch full HTML body; omit to list latest patch notes"`
	Limit  int    `json:"limit,omitempty" jsonschema:"number of latest patch notes to list, default 5"`
}

type patchNotesOut struct {
	Post  *aion2.Post  `json:"post,omitempty"`
	Posts []aion2.Post `json:"posts,omitempty"`
}

func registerGlobalArmoryTools(s *mcp.Server, d Deps) {
	mcp.AddTool(s,
		readOnlyTool("inspect_global_player", "Inspect any player on AION 2 Global",
			"Looks up any player on AION 2 Global (EU/NAE/NAW/SA/Asia) via NCSoft's official web API and returns their profile, stats, rankings, Daevanion summary, and worn equipment"),
		func(ctx context.Context, _ *mcp.CallToolRequest, args inspectRemoteArgs) (*mcp.CallToolResult, *ncsoft.RemotePlayerInspection, error) {
			out, err := d.NCSoft.FindAndInspectPlayer(ctx, args.Region, args.Locale, args.Name, args.Server)
			return nil, out, err
		},
	)

	mcp.AddTool(s,
		readOnlyTool("inspect_global_player_slot", "Inspect worn item details of a Global player",
			"Returns full rolls, breakthrough exceed stats, sub-stats, magic stones, and god stones on one worn equipment piece of any Global player"),
		func(ctx context.Context, _ *mcp.CallToolRequest, args inspectRemoteSlotArgs) (*mcp.CallToolResult, *aion2.EquippedItem, error) {
			out, err := d.NCSoft.GetEquippedSlotDetail(ctx, args.Region, args.Locale, args.ServerID, args.CharacterID, args.ItemID, args.SlotPos, args.EnchantLevel, args.ExceedLevel)
			return nil, out, err
		},
	)

	mcp.AddTool(s,
		readOnlyTool("get_global_patch_notes", "Read official AION 2 Global patch notes",
			"Lists the latest official AION 2 Global patch notes or fetches the full HTML body of one postId"),
		func(ctx context.Context, _ *mcp.CallToolRequest, args patchNotesArgs) (*mcp.CallToolResult, patchNotesOut, error) {
			if args.PostID != "" {
				post, err := d.NCSoft.GetPostBody(ctx, args.Region, args.Locale, "update", args.PostID)
				return nil, patchNotesOut{Post: post}, err
			}
			posts, err := d.NCSoft.LatestPatchNotes(ctx, args.Region, args.Locale, args.Limit)
			return nil, patchNotesOut{Posts: posts}, err
		},
	)
}
