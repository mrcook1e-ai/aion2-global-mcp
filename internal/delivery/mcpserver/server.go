package mcpserver

import (
	"context"
	_ "embed"

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
	Farm     *usecase.FarmService
	NCSoft   *ncsoft.Client
}

func New(d Deps) *mcp.Server {
	s := mcp.NewServer(
		&mcp.Implementation{
			Name:    "aion2-global",
			Title:   "AION 2 Global Tracker & Economy MCP",
			Version: "0.1.0",
		},
		&mcp.ServerOptions{Instructions: serverInstructions},
	)

	registerLocalTools(s, d)
	registerEconomyTools(s, d)
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

// --- 1. Local Character, Inventory, Tickets & Telemetry Tools ---

type emptyArgs struct{}

type charListOut struct {
	Characters []domain.Character `json:"characters"`
}

type charQueryArgs struct {
	Character string `json:"character,omitempty" jsonschema:"character name or serverId:Name; empty selects the currently active character"`
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
	FullCatalog bool `json:"fullCatalog,omitempty" jsonschema:"false (default) quickly syncs local .ndjson inventory logs; true also downloads/refreshes all items, recipes & entities from questlog.gg"`
}

func registerLocalTools(s *mcp.Server, d Deps) {
	mcp.AddTool(s,
		readOnlyTool("list_my_characters", "List my local characters",
			"Lists all local characters tracked by Questlog Desktop with serverId, class, combatPower, active flag, and saved crafting professions"),
		func(ctx context.Context, _ *mcp.CallToolRequest, _ emptyArgs) (*mcp.CallToolResult, charListOut, error) {
			chars, err := d.Char.ListCharacters(ctx)
			return nil, charListOut{Characters: chars}, err
		},
	)

	mcp.AddTool(s,
		readOnlyTool("get_my_character", "Get my character overview",
			"Returns full profile of a local character: resolved equipment & manastones, localized stats, currencies, dungeon/nightmare tickets, and inventory counts"),
		func(ctx context.Context, _ *mcp.CallToolRequest, args charQueryArgs) (*mcp.CallToolResult, *usecase.CharacterOverview, error) {
			out, err := d.Char.GetCharacterOverview(ctx, args.Character)
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
			"Incrementally indexes .ndjson inventory logs, or refreshes the full localized SQLite item/recipe/entity catalog from questlog.gg when fullCatalog=true"),
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

// --- 2. Economy, Crafting Profitability, Market & Farm Tools ---

type scanCraftsArgs struct {
	Character       string  `json:"character,omitempty" jsonschema:"character name or serverId:Name; empty uses active character"`
	ServerID        int     `json:"serverId,omitempty" jsonschema:"optional serverId override if character is not specified"`
	Profession      string  `json:"profession" jsonschema:"jewelcrafting (ювелирка), blacksmithing (кузнечка), tailoring (портняжка), alchemy (алхимия), cooking (кулинария)"`
	MasteryGrade    string  `json:"masteryGrade,omitempty" jsonschema:"optional tier label when overriding masteryLevel: beginner/новичок or intermediate/профи"`
	MasteryLevel    *int    `json:"masteryLevel,omitempty" jsonschema:"optional mastery level override; omit to use the level saved via set_profession"`
	MinMasteryLevel int     `json:"minMasteryLevel,omitempty" jsonschema:"optional minimum recipe mastery level to skip low-level crafts"`
	SubCategory     string  `json:"subCategory,omitempty" jsonschema:"optional subCategory filter, e.g. ring, necklace, earring, bow, staff"`
	SortBy          string  `json:"sortBy,omitempty" jsonschema:"net_profit (default, true market profit) or cash_flow (profit after using owned materials)"`
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
	Query     string `json:"query" jsonschema:"item name words or exact item ID"`
	Category  string `json:"category,omitempty" jsonschema:"optional category filter"`
	Grade     int    `json:"grade,omitempty" jsonschema:"optional numeric grade filter (11, 21, 31, 41, 51)"`
	Character string `json:"character,omitempty" jsonschema:"character to infer serverId from; empty uses active character"`
	ServerID  int    `json:"serverId,omitempty" jsonschema:"optional explicit serverId for market prices"`
	Limit     int    `json:"limit,omitempty" jsonschema:"max items, default 15"`
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

type farmArgs struct {
	Character string `json:"character,omitempty" jsonschema:"character to infer serverId from; empty uses active character"`
	ServerID  int    `json:"serverId,omitempty" jsonschema:"optional explicit serverId"`
	Days      int    `json:"days,omitempty" jsonschema:"number of recent drop log days to analyze (1..30, default 7)"`
}

func registerEconomyTools(s *mcp.Server, d Deps) {
	mcp.AddTool(s,
		readOnlyTool("scan_profitable_crafts", "Scan most profitable crafts for my profession & inventory",
			"Finds the best crafting recipes for the character's mastery level, cross-checks owned bag/storage materials (including bound equivalents), fetches live server market & NPC quotes, and calculates expected net profit"),
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
				SortBy:          args.SortBy,
				TaxRate:         args.TaxRate,
				ForceRefresh:    args.ForceRefresh,
				TopN:            args.TopN,
			})
			return nil, out, err
		},
	)

	mcp.AddTool(s,
		readOnlyTool("evaluate_recipe", "Evaluate single crafting recipe",
			"Detailed material & profit breakdown of one specific recipe against character inventory (including bound materials) and live market/NPC quotes"),
		func(ctx context.Context, _ *mcp.CallToolRequest, args evalRecipeArgs) (*mcp.CallToolResult, *domain.CraftEvaluation, error) {
			out, err := d.Craft.EvaluateSingleRecipe(ctx, domain.RecipeID(args.RecipeID), args.Query, args.Character, args.ServerID, args.MasteryGrade, args.MasteryLevel, args.ForceRefresh)
			return nil, out, err
		},
	)

	mcp.AddTool(s,
		readOnlyTool("search_catalog_and_market", "Search items, market prices & recipes",
			"Searches the localized SQLite item catalog and enriches matches with live server Auction House prices and crafting recipes"),
		func(ctx context.Context, _ *mcp.CallToolRequest, args searchMarketArgs) (*mcp.CallToolResult, searchMarketOut, error) {
			limit := args.Limit
			if limit <= 0 || limit > 40 {
				limit = 15
			}
			items, err := d.Catalog.SearchItems(ctx, args.Query, args.Category, args.Grade, limit)
			if err != nil {
				return nil, searchMarketOut{}, err
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
				quotes, _ = d.Resolver.EnsureMarketQuotes(ctx, srvID, ids, false)
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
		readOnlyTool("analyze_farm_drops", "Analyze farm loot value & Kinah/hour",
			"Values looted items from Questlog's drops/*.json against live server Auction House prices and calculates Kinah/hour per map/dungeon"),
		func(ctx context.Context, _ *mcp.CallToolRequest, args farmArgs) (*mcp.CallToolResult, *usecase.FarmReport, error) {
			out, err := d.Farm.AnalyzeDropsValue(ctx, args.Character, args.ServerID, args.Days)
			return nil, out, err
		},
	)
}

// --- 3. NCSoft Official Global Web API Tools (via nuriland/aion2-api) ---

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
