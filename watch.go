package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
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
func runWatch(logDir string, stats map[string]*dayStats, moneyRules []compiledMoneyRule, trackers []compiledTracker, reportPath string) error {
	states := make(map[string]*watchState)
	trackerNames := trackerNamesFrom(trackers)

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
			matched, err := readNewLines(f, states, stats, moneyRules, trackers)
			if err != nil {
				fmt.Printf("Błąd odczytu %s: %v\n", f, err)
				continue
			}
			if matched {
				anyNewMatch = true
			}
		}

		if anyNewMatch {
			if err := writeReport(reportPath, stats, trackerNames); err != nil {
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
func readNewLines(path string, states map[string]*watchState, stats map[string]*dayStats, moneyRules []compiledMoneyRule, trackers []compiledTracker) (bool, error) {
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

	// Czytamy cały nowy fragment naraz i dekodujemy jego kodowanie
	// (patrz encoding.go) — Windows-1250 jest kodowaniem jednobajtowym,
	// więc dekodowanie fragmentu pliku (bez kontekstu całości) jest
	// bezpieczne i nie wymaga znajomości reszty pliku.
	raw, err := io.ReadAll(file)
	if err != nil {
		return false, err
	}
	data := decodeFileBytes(raw)

	matchedAny := false
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 1024), 1024*1024)
	for scanner.Scan() {
		line := decodeLine(scanner.Bytes())
		_, ruleOK := processLine(line, stats, moneyRules, trackers)
		if ruleOK {
			matchedAny = true
			notifyMatch(line, moneyRules, trackers)
		}
	}
	if err := scanner.Err(); err != nil {
		return matchedAny, err
	}

	st.offset += int64(len(raw))
	return matchedAny, nil
}

// notifyMatch wypisuje na konsoli krótkie powiadomienie, gdy linia
// pasuje do któregoś z trackerów lub reguł pieniężnych (przydatne w trybie
// --watch, żeby zobaczyć na żywo np. "💰 Sprzedaż towaru (6748.80$)" albo
// "🎉 Nagroda dzienna").
func notifyMatch(line string, moneyRules []compiledMoneyRule, trackers []compiledTracker) {
	rest := line
	if m := dateRe.FindStringSubmatch(line); m != nil {
		rest = m[2]
	}

	for _, r := range moneyRules {
		rm := r.Re.FindStringSubmatch(rest)
		if rm == nil {
			continue
		}
		icon := "💰"
		if r.Kind == "expense" {
			icon = "💸"
		}
		detail := ""
		if amtStr, ok := namedGroup(r.Re, rm, "amount"); ok {
			detail = fmt.Sprintf(" (%.2f$)", parseMoneyAmount(amtStr))
		}
		fmt.Printf("  %s %s%s\n", icon, r.Name, detail)
		break // ta sama zasada co w processLine: pierwsze dopasowanie wygrywa
	}

	for _, t := range trackers {
		if t.Re.MatchString(rest) {
			fmt.Printf("  🎉 Wykryto: %s\n", t.Name)
		}
	}
}
