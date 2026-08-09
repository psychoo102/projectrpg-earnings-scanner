package main

import (
	"bufio"
	"bytes"
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
//
// Zwraca dwie informacje diagnostyczne:
//   - dateMatched: czy linia w ogóle miała rozpoznawalny znacznik czasu
//     "[RRRR-MM-DD GG:MM:SS]". Jeśli to zawsze false dla całego pliku,
//     to najczęściej oznacza złe kodowanie pliku albo inny format logów.
//   - ruleMatched: czy linia dopasowała się do jakiejkolwiek reguły
//     pieniężnej lub trackera (przydatne też dla trybu --watch).
//
// Uwaga: dla reguł pieniężnych stosujemy zasadę "pierwsze dopasowanie
// wygrywa" (przerywamy po pierwszej pasującej regule) — dzięki temu jedna
// linia loga nigdy nie zostanie policzona podwójnie, nawet gdyby dwie
// reguły przypadkiem się pokrywały.
func processLine(line string, stats map[string]*dayStats, moneyRules []compiledMoneyRule, trackers []compiledTracker) (dateMatched bool, ruleMatched bool) {
	m := dateRe.FindStringSubmatch(line)
	if m == nil {
		return false, false
	}
	date := m[1]
	rest := m[2]

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
		ruleMatched = true
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
		ruleMatched = true
	}

	return true, ruleMatched
}

func dayEntry(stats map[string]*dayStats, date string) *dayStats {
	d, ok := stats[date]
	if !ok {
		d = newDayStats()
		stats[date] = d
	}
	return d
}

// parseSummary to statystyki diagnostyczne z parsowania jednego lub
// wielu plików — pomaga wykryć np. zły format kodowania pliku bez
// potrzeby ręcznego debugowania.
type parseSummary struct {
	lines       int
	dateMatched int
	ruleMatched int
	// sampleLine to pierwsza niepusta linia w ogóle (przydatna, gdy nie
	// złapano żadnej daty — pokazuje "z czym program w ogóle miał do
	// czynienia").
	sampleLine string
	// sampleMatchedDateLine to pierwsza linia z poprawnie rozpoznaną datą,
	// która NIE dopasowała żadnej reguły/trackera — pokazywana tylko jako
	// ostateczny fallback, gdy nie znaleziono nic lepszego (patrz niżej).
	sampleMatchedDateLine string
	// sampleNearMissLine to pierwsza niedopasowana linia z datą, która mimo
	// to "wygląda" jak zdarzenie finansowe (zawiera "$", "XP" itp.) — dużo
	// bardziej użyteczna do diagnozy niż zupełnie przypadkowa pierwsza
	// niedopasowana linia (która często jest np. komunikatem połączenia).
	sampleNearMissLine string
}

func (s *parseSummary) add(other parseSummary) {
	s.lines += other.lines
	s.dateMatched += other.dateMatched
	s.ruleMatched += other.ruleMatched
	if s.sampleLine == "" {
		s.sampleLine = other.sampleLine
	}
	if s.sampleMatchedDateLine == "" {
		s.sampleMatchedDateLine = other.sampleMatchedDateLine
	}
	if s.sampleNearMissLine == "" {
		s.sampleNearMissLine = other.sampleNearMissLine
	}
}

// looksLikeMoneyEvent to prosty heurystyczny test "czy ta linia mogła być
// próbą zarobku/wydatku" — używany wyłącznie do diagnostyki (żeby nie
// pokazywać użytkownikowi losowej, nieistotnej linii jako przykładu).
func looksLikeMoneyEvent(line string) bool {
	return strings.Contains(line, "$") || strings.Contains(line, "XP")
}

// parseFile czyta cały plik, wykrywa i konwertuje jego kodowanie do UTF-8
// (patrz encoding.go), po czym przepuszcza każdą linię przez processLine.
func parseFile(path string, stats map[string]*dayStats, moneyRules []compiledMoneyRule, trackers []compiledTracker) (parseSummary, error) {
	var summary parseSummary

	raw, err := os.ReadFile(path)
	if err != nil {
		return summary, err
	}
	data := decodeFileBytes(raw)

	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 1024), 1024*1024)
	for scanner.Scan() {
		line := decodeLine(scanner.Bytes())
		summary.lines++
		if summary.sampleLine == "" && strings.TrimSpace(line) != "" {
			summary.sampleLine = line
		}
		dateOK, ruleOK := processLine(line, stats, moneyRules, trackers)
		if dateOK {
			summary.dateMatched++
			if !ruleOK {
				if summary.sampleMatchedDateLine == "" {
					summary.sampleMatchedDateLine = line
				}
				if summary.sampleNearMissLine == "" && looksLikeMoneyEvent(line) {
					summary.sampleNearMissLine = line
				}
			}
		}
		if ruleOK {
			summary.ruleMatched++
		}
	}
	return summary, scanner.Err()
}
