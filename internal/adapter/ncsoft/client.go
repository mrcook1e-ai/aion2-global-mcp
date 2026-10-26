package ncsoft

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	aion2 "github.com/nuriland/aion2-api"
)

// Client wraps github.com/nuriland/aion2-api specifically for AION 2 Global shards (eu, nae, naw, sa, asia).
type Client struct {
	defaultRegion aion2.Region
	defaultLocale aion2.Locale

	mu      sync.Mutex
	clients map[string]*aion2.Client
}

func New(defaultRegion string, defaultLocale string) *Client {
	reg := aion2.Region(strings.ToLower(strings.TrimSpace(defaultRegion)))
	if reg == "" {
		reg = aion2.RegionEU
	}
	loc := aion2.Locale(strings.TrimSpace(defaultLocale))
	if loc == "" {
		loc = aion2.LocaleEN
	}
	return &Client{
		defaultRegion: reg,
		defaultLocale: loc,
		clients:       make(map[string]*aion2.Client),
	}
}

func (c *Client) get(region, locale string) (*aion2.Client, error) {
	r := c.defaultRegion
	if region != "" {
		r = aion2.Region(strings.ToLower(strings.TrimSpace(region)))
	}
	l := c.defaultLocale
	if locale != "" {
		l = aion2.Locale(strings.TrimSpace(locale))
	}
	key := string(r) + ":" + string(l)

	c.mu.Lock()
	defer c.mu.Unlock()
	if cl, ok := c.clients[key]; ok {
		return cl, nil
	}
	cl, err := aion2.New(aion2.ConfigOpts{
		Region:    r,
		Locale:    l,
		RateLimit: 5,
	})
	if err != nil {
		return nil, err
	}
	c.clients[key] = cl
	return cl, nil
}

type RemotePlayerInspection struct {
	Character  *aion2.Character         `json:"character,omitempty"`
	Equipment  *aion2.Equipment         `json:"equipment,omitempty"`
	Candidates []aion2.CharacterSummary `json:"candidates,omitempty"`
	Note       string                   `json:"note,omitempty"`
}

func (c *Client) FindAndInspectPlayer(ctx context.Context, region, locale, name, server string) (*RemotePlayerInspection, error) {
	cl, err := c.get(region, locale)
	if err != nil {
		return nil, err
	}

	name = strings.TrimSpace(name)
	q := aion2.CharacterSearch{Keyword: name, Size: 200}

	if server != "" {
		srv, err := matchServer(ctx, cl, server)
		if err != nil {
			return nil, err
		}
		q.ServerID = srv.ServerID
		q.RaceID = srv.RaceID
	}

	var exact, near []aion2.CharacterSummary
	seen := make(map[aion2.CharacterRef]bool)
	for page := 1; page <= 5; page++ {
		q.Page = page
		res, err := cl.SearchCharacters(ctx, q)
		if err != nil {
			return nil, err
		}
		for _, hit := range res.Items {
			if seen[hit.Ref] {
				continue
			}
			seen[hit.Ref] = true
			if strings.EqualFold(hit.Name, name) {
				exact = append(exact, hit)
			} else if len(near) < 10 {
				near = append(near, hit)
			}
		}
		if page >= res.Page.LastPage || len(res.Items) == 0 || (q.ServerID != 0 && len(exact) > 0) {
			break
		}
	}

	switch {
	case len(exact) > 1:
		return &RemotePlayerInspection{
			Candidates: exact,
			Note:       fmt.Sprintf("найдено %d игроков с точным именем %q — укажи server для выбора", len(exact), name),
		}, nil
	case len(exact) == 0:
		return &RemotePlayerInspection{
			Candidates: near,
			Note:       fmt.Sprintf("точное имя %q не найдено на регионе %s", name, cl.Region()),
		}, nil
	}

	ref := exact[0].Ref
	ch, err := cl.Character(ctx, ref)
	if err != nil {
		return nil, err
	}
	out := &RemotePlayerInspection{Character: ch}
	if eq, err := cl.Equipment(ctx, ref); err == nil {
		out.Equipment = eq
	} else {
		out.Note = fmt.Sprintf("профиль получен, но экипировка временно недоступна: %v", err)
	}
	return out, nil
}

func (c *Client) GetEquippedSlotDetail(ctx context.Context, region, locale string, serverID int, characterID string, itemID, slotPos, enchantLevel int) (*aion2.EquippedItem, error) {
	cl, err := c.get(region, locale)
	if err != nil {
		return nil, err
	}
	return cl.EquippedItem(ctx,
		aion2.CharacterRef{ServerID: serverID, CharacterID: characterID},
		aion2.EquipSlot{ItemID: itemID, SlotPos: slotPos, EnchantLevel: enchantLevel},
	)
}

func (c *Client) ListGlobalServers(ctx context.Context, region, locale string) ([]aion2.Server, error) {
	cl, err := c.get(region, locale)
	if err != nil {
		return nil, err
	}
	return cl.Servers(ctx)
}

func (c *Client) LatestPatchNotes(ctx context.Context, region, locale string, limit int) ([]aion2.Post, error) {
	cl, err := c.get(region, locale)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 20 {
		limit = 5
	}
	var out []aion2.Post
	for post, err := range cl.Posts(ctx, aion2.BoardPatchNotes) {
		if err != nil {
			return out, err
		}
		out = append(out, post)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (c *Client) GetPostBody(ctx context.Context, region, locale, board, postID string) (*aion2.Post, error) {
	cl, err := c.get(region, locale)
	if err != nil {
		return nil, err
	}
	b := aion2.Board(board)
	if b == "" {
		b = aion2.BoardPatchNotes
	}
	return cl.Post(ctx, b, postID)
}

func matchServer(ctx context.Context, cl *aion2.Client, nameOrID string) (aion2.Server, error) {
	servers, err := cl.Servers(ctx)
	if err != nil {
		return aion2.Server{}, err
	}
	nameOrID = strings.TrimSpace(nameOrID)
	for _, s := range servers {
		if strings.EqualFold(s.Name, nameOrID) || strings.EqualFold(s.ShortName, nameOrID) || strconv.Itoa(s.ServerID) == nameOrID {
			return s, nil
		}
	}
	return aion2.Server{}, fmt.Errorf("сервер %q не найден на регионе %s", nameOrID, cl.Region())
}
