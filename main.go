package main

import (
	"bufio"
	"encoding/csv"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// getLogFiles zwraca pliki logów z podanego katalogu MTA: bieżący
// console.log oraz jego zrotowane wersje console.log.1, console.log.2 itd.
// (MTA rotuje logi numerowanym sufiksem, bez końcówki ".log" na
// zrotowanych plikach — dlatego dopasowujemy po PREFIKSIE "console.log",
// a nie po sufiksie ".log", żeby nie pomijać starszych, zrotowanych logów).
//
// Posortowane naturalnie (numerycznie) rosnąco wg numeru rotacji, żeby
// console.log.2 nie trafiał przed console.log.10 tak jak przy zwykłym
// sortowaniu alfabetycznym stringów — w praktyce kolejność przetwarzania
// plików nie wpływa na wynik (dane ze wszystkich plików i tak są
// sumowane do wspólnej mapy po dacie), ale czytelna kolejność ułatwia
// debugowanie i komunikaty w konsoli.
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
		if strings.HasPrefix(name, "console.log") {
			files = append(files, filepath.Join(dir, name))
		}
	}
	sort.Slice(files, func(i, j int) bool {
		return logFileSortKey(files[i]) < logFileSortKey(files[j])
	})
	return files, nil
}

// trackerNamesFrom zwraca posortowaną listę nazw skonfigurowanych trackerów.
// Używana, żeby raport/CSV pokazywały jawne TAK/NIE dla KAŻDEGO
// skonfigurowanego trackera każdego dnia — nie tylko tych, które akurat
// danego dnia wystąpiły.
func trackerNamesFrom(trackers []compiledTracker) []string {
	names := make([]string, 0, len(trackers))
	for _, t := range trackers {
		names = append(names, t.Name)
	}
	sort.Strings(names)
	return names
}

// trackerStatus zwraca czytelny status "TAK"/"TAK (detal)"/"NIE" dla
// danego trackera w danym dniu.
func trackerStatus(d *dayStats, name string) string {
	if hit, ok := d.trackers[name]; ok && hit.Detected {
		if hit.Detail != "" {
			return fmt.Sprintf("TAK (%s)", hit.Detail)
		}
		return "TAK"
	}
	return "NIE"
}

// writeReport zapisuje bieżący stan stats do pliku raportu (nadpisując go).
// trackerNames to lista WSZYSTKICH skonfigurowanych trackerów — dzięki temu
// dzień, w którym np. nagroda dzienna NIE została odebrana, też to jawnie
// pokazuje, zamiast po prostu pomijać tracker w tym dniu.
// logFileSortKey zwraca klucz sortowania dla nazwy pliku loga: 0 dla
// bieżącego "console.log", a numer rotacji dla "console.log.N" — dzięki
// temu sortowanie jest numeryczne, a nie alfabetyczne (co błędnie
// umieściłoby "console.log.10" przed "console.log.2").
func logFileSortKey(path string) int {
	name := filepath.Base(path)
	suffix := strings.TrimPrefix(name, "console.log")
	suffix = strings.TrimPrefix(suffix, ".")
	if suffix == "" {
		return 0
	}
	n, err := strconv.Atoi(suffix)
	if err != nil {
		// Nietypowa nazwa (np. "console.log.bak") — na koniec listy,
		// żeby nie zaburzać kolejności normalnych rotacji.
		return 1 << 30
	}
	return n
}

func writeReport(path string, stats map[string]*dayStats, trackerNames []string) error {
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

	var totalIncome, totalExpense float64
	totalXP := 0
	trackerHits := make(map[string]int, len(trackerNames))

	for _, date := range dates {
		d := stats[date]
		net := d.income - d.expense
		fmt.Fprintf(w, "%s -> %.2f$ netto (przychód %.2f$, wydatki %.2f$) | %d XP",
			date, net, d.income, d.expense, d.xp)

		for _, name := range trackerNames {
			status := trackerStatus(d, name)
			fmt.Fprintf(w, " | %s: %s", name, status)
			if hit, ok := d.trackers[name]; ok && hit.Detected {
				trackerHits[name]++
			}
		}
		fmt.Fprintln(w)

		totalIncome += d.income
		totalExpense += d.expense
		totalXP += d.xp
	}

	days := len(dates)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "=== PODSUMOWANIE ===")
	fmt.Fprintf(w, "Liczba dni w raporcie: %d\n", days)
	fmt.Fprintf(w, "Łączny przychód: %.2f$\n", totalIncome)
	fmt.Fprintf(w, "Łączne wydatki: %.2f$\n", totalExpense)
	fmt.Fprintf(w, "Saldo netto: %.2f$\n", totalIncome-totalExpense)
	fmt.Fprintf(w, "Łączne XP: %d\n", totalXP)
	for _, name := range trackerNames {
		fmt.Fprintf(w, "%s: %d/%d dni\n", name, trackerHits[name], days)
	}

	return nil
}

