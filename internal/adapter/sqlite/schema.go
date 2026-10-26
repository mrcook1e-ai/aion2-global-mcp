package sqlite

import (
	"context"
	"database/sql"
	"strings"
)

const baseDDL = `
PRAGMA journal_mode = WAL;
PRAGMA synchronous = NORMAL;
PRAGMA foreign_keys = ON;
PRAGMA busy_timeout = 5000;

CREATE TABLE IF NOT EXISTS items (
    id INTEGER PRIMARY KEY,
    locale TEXT NOT NULL DEFAULT 'ru',
    name TEXT NOT NULL,
    grade INTEGER DEFAULT 0,
    main_category TEXT DEFAULT '',
    sub_category TEXT DEFAULT '',
    icon TEXT DEFAULT '',
    item_level INTEGER DEFAULT 0,
    level_req INTEGER DEFAULT 0,
    tradable INTEGER,
    updated_at INTEGER DEFAULT 0,
    raw_json TEXT
);
CREATE INDEX IF NOT EXISTS idx_items_name ON items(name COLLATE NOCASE);
CREATE INDEX IF NOT EXISTS idx_items_cat ON items(main_category, sub_category);
CREATE INDEX IF NOT EXISTS idx_items_grade ON items(grade);

CREATE TABLE IF NOT EXISTS recipes (
    id INTEGER PRIMARY KEY,
    locale TEXT NOT NULL DEFAULT 'ru',
    name TEXT NOT NULL,
    grade INTEGER DEFAULT 0,
    main_category TEXT DEFAULT '',
    sub_category TEXT DEFAULT '',
    qualification_race TEXT DEFAULT '',
    mastery_grade TEXT DEFAULT '',
    mastery_level INTEGER DEFAULT 0,
    gold_cost INTEGER DEFAULT 0,
    is_guild_craft INTEGER DEFAULT 0,
    success_rates_json TEXT DEFAULT '',
    product_item_id INTEGER DEFAULT 0,
    product_item_name TEXT DEFAULT '',
    product_quantity INTEGER DEFAULT 1,
    combo_item_id INTEGER DEFAULT 0,
    combo_item_name TEXT DEFAULT '',
    combo_probability REAL DEFAULT 0.0,
    raw_json TEXT
);
CREATE INDEX IF NOT EXISTS idx_recipes_prod ON recipes(product_item_id);
CREATE INDEX IF NOT EXISTS idx_recipes_combo ON recipes(combo_item_id);
CREATE INDEX IF NOT EXISTS idx_recipes_prof_lvl ON recipes(main_category, mastery_level);
CREATE INDEX IF NOT EXISTS idx_recipes_name ON recipes(name COLLATE NOCASE);

CREATE TABLE IF NOT EXISTS recipe_ingredients (
    recipe_id INTEGER NOT NULL,
    item_id INTEGER NOT NULL,
    item_name TEXT NOT NULL DEFAULT '',
    quantity INTEGER NOT NULL DEFAULT 1,
    grade INTEGER DEFAULT 0,
    craftable_recipe_id INTEGER DEFAULT 0,
    PRIMARY KEY (recipe_id, item_id)
);
CREATE INDEX IF NOT EXISTS idx_ingr_item ON recipe_ingredients(item_id);

CREATE TABLE IF NOT EXISTS entities (
    kind TEXT NOT NULL,
    id INTEGER NOT NULL,
    locale TEXT NOT NULL DEFAULT 'ru',
    name TEXT NOT NULL,
    category TEXT DEFAULT '',
    meta_json TEXT DEFAULT '',
    updated_at INTEGER DEFAULT 0,
    PRIMARY KEY (kind, id, locale)
);
CREATE INDEX IF NOT EXISTS idx_entities_name ON entities(kind, name COLLATE NOCASE);

CREATE TABLE IF NOT EXISTS market_cache (
    item_id INTEGER NOT NULL,
    server_id INTEGER NOT NULL,
    market TEXT DEFAULT 'market',
    min_price INTEGER DEFAULT 0,
    median_7d INTEGER DEFAULT 0,
    low_30d INTEGER DEFAULT 0,
    high_30d INTEGER DEFAULT 0,
    listings INTEGER DEFAULT 0,
    change_24h REAL DEFAULT 0,
    change_7d REAL DEFAULT 0,
    updated_at INTEGER DEFAULT 0,
    PRIMARY KEY (item_id, server_id)
);

CREATE TABLE IF NOT EXISTS character_inventory (
    character_key TEXT NOT NULL,
    container TEXT NOT NULL DEFAULT 'bag',
    instance_id INTEGER NOT NULL,
    item_id INTEGER NOT NULL,
    count INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    PRIMARY KEY (character_key, instance_id)
);
CREATE INDEX IF NOT EXISTS idx_char_inv_item ON character_inventory(character_key, item_id);

CREATE TABLE IF NOT EXISTS indexed_log_files (
    file_path TEXT PRIMARY KEY,
    last_offset INTEGER NOT NULL DEFAULT 0,
    character_key TEXT NOT NULL DEFAULT '',
    updated_at INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS character_professions (
    character_key TEXT NOT NULL,
    profession TEXT NOT NULL,
    mastery_grade TEXT NOT NULL DEFAULT '',
    mastery_level INTEGER NOT NULL DEFAULT 0,
    updated_at INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (character_key, profession)
);
`

// extraAlterColumns upgrades an existing Python-created aion2_catalog.sqlite seamlessly in-place.
var extraAlterColumns = []string{
	`ALTER TABLE items ADD COLUMN locale TEXT NOT NULL DEFAULT 'ru'`,
	`ALTER TABLE items ADD COLUMN tradable INTEGER`,
	`ALTER TABLE items ADD COLUMN updated_at INTEGER DEFAULT 0`,
	`ALTER TABLE recipes ADD COLUMN locale TEXT NOT NULL DEFAULT 'ru'`,
	`ALTER TABLE recipes ADD COLUMN is_guild_craft INTEGER DEFAULT 0`,
	`ALTER TABLE recipes ADD COLUMN success_rates_json TEXT DEFAULT ''`,
	`ALTER TABLE recipe_ingredients ADD COLUMN craftable_recipe_id INTEGER DEFAULT 0`,
	`ALTER TABLE market_cache ADD COLUMN market TEXT DEFAULT 'market'`,
	`ALTER TABLE market_cache ADD COLUMN low_30d INTEGER DEFAULT 0`,
	`ALTER TABLE market_cache ADD COLUMN high_30d INTEGER DEFAULT 0`,
	`ALTER TABLE market_cache ADD COLUMN change_24h REAL DEFAULT 0`,
	`ALTER TABLE market_cache ADD COLUMN change_7d REAL DEFAULT 0`,
}

func migrate(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, baseDDL); err != nil {
		return err
	}
	for _, stmt := range extraAlterColumns {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			if !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
				return err
			}
		}
	}
	return nil
}
