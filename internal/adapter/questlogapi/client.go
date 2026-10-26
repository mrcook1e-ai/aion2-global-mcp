package questlogapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"golang.org/x/time/rate"

	"github.com/mrcook1e/aion2-global-mcp/internal/domain"
)

const (
	defaultBaseURL   = "https://questlog.gg/aion-2/api/trpc"
	defaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36"
)

type Client struct {
	baseURL string
	http    *http.Client
	limiter *rate.Limiter
}

func New(rps float64) *Client {
	if rps <= 0 {
		rps = 12
	}
	return &Client{
		baseURL: defaultBaseURL,
		http:    &http.Client{Timeout: 15 * time.Second},
		limiter: rate.NewLimiter(rate.Limit(rps), max(4, int(rps))),
	}
}

type trpcEnvelope struct {
	Result struct {
		Data json.RawMessage `json:"data"`
	} `json:"result"`
}

func (c *Client) callTRPC(ctx context.Context, procedure string, input any, out any) error {
	if err := c.limiter.Wait(ctx); err != nil {
		return err
	}
	paramBytes, err := json.Marshal(input)
	if err != nil {
		return err
	}
	u := c.baseURL + "/" + procedure + "?input=" + url.QueryEscape(string(paramBytes))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", defaultUserAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("questlog trpc %s: http %d", procedure, resp.StatusCode)
	}
	var env trpcEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return fmt.Errorf("questlog trpc %s decode envelope: %w", procedure, err)
	}
	if len(env.Result.Data) == 0 || string(env.Result.Data) == "null" {
		return nil
	}
	return json.Unmarshal(env.Result.Data, out)
}

type rawItem struct {
	ID                  json.Number `json:"id"`
	Name                string      `json:"name"`
	Grade               int         `json:"grade"`
	MainCategory        string      `json:"mainCategory"`
	SubCategory         string      `json:"subCategory"`
	Icon                string      `json:"icon"`
	MinLevelReq         int         `json:"minLevelRequirement"`
	LevelReq            int         `json:"levelRequirement"`
	Tradable            *bool       `json:"tradable"`
	IsSoldOnBlackMarket *bool       `json:"isSoldOnBlackMarket"`
	ExchangeType        string      `json:"exchangeType"`
	EquipmentInfo       *struct {
		ItemLevel int `json:"itemLevel"`
	} `json:"equipmentInfo"`
	ItemIsSoldByNpcs []struct {
		Count int `json:"count"`
		Price []struct {
			ID           json.Number `json:"id"`
			Quantity     int64       `json:"quantity"`
			MainCategory string      `json:"mainCategory"`
		} `json:"price"`
	} `json:"itemIsSoldByNpcs"`
}

func (ri rawItem) toDomain(locale string) (domain.Item, bool) {
	id, err := strconv.ParseInt(ri.ID.String(), 10, 64)
	if err != nil || id <= 0 || ri.Name == "" {
		return domain.Item{}, false
	}
	ilvl := 0
	if ri.EquipmentInfo != nil {
		ilvl = ri.EquipmentInfo.ItemLevel
	}
	lvlReq := max(ri.LevelReq, ri.MinLevelReq)

	tradable := ri.Tradable
	if tradable == nil {
		if ri.IsSoldOnBlackMarket != nil {
			tradable = ri.IsSoldOnBlackMarket
		} else if ri.ExchangeType == "nonexchangeable" {
			f := false
			tradable = &f
		}
	}

	var npcBuyPrice int64
	for _, vendor := range ri.ItemIsSoldByNpcs {
		cnt := int64(max(1, vendor.Count))
		for _, p := range vendor.Price {
			pid, _ := strconv.ParseInt(p.ID.String(), 10, 64)
			if domain.IsKinahItem(domain.ItemID(pid)) && p.Quantity > 0 {
				unit := p.Quantity / cnt
				if unit > 0 && (npcBuyPrice == 0 || unit < npcBuyPrice) {
					npcBuyPrice = unit
				}
			}
		}
	}

	return domain.Item{
		ID:           domain.ItemID(id),
		Locale:       locale,
		Name:         ri.Name,
		Grade:        ri.Grade,
		MainCategory: ri.MainCategory,
		SubCategory:  ri.SubCategory,
		Icon:         ri.Icon,
		ItemLevel:    ilvl,
		LevelReq:     lvlReq,
		Tradable:     tradable,
		NPCBuyPrice:  npcBuyPrice,
		UpdatedAt:    time.Now().UTC(),
	}, true
}

