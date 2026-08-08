package main

import (
	"bufio"
	"fmt"
	"os"
	"time"
)

// watchPollInterval określa, jak często sprawdzamy pliki logów pod kątem
// nowych linii w trybie --watch. MTA dopisuje do loga na bieżąco, więc
// zwykłe odpytywanie o rozmiar pliku (bez zewnętrznych zależności typu
// fsnotify) w zupełności wystarcza.
const watchPollInterval = 2 * time.Second

// watchState pamięta, ile bajtów danego pliku już przeczytaliśmy,
// żeby przy kolejnym sprawdzeniu doczytać tylko nowy fragment ("tail -f").
type watchState struct {
	offset int64
}

// runWatch uruchamia się w pętli: co watchPollInterval sprawdza katalog
// logów, doczytuje nowe linie z najnowszego pliku (a także z ewentualnych
// nowo utworzonych plików console*.log, np. po restarcie gry) i na bieżąco
// aktualizuje stats oraz raport.txt. Powiadamia w konsoli o każdym
// wykrytym trackerze (np. "Nagroda dzienna").
//
// Działa w pętli nieskończonej — zatrzymanie przez Ctrl+C.
func runWatch(logDir string, stats map[string]*dayStats, trackers []compiledTracker, reportPath string) error {
	states := make(map[string]*watchState)

	fmt.Println("Tryb obserwacji uruchomiony (Ctrl+C aby zakończyć).")
	fmt.Printf("Sprawdzam zmiany w logach co %s...\n\n", watchPollInterval)

	for {
		files, err := getLogFiles(logDir)
		if err != nil {
			fmt.Printf("Błąd odczytu katalogu logów: %v\n", err)
			time.Sleep(watchPollInterval)
			continue
		}

		anyNewMatch := false
		for _, f := range files {
			matched, err := readNewLines(f, states, stats, trackers)
			if err != nil {
				fmt.Printf("Błąd odczytu %s: %v\n", f, err)
				continue
			}
			if matched {
				anyNewMatch = true
			}
		}

		if anyNewMatch {
			if err := writeReport(reportPath, stats); err != nil {
				fmt.Printf("Błąd zapisu raportu: %v\n", err)
			} else {
				fmt.Printf("[%s] raport.txt zaktualizowany\n", time.Now().Format("15:04:05"))
			}
		}

		time.Sleep(watchPollInterval)
	}
}

// readNewLines doczytuje fragment pliku, który pojawił się od ostatniego
// sprawdzenia, i przepuszcza nowe linie przez processLine. Zwraca true,
// jeśli którakolwiek nowa linia coś dopasowała (zarobek lub tracker).
func readNewLines(path string, states map[string]*watchState, stats map[string]*dayStats, trackers []compiledTracker) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}

	st, known := states[path]
	if !known {
		// Nowo zauważony plik: zaczynamy śledzić od jego bieżącego końca,
		// żeby nie zalać konsoli powiadomieniami o starych wpisach.
		// Wyjątek: jeśli to jedyny/najnowszy plik przy starcie, dane i tak
		// trafiły już do stats przez wcześniejszy pełny skan w main().
		st = &watchState{offset: info.Size()}
		states[path] = st
		return false, nil
	}

	if info.Size() < st.offset {
		// Plik został obcięty/nadpisany (np. nowa sesja gry nadpisała
		// console.log) — czytamy od nowa.
		st.offset = 0
	}
	if info.Size() == st.offset {
		return false, nil
	}

	file, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer file.Close()

	if _, err := file.Seek(st.offset, 0); err != nil {
		return false, err
	}

	matchedAny := false
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if processLine(line, stats, trackers) {
			matchedAny = true
			notifyMatch(line, trackers)
		}
	}
	if err := scanner.Err(); err != nil {
		return matchedAny, err
	}

	newOffset, err := file.Seek(0, 1) // aktualna pozycja po odczycie
	if err != nil {
		return matchedAny, err
	}
	st.offset = newOffset
	return matchedAny, nil
}

// notifyMatch wypisuje na konsoli krótkie powiadomienie, gdy linia
// pasuje do któregoś z trackerów (przydatne w trybie --watch, żeby
// zobaczyć na żywo np. "🎉 Nagroda dzienna: odebrana").
func notifyMatch(line string, trackers []compiledTracker) {
	rest := line
	if m := dateRe.FindStringSubmatch(line); m != nil {
		rest = m[2]
	}
	for _, t := range trackers {
		if t.Re.MatchString(rest) {
			fmt.Printf("  🎉 Wykryto: %s\n", t.Name)
		}
	}
}
