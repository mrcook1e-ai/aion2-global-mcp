// Package canvas generates self-contained interactive HTML5 visualizations
// that open in the system browser with zero background servers.
package canvas

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/mrcook1e/aion2-global-mcp/internal/domain"
	"github.com/mrcook1e/aion2-global-mcp/internal/usecase"
)

//go:embed crafting_tree.html
var craftingTreeTmpl string

// CanvasResult is returned to the MCP/HTTP caller after HTML generation.
type CanvasResult struct {
	View       string                  `json:"view"`
	FilePath   string                  `json:"filePath"`
	URL        string                  `json:"url,omitempty"` // populated when HTTP server is running
	Opened     bool                    `json:"opened"`
	Summary    string                  `json:"summary"`
	Evaluation *domain.CraftEvaluation `json:"evaluation,omitempty"`
}

// CraftingTreeState is the JSON payload injected into the HTML template.
type CraftingTreeState struct {
	Character  string                  `json:"character"`
	ServerID   int                     `json:"serverId"`
	Evaluation *domain.CraftEvaluation `json:"evaluation"`
	IconURLs   map[string]string       `json:"iconUrls"` // itemId → full CDN icon URL
}

const cdnBase = "https://cdn.questlog.gg/aion-2"

// iconCDNURL converts a Questlog API icon path to a full CDN URL.
// Input:  "/assets/Game/UI/Resource/Texture/Item/ETC/Icon_Item_Gather_Od_A_u_001.Icon_Item_Gather_Od_A_u_001"
// Output: "https://cdn.questlog.gg/aion-2/assets/Game/UI/Resource/Texture/Item/ETC/Icon_Item_Gather_Od_A_u_001.webp"
func iconCDNURL(iconPath string) string {
	if iconPath == "" {
		return ""
	}
	// Strip duplicate suffix after last dot (e.g. ".Icon_Item_Gather_Od_A_u_001")
	if i := strings.LastIndex(iconPath, "."); i > 0 {
		iconPath = iconPath[:i]
	}
	return cdnBase + iconPath + ".webp"
}

// Service generates and manages canvas HTML artifacts.
type Service struct {
	craftSvc *usecase.CraftService
	charSvc  *usecase.CharacterService
	catalog  domain.CatalogRepository
	httpAddr string // e.g. "127.0.0.1:8099"; empty = no HTTP
	outDir   string
}

// NewService creates a canvas service.
func NewService(craftSvc *usecase.CraftService, charSvc *usecase.CharacterService, catalog domain.CatalogRepository, httpAddr string) *Service {
	dir := defaultOutDir()
	_ = os.MkdirAll(dir, 0755)
	return &Service{
		craftSvc: craftSvc,
		charSvc:  charSvc,
		catalog:  catalog,
		httpAddr: httpAddr,
		outDir:   dir,
	}
}

func defaultOutDir() string {
	if runtime.GOOS == "windows" {
		if tmp := os.Getenv("TEMP"); tmp != "" {
			return filepath.Join(tmp, "opencode", "aion2-canvas")
		}
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "aion2-canvas")
}

