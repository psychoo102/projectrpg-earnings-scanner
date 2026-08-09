package main

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// dateRe wyłuskuje datę z początku linii loga, np. "[2026-08-08 04:18:11]".
var dateRe = regexp.MustCompile(`^\[(\d{4}-\d{2}-\d{2}) [^\]]+\]\s*(.*)$`)

// compiledTracker to Tracker po skompilowaniu wzorca do regexp.Regexp.
type compiledTracker struct {
	Name string
	Re   *regexp.Regexp
}

// compiledMoneyRule to MoneyRule po skompilowaniu wzorca do regexp.Regexp.
type compiledMoneyRule struct {
	Name string
	Kind string
	Re   *regexp.Regexp
}

// compileMoneyRules kompiluje reguły przychodów/wydatków z configu.
// Błędne wzorce oraz nieznane "kind" są pomijane z ostrzeżeniem, żeby
// literówka w JSON-ie nie wywalała całego programu.
func compileMoneyRules(rules []MoneyRule) []compiledMoneyRule {
	compiled := make([]compiledMoneyRule, 0, len(rules))
	for _, r := range rules {
		if r.Kind != "income" && r.Kind != "expense" {
			fmt.Printf("Uwaga: pomijam regułę %q — 'kind' musi być \"income\" albo \"expense\" (jest: %q)\n", r.Name, r.Kind)
			continue
		}
		re, err := regexp.Compile(r.Pattern)
		if err != nil {
			fmt.Printf("Uwaga: pomijam regułę %q — błędny wzorzec regex: %v\n", r.Name, err)
			continue
		}
		if !hasNamedGroup(re, "amount") && !hasNamedGroup(re, "xp") {
			fmt.Printf("Uwaga: reguła %q nie ma grupy (?P<amount>...) ani (?P<xp>...) — nic nie policzy.\n", r.Name)
		}
		compiled = append(compiled, compiledMoneyRule{Name: r.Name, Kind: r.Kind, Re: re})
	}
	return compiled
}

func hasNamedGroup(re *regexp.Regexp, name string) bool {
	for _, n := range re.SubexpNames() {
		if n == name {
			return true
		}
	}
	return false
}

// namedGroup zwraca wartość nazwanej grupy przechwytującej z dopasowania,
// jeśli istnieje i faktycznie coś przechwyciła.
func namedGroup(re *regexp.Regexp, match []string, name string) (string, bool) {
	for i, n := range re.SubexpNames() {
		if n == name && i < len(match) && match[i] != "" {
			return match[i], true
		}
	}
	return "", false
}

// parseMoneyAmount parsuje kwotę, która może zawierać przecinek jako
// separator tysięcy (np. "6,748.80").
func parseMoneyAmount(s string) float64 {
	cleaned := strings.ReplaceAll(s, ",", "")
	v, _ := strconv.ParseFloat(cleaned, 64)
	return v
}

// compileTrackers kompiluje wzorce z configu. Błędne wzorce są pomijane
// z ostrzeżeniem, żeby literówka w JSON-ie nie wywalała całego programu.
func compileTrackers(trackers []Tracker) []compiledTracker {
	compiled := make([]compiledTracker, 0, len(trackers))
	for _, t := range trackers {
		re, err := regexp.Compile(t.Pattern)
		if err != nil {
			fmt.Printf("Uwaga: pomijam tracker %q — błędny wzorzec regex: %v\n", t.Name, err)
			continue
		}
		compiled = append(compiled, compiledTracker{Name: t.Name, Re: re})
	}
	return compiled
}

// trackerHit to wynik wykrycia jednej akcji w danym dniu.
type trackerHit struct {
	Detected bool
	// Detail to opcjonalna wartość z pierwszej grupy przechwytującej
	// wzorca (np. numer dnia streaka "Dzień 12"). Puste, jeśli wzorzec
	// nie ma grupy albo nic nie przechwycił.
	Detail string
}

// dayStats agreguje wszystkie dane dla jednego dnia kalendarzowego.
type dayStats struct {
	income   float64
	expense  float64
	xp       int
	trackers map[string]*trackerHit
}

func newDayStats() *dayStats {
	return &dayStats{trackers: make(map[string]*trackerHit)}
}

// processLine parsuje pojedynczą linię loga i aktualizuje stats.
// Zwraca true, jeśli cokolwiek zostało dopasowane (przydatne dla trybu --watch).
//
// Uwaga: dla reguł pieniężnych stosujemy zasadę "pierwsze dopasowanie
// wygrywa" (przerywamy po pierwszej pasującej regule) — dzięki temu jedna
// linia loga nigdy nie zostanie policzona podwójnie, nawet gdyby dwie
// reguły przypadkiem się pokrywały.
func processLine(line string, stats map[string]*dayStats, moneyRules []compiledMoneyRule, trackers []compiledTracker) bool {
	m := dateRe.FindStringSubmatch(line)
	if m == nil {
		return false
	}
	date := m[1]
	rest := m[2]
	matched := false

	for _, r := range moneyRules {
		rm := r.Re.FindStringSubmatch(rest)
		if rm == nil {
			continue
		}
		d := dayEntry(stats, date)
		if amtStr, ok := namedGroup(r.Re, rm, "amount"); ok {
			amount := parseMoneyAmount(amtStr)
			switch r.Kind {
			case "income":
				d.income += amount
			case "expense":
				d.expense += amount
			}
		}
		if xpStr, ok := namedGroup(r.Re, rm, "xp"); ok {
			xp, _ := strconv.Atoi(xpStr)
			d.xp += xp
		}
		matched = true
		break
	}

	for _, t := range trackers {
		tm := t.Re.FindStringSubmatch(rest)
		if tm == nil {
			continue
		}
		d := dayEntry(stats, date)
		hit := d.trackers[t.Name]
		if hit == nil {
			hit = &trackerHit{}
			d.trackers[t.Name] = hit
		}
		hit.Detected = true
		if len(tm) > 1 && tm[1] != "" {
			hit.Detail = tm[1]
		}
		matched = true
	}

	return matched
}

func dayEntry(stats map[string]*dayStats, date string) *dayStats {
	d, ok := stats[date]
	if !ok {
		d = newDayStats()
		stats[date] = d
	}
	return d
}

// parseFile czyta cały plik od początku i przepuszcza każdą linię przez processLine.
func parseFile(path string, stats map[string]*dayStats, moneyRules []compiledMoneyRule, trackers []compiledTracker) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024), 1024*1024)
	for scanner.Scan() {
		processLine(scanner.Text(), stats, moneyRules, trackers)
	}
	return scanner.Err()
}
