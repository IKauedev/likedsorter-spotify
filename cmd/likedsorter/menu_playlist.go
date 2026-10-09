package main

import (
	"fmt"
	"strconv"
)

// addSongs coleta playlist e músicas no menu e usa o mesmo fluxo de `playlist add`.
func (m *menu) addSongs() error {
	sess, err := newSession(m.ctx, cur.Market)
	if err != nil {
		fmt.Fprintln(m.out, m.paint("31", "erro: "+err.Error()))
		m.pause()
		return nil //nolint:nilerr // erro já mostrado ao usuário; segue normalmente
	}
	to, err := m.ask("Nome (ou link) da playlist de destino", "")
	if err != nil {
		return err
	}
	if to == "" {
		return nil
	}
	fmt.Fprintln(m.out, "Digite uma música por linha: link/URI/ID do Spotify ou texto de busca (ex.: djavan sina).")
	fmt.Fprintln(m.out, "Linha vazia termina.")
	var items []string
	for {
		s, err := m.ask(fmt.Sprintf("  música %d", len(items)+1), "")
		if err != nil {
			return err
		}
		if s == "" {
			break
		}
		items = append(items, s)
	}
	if len(items) == 0 {
		return nil
	}
	create, err := m.confirm("Criar a playlist se ela não existir?", true)
	if err != nil {
		return err
	}
	pick, err := m.confirm("Escolher entre os resultados de cada busca? (não = usar o primeiro)", true)
	if err != nil {
		return err
	}
	ui := playlistUI{
		confirm: func(q string) (bool, error) { return m.confirm(q, false) },
		choose: func(prompt string, labels []string) (int, error) {
			fmt.Fprintln(m.out, prompt)
			for i, l := range labels {
				fmt.Fprintf(m.out, "  %d) %s\n", i+1, l)
			}
			for {
				s, err := m.ask("Escolha (0 = pular)", "1")
				if err != nil {
					return -1, err
				}
				if n, e := strconv.Atoi(s); e == nil && n >= 0 && n <= len(labels) {
					return n - 1, nil
				}
				fmt.Fprintln(m.out, "Opção inválida.")
			}
		},
	}
	if err := addToPlaylist(m.ctx, sess.Client, addOptions{To: to, Create: create, Pick: pick, Items: items}, ui, m.out); err != nil {
		fmt.Fprintln(m.out, m.paint("31", "erro: "+err.Error()))
	}
	m.pause()
	return nil
}
