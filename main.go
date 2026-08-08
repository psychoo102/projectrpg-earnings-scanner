package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// getLogFiles zwraca posortowane alfabetycznie pliki console*.log
// z podanego katalogu logów MTA.
func getLogFiles(dir string) ([]string, error) {
	var files []string
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, "console") && strings.HasSuffix(name, ".log") {
			files = append(files, filepath.Join(dir, name))
		}
	}
	sort.Strings(files)
	return files, nil
}

// writeReport zapisuje bieżący stan stats do pliku raportu (nadpisując go).
func writeReport(path string, stats map[string]*dayStats) error {
	var dates []string
	for d := range stats {
		dates = append(dates, d)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(dates)))

	out, err := os.Create(path)
	if err != nil {
		return err
	}
	defer out.Close()

	w := bufio.NewWriter(out)
	defer w.Flush()

	fmt.Fprintln(w, "=== RAPORT DZIENNY ===")
	for _, date := range dates {
		d := stats[date]
		fmt.Fprintf(w, "%s -> %.2f$ | %d XP", date, d.money, d.xp)

		if len(d.trackers) > 0 {
			var names []string
			for name := range d.trackers {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				hit := d.trackers[name]
				status := "NIE"
				if hit.Detected {
					status = "TAK"
					if hit.Detail != "" {
						status = fmt.Sprintf("TAK (%s)", hit.Detail)
					}
				}
				fmt.Fprintf(w, " | %s: %s", name, status)
			}
		}
		fmt.Fprintln(w)
	}
	return nil
}

func main() {
	setPath := flag.Bool("set-path", false, "wymuś ponowne ustawienie ścieżki do katalogu logów MTA")
	watch := flag.Bool("watch", false, "uruchom w trybie ciągłej obserwacji logów na żywo")
	reportFlag := flag.String("out", "raport.txt", "ścieżka pliku wynikowego raportu")
	flag.Parse()

	reader := bufio.NewReader(os.Stdin)

	if *setPath {
		if err := resetPath(reader); err != nil {
			fmt.Println("Błąd:", err)
			pauseBeforeExit(reader)
			os.Exit(1)
		}
	}

	cfg, err := LoadOrCreateConfig(reader)
	if err != nil {
		fmt.Println("Błąd konfiguracji:", err)
		pauseBeforeExit(reader)
		os.Exit(1)
	}

	trackers := compileTrackers(cfg.Trackers)

	files, err := getLogFiles(cfg.MTAPath)
	if err != nil {
		fmt.Println("Nie udało się odczytać katalogu logów:", err)
		pauseBeforeExit(reader)
		os.Exit(1)
	}
	if len(files) == 0 {
		fmt.Println("Nie znaleziono żadnych plików console*.log w:", cfg.MTAPath)
		pauseBeforeExit(reader)
		os.Exit(1)
	}

	stats := make(map[string]*dayStats)
	for _, f := range files {
		if err := parseFile(f, stats, trackers); err != nil {
			// Jeden uszkodzony/zablokowany plik nie powinien wywalać
			// całego raportu — informujemy i kontynuujemy z resztą.
			fmt.Printf("Uwaga: nie udało się przetworzyć %s: %v\n", f, err)
		}
	}

	if err := writeReport(*reportFlag, stats); err != nil {
		fmt.Println("Błąd zapisu raportu:", err)
		pauseBeforeExit(reader)
		os.Exit(1)
	}
	fmt.Println("Raport wygenerowany:", *reportFlag)

	if *watch {
		if err := runWatch(cfg.MTAPath, stats, trackers, *reportFlag); err != nil {
			fmt.Println("Błąd trybu obserwacji:", err)
			pauseBeforeExit(reader)
			os.Exit(1)
		}
		return
	}

	pauseBeforeExit(reader)
}

// resetPath pozwala użytkownikowi ręcznie nadpisać zapisaną ścieżkę do MTA
// (flaga --set-path), niezależnie od tego, czy dotychczasowa jest wciąż
// poprawna.
func resetPath(reader *bufio.Reader) error {
	cfg, path, err := LoadConfig()
	if err != nil {
		return err
	}
	if cfg == nil {
		cfg = &Config{Trackers: defaultTrackers()}
	}
	fmt.Println("Ustawianie nowej ścieżki do katalogu logów MTA.")
	cfg.MTAPath = promptForPath(reader)
	return cfg.Save(path)
}

func pauseBeforeExit(reader *bufio.Reader) {
	fmt.Println("Naciśnij Enter, aby zamknąć...")
	reader.ReadString('\n')
}
