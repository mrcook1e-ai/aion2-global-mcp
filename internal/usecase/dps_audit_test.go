package usecase_test

import (
	"context"
	"testing"

	"github.com/mrcook1e/aion2-global-mcp/internal/domain"
	"github.com/mrcook1e/aion2-global-mcp/internal/usecase"
)

type dummyLocalFS struct{}

func (d *dummyLocalFS) Available() bool { return true }
func (d *dummyLocalFS) DataDir() string { return "/tmp/dummy" }
func (d *dummyLocalFS) ReadCharacters(ctx context.Context) ([]domain.Character, error) {
	return []domain.Character{
		{
			Key:         "2319:Cook1e",
			Name:        "Cook1e",
			ServerID:    2319,
			Class:       "Ranger",
			Level:       45,
			CombatPower: 38500,
			GearScore:   4200,
			Gear: []domain.GearSlot{
				{
					Slot:         1,
					ItemName:     "Лук Байзела",
					Grade:        51,
					ItemLevel:    45,
					EnchantLevel: 9,                 // triggers critical warning (< 10)
					ExceedLevel:  0,                 // triggers exceed warning
					ManastoneIDs: []domain.ItemID{}, // triggers manastone warning
				},
				{
					Slot:         2,
					ItemName:     "Перчатки ветра",
					Grade:        41,
					ItemLevel:    45,
					EnchantLevel: 13,
					ExceedLevel:  2,
					ManastoneIDs: []domain.ItemID{1001, 1002},
					Manastones:   []string{"Магический камень: Атака +5", "Магический камень: Крит +10"},
				},
			},
			Stats: []domain.StatValue{
				{StatID: 1, Name: "Attack", Value: 1508},
				{StatID: 2, Name: "Min Attack", Value: 507},
				{StatID: 3, Name: "Max Attack", Value: 613},
				{StatID: 4, Name: "Weapon Damage Boost", Value: 1000},
				{StatID: 5, Name: "Multi-hit Chance", Value: 1240},
				{StatID: 6, Name: "Power Shard Damage Bonus", Value: 161},
				{StatID: 7, Name: "Power Shard Damage Boost", Value: 1000},
				{StatID: 8, Name: "Attack Bonus", Value: 374},
				{StatID: 9, Name: "Might", Value: 109},
				{StatID: 10, Name: "Destruction (Zikel)", Value: 51},
				{StatID: 11, Name: "PvE Attack", Value: 264},
				{StatID: 12, Name: "Boss Attack", Value: 20},
				{StatID: 13, Name: "Back Attack", Value: 23},
				{StatID: 14, Name: "Penetration", Value: 1966},
				{StatID: 15, Name: "Damage Boost", Value: 1142},
				{StatID: 16, Name: "PvE Damage Boost", Value: 2957},
				{StatID: 17, Name: "Critical Hit", Value: 1001},
				{StatID: 18, Name: "Back Attack Critical Hit", Value: 147},
				{StatID: 19, Name: "Critical Damage Boost", Value: 2600},
				{StatID: 20, Name: "Double Chance", Value: 820},
				{StatID: 21, Name: "Combat Speed", Value: 2789},
				{StatID: 22, Name: "Illusion (Kaisinel)", Value: 15},
				{StatID: 23, Name: "Perfect Chance", Value: 1719},
			},
			Skills: []domain.CharacterSkill{
				{
					SkillID:               1,
					Name:                  "Взрывная стрела",
					Level:                 12,
					Equipped:              true,
					Specializations:       []int{1, 2},
					SpecializationDetails: []string{"Быстрый выстрел (КД -15с)", "Критический урон +30%"},
				},
				{
					SkillID:  2,
					Name:     "Шквал стрел",
					Level:    4, // triggers skill level warning (< 10)
					Equipped: true,
					// Specializations empty triggers critical warning
				},
			},
			Daevanion: []domain.DaevanionBoard{
				{BoardID: 31, UnlockedCount: 10},
				{BoardID: 32, UnlockedCount: 8},
			},
			Genus: []domain.GenusBranch{
				{
					Name:  "Intellect",
					Level: 10,
					Effects: []domain.GenusEffect{
						{Slot: 1, StatName: "Attack", Value: 25},
						{Slot: 2, StatName: "Регенерация HP", Value: 10},
						{Slot: 3, StatName: "Сопротивление магии", Value: 15},
					},
				},
			},
			EquippedWing: &domain.WingInfo{ID: 10, Name: "Крылья бури"},
			Pantheon: []domain.PantheonEntry{
				{Slot: 1, DecoID: 1, Name: "Реликвия Неджакана"},
			},
		},
	}, nil
}
func (d *dummyLocalFS) ReadActiveCharacterName(ctx context.Context) (string, error) {
	return "Cook1e", nil
}
func (d *dummyLocalFS) ReadCurrencies(ctx context.Context) (map[string][]domain.CurrencyBalance, error) {
	return nil, nil
}
func (d *dummyLocalFS) ReadTickets(ctx context.Context) (map[string][]domain.TicketEntry, error) {
	return nil, nil
}
func (d *dummyLocalFS) ReadDrops(ctx context.Context, limitDays int) ([]domain.DropStretch, error) {
	return nil, nil
}
func (d *dummyLocalFS) ReadLocalEntities(ctx context.Context, locale string) ([]domain.EntityEntry, error) {
	return nil, nil
}
func (d *dummyLocalFS) SyncNDJSONInventory(ctx context.Context, invRepo domain.InventoryRepository, knownChars []domain.Character) (int, int, error) {
	return 0, 0, nil
}
func (d *dummyLocalFS) ReadBossEncounters(ctx context.Context) ([]domain.BossEncounterSummary, error) {
	return nil, nil
}
func (d *dummyLocalFS) ReadBossEncounterDetail(ctx context.Context, encounterID string) (*domain.BossEncounterDetail, error) {
	return nil, nil
}
func (d *dummyLocalFS) ReadDungeonRuns(ctx context.Context) ([]domain.DungeonRunSummary, error) {
	return nil, nil
}
func (d *dummyLocalFS) ReadDungeonRunDetail(ctx context.Context, runID string) (*domain.DungeonRunDetail, error) {
	return nil, nil
}
func (d *dummyLocalFS) ReadPvPEncounters(ctx context.Context) ([]domain.PvPEncounterSummary, error) {
	return nil, nil
}
func (d *dummyLocalFS) ReadPvPEncounterDetail(ctx context.Context, pvpID string) (*domain.PvPEncounterDetail, error) {
	return nil, nil
}
func (d *dummyLocalFS) ReadPersonalBests(ctx context.Context) ([]domain.PersonalBestEntry, error) {
	return []domain.PersonalBestEntry{
		{
			CharacterKey:    "2319:Cook1e",
			BossName:        "Desecrator Newbold",
			DPS:             74714,
			Damage:          1700000,
			DurationSeconds: 23,
		},
	}, nil
}
func (d *dummyLocalFS) ReadSessionTracker(ctx context.Context) (*domain.SessionTrackerSnapshot, error) {
	return nil, nil
}

