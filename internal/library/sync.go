package library

import (
	"context"
	"fmt"
	"time"

	"github.com/ikauedeveloper/likedsorter/internal/cache"
	"github.com/ikauedeveloper/likedsorter/internal/spotify"
)

const (
	tracksFile      = "tracks.json"
	snapshotVersion = 1
)

// Snapshot é o que fica em disco entre execuções.
type Snapshot struct {
	Version  int                  `json:"version"`
	SyncedAt time.Time            `json:"synced_at"`
	Stats    Stats                `json:"stats"`  // acumulado; Stats.Fetched = itens vistos no Spotify
	Tracks   []spotify.SavedTrack `json:"tracks"` // mais recente primeiro
}

// SyncOptions controla Sync.
type SyncOptions struct {
	Full     bool          // --full-sync: relê tudo
	TTL      time.Duration // idade máxima do snapshot antes de forçar leitura completa (0 = nunca)
	Now      func() time.Time
	Progress func(done, total int)
}

// SyncResult descreve o que Sync fez.
type SyncResult struct {
	Tracks   []spotify.SavedTrack
	Total    int    // total informado pelo Spotify
	Stats    Stats  // acumulado (inclui o que já estava em cache)
	Mode     string // "full" ou "incremental"
	Reason   string // por que foi completo (vazio no incremental)
	New      int    // faixas novas nesta execução (incremental) ou total (full)
	SyncedAt time.Time
}

// Sync devolve as curtidas usando o cache quando possível:
//
//   - sem cache, --full-sync, TTL vencido ou --refresh/--no-cache → leitura completa;
//   - caso contrário lê só até a primeira faixa já conhecida (mesmo ID e added_at).
//
// O incremental não enxerga descurtidas por si só; por isso confere o total:
// itens vistos antes + itens novos deve ser igual ao total do Spotify, senão
// cai para leitura completa. O TTL cobre o caso residual (descurtir e curtir
// outra música entre dois syncs mantém o total igual).
func Sync(ctx context.Context, src TrackSource, store *cache.Store, opt SyncOptions) (*SyncResult, error) {
	now := opt.Now
	if now == nil {
		now = time.Now
	}

	var snap Snapshot
	found, err := store.ReadJSON(tracksFile, &snap)
	if err != nil {
		return nil, err
	}

	reason := ""
	switch {
	case opt.Full:
		reason = "--full-sync"
	case !found || snap.Version != snapshotVersion:
		reason = "sem cache"
	case cache.Expired(snap.SyncedAt, opt.TTL, now()):
		reason = "cache vencido (TTL)"
	}

	if reason == "" {
		known := make(map[string]time.Time, len(snap.Tracks))
		for _, t := range snap.Tracks {
			known[t.ID] = t.AddedAt
		}
		res, err := Collect(ctx, src, Options{
			Progress: opt.Progress,
			StopAt: func(t spotify.SavedTrack) bool {
				at, ok := known[t.ID]
				return ok && t.ID != "" && at.Equal(t.AddedAt)
			},
		})
		if err != nil {
			return nil, err
		}
		switch {
		case !res.Stopped:
			reason = "nenhuma faixa conhecida encontrada"
		case snap.Stats.Fetched+res.Stats.Fetched != res.Total:
			reason = fmt.Sprintf("total divergente (cache %d + novas %d ≠ Spotify %d; possíveis descurtidas)",
				snap.Stats.Fetched, res.Stats.Fetched, res.Total)
		default:
			return finish(store, merge(snap, res, now()), "incremental", "", len(res.Tracks))
		}
	}

	res, err := Collect(ctx, src, Options{Progress: opt.Progress})
	if err != nil {
		return nil, err
	}
	return finish(store, Snapshot{Version: snapshotVersion, SyncedAt: now(), Stats: res.Stats, Tracks: res.Tracks},
		"full", reason, len(res.Tracks))
}

// merge coloca as faixas novas à frente das antigas. Uma faixa re-curtida
// (mesmo ID, novo added_at) substitui a antiga.
func merge(old Snapshot, res *Result, at time.Time) Snapshot {
	fresh := make(map[string]struct{}, len(res.Tracks))
	for _, t := range res.Tracks {
		fresh[t.ID] = struct{}{}
	}
	tracks := make([]spotify.SavedTrack, 0, len(res.Tracks)+len(old.Tracks))
	tracks = append(tracks, res.Tracks...)
	for _, t := range old.Tracks {
		if _, replaced := fresh[t.ID]; !replaced {
			tracks = append(tracks, t)
		}
	}
	s := old.Stats
	s.Fetched += res.Stats.Fetched
	s.Local += res.Stats.Local
	s.Unavailable += res.Stats.Unavailable
	s.NoID += res.Stats.NoID
	s.Duplicates += res.Stats.Duplicates
	return Snapshot{Version: snapshotVersion, SyncedAt: at, Stats: s, Tracks: tracks}
}

func finish(store *cache.Store, snap Snapshot, mode, reason string, added int) (*SyncResult, error) {
	if err := store.WriteJSON(tracksFile, snap); err != nil {
		return nil, fmt.Errorf("gravar cache de faixas: %w", err)
	}
	return &SyncResult{
		Tracks: snap.Tracks, Total: snap.Stats.Fetched, Stats: snap.Stats,
		Mode: mode, Reason: reason, New: added, SyncedAt: snap.SyncedAt,
	}, nil
}
