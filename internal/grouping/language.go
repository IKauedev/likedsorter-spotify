package grouping

import (
	"strings"
	"unicode"

	"github.com/ikauedeveloper/likedsorter/internal/spotify"
)

const langUnknown = "Indefinido"

// language é EXPERIMENTAL: heurística simples, sem fonte externa. Ordem:
//  1. dicas de gênero (ex.: sertanejo → Português, k-pop → Coreano);
//  2. alfabeto do título/álbum/artista (hangul, kana, cirílico...);
//  3. palavras-função comuns (pt/es/en) e letras acentuadas típicas.
//
// Títulos curtos ou só com nomes próprios caem em "Indefinido".
type language struct{}

func (language) Name() string       { return "language" }
func (language) Experimental() bool { return true }

func (language) Keys(t spotify.SavedTrack, c *Context) []string {
	return []string{detectLanguage(t, c.PrimaryGenres(t))}
}

var genreLangHints = []struct{ sub, lang string }{
	{"k-pop", "Coreano"}, {"k-rap", "Coreano"}, {"korean", "Coreano"},
	{"j-pop", "Japonês"}, {"j-rock", "Japonês"}, {"anime", "Japonês"}, {"japanese", "Japonês"},
	{"sertanej", "Português"}, {"mpb", "Português"}, {"pagode", "Português"}, {"samba", "Português"},
	{"funk carioca", "Português"}, {"funk brasil", "Português"}, {"brazilian", "Português"}, {"forro", "Português"}, {"bossa nova", "Português"},
	{"reggaeton", "Espanhol"}, {"latin", "Espanhol"}, {"regional mexican", "Espanhol"}, {"mariachi", "Espanhol"}, {"flamenco", "Espanhol"}, {"cumbia", "Espanhol"},
	{"chanson", "Francês"}, {"french", "Francês"},
}

var stopwords = map[string]map[string]bool{
	"Português": set("de", "da", "do", "das", "dos", "que", "não", "nao", "você", "voce", "meu", "minha", "com", "uma", "para", "pra", "mais", "amor", "coração", "coracao", "é", "em", "no", "na", "te", "se", "eu", "ela", "ele", "vida", "tudo", "nós", "nos"),
	"Espanhol":  set("el", "la", "los", "las", "de", "del", "que", "con", "una", "para", "mi", "tu", "te", "amor", "corazón", "corazon", "yo", "por", "es", "en", "y", "no", "vida", "todo", "quiero"),
	"Inglês":    set("the", "and", "you", "your", "my", "me", "of", "in", "to", "love", "is", "it", "i", "on", "for", "with", "this", "that", "be", "we", "all", "not", "baby", "like"),
}

func set(words ...string) map[string]bool {
	m := make(map[string]bool, len(words))
	for _, w := range words {
		m[w] = true
	}
	return m
}

func detectLanguage(t spotify.SavedTrack, genres []string) string {
	for _, g := range genres {
		g = strings.ToLower(g)
		for _, h := range genreLangHints {
			if strings.Contains(g, h.sub) {
				return h.lang
			}
		}
	}

	var text strings.Builder
	text.WriteString(t.Name + " " + t.Album.Name)
	for _, a := range t.Artists {
		text.WriteString(" " + a.Name)
	}
	s := strings.ToLower(text.String())

	// alfabeto
	var hangul, kana, han, cyr, arab int
	for _, r := range s {
		switch {
		case unicode.Is(unicode.Hangul, r):
			hangul++
		case unicode.Is(unicode.Hiragana, r), unicode.Is(unicode.Katakana, r):
			kana++
		case unicode.Is(unicode.Han, r):
			han++
		case unicode.Is(unicode.Cyrillic, r):
			cyr++
		case unicode.Is(unicode.Arabic, r):
			arab++
		}
	}
	switch {
	case hangul > 0:
		return "Coreano"
	case kana > 0:
		return "Japonês"
	case han > 0:
		return "Chinês"
	case cyr > 0:
		return "Russo"
	case arab > 0:
		return "Árabe"
	}

	// palavras-função só do título e do álbum (nomes de artistas enviesam)
	words := strings.FieldsFunc(strings.ToLower(t.Name+" "+t.Album.Name), func(r rune) bool {
		return !unicode.IsLetter(r) && r != '\''
	})
	score := map[string]int{}
	for _, w := range words {
		for lang, sw := range stopwords {
			if sw[w] {
				score[lang]++
			}
		}
	}
	if strings.ContainsAny(s, "ãõç") {
		score["Português"] += 2
	}
	if strings.ContainsAny(s, "ñ¿¡") {
		score["Espanhol"] += 2
	}
	best, bestN, tie := "", 0, false
	for _, lang := range []string{"Português", "Espanhol", "Inglês"} { // ordem fixa = desempate determinístico
		switch n := score[lang]; {
		case n > bestN:
			best, bestN, tie = lang, n, false
		case n == bestN && n > 0:
			tie = true
		}
	}
	if bestN == 0 || tie {
		return langUnknown
	}
	return best
}