type dummyInvRepo struct{}

func (d *dummyInvRepo) GetCharacterStacks(ctx context.Context, characterKey string) ([]domain.InventoryStack, error) {
	return nil, nil
}
func (d *dummyInvRepo) GetAggregatedInventory(ctx context.Context, characterKey string) (map[domain.ItemID]domain.AggregatedItem, error) {
	return nil, nil
}
func (d *dummyInvRepo) ReplaceContainerSnapshot(ctx context.Context, characterKey string, container domain.ContainerKind, stacks []domain.InventoryStack) error {
	return nil
}
func (d *dummyInvRepo) ApplyStackUpdates(ctx context.Context, characterKey string, updates []domain.InventoryStack) error {
	return nil
}
func (d *dummyInvRepo) GetFileOffset(ctx context.Context, filePath string) (int64, string, error) {
	return 0, "", nil
}
func (d *dummyInvRepo) SaveFileOffset(ctx context.Context, filePath string, offset int64, charKey string) error {
	return nil
}

type dummyProfRepo struct{}

func (d *dummyProfRepo) GetProfessions(ctx context.Context, characterKey string) ([]domain.ProfessionProgress, error) {
	return nil, nil
}
func (d *dummyProfRepo) GetProfession(ctx context.Context, characterKey string, prof domain.Profession) (*domain.ProfessionProgress, error) {
	return nil, nil
}
func (d *dummyProfRepo) UpsertProfession(ctx context.Context, p domain.ProfessionProgress) error {
	return nil
}

type dummyCatalogRepo struct{}

