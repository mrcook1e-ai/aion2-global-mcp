package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mrcook1e/aion2-global-mcp/internal/adapter/ncsoft"
	"github.com/mrcook1e/aion2-global-mcp/internal/adapter/questlogapi"
	"github.com/mrcook1e/aion2-global-mcp/internal/adapter/questlogfs"
	"github.com/mrcook1e/aion2-global-mcp/internal/adapter/sqlite"
	"github.com/mrcook1e/aion2-global-mcp/internal/delivery/httpapi"
	"github.com/mrcook1e/aion2-global-mcp/internal/delivery/mcpserver"
	"github.com/mrcook1e/aion2-global-mcp/internal/usecase"
)

func defaultDBPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".aion2_global", "aion2_catalog.sqlite")
}

func main() {
	var (
		dbPath       = flag.String("db", defaultDBPath(), "SQLite database path (compatible with existing aion2_catalog.sqlite)")
		questlogDir  = flag.String("questlog-dir", questlogfs.DefaultDataDir(), "Path to %APPDATA%\\Questlog\\aion-2")
		locale       = flag.String("locale", "ru", "Catalog locale for Questlog dictionary (ru or en)")
		globalRegion = flag.String("region", "eu", "Default AION 2 Global shard for NCSoft Web API (eu, nae, naw, sa, asia)")
		httpAddr     = flag.String("http", "", "Serve Streamable HTTP MCP (/mcp) + REST API (/api/v1/*) on this address (e.g. 0.0.0.0:8090); empty runs stdio MCP")
		syncFullOnce = flag.Bool("sync-catalog", false, "Download/refresh full Questlog item & recipe catalog into SQLite and exit")
		watchSec     = flag.Int("watch-interval", 10, "Background .ndjson inventory tail interval in seconds (0 disables)")
		rps          = flag.Float64("rps", 12, "Max requests/sec to questlog.gg tRPC")
	)
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	store, err := sqlite.Open(ctx, *dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "aion2d: open sqlite: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	qlAPI := questlogapi.New(*rps)
	qlFS := questlogfs.New(*questlogDir)
	ncClient := ncsoft.New(*globalRegion, "en")

	resolver := usecase.NewResolver(store, store, qlAPI, *locale)
	syncSvc := usecase.NewSyncService(store, store, qlAPI, qlFS, resolver, logger)
	charSvc := usecase.NewCharacterService(qlFS, store, store, syncSvc, resolver)
	craftSvc := usecase.NewCraftService(store, store, store, charSvc, syncSvc, resolver)
	farmSvc := usecase.NewFarmService(qlFS, charSvc, resolver)

	if *syncFullOnce {
		logger.Info("starting full catalog sync", "db", *dbPath, "locale", *locale)
		sum, err := syncSvc.SyncFullCatalog(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "aion2d: sync failed: %v\n", err)
			os.Exit(1)
		}
		logger.Info("catalog sync complete",
			"items", sum.ItemsUpserted,
			"recipes", sum.RecipesUpserted,
			"entities", sum.EntitiesUpserted,
			"invEvents", sum.InvEventsApplied,
			"durationMs", sum.DurationMs,
		)
		return
	}

	if *watchSec > 0 {
		go syncSvc.RunBackgroundWatcher(ctx, time.Duration(*watchSec)*time.Second)
	}

	mcpSrv := mcpserver.New(mcpserver.Deps{
		Catalog:  store,
		Resolver: resolver,
		Sync:     syncSvc,
		Char:     charSvc,
		Craft:    craftSvc,
		Farm:     farmSvc,
		NCSoft:   ncClient,
	})

	if *httpAddr != "" {
		mux := http.NewServeMux()
		mux.Handle("/mcp", mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return mcpSrv }, nil))
		httpapi.New(charSvc, craftSvc, farmSvc, syncSvc).RegisterRoutes(mux)

		if err := runHTTPServer(ctx, *httpAddr, mux, logger); err != nil {
			fmt.Fprintf(os.Stderr, "aion2d: http server: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if err := mcpSrv.Run(ctx, &mcp.StdioTransport{}); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintf(os.Stderr, "aion2d: mcp stdio: %v\n", err)
		os.Exit(1)
	}
}

func runHTTPServer(ctx context.Context, addr string, handler http.Handler, logger *slog.Logger) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutCtx); err != nil {
			_ = srv.Close()
		}
	}()

	logger.Info("aion2d listening",
		"mcp_endpoint", fmt.Sprintf("http://%s/mcp", ln.Addr()),
		"rest_api", fmt.Sprintf("http://%s/api/v1/health", ln.Addr()),
	)
	if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	<-done
	return nil
}
