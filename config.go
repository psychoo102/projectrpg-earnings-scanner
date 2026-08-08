package main

import (
	"bufio"
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

// Config przechowuje wszystkie ustawienia użytkownika zapisywane na dysku.
type Config struct {
	MTAPath  string    `json:"mta_path"`
	Trackers []Tracker `json:"trackers"`
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
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
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
			if mergeMissingTrackers(cfg) {
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

	if err := cfg.Save(path); err != nil {
		return nil, err
	}
	fmt.Printf("Zapisano konfigurację w: %s\n\n", path)
	return cfg, nil
}

// mergeMissingTrackers dopisuje domyślne trackery, których użytkownik
// jeszcze nie ma w swoim configu (po nazwie). Zwraca true, jeśli coś dodano.
func mergeMissingTrackers(cfg *Config) bool {
	existing := make(map[string]bool, len(cfg.Trackers))
	for _, t := range cfg.Trackers {
		existing[t.Name] = true
	}
	added := false
	for _, t := range defaultTrackers() {
		if !existing[t.Name] {
			cfg.Trackers = append(cfg.Trackers, t)
			added = true
		}
	}
	return added
}
