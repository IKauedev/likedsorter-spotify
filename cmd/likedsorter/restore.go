package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"slices"
	"time"

	"github.com/ikauedeveloper/likedsorter/internal/backup"
	"github.com/ikauedeveloper/likedsorter/internal/config"
	"github.com/ikauedeveloper/likedsorter/internal/executor"
	"github.com/ikauedeveloper/likedsorter/internal/fsutil"
	"github.com/ikauedeveloper/likedsorter/internal/planner"
)

// runExport grava um snapshot JSON (somente leitura no Spotify).
//
//	export            → playlists gerenciadas (conteúdo e ordem), formato aceito por `restore`
//	export --liked    → suas curtidas (a partir do cache/sync)
func runExport(ctx context.Context, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	var d dataFlags
	d.register(fs)
	outFile := fs.String("out", "", "arquivo de saída (padrão: stdout)")
	liked := fs.Bool("liked", false, "exportar as curtidas em vez das playlists gerenciadas")
	if err := fs.Parse(args); err != nil {
		return err
	}

	var payload any
	if *liked {
		l, err := loadData(ctx, &d, false)
		if err != nil {
			return err
		}
		payload = map[string]any{"exported_at": time.Now().UTC(), "total": len(l.Sync.Tracks), "tracks": l.Sync.Tracks}
	} else {
		sess, err := newSession(ctx, d.market)
		if err != nil {
			return err
		}
		sstore, err := stateStore()
		if err != nil {
			return err
		}
		st, err := sstore.Load()
		if err != nil {
			return err
		}
		ex, err := planner.FetchManaged(ctx, sess.Client, st.KnownIDs())
		if err != nil {
			return err
		}
		snap, err := backup.FromExisting(ex, time.Now())
		if err != nil {
			return err
		}
		payload = snap
	}

	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	if *outFile == "" {
		_, err = out.Write(append(b, '\n'))
		return err
	}
	if err := fsutil.WriteFileAtomic(*outFile, b); err != nil {
		return err
	}
	fmt.Fprintf(out, "Exportado para %s\n", *outFile)
	return nil
}

// runRestore devolve playlists GERENCIADAS ao conteúdo e à ordem de um snapshot
// (backup automático do apply ou `export`). Antes de restaurar, salva o estado atual.
func runRestore(ctx context.Context, args []string, out io.Writer) error {
	return withJSON(args, out, func(a []string, text io.Writer) (any, error) { return restoreRun(ctx, a, text) })
}

func restoreRun(ctx context.Context, args []string, out io.Writer) (any, error) {
	fs := flag.NewFlagSet("restore", flag.ContinueOnError)
	file := fs.String("file", "", "snapshot JSON (de um backup do apply ou de `export`)")
	only := fs.String("playlist", "", "restaurar só esta playlist (ID)")
	yes := fs.Bool("yes", false, "não pedir confirmação")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if *file == "" {
		return nil, errors.New("informe --file com o snapshot (os backups ficam em <config>/backups/<data>/snapshot.json)")
	}
	snap, err := backup.Load(*file)
	if err != nil {
		return nil, err
	}

	sess, err := newSession(ctx, "from_token")
	if err != nil {
		return nil, err
	}
	sstore, err := stateStore()
	if err != nil {
		return nil, err
	}
	st, err := sstore.Load()
	if err != nil {
		return nil, err
	}
	// Lista as playlists atuais sem carregar itens (nenhum grupo desejado).
	current, err := planner.FetchExisting(ctx, sess.Client, nil, "", st.KnownIDs())
	if err != nil {
		return nil, err
	}

	type job struct {
		snap backup.Playlist
		cur  *planner.Existing
	}
	var jobs []job
	for _, sp := range snap.Playlists {
		if *only != "" && sp.ID != *only {
			continue
		}
		var cur *planner.Existing
		for i := range current {
			if current[i].ID == sp.ID {
				cur = &current[i]
			}
		}
		switch {
		case cur == nil:
			fmt.Fprintf(out, "- %q (%s): não existe mais na sua conta; pulando (o backup continua em %s)\n", sp.Name, sp.ID, *file)
			continue
		case !cur.Managed:
			fmt.Fprintf(out, "- %q (%s): não é gerenciada pelo likedsorter; recusando\n", sp.Name, sp.ID)
			continue
		}
		if err := planner.LoadItems(ctx, sess.Client, cur); err != nil {
			return nil, err
		}
		have := make([]string, len(cur.Items))
		for i, m := range cur.Items {
			have[i] = m.URI
		}
		if slices.Equal(have, sp.URIs()) {
			fmt.Fprintf(out, "- %q: já está igual ao backup\n", sp.Name)
			continue
		}
		fmt.Fprintf(out, "- %q: %d faixas hoje → %d no backup", sp.Name, len(have), len(sp.Items))
		if sp.LocalItems > 0 {
			fmt.Fprintf(out, " (%d arquivo(s) local(is) não podem ser restaurados)", sp.LocalItems)
		}
		fmt.Fprintln(out)
		jobs = append(jobs, job{sp, cur})
	}
	if len(jobs) == 0 {
		fmt.Fprintln(out, "Nada a restaurar.")
		return map[string]any{"restored": []string{}}, nil
	}
	if err := confirm(out, fmt.Sprintf("Isto vai reescrever %d playlist(s).", len(jobs)), *yes); err != nil {
		return nil, err
	}

	// Protege o estado atual antes de sobrescrever.
	var before []planner.Existing
	for _, j := range jobs {
		before = append(before, *j.cur)
	}
	dir, err := config.Dir()
	if err != nil {
		return nil, err
	}
	cur, err := backup.FromExisting(before, time.Now())
	if err != nil {
		return nil, err
	}
	path := backup.NewPath(dir, time.Now())
	if err := backup.Save(path, cur); err != nil {
		return nil, fmt.Errorf("não consegui salvar o backup do estado atual; nada foi alterado: %w", err)
	}
	fmt.Fprintf(out, "Estado atual salvo em %s\n", path)

	restored := []string{}
	for _, j := range jobs {
		fmt.Fprintf(out, "restaurando %q ... ", j.snap.Name)
		if _, err := executor.Rebuild(ctx, sess.Client, j.snap.ID, j.snap.Name, j.snap.URIs(), 0, nil); err != nil {
			fmt.Fprintln(out, "falhou")
			return nil, fmt.Errorf("%q: %w", j.snap.Name, err)
		}
		fmt.Fprintln(out, "ok")
		restored = append(restored, j.snap.Name)
	}
	return map[string]any{"restored": restored, "backup_of_previous_state": path}, nil
}
