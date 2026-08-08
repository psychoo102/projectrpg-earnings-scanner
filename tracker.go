package main

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strconv"
)

// dateRe wyłuskuje datę z początku linii loga, np. "[2026-08-08 04:18:11]".
var dateRe = regexp.MustCompile(`^\[(\d{4}-\d{2}-\d{2}) [^\]]+\]\s*(.*)$`)

// earningsRe wyłuskuje standardowe wpisy o zarobku + XP,
// np. "Otrzymałeś 150.50$ ... +25 XP".
var earningsRe = regexp.MustCompile(`Otrzymałeś ([0-9]+(?:\.[0-9]+)?)\$.*\+([0-9]+) XP`)

// compiledTracker to Tracker po skompilowaniu wzorca do regexp.Regexp.
type compiledTracker struct {
	Name string
	Re   *regexp.Regexp
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
	money    float64
	xp       int
	trackers map[string]*trackerHit
}

func newDayStats() *dayStats {
	return &dayStats{trackers: make(map[string]*trackerHit)}
}

// processLine parsuje pojedynczą linię loga i aktualizuje stats.
// Zwraca true, jeśli cokolwiek zostało dopasowane (przydatne dla trybu --watch).
func processLine(line string, stats map[string]*dayStats, trackers []compiledTracker) bool {
	m := dateRe.FindStringSubmatch(line)
	if m == nil {
		return false
	}
	date := m[1]
	rest := m[2]
	matched := false

	if em := earningsRe.FindStringSubmatch(rest); em != nil {
		money, _ := strconv.ParseFloat(em[1], 64)
		xp, _ := strconv.Atoi(em[2])
		d := dayEntry(stats, date)
		d.money += money
		d.xp += xp
		matched = true
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
func parseFile(path string, stats map[string]*dayStats, trackers []compiledTracker) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024), 1024*1024)
	for scanner.Scan() {
		processLine(scanner.Text(), stats, trackers)
	}
	return scanner.Err()
}
