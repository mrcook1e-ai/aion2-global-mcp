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
	View     string `json:"view"`
	FilePath string `json:"filePath"`
	URL      string `json:"url,omitempty"` // populated when HTTP server is running
	Opened   bool   `json:"opened"`
	Summary  string `json:"summary"`
}

// CraftingTreeState is the JSON payload injected into the HTML template.
type CraftingTreeState struct {
	Character  string                  `json:"character"`
	ServerID   int                     `json:"serverId"`
	Evaluation *domain.CraftEvaluation `json:"evaluation"`
}

// Service generates and manages canvas HTML artifacts.
type Service struct {
	craftSvc *usecase.CraftService
	charSvc  *usecase.CharacterService
	httpAddr string // e.g. "127.0.0.1:8099"; empty = no HTTP
	outDir   string
}

// NewService creates a canvas service.
func NewService(craftSvc *usecase.CraftService, charSvc *usecase.CharacterService, httpAddr string) *Service {
	dir := defaultOutDir()
	_ = os.MkdirAll(dir, 0755)
	return &Service{
		craftSvc: craftSvc,
		charSvc:  charSvc,
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

	state := CraftingTreeState{
		Character:  charName,
		ServerID:   serverID,
		Evaluation: ev,
	}
	stateJSON, err := json.Marshal(state)
	if err != nil {
		return nil, fmt.Errorf("marshal state: %w", err)
	}

	// Render HTML template
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
		View:     "crafting_tree",
		FilePath: filePath,
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

	state := CraftingTreeState{
		Character:  charName,
		ServerID:   serverID,
		Evaluation: ev,
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

// FormatResultText produces the text block returned to the LLM agent.
func FormatResultText(r *CanvasResult) string {
	var sb strings.Builder
	sb.WriteString("📊 **Visual Canvas открыт!**\n\n")
	sb.WriteString(fmt.Sprintf("- **Режим:** %s\n", r.View))
	sb.WriteString(fmt.Sprintf("- **Файл:** `%s`\n", r.FilePath))
	if r.URL != "" {
		sb.WriteString(fmt.Sprintf("- **URL:** %s\n", r.URL))
	}
	sb.WriteString(fmt.Sprintf("- **Открыт в браузере:** %v\n", r.Opened))
	sb.WriteString(fmt.Sprintf("- **Сводка:** %s\n", r.Summary))
	return sb.String()
}