// formatCSVNumber formatuje liczbę z przecinkiem jako separatorem
// dziesiętnym — tak, jak oczekuje polski Excel przy separatorze ';'.
func formatCSVNumber(v float64) string {
	return strings.Replace(fmt.Sprintf("%.2f", v), ".", ",", 1)
}

// writeCSVReport zapisuje bieżący stan stats jako tabelę CSV, gotową do
// otwarcia w Excelu/Arkuszach Google. Separator ';' i przecinek dziesiętny
// dobrane pod polską wersję Excela; plik ma BOM, żeby polskie znaki
// wyświetlały się poprawnie.
func writeCSVReport(path string, stats map[string]*dayStats, trackerNames []string) error {
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

	if _, err := out.WriteString("\ufeff"); err != nil {
		return err
	}

	w := csv.NewWriter(out)
	w.Comma = ';'
	defer w.Flush()

	header := append([]string{"Data", "Przychod", "Wydatki", "Netto", "XP"}, trackerNames...)
	if err := w.Write(header); err != nil {
		return err
	}

	var totalIncome, totalExpense float64
	totalXP := 0
	trackerHits := make(map[string]int, len(trackerNames))

	for _, date := range dates {
		d := stats[date]
		row := []string{
			date,
			formatCSVNumber(d.income),
			formatCSVNumber(d.expense),
			formatCSVNumber(d.income - d.expense),
			strconv.Itoa(d.xp),
		}
		for _, name := range trackerNames {
			row = append(row, trackerStatus(d, name))
			if hit, ok := d.trackers[name]; ok && hit.Detected {
				trackerHits[name]++
			}
		}
		if err := w.Write(row); err != nil {
			return err
		}

		totalIncome += d.income
		totalExpense += d.expense
		totalXP += d.xp
	}

	// Wiersz podsumowujący na końcu — łączny przychód/wydatki/netto/XP,
	// a dla trackerów liczba dni "X/Y", w których dana akcja wystąpiła.
	days := len(dates)
	summaryRow := []string{
		"SUMA",
		formatCSVNumber(totalIncome),
		formatCSVNumber(totalExpense),
		formatCSVNumber(totalIncome - totalExpense),
		strconv.Itoa(totalXP),
	}
	for _, name := range trackerNames {
		summaryRow = append(summaryRow, fmt.Sprintf("%d/%d dni", trackerHits[name], days))
	}
	if err := w.Write(summaryRow); err != nil {
		return err
	}

	return w.Error()
}

// runInteractiveMenu pokazuje proste menu tekstowe i wykonuje wybraną akcję.
// Używane, gdy program uruchomiono bez żadnych flag sterujących trybem
// (czyli najczęściej: zwykłe podwójne kliknięcie .exe).
func runInteractiveMenu(reader *bufio.Reader, stats map[string]*dayStats, moneyRules []compiledMoneyRule, trackers []compiledTracker, cfg *Config, reportPath, csvPath string) error {
	names := trackerNamesFrom(trackers)

	for {
		fmt.Println()
		fmt.Println("Co chcesz zrobić?")
		fmt.Println("  1. Wygeneruj raport.txt")
		fmt.Println("  2. Wygeneruj tabelę CSV")
		fmt.Println("  3. Uruchom w trybie obserwacji na żywo")
		fmt.Println("  4. Wygeneruj raport.txt oraz CSV")
		fmt.Println("  0. Wyjdź")
		fmt.Print("Wybór: ")

		line, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("nie udało się odczytać wyboru: %w", err)
		}
		choice := strings.TrimSpace(line)

		switch choice {
		case "1":
			if err := writeReport(reportPath, stats, names); err != nil {
				return err
			}
			fmt.Println("Raport wygenerowany:", reportPath)
			return nil

		case "2":
			if err := writeCSVReport(csvPath, stats, names); err != nil {
				return err
			}
			fmt.Println("Plik CSV wygenerowany:", csvPath)
			return nil

		case "3":
			if err := writeReport(reportPath, stats, names); err != nil {
				return err
			}
			fmt.Println("Raport wygenerowany:", reportPath)
			return runWatch(cfg.MTAPath, stats, moneyRules, trackers, reportPath)

		case "4":
			if err := writeReport(reportPath, stats, names); err != nil {
				return err
			}
			if err := writeCSVReport(csvPath, stats, names); err != nil {
				return err
			}
			fmt.Println("Raport wygenerowany:", reportPath)
			fmt.Println("Plik CSV wygenerowany:", csvPath)
			return nil

		case "0":
			return nil

		default:
			fmt.Println("Nieprawidłowy wybór, spróbuj ponownie.")
		}
	}
}