// GenerateCraftingTree builds a self-contained HTML visualization
// of a recipe's DAG with craft-vs-buy analysis.
func (s *Service) GenerateCraftingTree(
	ctx context.Context,
	recipeID domain.RecipeID,
	recipeQuery string,
	character string,
	serverID int,
	autoOpen bool,
) (*CanvasResult, error) {
	// Evaluate recipe with full sub-craft tree
	ev, err := s.craftSvc.EvaluateSingleRecipe(ctx, recipeID, recipeQuery, character, serverID, "", nil, false)
	if err != nil {
		return nil, fmt.Errorf("evaluate recipe: %w", err)
	}

	// Determine character name
	charName := character
	if charName == "" {
		if ch, _ := s.charSvc.FindCharacter(ctx, ""); ch != nil {
			charName = ch.Name
		}
	}

	// Collect icon CDN URLs from SQLite catalog
	iconURLs := s.collectIconURLs(ctx, ev)

	state := CraftingTreeState{
		Character:  charName,
		ServerID:   serverID,
		Evaluation: ev,
		IconURLs:   iconURLs,
	}
	stateJSON, err := json.Marshal(state)
	if err != nil {
		return nil, fmt.Errorf("marshal state: %w", err)
	}
	tmpl, err := template.New("crafting_tree").Parse(craftingTreeTmpl)
	if err != nil {
		return nil, fmt.Errorf("parse template: %w", err)
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, map[string]any{
		"Title":     ev.ProductName,
		"StateJSON": template.JS(stateJSON),
	})
	if err != nil {
		return nil, fmt.Errorf("execute template: %w", err)
	}

	// Write HTML file
	filename := fmt.Sprintf("canvas_crafting_%d.html", ev.RecipeID)
	filePath := filepath.Join(s.outDir, filename)
	if err := os.WriteFile(filePath, buf.Bytes(), 0644); err != nil {
		return nil, fmt.Errorf("write html: %w", err)
	}

	result := &CanvasResult{
		View:       "crafting_tree",
		FilePath:   filePath,
		Evaluation: ev,
	}

	// Build URL if HTTP server is running
	if s.httpAddr != "" {
		result.URL = fmt.Sprintf("http://%s/canvas/crafting_tree?recipeId=%d", s.httpAddr, ev.RecipeID)
	}

	// Build summary
	profitLabel := "убыток"
	if ev.NetProfitMarket >= 0 {
		profitLabel = "профит"
	}
	result.Summary = fmt.Sprintf(
		"%s → %s: %+d Kinah %s, маржа %.1f%%, шанс %d%%, %d материалов",
		ev.RecipeName, ev.ProductName, ev.NetProfitMarket, profitLabel,
		ev.MarginPercent, int(ev.SuccessRate*100), len(ev.Ingredients),
	)

	// Auto-open in browser
	if autoOpen {
		result.Opened = openInBrowser(filePath)
	}

	return result, nil
}

// ServeHTTP returns the generated HTML content for an HTTP endpoint.
func (s *Service) ServeHTTP(
	ctx context.Context,
	recipeID domain.RecipeID,
	recipeQuery string,
	character string,
	serverID int,
) ([]byte, error) {
	ev, err := s.craftSvc.EvaluateSingleRecipe(ctx, recipeID, recipeQuery, character, serverID, "", nil, false)
	if err != nil {
		return nil, fmt.Errorf("evaluate recipe: %w", err)
	}

	charName := character
	if charName == "" {
		if ch, _ := s.charSvc.FindCharacter(ctx, ""); ch != nil {
			charName = ch.Name
		}
	}

	iconURLs := s.collectIconURLs(ctx, ev)

	state := CraftingTreeState{
		Character:  charName,
		ServerID:   serverID,
		Evaluation: ev,
		IconURLs:   iconURLs,
	}
	stateJSON, err := json.Marshal(state)
	if err != nil {
		return nil, err
	}

	tmpl, err := template.New("crafting_tree").Parse(craftingTreeTmpl)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, map[string]any{
		"Title":     ev.ProductName,
		"StateJSON": template.JS(stateJSON),
	})
	return buf.Bytes(), err
}

// collectIconURLs gathers CDN icon URLs for all items referenced in a CraftEvaluation.
func (s *Service) collectIconURLs(ctx context.Context, ev *domain.CraftEvaluation) map[string]string {
	iconURLs := make(map[string]string)
	if s.catalog == nil {
		return iconURLs
	}
	var itemIDs []domain.ItemID
	itemIDs = append(itemIDs, ev.ProductItemID)
	if ev.ComboItemID > 0 {
		itemIDs = append(itemIDs, ev.ComboItemID)
	}
	for _, ing := range ev.Ingredients {
		itemIDs = append(itemIDs, ing.ItemID)
	}
	items, _ := s.catalog.GetItemsByIDs(ctx, itemIDs)
	for _, it := range items {
		if it.Icon != "" {
			iconURLs[fmt.Sprintf("%d", it.ID)] = iconCDNURL(it.Icon)
		}
	}
	return iconURLs
}

// openInBrowser tries to open a local file in the default browser.
func openInBrowser(filePath string) bool {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", filePath)
	case "darwin":
		cmd = exec.Command("open", filePath)
	default:
		cmd = exec.Command("xdg-open", filePath)
	}
	return cmd.Start() == nil
}

