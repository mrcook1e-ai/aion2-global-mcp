package questlogfs

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mrcook1e/aion2-global-mcp/internal/domain"
)

var (
	markerInventory = []byte(`"inventory`)
	markerSelfName  = []byte(`"selfName"`)
	markerSelfTrue  = []byte(`"self":true`)
)

// SyncNDJSONInventory incrementally scans combat-logs/*.ndjson from their saved byte offsets,
// coalescing all stack updates per file in memory and flushing once per file for sub-second speed.
func (r *Reader) SyncNDJSONInventory(
	ctx context.Context,
	invRepo domain.InventoryRepository,
	knownChars []domain.Character,
) (filesProcessed int, eventsApplied int, err error) {
	logsDir := filepath.Join(r.dataDir, "combat-logs")
	files, err := filepath.Glob(filepath.Join(logsDir, "*.ndjson"))
	if err != nil || len(files) == 0 {
		return 0, 0, err
	}
	sort.Strings(files)

	charByName := make(map[string]string, len(knownChars))
	for _, ch := range knownChars {
		charByName[strings.ToLower(ch.Name)] = ch.Key
	}

	for _, fp := range files {
		if ctx.Err() != nil {
			return filesProcessed, eventsApplied, ctx.Err()
		}
		st, err := os.Stat(fp)
		if err != nil || st.IsDir() {
			continue
		}

		savedOffset, savedCharKey, err := invRepo.GetFileOffset(ctx, fp)
		if err != nil {
			return filesProcessed, eventsApplied, err
		}
		if st.Size() < savedOffset {
			savedOffset = 0
		}
		if st.Size() == savedOffset {
			continue
		}

		applied, newOffset, charKey, err := processNDJSONFile(ctx, fp, savedOffset, savedCharKey, charByName, invRepo)
		if err != nil {
			return filesProcessed, eventsApplied, err
		}
		if err := invRepo.SaveFileOffset(ctx, fp, newOffset, charKey); err != nil {
			return filesProcessed, eventsApplied, err
		}
		filesProcessed++
		eventsApplied += applied
	}
	return filesProcessed, eventsApplied, nil
}

type ndjsonInvEvent struct {
	Type   string `json:"type"`
	At     int64  `json:"at"`
	Stacks []struct {
		Instance int64 `json:"instance"`
		Item     int64 `json:"item"`
		Count    int64 `json:"count"`
	} `json:"stacks"`
}

func processNDJSONFile(
	ctx context.Context,
	filePath string,
	startOffset int64,
	charKey string,
	charByName map[string]string,
	invRepo domain.InventoryRepository,
) (applied int, endOffset int64, resolvedCharKey string, err error) {
	f, err := os.Open(filePath)
	if err != nil {
		return 0, startOffset, charKey, err
	}
	defer f.Close()

	if startOffset > 0 {
		if _, err := f.Seek(startOffset, io.SeekStart); err != nil {
			return 0, startOffset, charKey, err
		}
	}

	resolvedCharKey = charKey
	var events []ndjsonInvEvent

	reader := bufio.NewReaderSize(f, 256*1024)
	offset := startOffset

	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 {
			if line[len(line)-1] != '\n' && readErr == io.EOF {
				break
			}
			offset += int64(len(line))

			if bytes.Contains(line, markerSelfName) || bytes.Contains(line, markerSelfTrue) {
				var hdr struct {
					SelfName string `json:"selfName"`
					Self     bool   `json:"self"`
					Name     string `json:"name"`
					Server   int    `json:"server"`
				}
				if json.Unmarshal(line, &hdr) == nil {
					if hdr.Self && hdr.Name != "" && hdr.Server > 0 {
						resolvedCharKey = fmt.Sprintf("%d:%s", hdr.Server, hdr.Name)
					} else if hdr.SelfName != "" && resolvedCharKey == "" {
						if k, ok := charByName[strings.ToLower(hdr.SelfName)]; ok {
							resolvedCharKey = k
						} else {
							resolvedCharKey = hdr.SelfName
						}
					}
				}
			}

			if bytes.Contains(line, markerInventory) {
				var ev ndjsonInvEvent
				if json.Unmarshal(line, &ev) == nil && (ev.Type == "inventory" || ev.Type == "inventory-update") {
					events = append(events, ev)
				}
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			return 0, offset, resolvedCharKey, readErr
		}
	}

	if resolvedCharKey == "" || len(events) == 0 {
		return 0, offset, resolvedCharKey, nil
	}

	if err := applyBatchEventsInMemoryAndFlush(ctx, invRepo, resolvedCharKey, events); err != nil {
		return 0, startOffset, resolvedCharKey, err
	}
	return len(events), offset, resolvedCharKey, nil
}

