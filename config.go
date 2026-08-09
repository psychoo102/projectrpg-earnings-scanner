package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const configFileName = "config.json"

// Tracker to pojedyncza reguła wykrywania "akcji dziennej" w logach,
// np. odebranie nagrody za codzienne logowanie.
//
// Pattern jest wyrażeniem regularnym dopasowywanym do treści linii loga
// (bez fragmentu z datą/godziną). Jeśli wzorzec zawiera grupę przechwytującą,
// jej wartość (np. numer dnia streaka) trafia do raportu jako dodatkowa
// informacja.
type Tracker struct {
	Name    string `json:"name"`
	Pattern string `json:"pattern"`
}

// MoneyRule to reguła rozpoznająca w logu jeden konkretny rodzaj wpływu
// (income) lub wydatku (expense) — np. sprzedaż towaru, tankowanie paliwa.
//
// Pattern to wyrażenie regularne dopasowywane do treści linii loga
// (bez fragmentu z datą/godziną). Musi zawierać nazwaną grupę
// "(?P<amount>...)" z kwotą (może zawierać przecinek jako separator
// tysięcy, np. "6,748.80" — zostanie poprawnie zinterpretowany).
// Opcjonalnie może też zawierać nazwaną grupę "(?P<xp>...)", jeśli ta sama
// linia niesie też informację o zdobytym XP.
//
// Dzięki nazwanym grupom dodanie nowego rodzaju przychodu/kosztu
// (np. kolejnej stawki, nowego paliwa, innej pracy) wymaga tylko dopisania
// wpisu w config.json — bez zmian w kodzie.
type MoneyRule struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"` // "income" albo "expense"
	Pattern string `json:"pattern"`
}

// Config przechowuje wszystkie ustawienia użytkownika zapisywane na dysku.
type Config struct {
	MTAPath    string      `json:"mta_path"`
	Trackers   []Tracker   `json:"trackers"`
	MoneyRules []MoneyRule `json:"money_rules"`
}

// defaultTrackers to zestaw wykrywaczy dodawany automatycznie przy
// pierwszym uruchomieniu. Użytkownik może je dowolnie edytować lub
// dopisywać własne w pliku config.json.
func defaultTrackers() []Tracker {
	return []Tracker{
		{
			Name:    "Nagroda dzienna",
			Pattern: `Otrzymałeś \d+(?:\.\d+)? \$ za codzienne logowanie(?:.*\(Dzień (\d+)\))?`,
		},
	}
}

// amountPattern to współdzielony fragment regexu dopasowujący kwotę
// pieniężną, opcjonalnie z przecinkiem jako separatorem tysięcy,
// np. "150.50" albo "6,748.80".
const amountPattern = `[0-9]+(?:,[0-9]{3})*(?:\.[0-9]+)?`

// defaultMoneyRules to zestaw reguł przychodów/wydatków dodawany
// automatycznie przy pierwszym uruchomieniu. Użytkownik może dopisywać
// kolejne (np. nowe paliwa, nowe prace) bezpośrednio w config.json.
func defaultMoneyRules() []MoneyRule {
	return []MoneyRule{
		{
			Name:    "Zarobek (standardowy)",
			Kind:    "income",
			Pattern: `Otrzymałeś (?P<amount>` + amountPattern + `)\$.*\+(?P<xp>[0-9]+) XP`,
		},
		{
			Name:    "Zrzucenie towaru",
			Kind:    "income",
			Pattern: `Zrzucono [0-9.,]+ kg .*? na stos \(\+(?P<xp>[0-9]+) XP\)`,
		},
		{
			Name:    "Sprzedaż towaru",
			Kind:    "income",
			Pattern: `Sprzedano [0-9.,]+ kg .*? za (?P<amount>` + amountPattern + `)\$`,
		},
		{
			Name:    "Tankowanie paliwa",
			Kind:    "expense",
			// Nazwa paliwa (LPG, Pb 95, Pb 98, On, ...) celowo nie jest
			// wymieniona wprost — ".+?" złapie dowolną nazwę, więc nowe
			// rodzaje paliwa nie wymagają zmiany wzorca.
			Pattern: `Pomyślnie zatankowano [0-9.,]+l .+? za kwotę (?P<amount>` + amountPattern + `)\$`,
		},
	}
}

// defaultMTAPaths to standardowe lokalizacje instalacji MTA:SA na Windows.
func defaultMTAPaths() []string {
	return []string{
		`C:\Program Files (x86)\MTA San Andreas 1.6\MTA\logs`,
		`C:\Program Files\MTA San Andreas 1.6\MTA\logs`,
	}
}

// configDir zwraca katalog, w którym trzymamy config.json.
// Używamy katalogu konfiguracyjnego użytkownika (np. %APPDATA% na Windows),
// żeby działało to również gdy program leży w katalogu tylko do odczytu
// (np. Program Files).
func configDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		// Fallback: katalog obok binarki / bieżący katalog roboczy.
		exe, exErr := os.Executable()
		if exErr == nil {
			return filepath.Dir(exe), nil
		}
		return ".", nil
	}
	return filepath.Join(base, "projectrpg-earnings-scanner"), nil
}

func configPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, configFileName), nil
}