// printParseDiagnostics wypisuje krótkie podsumowanie parsowania logów oraz,
// jeśli coś wygląda podejrzanie (zero rozpoznanych linii, zero dopasowanych
// reguł mimo poprawnych znaczników czasu), praktyczne wskazówki — żeby przy
// zgłoszeniu "program nic nie pokazuje" dało się to zdiagnozować z samego
// komunikatu, bez proszenia użytkownika o pliki.
// printActiveRules wypisuje, ile reguł/trackerów faktycznie się skompilowało
// z config.json. Jeśli ta liczba jest niższa niż oczekujesz (albo zerowa),
// to znaczy że któreś wpisy zostały po cichu odrzucone wcześniej (patrz
// ostrzeżenia "Uwaga: pomijam..." wypisywane przez compileMoneyRules/
// compileTrackers) — zamiast dopiero zgadywać przy pustym raporcie.
func printActiveRules(moneyRules []compiledMoneyRule, trackers []compiledTracker) {
	names := make([]string, 0, len(moneyRules))
	for _, r := range moneyRules {
		names = append(names, r.Name)
	}
	trackerNames := make([]string, 0, len(trackers))
	for _, t := range trackers {
		trackerNames = append(trackerNames, t.Name)
	}
	fmt.Printf("Aktywne reguły przychodów/wydatków (%d): %s\n", len(names), strings.Join(names, ", "))
	fmt.Printf("Aktywne trackery (%d): %s\n\n", len(trackerNames), strings.Join(trackerNames, ", "))
}

func printParseDiagnostics(s parseSummary) {
	fmt.Printf("Przetworzono %d linii logów (rozpoznany znacznik czasu: %d, dopasowane zdarzenia: %d)\n",
		s.lines, s.dateMatched, s.ruleMatched)

	if s.lines == 0 {
		fmt.Println("⚠ Pliki logów są puste — nie ma czego analizować.")
		return
	}

	if s.dateMatched == 0 {
		fmt.Println("⚠ Żadna linia nie pasuje do oczekiwanego formatu znacznika czasu \"[RRRR-MM-DD GG:MM:SS]\".")
		fmt.Println("  Możliwe przyczyny: nietypowe kodowanie pliku (program próbuje wykryć")
		fmt.Println("  UTF-8/Windows-1250/UTF-16 automatycznie) albo inny format logów w Twojej wersji MTA.")
		if s.sampleLine != "" {
			fmt.Println("  Przykładowa linia z pliku (dla porównania z oczekiwanym formatem):")
			fmt.Println("   ", s.sampleLine)
		}
		return
	}

	if s.ruleMatched == 0 {
		fmt.Println("⚠ Znaczniki czasu są rozpoznawane, ale żadna linia nie pasuje do skonfigurowanych")
		fmt.Println("  reguł przychodów/wydatków ani trackerów.")
		switch {
		case s.sampleNearMissLine != "":
			fmt.Println("  Ta linia wygląda jak zdarzenie finansowe, ale nie pasuje do żadnej reguły")
			fmt.Println("  w config.json — porównaj jej format z wzorcami w sekcji \"money_rules\"/\"trackers\":")
			fmt.Println("   ", s.sampleNearMissLine)
		case s.sampleMatchedDateLine != "":
			fmt.Println("  Nie znalazłem w tych logach żadnej linii zawierającej \"$\" ani \"XP\" —")
			fmt.Println("  wygląda na to, że w tej sesji faktycznie nie doszło do żadnego zarejestrowanego")
			fmt.Println("  zarobku/wydatku/nagrody (a nie że reguły są zepsute). Przykładowa linia z logów:")
			fmt.Println("   ", s.sampleMatchedDateLine)
		}
	}
}

