package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/mrcook1e/aion2-global-mcp/internal/domain"
)

type Store struct {
	db *sql.DB
}

func Open(ctx context.Context, dbPath string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("create sqlite dir: %w", err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// Single writer friendliness in SQLite WAL
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)

	if err := migrate(ctx, db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate sqlite: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

// --- CatalogRepository ---

func (s *Store) GetItem(ctx context.Context, id domain.ItemID) (*domain.Item, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, COALESCE(locale, 'ru'), name, COALESCE(grade, 0), COALESCE(main_category, ''),
		       COALESCE(sub_category, ''), COALESCE(icon, ''), COALESCE(item_level, 0),
		       COALESCE(level_req, 0), tradable, COALESCE(npc_buy_price, 0), COALESCE(updated_at, 0)
		FROM items WHERE id = ?`, id)
	it, err := scanItem(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &it, nil
}

func (s *Store) GetItemsByIDs(ctx context.Context, ids []domain.ItemID) (map[domain.ItemID]domain.Item, error) {
	out := make(map[domain.ItemID]domain.Item, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	// Chunk by 500 to stay well below SQLite variable limits
	for start := 0; start < len(ids); start += 500 {
		chunk := ids[start:min(len(ids), start+500)]
		placeholders := strings.Repeat("?,", len(chunk))
		placeholders = placeholders[:len(placeholders)-1]
		args := make([]any, len(chunk))
		for i, id := range chunk {
			args[i] = id
		}
		rows, err := s.db.QueryContext(ctx, `
			SELECT id, COALESCE(locale, 'ru'), name, COALESCE(grade, 0), COALESCE(main_category, ''),
			       COALESCE(sub_category, ''), COALESCE(icon, ''), COALESCE(item_level, 0),
			       COALESCE(level_req, 0), tradable, COALESCE(npc_buy_price, 0), COALESCE(updated_at, 0)
			FROM items WHERE id IN (`+placeholders+`)`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			it, err := scanItem(rows)
			if err != nil {
				_ = rows.Close()
				return nil, err
			}
			out[it.ID] = it
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Store) SearchItems(ctx context.Context, query, category string, grade, limit int) ([]domain.Item, error) {
	if limit <= 0 || limit > 200 {
		limit = 30
	}
	var (
		sb   strings.Builder
		args []any
	)
	sb.WriteString(`
		SELECT id, COALESCE(locale, 'ru'), name, COALESCE(grade, 0), COALESCE(main_category, ''),
		       COALESCE(sub_category, ''), COALESCE(icon, ''), COALESCE(item_level, 0),
		       COALESCE(level_req, 0), tradable, COALESCE(npc_buy_price, 0), COALESCE(updated_at, 0)
		FROM items WHERE 1=1`)
	for _, word := range strings.Fields(query) {
		sb.WriteString(` AND name LIKE ? COLLATE NOCASE`)
		args = append(args, "%"+word+"%")
	}
	if category != "" {
		sb.WriteString(` AND (main_category = ? OR sub_category = ?)`)
		args = append(args, category, category)
	}
	if grade > 0 {
		sb.WriteString(` AND grade = ?`)
		args = append(args, grade)
	}
	sb.WriteString(` ORDER BY grade DESC, item_level DESC, id DESC LIMIT ?`)
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, sb.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []domain.Item
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

func (s *Store) UpsertItems(ctx context.Context, items []domain.Item) error {
	if len(items) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO items (id, locale, name, grade, main_category, sub_category, icon, item_level, level_req, tradable, npc_buy_price, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			locale = excluded.locale,
			name = excluded.name,
			grade = CASE WHEN excluded.grade > 0 THEN excluded.grade ELSE items.grade END,
			main_category = CASE WHEN excluded.main_category != '' THEN excluded.main_category ELSE items.main_category END,
			sub_category = CASE WHEN excluded.sub_category != '' THEN excluded.sub_category ELSE items.sub_category END,
			icon = CASE WHEN excluded.icon != '' THEN excluded.icon ELSE items.icon END,
			item_level = CASE WHEN excluded.item_level > 0 THEN excluded.item_level ELSE items.item_level END,
			level_req = CASE WHEN excluded.level_req > 0 THEN excluded.level_req ELSE items.level_req END,
			tradable = COALESCE(excluded.tradable, items.tradable),
			npc_buy_price = CASE WHEN excluded.npc_buy_price > 0 THEN excluded.npc_buy_price ELSE COALESCE(items.npc_buy_price, 0) END,
			updated_at = excluded.updated_at`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	now := time.Now().Unix()
	for _, it := range items {
		loc := it.Locale
		if loc == "" {
			loc = "ru"
		}
		var tradable any
		if it.Tradable != nil {
			if *it.Tradable {
				tradable = 1
			} else {
				tradable = 0
			}
		}
		if _, err := stmt.ExecContext(ctx, it.ID, loc, it.Name, it.Grade, it.MainCategory, it.SubCategory, it.Icon, it.ItemLevel, it.LevelReq, tradable, it.NPCBuyPrice, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) GetRecipe(ctx context.Context, id domain.RecipeID) (*domain.Recipe, error) {
	rows, err := s.queryRecipes(ctx, `WHERE id = ?`, []any{id})
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return &rows[0], nil
}

func (s *Store) GetRecipesForItem(ctx context.Context, itemID domain.ItemID) ([]domain.Recipe, error) {
	return s.queryRecipes(ctx, `WHERE product_item_id = ? OR combo_item_id = ? ORDER BY grade DESC, mastery_level DESC`, []any{itemID, itemID})
}

func (s *Store) SearchRecipes(ctx context.Context, f domain.RecipeFilter) ([]domain.Recipe, error) {
	var (
		sb   strings.Builder
		args []any
	)
	sb.WriteString(`WHERE 1=1`)
	if f.Profession != "" {
		sb.WriteString(` AND main_category = ?`)
		args = append(args, string(f.Profession))
	}
	if f.SubCategory != "" {
		sb.WriteString(` AND sub_category = ?`)
		args = append(args, f.SubCategory)
	}
	if f.QualificationRace != "" {
		sb.WriteString(` AND (qualification_race = '' OR qualification_race IS NULL OR qualification_race = ?)`)
		args = append(args, f.QualificationRace)
	}
	if f.MaxMasteryLevel != nil {
		sb.WriteString(` AND COALESCE(mastery_level, 0) <= ?`)
		args = append(args, *f.MaxMasteryLevel)
	}
	if f.MinMasteryLevel > 0 {
		sb.WriteString(` AND COALESCE(mastery_level, 0) >= ?`)
		args = append(args, f.MinMasteryLevel)
	}
	for _, word := range strings.Fields(f.Query) {
		sb.WriteString(` AND (name LIKE ? COLLATE NOCASE OR product_item_name LIKE ? COLLATE NOCASE OR combo_item_name LIKE ? COLLATE NOCASE)`)
		pat := "%" + word + "%"
		args = append(args, pat, pat, pat)
	}
	sb.WriteString(` ORDER BY mastery_level DESC, grade DESC, id DESC`)
	if f.Limit > 0 {
		sb.WriteString(` LIMIT ?`)
		args = append(args, f.Limit)
	}
	recipes, err := s.queryRecipes(ctx, sb.String(), args)
	if err != nil {
		return nil, err
	}
	if f.ExcludeGuildCraft {
		filtered := recipes[:0]
		for _, r := range recipes {
			if !r.IsGuildCraft {
				filtered = append(filtered, r)
			}
		}
		recipes = filtered
	}
	return recipes, nil
}

func (s *Store) queryRecipes(ctx context.Context, whereAndOrder string, args []any) ([]domain.Recipe, error) {
	q := `
		SELECT id, COALESCE(locale, 'ru'), name, COALESCE(grade, 0), COALESCE(main_category, ''),
		       COALESCE(sub_category, ''), COALESCE(qualification_race, ''), COALESCE(mastery_grade, ''),
		       COALESCE(mastery_level, 0), COALESCE(gold_cost, 0), COALESCE(is_guild_craft, 0),
		       COALESCE(success_rates_json, ''), COALESCE(product_item_id, 0), COALESCE(product_item_name, ''),
		       COALESCE(product_quantity, 1), COALESCE(combo_item_id, 0), COALESCE(combo_item_name, ''),
		       COALESCE(combo_probability, 0), COALESCE(raw_json, '')
		FROM recipes ` + whereAndOrder

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	var (
		recipes   []domain.Recipe
		recipeIDs []domain.RecipeID
		rawJSONs  []string
	)
	for rows.Next() {
		var (
			r         domain.Recipe
			prof      string
			isGuild   int
			ratesJSON string
			rawJSON   string
		)
		if err := rows.Scan(
			&r.ID, &r.Locale, &r.Name, &r.Grade, &prof,
			&r.SubCategory, &r.QualificationRace, &r.MasteryGrade,
			&r.MasteryLevel, &r.GoldCost, &isGuild,
			&ratesJSON, &r.ProductItemID, &r.ProductItemName,
			&r.ProductQuantity, &r.ComboItemID, &r.ComboItemName,
			&r.ComboProbability, &rawJSON,
		); err != nil {
			_ = rows.Close()
			return nil, err
		}
		r.Profession = domain.Profession(prof)
		r.IsGuildCraft = isGuild != 0

		if ratesJSON != "" {
			_ = json.Unmarshal([]byte(ratesJSON), &r.SuccessRates)
		} else if rawJSON != "" {
			// Seamless extraction from existing Python raw_json column!
			var rawMeta struct {
				SuccessRates []float64 `json:"successRates"`
				IsGuildCraft bool      `json:"isGuildCraft"`
			}
			if json.Unmarshal([]byte(rawJSON), &rawMeta) == nil {
				r.SuccessRates = rawMeta.SuccessRates
				if rawMeta.IsGuildCraft {
					r.IsGuildCraft = true
				}
			}
		}
		recipes = append(recipes, r)
		recipeIDs = append(recipeIDs, r.ID)
		rawJSONs = append(rawJSONs, rawJSON)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if len(recipes) == 0 {
		return nil, nil
	}

	ingMap, err := s.loadIngredients(ctx, recipeIDs)
	if err != nil {
		return nil, err
	}
	for i := range recipes {
		recipes[i].Ingredients = ingMap[recipes[i].ID]
		if len(recipes[i].Ingredients) == 0 && rawJSONs[i] != "" {
			var fallback struct {
				RecipeInputItems []struct {
					ID       json.Number `json:"id"`
					Name     string      `json:"name"`
					Quantity int         `json:"quantity"`
					Grade    int         `json:"grade"`
				} `json:"recipeInputItems"`
			}
			if json.Unmarshal([]byte(rawJSONs[i]), &fallback) == nil {
				for _, inp := range fallback.RecipeInputItems {
					if iid, err := strconv.ParseInt(inp.ID.String(), 10, 64); err == nil && iid > 0 {
						recipes[i].Ingredients = append(recipes[i].Ingredients, domain.RecipeIngredient{
							RecipeID: recipes[i].ID,
							ItemID:   domain.ItemID(iid),
							ItemName: inp.Name,
							Quantity: max(1, inp.Quantity),
							Grade:    inp.Grade,
						})
					}
				}
			}
		}
	}
	return recipes, nil
}

func (s *Store) loadIngredients(ctx context.Context, recipeIDs []domain.RecipeID) (map[domain.RecipeID][]domain.RecipeIngredient, error) {
	out := make(map[domain.RecipeID][]domain.RecipeIngredient, len(recipeIDs))
	for start := 0; start < len(recipeIDs); start += 500 {
		chunk := recipeIDs[start:min(len(recipeIDs), start+500)]
		ph := strings.Repeat("?,", len(chunk))
		ph = ph[:len(ph)-1]
		args := make([]any, len(chunk))
		for i, id := range chunk {
			args[i] = id
		}
		rows, err := s.db.QueryContext(ctx, `
			SELECT recipe_id, item_id, COALESCE(item_name, ''), COALESCE(quantity, 1), COALESCE(grade, 0), COALESCE(craftable_recipe_id, 0)
			FROM recipe_ingredients WHERE recipe_id IN (`+ph+`)`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var ing domain.RecipeIngredient
			if err := rows.Scan(&ing.RecipeID, &ing.ItemID, &ing.ItemName, &ing.Quantity, &ing.Grade, &ing.CraftableRecipe); err != nil {
				_ = rows.Close()
				return nil, err
			}
			out[ing.RecipeID] = append(out[ing.RecipeID], ing)
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Store) UpsertRecipes(ctx context.Context, recipes []domain.Recipe) error {
	if len(recipes) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	recStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO recipes (
			id, locale, name, grade, main_category, sub_category, qualification_race,
			mastery_grade, mastery_level, gold_cost, is_guild_craft, success_rates_json,
			product_item_id, product_item_name, product_quantity,
			combo_item_id, combo_item_name, combo_probability
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			locale = excluded.locale,
			name = excluded.name,
			grade = excluded.grade,
			main_category = excluded.main_category,
			sub_category = excluded.sub_category,
			qualification_race = excluded.qualification_race,
			mastery_grade = excluded.mastery_grade,
			mastery_level = excluded.mastery_level,
			gold_cost = excluded.gold_cost,
			is_guild_craft = excluded.is_guild_craft,
			success_rates_json = excluded.success_rates_json,
			product_item_id = excluded.product_item_id,
			product_item_name = excluded.product_item_name,
			product_quantity = excluded.product_quantity,
			combo_item_id = excluded.combo_item_id,
			combo_item_name = excluded.combo_item_name,
			combo_probability = excluded.combo_probability`)
	if err != nil {
		return err
	}
	defer recStmt.Close()

	delIngStmt, err := tx.PrepareContext(ctx, `DELETE FROM recipe_ingredients WHERE recipe_id = ?`)
	if err != nil {
		return err
	}
	defer delIngStmt.Close()

	insIngStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO recipe_ingredients (recipe_id, item_id, item_name, quantity, grade, craftable_recipe_id)
		VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer insIngStmt.Close()

	for _, r := range recipes {
		loc := r.Locale
		if loc == "" {
			loc = "ru"
		}
		isGuild := 0
		if r.IsGuildCraft {
			isGuild = 1
		}
		ratesBytes, _ := json.Marshal(r.SuccessRates)
		if _, err := recStmt.ExecContext(ctx,
			r.ID, loc, r.Name, r.Grade, string(r.Profession), r.SubCategory, r.QualificationRace,
			r.MasteryGrade, r.MasteryLevel, r.GoldCost, isGuild, string(ratesBytes),
			r.ProductItemID, r.ProductItemName, max(1, r.ProductQuantity),
			r.ComboItemID, r.ComboItemName, r.ComboProbability,
		); err != nil {
			return err
		}
		// Delete stale ingredients before re-inserting (fixes phantom ingredient bug!)
		if _, err := delIngStmt.ExecContext(ctx, r.ID); err != nil {
			return err
		}
		for _, ing := range r.Ingredients {
			if _, err := insIngStmt.ExecContext(ctx, r.ID, ing.ItemID, ing.ItemName, max(1, ing.Quantity), ing.Grade, ing.CraftableRecipe); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func (s *Store) GetEntity(ctx context.Context, kind domain.EntityKind, id int64, locale string) (*domain.EntityEntry, error) {
	if locale == "" {
		locale = "ru"
	}
	var e domain.EntityEntry
	var ts int64
	err := s.db.QueryRowContext(ctx, `
		SELECT kind, id, locale, name, category, meta_json, updated_at
		FROM entities WHERE kind = ? AND id = ? AND locale = ?`, string(kind), id, locale).
		Scan(&e.Kind, &e.ID, &e.Locale, &e.Name, &e.Category, &e.MetaJSON, &ts)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if ts > 0 {
		e.UpdatedAt = time.Unix(ts, 0).UTC()
	}
	return &e, nil
}

func (s *Store) GetEntitiesByIDs(ctx context.Context, kind domain.EntityKind, ids []int64, locale string) (map[int64]domain.EntityEntry, error) {
	out := make(map[int64]domain.EntityEntry, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	if locale == "" {
		locale = "ru"
	}
	for start := 0; start < len(ids); start += 500 {
		chunk := ids[start:min(len(ids), start+500)]
		ph := strings.Repeat("?,", len(chunk))
		ph = ph[:len(ph)-1]
		args := make([]any, 0, len(chunk)+2)
		args = append(args, string(kind), locale)
		for _, id := range chunk {
			args = append(args, id)
		}
		rows, err := s.db.QueryContext(ctx, `
			SELECT kind, id, locale, name, category, meta_json, updated_at
			FROM entities WHERE kind = ? AND locale = ? AND id IN (`+ph+`)`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var e domain.EntityEntry
			var ts int64
			if err := rows.Scan(&e.Kind, &e.ID, &e.Locale, &e.Name, &e.Category, &e.MetaJSON, &ts); err != nil {
				_ = rows.Close()
				return nil, err
			}
			if ts > 0 {
				e.UpdatedAt = time.Unix(ts, 0).UTC()
			}
			out[e.ID] = e
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Store) GetEntitiesByKind(ctx context.Context, kind domain.EntityKind, locale string) (map[int64]domain.EntityEntry, error) {
	if locale == "" {
		locale = "ru"
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT kind, id, locale, name, category, meta_json, updated_at
		FROM entities WHERE kind = ? AND locale = ?`, string(kind), locale)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[int64]domain.EntityEntry)
	for rows.Next() {
		var e domain.EntityEntry
		var ts int64
		if err := rows.Scan(&e.Kind, &e.ID, &e.Locale, &e.Name, &e.Category, &e.MetaJSON, &ts); err != nil {
			return nil, err
		}
		if ts > 0 {
			e.UpdatedAt = time.Unix(ts, 0).UTC()
		}
		out[e.ID] = e
	}
	return out, rows.Err()
}

func (s *Store) UpsertEntities(ctx context.Context, entries []domain.EntityEntry) error {
	if len(entries) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO entities (kind, id, locale, name, category, meta_json, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(kind, id, locale) DO UPDATE SET
			name = excluded.name,
			category = CASE WHEN excluded.category != '' THEN excluded.category ELSE entities.category END,
			meta_json = CASE WHEN excluded.meta_json != '' THEN excluded.meta_json ELSE entities.meta_json END,
			updated_at = excluded.updated_at`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	now := time.Now().Unix()
	for _, e := range entries {
		loc := e.Locale
		if loc == "" {
			loc = "ru"
		}
		if _, err := stmt.ExecContext(ctx, string(e.Kind), e.ID, loc, e.Name, e.Category, e.MetaJSON, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// --- MarketRepository ---

func (s *Store) GetMarketQuotes(ctx context.Context, serverID domain.ServerID, ids []domain.ItemID) (map[domain.ItemID]*domain.MarketQuote, error) {
	out := make(map[domain.ItemID]*domain.MarketQuote, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	now := time.Now().UTC()
	for start := 0; start < len(ids); start += 500 {
		chunk := ids[start:min(len(ids), start+500)]
		ph := strings.Repeat("?,", len(chunk))
		ph = ph[:len(ph)-1]
		args := make([]any, 0, len(chunk)+1)
		args = append(args, serverID)
		for _, id := range chunk {
			args = append(args, id)
		}
		rows, err := s.db.QueryContext(ctx, `
			SELECT item_id, server_id, COALESCE(market, 'market'),
			       CAST(ROUND(COALESCE(min_price, 0)) AS INTEGER),
			       CAST(ROUND(COALESCE(median_7d, 0)) AS INTEGER),
			       CAST(ROUND(COALESCE(low_30d, 0)) AS INTEGER),
			       CAST(ROUND(COALESCE(high_30d, 0)) AS INTEGER),
			       COALESCE(listings, 0), COALESCE(change_24h, 0), COALESCE(change_7d, 0), COALESCE(updated_at, 0)
			FROM market_cache WHERE server_id = ? AND item_id IN (`+ph+`)`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var q domain.MarketQuote
			var ts int64
			if err := rows.Scan(&q.ItemID, &q.ServerID, &q.Market, &q.MinPrice, &q.Median7d, &q.Low30d, &q.High30d, &q.Listings, &q.Change24h, &q.Change7d, &ts); err != nil {
				_ = rows.Close()
				return nil, err
			}
			if ts > 0 {
				q.UpdatedAt = time.Unix(ts, 0).UTC()
				q.HydrateFreshness(now)
			}
			out[q.ItemID] = &q
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Store) GetMultiServerQuotes(ctx context.Context, ids []domain.ItemID) (map[domain.ItemID][]domain.MarketQuote, error) {
	out := make(map[domain.ItemID][]domain.MarketQuote, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	now := time.Now().UTC()
	for start := 0; start < len(ids); start += 500 {
		chunk := ids[start:min(len(ids), start+500)]
		ph := strings.Repeat("?,", len(chunk))
		ph = ph[:len(ph)-1]
		args := make([]any, len(chunk))
		for i, id := range chunk {
			args[i] = id
		}
		rows, err := s.db.QueryContext(ctx, `
			SELECT item_id, server_id, COALESCE(market, 'market'),
			       CAST(ROUND(COALESCE(min_price, 0)) AS INTEGER),
			       CAST(ROUND(COALESCE(median_7d, 0)) AS INTEGER),
			       CAST(ROUND(COALESCE(low_30d, 0)) AS INTEGER),
			       CAST(ROUND(COALESCE(high_30d, 0)) AS INTEGER),
			       COALESCE(listings, 0), COALESCE(change_24h, 0), COALESCE(change_7d, 0), COALESCE(updated_at, 0)
			FROM market_cache
			WHERE item_id IN (`+ph+`) AND (min_price > 0 OR median_7d > 0)
			ORDER BY item_id, server_id`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var q domain.MarketQuote
			var ts int64
			if err := rows.Scan(&q.ItemID, &q.ServerID, &q.Market, &q.MinPrice, &q.Median7d, &q.Low30d, &q.High30d, &q.Listings, &q.Change24h, &q.Change7d, &ts); err != nil {
				_ = rows.Close()
				return nil, err
			}
			if ts > 0 {
				q.UpdatedAt = time.Unix(ts, 0).UTC()
				q.HydrateFreshness(now)
			}
			out[q.ItemID] = append(out[q.ItemID], q)
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Store) ListServerMarketWithItems(ctx context.Context, serverID domain.ServerID, category string, minListings int) ([]domain.MarketQuoteWithItem, error) {
	var (
		sb   strings.Builder
		args = []any{serverID, minListings}
	)
	sb.WriteString(`
		SELECT m.item_id, m.server_id, COALESCE(m.market, 'market'),
		       CAST(ROUND(COALESCE(m.min_price, 0)) AS INTEGER),
		       CAST(ROUND(COALESCE(m.median_7d, 0)) AS INTEGER),
		       CAST(ROUND(COALESCE(m.low_30d, 0)) AS INTEGER),
		       CAST(ROUND(COALESCE(m.high_30d, 0)) AS INTEGER),
		       COALESCE(m.listings, 0), COALESCE(m.change_24h, 0), COALESCE(m.change_7d, 0), COALESCE(m.updated_at, 0),
		       i.id, COALESCE(i.locale, 'ru'), i.name, COALESCE(i.grade, 0), COALESCE(i.main_category, ''),
		       COALESCE(i.sub_category, ''), COALESCE(i.icon, ''), COALESCE(i.item_level, 0),
		       COALESCE(i.level_req, 0), i.tradable, COALESCE(i.npc_buy_price, 0), COALESCE(i.updated_at, 0)
		FROM market_cache m
		INNER JOIN items i ON i.id = m.item_id
		WHERE m.server_id = ? AND COALESCE(m.listings, 0) >= ? AND m.min_price > 0`)

	cat := strings.TrimSpace(category)
	if cat != "" {
		sb.WriteString(` AND (i.main_category = ? COLLATE NOCASE OR i.sub_category = ? COLLATE NOCASE)`)
		args = append(args, cat, cat)
	}

	rows, err := s.db.QueryContext(ctx, sb.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	now := time.Now().UTC()
	var out []domain.MarketQuoteWithItem
	for rows.Next() {
		var (
			row      domain.MarketQuoteWithItem
			qTs, iTs int64
			tradable sql.NullInt64
		)
		if err := rows.Scan(
			&row.Quote.ItemID, &row.Quote.ServerID, &row.Quote.Market,
			&row.Quote.MinPrice, &row.Quote.Median7d, &row.Quote.Low30d, &row.Quote.High30d,
			&row.Quote.Listings, &row.Quote.Change24h, &row.Quote.Change7d, &qTs,
			&row.Item.ID, &row.Item.Locale, &row.Item.Name, &row.Item.Grade, &row.Item.MainCategory,
			&row.Item.SubCategory, &row.Item.Icon, &row.Item.ItemLevel,
			&row.Item.LevelReq, &tradable, &row.Item.NPCBuyPrice, &iTs,
		); err != nil {
			return nil, err
		}
		if qTs > 0 {
			row.Quote.UpdatedAt = time.Unix(qTs, 0).UTC()
			row.Quote.HydrateFreshness(now)
		}
		if tradable.Valid {
			b := tradable.Int64 != 0
			row.Item.Tradable = &b
		}
		if iTs > 0 {
			row.Item.UpdatedAt = time.Unix(iTs, 0).UTC()
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *Store) UpsertMarketQuotes(ctx context.Context, quotes []domain.MarketQuote) error {
	if len(quotes) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO market_cache (item_id, server_id, market, min_price, median_7d, low_30d, high_30d, listings, change_24h, change_7d, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(item_id, server_id) DO UPDATE SET
			market = excluded.market,
			min_price = excluded.min_price,
			median_7d = excluded.median_7d,
			low_30d = excluded.low_30d,
			high_30d = excluded.high_30d,
			listings = excluded.listings,
			change_24h = excluded.change_24h,
			change_7d = excluded.change_7d,
			updated_at = excluded.updated_at`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	now := time.Now().Unix()
	for _, q := range quotes {
		ts := now
		if !q.UpdatedAt.IsZero() {
			ts = q.UpdatedAt.Unix()
		}
		if _, err := stmt.ExecContext(ctx, q.ItemID, q.ServerID, q.Market, q.MinPrice, q.Median7d, q.Low30d, q.High30d, q.Listings, q.Change24h, q.Change7d, ts); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// --- InventoryRepository ---

func (s *Store) GetCharacterStacks(ctx context.Context, characterKey string) ([]domain.InventoryStack, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT ci.character_key, ci.container, ci.instance_id, ci.item_id, COALESCE(i.name, ''), ci.count, ci.updated_at
		FROM character_inventory ci
		LEFT JOIN items i ON i.id = ci.item_id
		WHERE ci.character_key = ? AND ci.count > 0
		ORDER BY ci.container, ci.item_id`, characterKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stacks []domain.InventoryStack
	for rows.Next() {
		var st domain.InventoryStack
		var cont string
		var ts int64
		if err := rows.Scan(&st.CharacterKey, &cont, &st.InstanceID, &st.ItemID, &st.ItemName, &st.Count, &ts); err != nil {
			return nil, err
		}
		st.Container = domain.ContainerKind(cont)
		st.UpdatedAt = time.Unix(ts, 0).UTC()
		stacks = append(stacks, st)
	}
	return stacks, rows.Err()
}

func (s *Store) GetAggregatedInventory(ctx context.Context, characterKey string) (map[domain.ItemID]domain.AggregatedItem, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT ci.item_id, COALESCE(i.name, ''), COALESCE(i.grade, 0), COALESCE(i.main_category, ''),
		       SUM(CASE WHEN ci.container = 'bag' THEN ci.count ELSE 0 END) AS bag_cnt,
		       SUM(CASE WHEN ci.container != 'bag' THEN ci.count ELSE 0 END) AS storage_cnt,
		       SUM(ci.count) AS total_cnt
		FROM character_inventory ci
		LEFT JOIN items i ON i.id = ci.item_id
		WHERE ci.character_key = ? AND ci.count > 0
		GROUP BY ci.item_id`, characterKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[domain.ItemID]domain.AggregatedItem)
	for rows.Next() {
		var a domain.AggregatedItem
		if err := rows.Scan(&a.ItemID, &a.ItemName, &a.Grade, &a.Category, &a.BagCount, &a.StorageCount, &a.TotalCount); err != nil {
			return nil, err
		}
		out[a.ItemID] = a
	}
	return out, rows.Err()
}

func (s *Store) ReplaceContainerSnapshot(ctx context.Context, characterKey string, container domain.ContainerKind, stacks []domain.InventoryStack) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DELETE FROM character_inventory WHERE character_key = ? AND container = ?`, characterKey, string(container)); err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO character_inventory (character_key, container, instance_id, item_id, count, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(character_key, instance_id) DO UPDATE SET
			container = excluded.container,
			item_id = excluded.item_id,
			count = excluded.count,
			updated_at = excluded.updated_at`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	now := time.Now().Unix()
	for _, st := range stacks {
		if st.Count <= 0 {
			continue
		}
		ts := now
		if !st.UpdatedAt.IsZero() {
			ts = st.UpdatedAt.Unix()
		}
		if _, err := stmt.ExecContext(ctx, characterKey, string(container), st.InstanceID, st.ItemID, st.Count, ts); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ApplyStackUpdates(ctx context.Context, characterKey string, updates []domain.InventoryStack) error {
	if len(updates) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	delStmt, err := tx.PrepareContext(ctx, `DELETE FROM character_inventory WHERE character_key = ? AND instance_id = ?`)
	if err != nil {
		return err
	}
	defer delStmt.Close()

	upsStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO character_inventory (character_key, container, instance_id, item_id, count, updated_at)
		VALUES (?, 'bag', ?, ?, ?, ?)
		ON CONFLICT(character_key, instance_id) DO UPDATE SET
			item_id = excluded.item_id,
			count = excluded.count,
			updated_at = excluded.updated_at`)
	if err != nil {
		return err
	}
	defer upsStmt.Close()

	now := time.Now().Unix()
	for _, u := range updates {
		if u.Count <= 0 {
			if _, err := delStmt.ExecContext(ctx, characterKey, u.InstanceID); err != nil {
				return err
			}
			continue
		}
		ts := now
		if !u.UpdatedAt.IsZero() {
			ts = u.UpdatedAt.Unix()
		}
		if _, err := upsStmt.ExecContext(ctx, characterKey, u.InstanceID, u.ItemID, u.Count, ts); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) GetFileOffset(ctx context.Context, filePath string) (int64, string, error) {
	var offset int64
	var charKey string
	err := s.db.QueryRowContext(ctx, `SELECT last_offset, character_key FROM indexed_log_files WHERE file_path = ?`, filePath).Scan(&offset, &charKey)
	if err == sql.ErrNoRows {
		return 0, "", nil
	}
	return offset, charKey, err
}

func (s *Store) SaveFileOffset(ctx context.Context, filePath string, offset int64, charKey string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO indexed_log_files (file_path, last_offset, character_key, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(file_path) DO UPDATE SET
			last_offset = excluded.last_offset,
			character_key = CASE WHEN excluded.character_key != '' THEN excluded.character_key ELSE indexed_log_files.character_key END,
			updated_at = excluded.updated_at`,
		filePath, offset, charKey, time.Now().Unix())
	return err
}

// --- ProfessionRepository ---

func (s *Store) GetProfessions(ctx context.Context, characterKey string) ([]domain.ProfessionProgress, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT character_key, profession, mastery_grade, mastery_level, updated_at
		FROM character_professions WHERE character_key = ? ORDER BY profession`, characterKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.ProfessionProgress
	for rows.Next() {
		var p domain.ProfessionProgress
		var prof string
		var ts int64
		if err := rows.Scan(&p.CharacterKey, &prof, &p.MasteryGrade, &p.MasteryLevel, &ts); err != nil {
			return nil, err
		}
		p.Profession = domain.Profession(prof)
		p.MasteryGrade, p.MasteryLevel, p.DisplayLevel = domain.NormalizeMastery(p.MasteryGrade, p.MasteryLevel)
		p.UpdatedAt = time.Unix(ts, 0).UTC()
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) GetProfession(ctx context.Context, characterKey string, prof domain.Profession) (*domain.ProfessionProgress, error) {
	var p domain.ProfessionProgress
	var pr string
	var ts int64
	err := s.db.QueryRowContext(ctx, `
		SELECT character_key, profession, mastery_grade, mastery_level, updated_at
		FROM character_professions WHERE character_key = ? AND profession = ?`, characterKey, string(prof)).
		Scan(&p.CharacterKey, &pr, &p.MasteryGrade, &p.MasteryLevel, &ts)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p.Profession = domain.Profession(pr)
	p.MasteryGrade, p.MasteryLevel, p.DisplayLevel = domain.NormalizeMastery(p.MasteryGrade, p.MasteryLevel)
	p.UpdatedAt = time.Unix(ts, 0).UTC()
	return &p, nil
}

func (s *Store) UpsertProfession(ctx context.Context, p domain.ProfessionProgress) error {
	grade, internalLvl, _ := domain.NormalizeMastery(p.MasteryGrade, p.MasteryLevel)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO character_professions (character_key, profession, mastery_grade, mastery_level, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(character_key, profession) DO UPDATE SET
			mastery_grade = excluded.mastery_grade,
			mastery_level = excluded.mastery_level,
			updated_at = excluded.updated_at`,
		p.CharacterKey, string(p.Profession), grade, internalLvl, time.Now().Unix())
	return err
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanItem(r rowScanner) (domain.Item, error) {
	var (
		it       domain.Item
		tradable sql.NullInt64
		ts       int64
	)
	if err := r.Scan(&it.ID, &it.Locale, &it.Name, &it.Grade, &it.MainCategory, &it.SubCategory, &it.Icon, &it.ItemLevel, &it.LevelReq, &tradable, &it.NPCBuyPrice, &ts); err != nil {
		return domain.Item{}, err
	}
	if tradable.Valid {
		b := tradable.Int64 != 0
		it.Tradable = &b
	}
	if ts > 0 {
		it.UpdatedAt = time.Unix(ts, 0).UTC()
	}
	return it, nil
}
