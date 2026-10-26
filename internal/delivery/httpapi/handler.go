package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/mrcook1e/aion2-global-mcp/internal/usecase"
)

type Handler struct {
	charSvc  *usecase.CharacterService
	craftSvc *usecase.CraftService
	farmSvc  *usecase.FarmService
	syncSvc  *usecase.SyncService
}

func New(charSvc *usecase.CharacterService, craftSvc *usecase.CraftService, farmSvc *usecase.FarmService, syncSvc *usecase.SyncService) *Handler {
	return &Handler{
		charSvc:  charSvc,
		craftSvc: craftSvc,
		farmSvc:  farmSvc,
		syncSvc:  syncSvc,
	}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "service": "aion2-global-mcp"})
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
		ov, err := h.charSvc.GetCharacterOverview(r.Context(), r.URL.Query().Get("name"))
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