// LoadConfig wczytuje config.json, jeśli istnieje. Zwraca (nil, nil) jeśli
// pliku jeszcze nie ma.
func LoadConfig() (*Config, string, error) {
	path, err := configPath()
	if err != nil {
		return nil, "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, path, nil
		}
		return nil, path, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, path, fmt.Errorf("plik %s jest uszkodzony: %w", path, err)
	}
	return &cfg, path, nil
}

// Save zapisuje config na dysk (tworząc katalog, jeśli trzeba).
func (c *Config) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("nie można utworzyć katalogu konfiguracji: %w", err)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	// Wyłączamy domyślne escapowanie HTML (<, >, &), bo inaczej wzorce
	// regex z nazwanymi grupami "(?P<amount>...)" byłyby zapisane jako
	// nieczytelne "(?P\u003camount\u003e...)" — a ten plik ma być
	// wygodny do ręcznej edycji.
	enc.SetEscapeHTML(false)
	if err := enc.Encode(c); err != nil {
		return err
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("nie można zapisać %s: %w", path, err)
	}
	return nil
}

// isValidLogDir sprawdza, czy podana ścieżka to istniejący katalog.
func isValidLogDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// detectDefaultMTAPath próbuje znaleźć MTA pod standardowymi ścieżkami.
func detectDefaultMTAPath() (string, bool) {
	for _, p := range defaultMTAPaths() {
		if isValidLogDir(p) {
			return p, true
		}
	}
	return "", false
}

// promptForPath pyta użytkownika o ścieżkę do katalogu logs i waliduje ją.
// Ponawia pytanie, dopóki nie dostanie poprawnego katalogu (lub użytkownik
// nie przerwie programu Ctrl+C).
func promptForPath(reader *bufio.Reader) string {
	for {
		fmt.Print("Podaj pełną ścieżkę do katalogu 'logs' Twojej instalacji MTA:SA: ")
		line, err := reader.ReadString('\n')
		if err != nil {
			fmt.Println("Nie udało się odczytać danych wejściowych, spróbuj ponownie.")
			continue
		}
		path := strings.TrimSpace(line)
		path = strings.Trim(path, `"`) // gdyby ktoś wkleił ścieżkę w cudzysłowie

		if path == "" {
			fmt.Println("Ścieżka nie może być pusta.")
			continue
		}
		if !isValidLogDir(path) {
			fmt.Printf("Katalog %q nie istnieje. Spróbuj ponownie.\n", path)
			continue
		}
		return path
	}
}

// LoadOrCreateConfig to główny punkt wejścia: wczytuje istniejący config,
// a jeśli go nie ma (albo zapisana ścieżka MTA już nie istnieje),
// przeprowadza użytkownika przez pierwszą konfigurację i zapisuje wynik.
func LoadOrCreateConfig(reader *bufio.Reader) (*Config, error) {
	cfg, path, err := LoadConfig()
	if err != nil {
		return nil, err
	}

	if cfg != nil {
		if isValidLogDir(cfg.MTAPath) {
			// Dogrywamy ewentualne nowe domyślne trackery dodane w nowszej
			// wersji programu, żeby użytkownicy aktualizujący aplikację
			// automatycznie dostawali nowe wykrywacze.
			if mergeMissingDefaults(cfg) {
				_ = cfg.Save(path)
			}
			return cfg, nil
		}
		fmt.Printf("Zapisana ścieżka do MTA (%s) już nie istnieje.\n", cfg.MTAPath)
	} else {
		fmt.Println("Pierwsze uruchomienie — konfiguruję program.")
		cfg = &Config{}
	}

	if autoPath, ok := detectDefaultMTAPath(); ok {
		fmt.Printf("Wykryto domyślną instalację MTA: %s\n", autoPath)
		cfg.MTAPath = autoPath
	} else {
		fmt.Println("Nie znaleziono MTA pod domyślnymi ścieżkami.")
		cfg.MTAPath = promptForPath(reader)
	}

	if len(cfg.Trackers) == 0 {
		cfg.Trackers = defaultTrackers()
	}
	if len(cfg.MoneyRules) == 0 {
		cfg.MoneyRules = defaultMoneyRules()
	}

	if err := cfg.Save(path); err != nil {
		return nil, err
	}
	fmt.Printf("Zapisano konfigurację w: %s\n\n", path)
	return cfg, nil
}

// mergeMissingDefaults dopisuje domyślne trackery i reguły
// przychodów/wydatków, których użytkownik jeszcze nie ma w swoim configu
// (po nazwie) — dotyczy głównie osób aktualizujących program ze starszej
// wersji, żeby automatycznie dostały nowe wykrywacze. Zwraca true, jeśli
// cokolwiek dodano.
func mergeMissingDefaults(cfg *Config) bool {
	existingTrackers := make(map[string]bool, len(cfg.Trackers))
	for _, t := range cfg.Trackers {
		existingTrackers[t.Name] = true
	}
	added := false
	for _, t := range defaultTrackers() {
		if !existingTrackers[t.Name] {
			cfg.Trackers = append(cfg.Trackers, t)
			added = true
		}
	}

	existingRules := make(map[string]bool, len(cfg.MoneyRules))
	for _, r := range cfg.MoneyRules {
		existingRules[r.Name] = true
	}
	for _, r := range defaultMoneyRules() {
		if !existingRules[r.Name] {
			cfg.MoneyRules = append(cfg.MoneyRules, r)
			added = true
		}
	}

	return added
}
