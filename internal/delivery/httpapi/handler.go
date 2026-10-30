package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/mrcook1e/aion2-global-mcp/internal/delivery/canvas"
	"github.com/mrcook1e/aion2-global-mcp/internal/domain"
	"github.com/mrcook1e/aion2-global-mcp/internal/usecase"
)

type Handler struct {
	charSvc   *usecase.CharacterService
	craftSvc  *usecase.CraftService
	marketSvc *usecase.MarketScannerService
	farmSvc   *usecase.FarmService
	combatSvc *usecase.CombatService
	syncSvc   *usecase.SyncService
	canvasSvc *canvas.Service
}

func New(
	charSvc *usecase.CharacterService,
	craftSvc *usecase.CraftService,
	marketSvc *usecase.MarketScannerService,
	farmSvc *usecase.FarmService,
	combatSvc *usecase.CombatService,
	syncSvc *usecase.SyncService,
	canvasSvc *canvas.Service,
) *Handler {
	return &Handler{
		charSvc:   charSvc,
		craftSvc:  craftSvc,
		marketSvc: marketSvc,
		farmSvc:   farmSvc,
		combatSvc: combatSvc,
		syncSvc:   syncSvc,
		canvasSvc: canvasSvc,
	}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "service": "aion2-global-mcp", "version": "0.2.0"})
	})

	mux.HandleFunc("GET /api/v1/characters", func(w http.ResponseWriter, r *http.Request) {
		chars, err := h.charSvc.ListCharacters(r.Context())
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, chars)
	})

	mux.HandleFunc("GET /api/v1/character", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		ov, err := h.charSvc.GetCharacterOverview(r.Context(), q.Get("name"), q.Get("collections") == "true")
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, ov)
	})

	mux.HandleFunc("GET /api/v1/inventory", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		key, items, err := h.charSvc.GetInventory(r.Context(), q.Get("character"), q.Get("q"), q.Get("category"))
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"characterKey": key, "items": items})
	})

	mux.HandleFunc("GET /api/v1/craft/profitable", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		var lvlPtr *int
		if s := q.Get("level"); s != "" {
			if n, err := strconv.Atoi(s); err == nil {
				lvlPtr = &n
			}
		}
		srvID, _ := strconv.Atoi(q.Get("serverId"))
		topN, _ := strconv.Atoi(q.Get("top"))

		rep, err := h.craftSvc.ScanProfitableCrafts(r.Context(), usecase.ProfitableCraftsQuery{
			Character:    q.Get("character"),
			ServerID:     srvID,
			Profession:   q.Get("profession"),
			MasteryGrade: q.Get("grade"),
			MasteryLevel: lvlPtr,
			SubCategory:  q.Get("subCategory"),
			UseInventory: q.Get("useInventory") != "false",
			MultiStep:    q.Get("multiStep") == "true",
			SortBy:       q.Get("sortBy"),
			ForceRefresh: q.Get("refresh") == "true",
			TopN:         topN,
		})
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, rep)
	})

	mux.HandleFunc("GET /api/v1/craft/evaluate", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		recID, _ := strconv.ParseInt(q.Get("recipeId"), 10, 64)
		srvID, _ := strconv.Atoi(q.Get("serverId"))
		ev, err := h.craftSvc.EvaluateSingleRecipe(r.Context(), domain.RecipeID(recID), q.Get("q"), q.Get("character"), srvID, "", nil, q.Get("refresh") == "true")
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, ev)
	})

	mux.HandleFunc("GET /api/v1/market/deals", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		srvID, _ := strconv.Atoi(q.Get("serverId"))
		minList, _ := strconv.Atoi(q.Get("minListings"))
		rep, err := h.marketSvc.ScanMarketDeals(r.Context(), usecase.MarketDealsQuery{
			Character:     q.Get("character"),
			ServerID:      srvID,
			Category:      q.Get("category"),
			MinListings:   minList,
			RefreshSample: q.Get("refresh") == "true",
		})
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, rep)
	})

	mux.HandleFunc("GET /api/v1/market/compare", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		srvID, _ := strconv.Atoi(q.Get("homeServerId"))
		rep, err := h.marketSvc.CompareServerPrices(r.Context(), usecase.CompareServersQuery{
			Query:        q.Get("q"),
			Character:    q.Get("character"),
			HomeServerID: srvID,
			ForceRefresh: q.Get("refresh") == "true",
		})
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, rep)
	})

	mux.HandleFunc("GET /api/v1/combat/encounters", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		limit, _ := strconv.Atoi(q.Get("limit"))
		out, err := h.combatSvc.GetBossEncounters(r.Context(), usecase.BossEncounterQuery{
			EncounterID: q.Get("id"),
			Character:   q.Get("character"),
			BossQuery:   q.Get("boss"),
			OnlyKills:   q.Get("onlyKills") == "true",
			Limit:       limit,
		})
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	})

	mux.HandleFunc("GET /api/v1/combat/runs", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		limit, _ := strconv.Atoi(q.Get("limit"))
		out, err := h.combatSvc.GetDungeonRuns(r.Context(), usecase.DungeonRunsQuery{
			RunID:        q.Get("id"),
			Character:    q.Get("character"),
			DungeonQuery: q.Get("dungeon"),
			Limit:        limit,
		})
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	})

	mux.HandleFunc("GET /api/v1/combat/pvp", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		limit, _ := strconv.Atoi(q.Get("limit"))
		out, err := h.combatSvc.GetPvPLogs(r.Context(), usecase.PvPLogsQuery{
			PvPID:         q.Get("id"),
			Character:     q.Get("character"),
			OpponentQuery: q.Get("opponent"),
			Limit:         limit,
		})
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	})

	mux.HandleFunc("GET /api/v1/combat/bests", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		limit, _ := strconv.Atoi(q.Get("limit"))
		out, err := h.combatSvc.GetPersonalBests(r.Context(), q.Get("character"), q.Get("boss"), limit)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	})

	mux.HandleFunc("GET /api/v1/combat/session", func(w http.ResponseWriter, r *http.Request) {
		out, err := h.combatSvc.GetSessionTracker(r.Context())
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	})

	mux.HandleFunc("GET /api/v1/farm", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		srvID, _ := strconv.Atoi(q.Get("serverId"))
		days, _ := strconv.Atoi(q.Get("days"))
		rep, err := h.farmSvc.AnalyzeDropsValue(r.Context(), q.Get("character"), srvID, days)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, rep)
	})

	mux.HandleFunc("POST /api/v1/sync", func(w http.ResponseWriter, r *http.Request) {
		full := r.URL.Query().Get("full") == "true"
		var (
			sum usecase.SyncSummary
			err error
		)
		if full {
			sum, err = h.syncSvc.SyncFullCatalog(r.Context())
		} else {
			sum, err = h.syncSvc.SyncTelemetry(r.Context())
		}
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, sum)
	})

	// --- Canvas ---
	if h.canvasSvc != nil {
		mux.HandleFunc("GET /canvas/crafting_tree", func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			recID, _ := strconv.ParseInt(q.Get("recipeId"), 10, 64)
			srvID, _ := strconv.Atoi(q.Get("serverId"))
			html, err := h.canvasSvc.ServeHTTP(r.Context(), domain.RecipeID(recID), q.Get("q"), q.Get("character"), srvID)
			if err != nil {
				writeErr(w, http.StatusBadRequest, err)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(html)
		})
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func writeErr(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}