func (c *Client) FetchItem(ctx context.Context, id domain.ItemID, locale string) (*domain.Item, error) {
	if locale == "" {
		locale = "ru"
	}
	var raw rawItem
	if err := c.callTRPC(ctx, "database.getItem", map[string]any{"id": strconv.FormatInt(int64(id), 10), "language": locale}, &raw); err != nil {
		return nil, err
	}
	it, ok := raw.toDomain(locale)
	if !ok {
		return nil, nil
	}
	return &it, nil
}

func (c *Client) FetchItemMarket(ctx context.Context, id domain.ItemID) ([]domain.MarketQuote, error) {
	var rows []struct {
		ServerID  int     `json:"serverId"`
		Market    string  `json:"market"`
		MinPrice  float64 `json:"minPrice"`
		Median7d  float64 `json:"median7d"`
		Low30d    float64 `json:"low30d"`
		High30d   float64 `json:"high30d"`
		Listings  int     `json:"listings"`
		Change24h float64 `json:"change24h"`
		Change7d  float64 `json:"change7d"`
	}
	if err := c.callTRPC(ctx, "auctionHouse.getItemMarket", map[string]any{"itemId": int64(id)}, &rows); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	out := make([]domain.MarketQuote, 0, len(rows))
	for _, r := range rows {
		if r.ServerID <= 0 {
			continue
		}
		out = append(out, domain.MarketQuote{
			ItemID:    id,
			ServerID:  domain.ServerID(r.ServerID),
			Market:    r.Market,
			MinPrice:  int64(r.MinPrice + 0.5),
			Median7d:  int64(r.Median7d + 0.5),
			Low30d:    int64(r.Low30d + 0.5),
			High30d:   int64(r.High30d + 0.5),
			Listings:  r.Listings,
			Change24h: r.Change24h,
			Change7d:  r.Change7d,
			UpdatedAt: now,
		})
	}
	return out, nil
}

func (c *Client) FetchAllAuctionItems(ctx context.Context, locale string) ([]domain.Item, error) {
	if locale == "" {
		locale = "ru"
	}
	var rows []rawItem
	if err := c.callTRPC(ctx, "auctionHouse.getItems", map[string]any{"language": locale}, &rows); err != nil {
		return nil, err
	}
	tradable := true
	items := make([]domain.Item, 0, len(rows))
	for _, r := range rows {
		if it, ok := r.toDomain(locale); ok {
			it.Tradable = &tradable
			items = append(items, it)
		}
	}
	return items, nil
}

func (c *Client) FetchAllEquipmentItems(ctx context.Context, locale string) ([]domain.Item, error) {
	if locale == "" {
		locale = "ru"
	}
	var rawMap map[string]rawItem
	if err := c.callTRPC(ctx, "characterBuilder.getEquipmentItems", map[string]any{"language": locale}, &rawMap); err != nil {
		return nil, err
	}
	items := make([]domain.Item, 0, len(rawMap))
	for _, r := range rawMap {
		if it, ok := r.toDomain(locale); ok {
			items = append(items, it)
		}
	}
	return items, nil
}

