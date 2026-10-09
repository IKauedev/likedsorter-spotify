package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/ikauedeveloper/likedsorter/internal/fsutil"
)

// Settings são os padrões das flags, resolvidos em camadas:
//
//	flags (aplicadas pelo parser de flags) > variáveis de ambiente > config.yaml > padrões embutidos
//
// As camadas abaixo das flags são resolvidas aqui; os comandos usam os valores
// resultantes como DEFAULT de cada flag, então passar a flag sempre vence.
//
// Por segurança, opções destrutivas (--allow-remove, --yes) NÃO são configuráveis
// por arquivo/ambiente: precisam ser explícitas a cada execução.
type Settings struct {
	LogLevel string

	Market          string
	TracksTTL       time.Duration
	ArtistsTTL      time.Duration
	GenreSource     string
	Concurrency     int
	IncludeFeatured bool

	By          string
	MinSize     int
	SmallGroups string
	MaxSize     int
	MultiGenre  bool
	Sort        string
	MacroMap    string
	SplitOver   int
	SplitBy     string

	NameTemplate string

	Mode         string
	Public       bool
	MaxPlaylists int
}

// DefaultSettings devolve os padrões embutidos.
func DefaultSettings() Settings {
	return Settings{
		LogLevel: "warn",
		Market:   "from_token", TracksTTL: 7 * 24 * time.Hour, ArtistsTTL: 30 * 24 * time.Hour,
		GenreSource: "spotify,musicbrainz", Concurrency: 4,
		By: "macro-genre", SmallGroups: "other", Sort: "added", SplitBy: "decade",
		NameTemplate: "Curtidas • {group}",
		Mode:         "sync", MaxPlaylists: 100,
	}
}

type setting struct {
	key  string
	help string
	get  func(*Settings) string
	set  func(*Settings, string) error
}

func oneOf(vals ...string) func(string) error {
	return func(v string) error {
		for _, ok := range vals {
			if v == ok {
				return nil
			}
		}
		return fmt.Errorf("valor %q inválido (use: %s)", v, strings.Join(vals, ", "))
	}
}

func strS(key, help string, f func(*Settings) *string, check func(string) error) setting {
	return setting{key, help,
		func(s *Settings) string { return *f(s) },
		func(s *Settings, v string) error {
			v = strings.TrimSpace(v)
			if check != nil {
				if err := check(v); err != nil {
					return err
				}
			}
			*f(s) = v
			return nil
		}}
}

func intS(key, help string, min int, f func(*Settings) *int) setting {
	return setting{key, help,
		func(s *Settings) string { return strconv.Itoa(*f(s)) },
		func(s *Settings, v string) error {
			n, err := strconv.Atoi(strings.TrimSpace(v))
			if err != nil || n < min {
				return fmt.Errorf("valor %q inválido (inteiro >= %d)", v, min)
			}
			*f(s) = n
			return nil
		}}
}

func boolS(key, help string, f func(*Settings) *bool) setting {
	return setting{key, help,
		func(s *Settings) string { return strconv.FormatBool(*f(s)) },
		func(s *Settings, v string) error {
			b, err := strconv.ParseBool(strings.TrimSpace(v))
			if err != nil {
				return fmt.Errorf("valor %q inválido (true ou false)", v)
			}
			*f(s) = b
			return nil
		}}
}

func durS(key, help string, f func(*Settings) *time.Duration) setting {
	return setting{key, help,
		func(s *Settings) string { return formatDuration(*f(s)) },
		func(s *Settings, v string) error {
			d, err := time.ParseDuration(strings.TrimSpace(v))
			if err != nil || d < 0 {
				return fmt.Errorf("duração %q inválida (ex.: 72h, 30m; 0 = nunca expira)", v)
			}
			*f(s) = d
			return nil
		}}
}

func formatDuration(d time.Duration) string {
	if d != 0 && d%time.Hour == 0 {
		return fmt.Sprintf("%dh", int(d/time.Hour))
	}
	return d.String()
}