// applyBatchEventsInMemoryAndFlush loads the character's current stacks once, replays all events in RAM
// (keeping Bag and Storage separated by InstanceID overlap), and flushes the final state in at most two SQLite transactions.
func applyBatchEventsInMemoryAndFlush(
	ctx context.Context,
	invRepo domain.InventoryRepository,
	charKey string,
	events []ndjsonInvEvent,
) error {
	existing, err := invRepo.GetCharacterStacks(ctx, charKey)
	if err != nil {
		return err
	}

	byInst := make(map[domain.InstanceID]domain.InventoryStack, len(existing)+256)
	for _, st := range existing {
		byInst[st.InstanceID] = st
	}

	bagTouched := false
	storageTouched := false
	var dirtyUpdates []domain.InventoryStack
	hasSnapshot := false

	for _, ev := range events {
		ts := time.Now().UTC()
		if ev.At > 0 {
			ts = time.UnixMilli(ev.At).UTC()
		}

		if ev.Type == "inventory-update" {
			for _, s := range ev.Stacks {
				if s.Instance <= 0 || s.Item <= 0 {
					continue
				}
				instID := domain.InstanceID(s.Instance)
				if s.Count <= 0 {
					if prev, ok := byInst[instID]; ok {
						if prev.Container == domain.ContainerStorage {
							storageTouched = true
						} else {
							bagTouched = true
						}
						delete(byInst, instID)
					}
					if !hasSnapshot {
						dirtyUpdates = append(dirtyUpdates, domain.InventoryStack{
							CharacterKey: charKey,
							InstanceID:   instID,
							ItemID:       domain.ItemID(s.Item),
							Count:        0,
							UpdatedAt:    ts,
						})
					}
					continue
				}

				cont := domain.ContainerBag
				if prev, ok := byInst[instID]; ok {
					cont = prev.Container
				}
				st := domain.InventoryStack{
					CharacterKey: charKey,
					Container:    cont,
					InstanceID:   instID,
					ItemID:       domain.ItemID(s.Item),
					Count:        s.Count,
					UpdatedAt:    ts,
				}
				byInst[instID] = st
				if cont == domain.ContainerStorage {
					storageTouched = true
				} else {
					bagTouched = true
				}
				if !hasSnapshot {
					dirtyUpdates = append(dirtyUpdates, st)
				}
			}
			continue
		}

		// Full container snapshot ("inventory"): classify Bag vs Storage by InstanceID overlap!
		hasSnapshot = true
		incoming := make(map[domain.InstanceID]domain.InventoryStack, len(ev.Stacks))
		for _, s := range ev.Stacks {
			if s.Instance <= 0 || s.Item <= 0 {
				continue
			}
			instID := domain.InstanceID(s.Instance)
			incoming[instID] = domain.InventoryStack{
				CharacterKey: charKey,
				InstanceID:   instID,
				ItemID:       domain.ItemID(s.Item),
				Count:        s.Count,
				UpdatedAt:    ts,
			}
		}

		var bagTotal, bagOverlap, storageOverlap int
		for instID, ex := range byInst {
			if ex.Count <= 0 {
				continue
			}
			_, hit := incoming[instID]
			if ex.Container == domain.ContainerBag {
				bagTotal++
				if hit {
					bagOverlap++
				}
			} else if hit {
				storageOverlap++
			}
		}

		target := domain.ContainerBag
		if bagTotal > 0 && bagOverlap == 0 && storageOverlap >= 0 {
			target = domain.ContainerStorage
		}

		// Wipe previous stacks belonging ONLY to target container, then insert new positive stacks
		for instID, ex := range byInst {
			if ex.Container == target {
				delete(byInst, instID)
			}
		}
		for instID, st := range incoming {
			if st.Count > 0 {
				st.Container = target
				byInst[instID] = st
			}
		}
		if target == domain.ContainerStorage {
			storageTouched = true
		} else {
			bagTouched = true
		}
	}

	if !hasSnapshot {
		return invRepo.ApplyStackUpdates(ctx, charKey, dirtyUpdates)
	}

	if bagTouched {
		var bagStacks []domain.InventoryStack
		for _, st := range byInst {
			if st.Container == domain.ContainerBag && st.Count > 0 {
				bagStacks = append(bagStacks, st)
			}
		}
		if err := invRepo.ReplaceContainerSnapshot(ctx, charKey, domain.ContainerBag, bagStacks); err != nil {
			return err
		}
	}
	if storageTouched {
		var storageStacks []domain.InventoryStack
		for _, st := range byInst {
			if st.Container == domain.ContainerStorage && st.Count > 0 {
				storageStacks = append(storageStacks, st)
			}
		}
		if err := invRepo.ReplaceContainerSnapshot(ctx, charKey, domain.ContainerStorage, storageStacks); err != nil {
			return err
		}
	}
	return nil
}