func (d *dummyCatalogRepo) GetItem(ctx context.Context, id domain.ItemID) (*domain.Item, error) {
	return nil, nil
}
func (d *dummyCatalogRepo) GetItemsByIDs(ctx context.Context, ids []domain.ItemID) (map[domain.ItemID]domain.Item, error) {
	return nil, nil
}
func (d *dummyCatalogRepo) SearchItems(ctx context.Context, query, category string, grade, limit int) ([]domain.Item, error) {
	return nil, nil
}
func (d *dummyCatalogRepo) UpsertItems(ctx context.Context, items []domain.Item) error { return nil }
func (d *dummyCatalogRepo) GetRecipe(ctx context.Context, id domain.RecipeID) (*domain.Recipe, error) {
	return nil, nil
}
func (d *dummyCatalogRepo) SearchRecipes(ctx context.Context, filter domain.RecipeFilter) ([]domain.Recipe, error) {
	return nil, nil
}
func (d *dummyCatalogRepo) GetRecipesForItem(ctx context.Context, itemID domain.ItemID) ([]domain.Recipe, error) {
	return nil, nil
}
func (d *dummyCatalogRepo) UpsertRecipes(ctx context.Context, recipes []domain.Recipe) error {
	return nil
}
func (d *dummyCatalogRepo) GetEntity(ctx context.Context, kind domain.EntityKind, id int64, locale string) (*domain.EntityEntry, error) {
	return nil, nil
}
func (d *dummyCatalogRepo) GetEntitiesByIDs(ctx context.Context, kind domain.EntityKind, ids []int64, locale string) (map[int64]domain.EntityEntry, error) {
	return nil, nil
}
func (d *dummyCatalogRepo) GetEntitiesByKind(ctx context.Context, kind domain.EntityKind, locale string) (map[int64]domain.EntityEntry, error) {
	return nil, nil
}
func (d *dummyCatalogRepo) UpsertEntities(ctx context.Context, entries []domain.EntityEntry) error {
	return nil
}

type dummyRemoteCatalog struct{}

func (d *dummyRemoteCatalog) FetchItem(ctx context.Context, id domain.ItemID, locale string) (*domain.Item, error) {
	return nil, nil
}
func (d *dummyRemoteCatalog) FetchItemMarket(ctx context.Context, id domain.ItemID) ([]domain.MarketQuote, error) {
	return nil, nil
}
func (d *dummyRemoteCatalog) FetchAllAuctionItems(ctx context.Context, locale string) ([]domain.Item, error) {
	return nil, nil
}
func (d *dummyRemoteCatalog) FetchAllEquipmentItems(ctx context.Context, locale string) ([]domain.Item, error) {
	return nil, nil
}
func (d *dummyRemoteCatalog) FetchAllRecipes(ctx context.Context, locale string) ([]domain.Recipe, error) {
	return nil, nil
}
func (d *dummyRemoteCatalog) FetchEntities(ctx context.Context, kind domain.EntityKind, locale string) ([]domain.EntityEntry, error) {
	return nil, nil
}
func (d *dummyRemoteCatalog) FetchEntityByID(ctx context.Context, kind domain.EntityKind, id int64, locale string) (*domain.EntityEntry, error) {
	return nil, nil
}

func TestDPSAuditService(t *testing.T) {
	fs := &dummyLocalFS{}
	invRepo := &dummyInvRepo{}
	profRepo := &dummyProfRepo{}
	catRepo := &dummyCatalogRepo{}
	remoteCat := &dummyRemoteCatalog{}

	resolver := usecase.NewResolver(catRepo, nil, remoteCat, "ru")
	syncSvc := usecase.NewSyncService(catRepo, invRepo, remoteCat, fs, resolver, nil)
	charSvc := usecase.NewCharacterService(fs, invRepo, profRepo, syncSvc, resolver)
	combatSvc := usecase.NewCombatService(fs, charSvc, syncSvc, resolver)

	auditSvc := usecase.NewDPSAuditService(charSvc, combatSvc, resolver)

	res, err := auditSvc.AuditCharacterDPS(context.Background(), usecase.DPSAuditQuery{
		Character: "Cook1e",
		Mode:      "pve",
		TargetDef: 4500,
	})
	if err != nil {
		t.Fatalf("AuditCharacterDPS failed: %v", err)
	}

	if res.Character != "Cook1e" {
		t.Errorf("expected Cook1e, got %s", res.Character)
	}

	if res.Pipeline.TotalAttackPower <= 0 {
		t.Errorf("expected positive TotalAttackPower, got %f", res.Pipeline.TotalAttackPower)
	}

	if len(res.GearAudit.Issues) == 0 {
		t.Errorf("expected gear issues flagged (enchant, exceed, manastones), got 0")
	}

	if len(res.SkillAudit.Issues) == 0 {
		t.Errorf("expected skill issues flagged (tripods missing), got 0")
	}

	if len(res.StatWeights) == 0 {
		t.Errorf("expected stat weights calculated, got 0")
	}

	if len(res.Bottlenecks) == 0 {
		t.Errorf("expected prioritized bottlenecks synthesized, got 0")
	}

	if res.PersonalBest == nil || res.PersonalBest.DPS != 74714 {
		t.Errorf("expected personal best 74714 DPS, got %+v", res.PersonalBest)
	}
}
