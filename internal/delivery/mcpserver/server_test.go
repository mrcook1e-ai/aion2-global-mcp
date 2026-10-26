package mcpserver

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mrcook1e/aion2-global-mcp/internal/adapter/ncsoft"
	"github.com/mrcook1e/aion2-global-mcp/internal/adapter/questlogfs"
	"github.com/mrcook1e/aion2-global-mcp/internal/adapter/sqlite"
	"github.com/mrcook1e/aion2-global-mcp/internal/usecase"
)

func TestMCPServerInitAndListTools(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "mcp_test.sqlite"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer store.Close()

	qlFS := questlogfs.New(t.TempDir())
	resolver := usecase.NewResolver(store, store, nil, "ru")
	syncSvc := usecase.NewSyncService(store, store, nil, qlFS, resolver, nil)
	charSvc := usecase.NewCharacterService(qlFS, store, store, syncSvc, resolver)
	craftSvc := usecase.NewCraftService(store, store, store, charSvc, syncSvc, resolver)
	marketSvc := usecase.NewMarketScannerService(store, store, store, charSvc, syncSvc, resolver)
	farmSvc := usecase.NewFarmService(qlFS, charSvc, resolver)
	combatSvc := usecase.NewCombatService(qlFS, charSvc, syncSvc, resolver)

	srv := New(Deps{
		Catalog:  store,
		Resolver: resolver,
		Sync:     syncSvc,
		Char:     charSvc,
		Craft:    craftSvc,
		Market:   marketSvc,
		Farm:     farmSvc,
		Combat:   combatSvc,
		NCSoft:   ncsoft.New("eu", "en"),
	})

	t1, t2 := mcp.NewInMemoryTransports()
	serverSession, err := srv.Connect(ctx, t1, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer serverSession.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.2.0"}, nil)
	clientSession, err := client.Connect(ctx, t2, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer clientSession.Close()

	res, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(res.Tools) != 19 {
		t.Fatalf("registered tools = %d, want 19", len(res.Tools))
	}
}