func (c *Client) FetchAllRecipes(ctx context.Context, locale string) ([]domain.Recipe, error) {
	if locale == "" {
		locale = "ru"
	}
	var rawMap map[string]struct {
		ID                json.Number `json:"id"`
		Name              string      `json:"name"`
		Grade             int         `json:"grade"`
		MainCategory      string      `json:"mainCategory"`
		SubCategory       string      `json:"subCategory"`
		QualificationRace string      `json:"qualificationRace"`
		MasteryGrade      string      `json:"masteryGrade"`
		MasteryLevel      int         `json:"masteryLevel"`
		GoldCost          int64       `json:"goldCost"`
		IsGuildCraft      bool        `json:"isGuildCraft"`
		SuccessRates      []float64   `json:"successRates"`
		RecipeInputItems  []struct {
			ID              json.Number `json:"id"`
			Name            string      `json:"name"`
			Quantity        int         `json:"quantity"`
			Grade           int         `json:"grade"`
			CraftableRecipe *struct {
				ID json.Number `json:"id"`
			} `json:"craftableRecipe"`
		} `json:"recipeInputItems"`
		RecipeOutputItems struct {
			ComboProbability float64 `json:"comboProbability"` // basis points: 2500 = 25.0%
			ProductItem      *struct {
				ID       json.Number `json:"id"`
				Name     string      `json:"name"`
				Quantity int         `json:"quantity"`
			} `json:"productItem"`
			ComboProductItem *struct {
				ID   json.Number `json:"id"`
				Name string      `json:"name"`
			} `json:"comboProductItem"`
		} `json:"recipeOutputItems"`
	}

	if err := c.callTRPC(ctx, "crafting.getRecipeData", map[string]any{"language": locale}, &rawMap); err != nil {
		return nil, err
	}

	recipes := make([]domain.Recipe, 0, len(rawMap))
	for key, r := range rawMap {
		rid, err := strconv.ParseInt(r.ID.String(), 10, 64)
		if err != nil || rid <= 0 {
			rid, _ = strconv.ParseInt(key, 10, 64)
		}
		if rid <= 0 || r.Name == "" {
			continue
		}
		rec := domain.Recipe{
			ID:                domain.RecipeID(rid),
			Locale:            locale,
			Name:              r.Name,
			Grade:             r.Grade,
			Profession:        domain.Profession(r.MainCategory),
			SubCategory:       r.SubCategory,
			QualificationRace: r.QualificationRace,
			MasteryGrade:      r.MasteryGrade,
			MasteryLevel:      r.MasteryLevel,
			GoldCost:          r.GoldCost,
			IsGuildCraft:      r.IsGuildCraft,
			SuccessRates:      r.SuccessRates,
			ComboProbability:  r.RecipeOutputItems.ComboProbability / 100.0,
		}
		if p := r.RecipeOutputItems.ProductItem; p != nil {
			pid, _ := strconv.ParseInt(p.ID.String(), 10, 64)
			rec.ProductItemID = domain.ItemID(pid)
			rec.ProductItemName = p.Name
			rec.ProductQuantity = max(1, p.Quantity)
		}
		if cp := r.RecipeOutputItems.ComboProductItem; cp != nil {
			cid, _ := strconv.ParseInt(cp.ID.String(), 10, 64)
			rec.ComboItemID = domain.ItemID(cid)
			rec.ComboItemName = cp.Name
		}
		for _, inp := range r.RecipeInputItems {
			iid, _ := strconv.ParseInt(inp.ID.String(), 10, 64)
			if iid <= 0 {
				continue
			}
			var craftRecID domain.RecipeID
			if inp.CraftableRecipe != nil {
				crid, _ := strconv.ParseInt(inp.CraftableRecipe.ID.String(), 10, 64)
				craftRecID = domain.RecipeID(crid)
			}
			rec.Ingredients = append(rec.Ingredients, domain.RecipeIngredient{
				RecipeID:        rec.ID,
				ItemID:          domain.ItemID(iid),
				ItemName:        inp.Name,
				Quantity:        max(1, inp.Quantity),
				Grade:           inp.Grade,
				CraftableRecipe: craftRecID,
			})
		}
		recipes = append(recipes, rec)
	}
	return recipes, nil
}

func (c *Client) FetchEntities(ctx context.Context, kind domain.EntityKind, locale string) ([]domain.EntityEntry, error) {
	if locale == "" {
		locale = "ru"
	}
	var procedure, idField string
	switch kind {
	case domain.EntityStat:
		procedure, idField = "statFormat.getStatFormat", "statId"
	case domain.EntityBoss:
		procedure, idField = "combatLog.catalog.bosses", "npc"
	case domain.EntityDungeon:
		procedure, idField = "combatLog.catalog.dungeons", "id"
	default:
		return nil, nil
	}

	var raw json.RawMessage
	if err := c.callTRPC(ctx, procedure, map[string]any{"language": locale}, &raw); err != nil || len(raw) == 0 {
		return nil, err
	}
	return parseGenericEntities(raw, kind, locale, idField), nil
}

func parseGenericEntities(raw json.RawMessage, kind domain.EntityKind, locale, idField string) []domain.EntityEntry {
	var list []map[string]any
	if err := json.Unmarshal(raw, &list); err != nil {
		var m map[string]map[string]any
		if json.Unmarshal(raw, &m) == nil {
			for _, v := range m {
				list = append(list, v)
			}
		}
	}
	now := time.Now().UTC()
	out := make([]domain.EntityEntry, 0, len(list))
	for _, row := range list {
		id := extractInt64(row[idField])
		if id <= 0 {
			id = extractInt64(row["id"])
		}
		name, _ := row["name"].(string)
		if name == "" {
			name, _ = row["label"].(string)
		}
		if id <= 0 || name == "" {
			continue
		}
		metaBytes, _ := json.Marshal(row)
		out = append(out, domain.EntityEntry{
			Kind:      kind,
			ID:        id,
			Locale:    locale,
			Name:      name,
			MetaJSON:  string(metaBytes),
			UpdatedAt: now,
		})
	}
	return out
}

func extractInt64(v any) int64 {
	switch x := v.(type) {
	case float64:
		return int64(x)
	case int64:
		return x
	case json.Number:
		n, _ := x.Int64()
		return n
	case string:
		n, _ := strconv.ParseInt(x, 10, 64)
		return n
	}
	return 0
}