// SupportedViews returns the list of canvas views for tool descriptions.
func SupportedViews() []string {
	return []string{"crafting_tree"}
}

// FormatResultText produces a rich GitHub-flavored Markdown card with details, economics, and shopping list for chat display.
func FormatResultText(r *CanvasResult) string {
	var sb strings.Builder
	sb.WriteString("### 📊 Interactive Visual Canvas\n\n")

	if r.URL != "" {
		sb.WriteString(fmt.Sprintf("🔗 **[Открыть интерактивный Canvas в браузере](%s)**\n\n", r.URL))
	} else if r.FilePath != "" {
		sb.WriteString(fmt.Sprintf("📁 **Локальный файл:** `%s`\n\n", r.FilePath))
	}

	ev := r.Evaluation
	if ev == nil {
		sb.WriteString(fmt.Sprintf("- **Режим:** `%s`\n", r.View))
		sb.WriteString(fmt.Sprintf("- **Сводка:** %s\n", r.Summary))
		return sb.String()
	}

	// Economic summary card
	profitSign := "+"
	if ev.NetProfitMarket < 0 {
		profitSign = ""
	}
	sb.WriteString("| Показатель | Значение |\n")
	sb.WriteString("|:---|:---|\n")
	sb.WriteString(fmt.Sprintf("| **Продукт** | **%s** (×%d) |\n", ev.ProductName, ev.ProductQty))
	sb.WriteString(fmt.Sprintf("| **Рецепт** | %s (`#%d`, %s, ур. %d) |\n", ev.RecipeName, ev.RecipeID, ev.Profession, ev.MasteryLevelReq))
	sb.WriteString(fmt.Sprintf("| **Себестоимость** | %s Kinah |\n", formatKinah(ev.FullMaterialCost)))
	sb.WriteString(fmt.Sprintf("| **Из кармана** | %s Kinah |\n", formatKinah(ev.OutOfPocketCost)))
	sb.WriteString(fmt.Sprintf("| **Чистая прибыль** | **%s%s Kinah** (маржа **%.1f%%**) |\n", profitSign, formatKinah(ev.NetProfitMarket), ev.MarginPercent))
	sb.WriteString(fmt.Sprintf("| **Шанс успеха** | %.0f%% (крит-прок: %.0f%%) |\n", ev.SuccessRate*100, ev.ComboProbability*100))
	sb.WriteString(fmt.Sprintf("| **Покрытие сумкой** | %.0f%% |\n\n", ev.MaterialsCoverage))

	if len(ev.Ingredients) > 0 {
		sb.WriteString("#### 📦 Материалы крафта:\n\n")
		sb.WriteString("| Ингредиент | Нужно | В сумке | Докупить | Цена/шт | Итого |\n")
		sb.WriteString("|:---|:---:|:---:|:---:|---:|---:|\n")
		for _, ing := range ev.Ingredients {
			status := "✅ Есть"
			if ing.MissingToBuyQty > 0 {
				status = fmt.Sprintf("🛒 **%d**", ing.MissingToBuyQty)
			}
			sb.WriteString(fmt.Sprintf("| %s | %d | %d | %s | %s ₭ | %s ₭ |\n",
				ing.Name, ing.RequiredQty, ing.OwnedQty, status,
				formatKinah(ing.UnitPrice), formatKinah(ing.FullMarketCost),
			))
		}
		sb.WriteString("\n")
	}

	if len(ev.Warnings) > 0 {
		sb.WriteString("⚠️ **Предупреждения:**\n")
		for _, w := range ev.Warnings {
			sb.WriteString(fmt.Sprintf("- %s\n", w))
		}
		sb.WriteString("\n")
	}

	sb.WriteString(fmt.Sprintf("> 💡 *Интерактивный граф со связями, зумом и симулятором открыт в браузере:* `%s`\n", r.FilePath))
	return sb.String()
}

func formatKinah(v int64) string {
	n := fmt.Sprintf("%d", v)
	if len(n) <= 3 {
		return n
	}
	var res []string
	rem := len(n) % 3
	if rem > 0 {
		res = append(res, n[:rem])
	}
	for i := rem; i < len(n); i += 3 {
		res = append(res, n[i:i+3])
	}
	return strings.Join(res, " ")
}