var registry = []setting{
	strS("log_level", "debug | info | warn | error", func(s *Settings) *string { return &s.LogLevel }, oneOf("debug", "info", "warn", "error")),

	strS("sync.market", "market enviado ao Spotify (from_token ou código ISO, ex.: BR)", func(s *Settings) *string { return &s.Market }, nil),
	durS("sync.tracks_ttl", "idade máxima do cache de faixas antes de reler tudo (0 = nunca)", func(s *Settings) *time.Duration { return &s.TracksTTL }),
	durS("sync.artists_ttl", "validade do cache de artistas (0 = nunca)", func(s *Settings) *time.Duration { return &s.ArtistsTTL }),
	strS("sync.genre_source", "fontes de gênero em ordem: spotify,musicbrainz,lastfm", func(s *Settings) *string { return &s.GenreSource }, nil),
	intS("sync.concurrency", "chamadas simultâneas ao Spotify para artistas", 1, func(s *Settings) *int { return &s.Concurrency }),
	boolS("sync.include_featured", "considerar artistas de participação", func(s *Settings) *bool { return &s.IncludeFeatured }),

	strS("group.by", "estratégia: artist, genre, macro-genre, decade, year, added-period, rules, language (ou a+b)", func(s *Settings) *string { return &s.By }, validStrategySpec),
	intS("group.min_size", "grupos menores que N vão para \"Outros\" ou são ignorados (0 = desligado)", 0, func(s *Settings) *int { return &s.MinSize }),
	strS("group.small_groups", "other | skip", func(s *Settings) *string { return &s.SmallGroups }, oneOf("other", "skip")),
	intS("group.max_size", "divide grupos maiores que N em partes (0 = 10000)", 0, func(s *Settings) *int { return &s.MaxSize }),
	boolS("group.multi_genre", "a faixa pode entrar em mais de um grupo de gênero", func(s *Settings) *bool { return &s.MultiGenre }),
	strS("group.sort", "ordem dentro do grupo: added | release | title", func(s *Settings) *string { return &s.Sort }, oneOf("added", "release", "title")),
	strS("group.macro_map", "caminho do YAML de macro-gêneros (vazio = padrão)", func(s *Settings) *string { return &s.MacroMap }, nil),

	intS("group.split_over", "divide grupos com mais de N faixas por década/ano de lançamento (0 = desligado)", 0, func(s *Settings) *int { return &s.SplitOver }),
	strS("group.split_by", "como dividir os grupos grandes: decade | year", func(s *Settings) *string { return &s.SplitBy }, oneOf("decade", "year")),

	strS("plan.name_template", "nome das playlists; {group} é o nome do grupo", func(s *Settings) *string { return &s.NameTemplate }, func(v string) error {
		if !strings.Contains(v, "{group}") {
			return errors.New("precisa conter {group}")
		}
		return nil
	}),

	strS("apply.mode", "create-only | sync | recreate", func(s *Settings) *string { return &s.Mode }, oneOf("create-only", "sync", "recreate")),
	boolS("apply.public", "criar playlists públicas (padrão: privadas)", func(s *Settings) *bool { return &s.Public }),
	intS("apply.max_playlists", "trava: máximo de playlists NOVAS por execução", 1, func(s *Settings) *int { return &s.MaxPlaylists }),
}

// forbidden são chaves que parecem configuráveis mas não podem ser fixadas em arquivo/ambiente.
var forbidden = map[string]string{
	"apply.allow_remove": "remoção de faixas precisa ser pedida a cada execução com --allow-remove",
	"apply.yes":          "a confirmação não pode ser desligada por configuração; use --yes na execução",
	"yes":                "a confirmação não pode ser desligada por configuração; use --yes na execução",
}

// EnvName devolve a variável de ambiente de uma chave: sync.market → LIKEDSORTER_SYNC_MARKET.
func EnvName(key string) string {
	return "LIKEDSORTER_" + strings.ToUpper(strings.ReplaceAll(key, ".", "_"))
}

// Entry descreve o valor efetivo de uma chave e de onde veio.
type Entry struct {
	Key    string `json:"key"`
	Env    string `json:"env"`
	Value  string `json:"value"`
	Source string `json:"source"`
	Help   string `json:"help"`
}

// Sources possíveis de um valor.
const (
	SourceDefault = "padrão"
	SourceFile    = "arquivo"
	SourceEnv     = "ambiente"
)

// SettingsPath devolve o caminho padrão do config.yaml.
func SettingsPath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.yaml"), nil
}

func validKeys() string {
	keys := make([]string, len(registry))
	for i, r := range registry {
		keys[i] = r.key
	}
	return strings.Join(keys, ", ")
}