// appVersion identyfikuje wersję kodu — wypisywana na starcie programu,
// żeby przy problemach dało się jednoznacznie stwierdzić, czy uruchomiony
// .exe faktycznie odpowiada najnowszym plikom źródłowym, bez zgadywania
// (np. czy build w GoLandzie/CI nie użył starego cache).
// Podbijaj tę wartość przy każdej istotnej zmianie.
const appVersion = "1.2.2-dev (obsługa XP/EXP + nagroda dzienna w dowolnej walucie + wymiana P2P + rotacja logów)"

func main() {
	fmt.Println("ProjectRPG Earnings Scanner —", appVersion)
	fmt.Println()

	setPath := flag.Bool("set-path", false, "wymuś ponowne ustawienie ścieżki do katalogu logów MTA")
	watch := flag.Bool("watch", false, "uruchom w trybie ciągłej obserwacji logów na żywo (pomija menu)")
	reportFlag := flag.String("out", "raport.txt", "ścieżka pliku wynikowego raportu")
	csvFlag := flag.String("csv", "raport.csv", "ścieżka pliku CSV; podanie tej flagi jawnie pomija menu i od razu generuje CSV")
	flag.Parse()

	// Jeśli użytkownik jawnie podał flagę --watch lub --csv, traktujemy to
	// jako uruchomienie "automatyczne" (np. skrypt/harmonogram zadań) i
	// pomijamy interaktywne menu — program po prostu robi to, co kazano.
	flagsGiven := map[string]bool{}
	flag.Visit(func(f *flag.Flag) { flagsGiven[f.Name] = true })
	nonInteractive := flagsGiven["watch"] || flagsGiven["csv"]

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
	moneyRules := compileMoneyRules(cfg.MoneyRules)
	printActiveRules(moneyRules, trackers)

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
	var totalSummary parseSummary
	for _, f := range files {
		summary, err := parseFile(f, stats, moneyRules, trackers)
		totalSummary.add(summary)
		if err != nil {
			// Jeden uszkodzony/zablokowany plik nie powinien wywalać
			// całego raportu — informujemy i kontynuujemy z resztą.
			fmt.Printf("Uwaga: nie udało się przetworzyć %s: %v\n", f, err)
		}
	}
	printParseDiagnostics(totalSummary)

	if nonInteractive {
		names := trackerNamesFrom(trackers)

		if err := writeReport(*reportFlag, stats, names); err != nil {
			fmt.Println("Błąd zapisu raportu:", err)
			pauseBeforeExit(reader)
			os.Exit(1)
		}
		fmt.Println("Raport wygenerowany:", *reportFlag)

		if flagsGiven["csv"] {
			if err := writeCSVReport(*csvFlag, stats, names); err != nil {
				fmt.Println("Błąd zapisu CSV:", err)
				pauseBeforeExit(reader)
				os.Exit(1)
			}
			fmt.Println("Plik CSV wygenerowany:", *csvFlag)
		}

		if *watch {
			if err := runWatch(cfg.MTAPath, stats, moneyRules, trackers, *reportFlag); err != nil {
				fmt.Println("Błąd trybu obserwacji:", err)
				pauseBeforeExit(reader)
				os.Exit(1)
			}
			return
		}

		pauseBeforeExit(reader)
		return
	}

	// Brak flag sterujących trybem => zwykłe podwójne kliknięcie .exe.
	// Pytamy, co użytkownik chce zrobić, zamiast zgadywać.
	if err := runInteractiveMenu(reader, stats, moneyRules, trackers, cfg, *reportFlag, *csvFlag); err != nil {
		fmt.Println("Błąd:", err)
		pauseBeforeExit(reader)
		os.Exit(1)
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
		cfg = &Config{Trackers: defaultTrackers(), MoneyRules: defaultMoneyRules()}
	}
	fmt.Println("Ustawianie nowej ścieżki do katalogu logów MTA.")
	cfg.MTAPath = promptForPath(reader)
	return cfg.Save(path)
}

func pauseBeforeExit(reader *bufio.Reader) {
	fmt.Println("Naciśnij Enter, aby zamknąć...")
	reader.ReadString('\n')
}
