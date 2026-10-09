package spotify

import (
	"context"
	"net/url"
	"strconv"
	"time"
)

// TimeRange é o período de /me/top.
type TimeRange string

const (
	RangeShort  TimeRange = "short_term"  // ~4 semanas
	RangeMedium TimeRange = "medium_term" // ~6 meses
	RangeLong   TimeRange = "long_term"   // ~1 ano ou mais
)

// ParseTimeRange aceita short|medium|long (e os nomes da API).
func ParseTimeRange(s string) (TimeRange, bool) {
	switch s {
	case "short", "short_term":
		return RangeShort, true
	case "medium", "medium_term", "":
		return RangeMedium, true
	case "long", "long_term":
		return RangeLong, true
	}
	return "", false
}

// TopTracks devolve as faixas mais ouvidas no período (GET /me/top/tracks, escopo user-top-read).
func (c *Client) TopTracks(ctx context.Context, tr TimeRange, offset, limit int) ([]TrackInfo, error) {
	q := url.Values{"time_range": {string(tr)}, "limit": {strconv.Itoa(limit)}, "offset": {strconv.Itoa(offset)}}
	var raw struct {
		Items []rawTrack `json:"items"`
	}
	if err := c.get(ctx, "/me/top/tracks", q, &raw); err != nil {
		return nil, err
	}
	out := make([]TrackInfo, 0, len(raw.Items))
	for _, it := range raw.Items {
		if it.ID != nil && !it.IsLocal {
			out = append(out, it.info())
		}
	}
	return out, nil
}

// TopArtists devolve os artistas mais ouvidos no período (GET /me/top/artists).
func (c *Client) TopArtists(ctx context.Context, tr TimeRange, offset, limit int) ([]Artist, error) {
	q := url.Values{"time_range": {string(tr)}, "limit": {strconv.Itoa(limit)}, "offset": {strconv.Itoa(offset)}}
	var raw struct {
		Items []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"items"`
	}
	if err := c.get(ctx, "/me/top/artists", q, &raw); err != nil {
		return nil, err
	}
	out := make([]Artist, len(raw.Items))
	for i, it := range raw.Items {
		out[i] = Artist{ID: it.ID, Name: it.Name}
	}
	return out, nil
}

// PlayEvent é uma reprodução (GET /me/player/recently-played).
type PlayEvent struct {
	PlayedAt time.Time
	Track    TrackInfo
}

// RecentlyPlayed devolve até 50 reproduções recentes. after (ms desde a época, 0 = sem filtro)
// pede só as posteriores. O Spotify guarda apenas as últimas 50, então chamar com frequência
// (ex.: via `auto`/`schedule`) é o que acumula histórico.
func (c *Client) RecentlyPlayed(ctx context.Context, afterMs int64) ([]PlayEvent, error) {
	q := url.Values{"limit": {"50"}}
	if afterMs > 0 {
		q.Set("after", strconv.FormatInt(afterMs, 10))
	}
	var raw struct {
		Items []struct {
			PlayedAt time.Time `json:"played_at"`
			Track    *rawTrack `json:"track"`
		} `json:"items"`
	}
	if err := c.get(ctx, "/me/player/recently-played", q, &raw); err != nil {
		return nil, err
	}
	out := make([]PlayEvent, 0, len(raw.Items))
	for _, it := range raw.Items {
		if it.Track == nil || it.Track.ID == nil || it.Track.IsLocal {
			continue
		}
		out = append(out, PlayEvent{PlayedAt: it.PlayedAt, Track: it.Track.info()})
	}
	return out, nil
}