// LoadSettings resolve padrões ← arquivo ← ambiente. path vazio usa o caminho padrão;
// arquivo ausente não é erro (a menos que path tenha sido informado explicitamente).
func LoadSettings(path string) (Settings, []Entry, error) {
	explicit := path != ""
	if !explicit {
		p, err := SettingsPath()
		if err != nil {
			return Settings{}, nil, err
		}
		path = p
	}

	s := DefaultSettings()
	source := map[string]string{}
	for _, r := range registry {
		source[r.key] = SourceDefault
	}

	b, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if explicit {
			return Settings{}, nil, fmt.Errorf("arquivo de configuração não encontrado: %s", path)
		}
	case err != nil:
		return Settings{}, nil, fmt.Errorf("ler %s: %w", path, err)
	default:
		if err := applyFile(&s, b, source); err != nil {
			return Settings{}, nil, fmt.Errorf("%s: %w", path, err)
		}
	}

	for _, r := range registry {
		env := EnvName(r.key)
		if v, ok := os.LookupEnv(env); ok && strings.TrimSpace(v) != "" {
			if err := r.set(&s, v); err != nil {
				return Settings{}, nil, fmt.Errorf("variável %s: %w", env, err)
			}
			source[r.key] = SourceEnv
		}
	}

	entries := make([]Entry, len(registry))
	for i, r := range registry {
		entries[i] = Entry{Key: r.key, Env: EnvName(r.key), Value: r.get(&s), Source: source[r.key], Help: r.help}
	}
	return s, entries, nil
}

func applyFile(s *Settings, b []byte, source map[string]string) error {
	var raw map[string]any
	if err := yaml.Unmarshal(b, &raw); err != nil {
		return fmt.Errorf("YAML inválido: %w", err)
	}
	flat := map[string]any{}
	flatten("", raw, flat)

	byKey := map[string]setting{}
	for _, r := range registry {
		byKey[r.key] = r
	}
	keys := make([]string, 0, len(flat))
	for k := range flat {
		keys = append(keys, k)
	}
	sort.Strings(keys) // erros determinísticos
	for _, k := range keys {
		r, ok := byKey[k]
		if !ok {
			if why, bad := forbidden[k]; bad {
				return fmt.Errorf("chave %q não é permitida em arquivo: %s", k, why)
			}
			return fmt.Errorf("chave desconhecida %q (válidas: %s)", k, validKeys())
		}
		if err := r.set(s, scalar(flat[k])); err != nil {
			return fmt.Errorf("chave %q: %w", k, err)
		}
		source[k] = SourceFile
	}
	return nil
}

func flatten(prefix string, m map[string]any, out map[string]any) {
	for k, v := range m {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}
		if sub, ok := v.(map[string]any); ok {
			flatten(key, sub, out)
			continue
		}
		out[key] = v
	}
}

// scalar converte um valor YAML em texto; listas viram "a,b,c".
func scalar(v any) string {
	if list, ok := v.([]any); ok {
		parts := make([]string, len(list))
		for i, x := range list {
			parts[i] = fmt.Sprint(x)
		}
		return strings.Join(parts, ",")
	}
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

// SampleFile gera um config.yaml completo com os padrões e a ajuda de cada chave.
func SampleFile() string {
	def := DefaultSettings()
	var b strings.Builder
	b.WriteString("# likedsorter: configuração.\n")
	b.WriteString("# Precedência: flags > variáveis de ambiente (LIKEDSORTER_<SEÇÃO>_<CHAVE>) > este arquivo > padrões.\n")
	b.WriteString("# Remover faixas (--allow-remove) e pular a confirmação (--yes) só valem como flags, nunca aqui.\n\n")
	section := ""
	for _, r := range registry {
		sec, name, nested := strings.Cut(r.key, ".")
		indent := ""
		if nested {
			if sec != section {
				fmt.Fprintf(&b, "\n%s:\n", sec)
				section = sec
			}
			indent = "  "
		} else {
			name = r.key
		}
		val, _ := yaml.Marshal(scalarForYAML(r.get(&def)))
		fmt.Fprintf(&b, "%s# %s\n%s%s: %s\n", indent, r.help, indent, name, strings.TrimSpace(string(val)))
	}
	return b.String()
}

// scalarForYAML mantém números e booleanos como tais e o resto como string.
func scalarForYAML(v string) any {
	if n, err := strconv.Atoi(v); err == nil {
		return n
	}
	if b, err := strconv.ParseBool(v); err == nil && (v == "true" || v == "false") {
		return b
	}
	return v
}

// ------------------------------------------------------------------ edição do config.yaml

// SettingKeys devolve as chaves editáveis, na ordem do arquivo.
func SettingKeys() []string {
	keys := make([]string, len(registry))
	for i, r := range registry {
		keys[i] = r.key
	}
	return keys
}

// ValidateSetting confere key/value sem gravar nada.
func ValidateSetting(key, value string) error {
	if why, bad := forbidden[key]; bad {
		return fmt.Errorf("chave %q não é permitida em arquivo: %s", key, why)
	}
	for _, r := range registry {
		if r.key == key {
			s := DefaultSettings()
			return r.set(&s, value)
		}
	}
	return fmt.Errorf("chave desconhecida %q (válidas: %s)", key, validKeys())
}

// readFileValues lê as chaves presentes no arquivo (ou nada se ele não existe).
func readFileValues(path string) (map[string]string, error) {
	vals := map[string]string{}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return vals, nil
	}
	if err != nil {
		return nil, err
	}
	var raw map[string]any
	if err := yaml.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("%s: YAML inválido: %w", path, err)
	}
	flat := map[string]any{}
	flatten("", raw, flat)
	for k, v := range flat {
		vals[k] = scalar(v)
	}
	return vals, nil
}

