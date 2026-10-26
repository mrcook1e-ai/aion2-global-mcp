package domain

import "context"

// CatalogRepository manages SQLite-cached items, recipes, and localized dictionary entities.
type CatalogRepository interface {
	GetItem(ctx context.Context, id ItemID) (*Item, error)
	GetItemsByIDs(ctx context.Context, ids []ItemID) (map[ItemID]Item, error)
	SearchItems(ctx context.Context, query, category string, grade, limit int) ([]Item, error)
	UpsertItems(ctx context.Context, items []Item) error

	GetRecipe(ctx context.Context, id RecipeID) (*Recipe, error)
	SearchRecipes(ctx context.Context, filter RecipeFilter) ([]Recipe, error)
	GetRecipesForItem(ctx context.Context, itemID ItemID) ([]Recipe, error)
	UpsertRecipes(ctx context.Context, recipes []Recipe) error

	GetEntity(ctx context.Context, kind EntityKind, id int64, locale string) (*EntityEntry, error)
	GetEntitiesByIDs(ctx context.Context, kind EntityKind, ids []int64, locale string) (map[int64]EntityEntry, error)
	GetEntitiesByKind(ctx context.Context, kind EntityKind, locale string) (map[int64]EntityEntry, error)
	UpsertEntities(ctx context.Context, entries []EntityEntry) error
}

// RecipeFilter constrains recipe queries in SQLite.
type RecipeFilter struct {
	Query             string
	Profession        Profession
	SubCategory       string
	QualificationRace string // "", "light", "dark"
	MaxMasteryLevel   *int   // nil = no level cap
	MinMasteryLevel   int
	ExcludeGuildCraft bool
	Limit             int
}

// MarketQuoteWithItem pairs a cached MarketQuote with its Item metadata for fast SQLite scanning.
type MarketQuoteWithItem struct {
	Quote MarketQuote
	Item  Item
}

// MarketRepository caches live auction house quotes per server.
type MarketRepository interface {
	GetMarketQuotes(ctx context.Context, serverID ServerID, ids []ItemID) (map[ItemID]*MarketQuote, error)
	GetMultiServerQuotes(ctx context.Context, ids []ItemID) (map[ItemID][]MarketQuote, error)
	ListServerMarketWithItems(ctx context.Context, serverID ServerID, category string, minListings int) ([]MarketQuoteWithItem, error)
	UpsertMarketQuotes(ctx context.Context, quotes []MarketQuote) error
}

// InventoryRepository stores live character inventory & storage stacks indexed from .ndjson logs.
type InventoryRepository interface {
	GetCharacterStacks(ctx context.Context, characterKey string) ([]InventoryStack, error)
	GetAggregatedInventory(ctx context.Context, characterKey string) (map[ItemID]AggregatedItem, error)
	ReplaceContainerSnapshot(ctx context.Context, characterKey string, container ContainerKind, stacks []InventoryStack) error
	ApplyStackUpdates(ctx context.Context, characterKey string, updates []InventoryStack) error
	GetFileOffset(ctx context.Context, filePath string) (offset int64, charKey string, err error)
	SaveFileOffset(ctx context.Context, filePath string, offset int64, charKey string) error
}

// ProfessionRepository persists per-character crafting mastery levels.
type ProfessionRepository interface {
	GetProfessions(ctx context.Context, characterKey string) ([]ProfessionProgress, error)
	GetProfession(ctx context.Context, characterKey string, prof Profession) (*ProfessionProgress, error)
	UpsertProfession(ctx context.Context, p ProfessionProgress) error
}

// RemoteCatalogProvider fetches missing dictionary data and live market prices from Questlog.gg tRPC.
type RemoteCatalogProvider interface {
	FetchItem(ctx context.Context, id ItemID, locale string) (*Item, error)
	FetchItemMarket(ctx context.Context, id ItemID) ([]MarketQuote, error)
	FetchAllAuctionItems(ctx context.Context, locale string) ([]Item, error)
	FetchAllEquipmentItems(ctx context.Context, locale string) ([]Item, error)
	FetchAllRecipes(ctx context.Context, locale string) ([]Recipe, error)
	FetchEntities(ctx context.Context, kind EntityKind, locale string) ([]EntityEntry, error)
	FetchEntityByID(ctx context.Context, kind EntityKind, id int64, locale string) (*EntityEntry, error)
}

// LocalTelemetryReader reads live character/currency/ticket/drop/combat files from %APPDATA%\Questlog\aion-2.
type LocalTelemetryReader interface {
	Available() bool
	DataDir() string
	ReadCharacters(ctx context.Context) ([]Character, error)
	ReadActiveCharacterName(ctx context.Context) (string, error)
	ReadCurrencies(ctx context.Context) (map[string][]CurrencyBalance, error)
	ReadTickets(ctx context.Context) (map[string][]TicketEntry, error)
	ReadDrops(ctx context.Context, limitDays int) ([]DropStretch, error)
	ReadLocalEntities(ctx context.Context, locale string) ([]EntityEntry, error)
	SyncNDJSONInventory(ctx context.Context, invRepo InventoryRepository, knownChars []Character) (filesProcessed int, eventsApplied int, err error)

	// Combat, Dungeon, PvP, Personal Bests & Live Session Tracker
	ReadBossEncounters(ctx context.Context) ([]BossEncounterSummary, error)
	ReadBossEncounterDetail(ctx context.Context, encounterID string) (*BossEncounterDetail, error)
	ReadDungeonRuns(ctx context.Context) ([]DungeonRunSummary, error)
	ReadDungeonRunDetail(ctx context.Context, runID string) (*DungeonRunDetail, error)
	ReadPvPEncounters(ctx context.Context) ([]PvPEncounterSummary, error)
	ReadPvPEncounterDetail(ctx context.Context, pvpID string) (*PvPEncounterDetail, error)
	ReadPersonalBests(ctx context.Context) ([]PersonalBestEntry, error)
	ReadSessionTracker(ctx context.Context) (*SessionTrackerSnapshot, error)
}
