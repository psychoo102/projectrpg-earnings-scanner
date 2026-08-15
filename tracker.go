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

// outputPrefixRe usuwa standardowy prefiks "[Output] : " z reszty linii,
// żeby sprawdzić, co faktycznie następuje po nim (patrz isChatLine).
var outputPrefixRe = regexp.MustCompile(`^\[Output\]\s*:\s*`)

// chatLinePrefixRe rozpoznaje typowe prefiksy wiadomości na czacie w MTA,
// np. "OG > (24) CzajKA:", "G> Gracz123:", "<< [124] kxaf:" — czyli
// "kanał + opcjonalny numer + nazwa gracza + dwukropek" na samym początku
// treści.
var chatLinePrefixRe = regexp.MustCompile(`^(?:[A-Za-zżźćńółęąśŻŹĆŃÓŁĘĄŚ]{1,10}\s*>|<<|>>)\s*(?:[\(\[]\d+[\)\]]\s*)?[^\s:]+\s*:`)

// isChatLine sprawdza, czy linia (fragment po znaczniku czasu) wygląda na
// wiadomość na czacie, a nie prawdziwy komunikat systemowy gry. Potrzebne,
// bo gracze potrafią zacytować/wkleić na czacie tekst identyczny z
// prawdziwym komunikatem systemowym (np. serwer publicznie ogłasza czyjś
// kamień milowy streaka logowań) — bez tego sprawdzenia taka linia
// zostałaby błędnie policzona jako prawdziwe zdarzenie.
func isChatLine(rest string) bool {
	content := outputPrefixRe.ReplaceAllString(rest, "")
	return chatLinePrefixRe.MatchString(content)
}

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
// matchDate sprawdza, czy linia ma rozpoznawalny znacznik czasu
// "[RRRR-MM-DD GG:MM:SS]" i jeśli tak, zwraca datę oraz resztę linii.
func matchDate(line string) (date string, rest string, ok bool) {
	m := dateRe.FindStringSubmatch(line)
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}

// processLine dopasowuje POJEDYNCZĄ, już wyodrębnioną linię (data + reszta)
// do reguł pieniężnych i trackerów — to proste, jednoliniowe dopasowania
// regex. Sekwencje wieloliniowe (np. wymiana P2P) obsługuje osobno
// tradeState.processLine (patrz trade.go), wywoływany równolegle w parseFile.
//
// Uwaga: dla reguł pieniężnych stosujemy zasadę "pierwsze dopasowanie
// wygrywa" (przerywamy po pierwszej pasującej regule) — dzięki temu jedna
// linia loga nigdy nie zostanie policzona podwójnie, nawet gdyby dwie
// reguły przypadkiem się pokrywały.
func processLine(date, rest string, stats map[string]*dayStats, moneyRules []compiledMoneyRule, trackers []compiledTracker) (ruleMatched bool) {
	if isChatLine(rest) {
		return false
	}

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

	return ruleMatched
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
// (linia po linii — patrz encoding.go), po czym przepuszcza każdą linię
// przez processLine (proste reguły jednoliniowe) oraz przez tradeState
// (wieloliniowe sekwencje wymiany P2P — patrz trade.go).
func parseFile(path string, stats map[string]*dayStats, moneyRules []compiledMoneyRule, trackers []compiledTracker) (parseSummary, error) {
	var summary parseSummary
	var trade tradeState

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

		date, rest, dateOK := matchDate(line)
		if !dateOK {
			continue
		}
		summary.dateMatched++

		ruleOK := processLine(date, rest, stats, moneyRules, trackers)
		tradeOK := trade.processLine(date, rest, stats)

		if ruleOK || tradeOK {
			summary.ruleMatched++
			continue
		}

		if summary.sampleMatchedDateLine == "" {
			summary.sampleMatchedDateLine = line
		}
		if summary.sampleNearMissLine == "" && looksLikeMoneyEvent(line) && !isChatLine(rest) {
			summary.sampleNearMissLine = line
		}
	}
	return summary, scanner.Err()
}