// SetSetting grava key=value no config.yaml (criando-o se preciso), validando antes.
// O arquivo é regravado com os comentários de ajuda; só as chaves definidas ficam ativas.
func SetSetting(path, key, value string) error {
	value = strings.TrimSpace(value)
	if err := ValidateSetting(key, value); err != nil {
		return err
	}
	vals, err := readFileValues(path)
	if err != nil {
		return err
	}
	vals[key] = value
	return writeSettings(path, vals)
}

// UnsetSetting volta key ao padrão (remove do arquivo); informa se ela estava definida.
func UnsetSetting(path, key string) (bool, error) {
	if err := ValidateSetting(key, DefaultValue(key)); err != nil {
		return false, err
	}
	vals, err := readFileValues(path)
	if err != nil {
		return false, err
	}
	if _, ok := vals[key]; !ok {
		return false, nil
	}
	delete(vals, key)
	return true, writeSettings(path, vals)
}

// DefaultValue devolve o padrão embutido de uma chave ("" se desconhecida).
func DefaultValue(key string) string {
	def := DefaultSettings()
	for _, r := range registry {
		if r.key == key {
			return r.get(&def)
		}
	}
	return ""
}

func writeSettings(path string, vals map[string]string) error {
	// Revalida o conjunto inteiro: nunca grava um arquivo que o próprio programa recusaria ao ler.
	s := DefaultSettings()
	for _, r := range registry {
		if v, ok := vals[r.key]; ok {
			if err := r.set(&s, v); err != nil {
				return fmt.Errorf("chave %q: %w", r.key, err)
			}
		}
	}
	return fsutil.WriteFileAtomic(path, []byte(RenderFile(vals)))
}

// RenderFile gera o config.yaml: chaves em vals ficam ativas; as demais aparecem
// comentadas com o padrão, para o arquivo continuar servindo de documentação.
func RenderFile(vals map[string]string) string {
	def := DefaultSettings()
	var b strings.Builder
	b.WriteString("# likedsorter: configuração.\n")
	b.WriteString("# Precedência: flags > variáveis de ambiente (LIKEDSORTER_<SEÇÃO>_<CHAVE>) > este arquivo > padrões.\n")
	b.WriteString("# Edite à mão ou use: likedsorter config set <chave> <valor> | unset <chave>\n")
	b.WriteString("# Remover faixas (--allow-remove) e pular a confirmação (--yes) só valem como flags, nunca aqui.\n")

	activeIn := map[string]bool{}
	for k := range vals {
		if sec, _, nested := strings.Cut(k, "."); nested {
			activeIn[sec] = true
		}
	}
	section := ""
	for _, r := range registry {
		sec, name, nested := strings.Cut(r.key, ".")
		indent := ""
		if nested {
			if sec != section {
				section = sec
				if activeIn[sec] {
					fmt.Fprintf(&b, "\n%s:\n", sec)
				} else {
					fmt.Fprintf(&b, "\n# %s:\n", sec)
				}
			}
			indent = "  "
		} else {
			name = r.key
		}
		v, active := vals[r.key]
		if !active {
			v = r.get(&def)
		}
		enc, _ := yaml.Marshal(scalarForYAML(v))
		line := fmt.Sprintf("%s: %s", name, strings.TrimSpace(string(enc)))
		prefix := ""
		if !active {
			prefix = "# "
		}
		fmt.Fprintf(&b, "%s# %s\n%s%s%s\n", indent, r.help, indent, prefix, line)
	}
	return b.String()
}

// validStrategySpec aceita nomes de estratégia simples ou combinados com "+".
func validStrategySpec(v string) error {
	known := map[string]bool{"artist": true, "genre": true, "macro-genre": true, "decade": true, "year": true,
		"added-period": true, "language": true, "rules": true, "listening": true}
	for _, p := range strings.Split(strings.ToLower(v), "+") {
		if !known[strings.TrimSpace(p)] {
			return fmt.Errorf("estratégia %q desconhecida (use artist, genre, macro-genre, decade, year, added-period, rules, language; combine com +)", p)
		}
	}
	return nil
}
