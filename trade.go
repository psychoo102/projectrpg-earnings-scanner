package main

import (
	"regexp"
	"strings"
)

// tradeState śledzi stan pojedynczej sekwencji "wymiany" (handlu P2P
// z innym graczem) w logu. W przeciwieństwie do reguł w money_rules, to
// zdarzenie rozciąga się na kilka kolejnych linii:
//
//	ⓘ Otrzymałeś:
//	  -- Punkty Premium: 500PP
//	ⓘ Oddając:
//	  -- Gotówkę: 6,500$
//	✔ Wymiana zakończona sukcesem.
//
// żeby wiedzieć, czy kwota przy "Gotówkę: X$" to przychód czy wydatek,
// trzeba pamiętać, w której sekcji ("Otrzymałeś"/"Oddając") się właśnie
// jesteśmy — stąd osobny, stanowy parser zamiast zwykłej reguły regex.
//
// Kwoty są zbierane TYMCZASOWO (w polach income/expense poniżej) i trafiają
// do stats DOPIERO przy napotkaniu "Wymiana zakończona sukcesem." — dzięki
// temu anulowana albo nieudana wymiana (bez tego potwierdzenia) nigdy nie
// zostanie policzona jako prawdziwy przychód/wydatek.
type tradeState struct {
	active  bool
	date    string
	section string // "" | "receiving" | "giving"
	income  float64
	expense float64
}

// tradeCashRe wyłuskuje kwotę gotówki z linii pozycji wymiany, np.
// "  -- Gotówkę: 6,500$" (obsługuje też wariant ze spacją przed $,
// "3,900 $", zaobserwowany w rzeczywistych logach).
var tradeCashRe = regexp.MustCompile(`--\s*Gotówkę:\s*(?P<amount>` + amountPattern + `)\s*\$`)

const (
	tradeReceivingHint = "Otrzymałeś:"
	tradeGivingHint    = "Oddając:"
	tradeSuccessHint   = "Wymiana zakończona sukcesem"
)

// processLine obsługuje jedną (już zdekodowaną i pozbawioną znacznika
// czasu) linię pod kątem sekwencji wymiany P2P. Zwraca true, jeśli linia
// była rozpoznaną częścią takiej sekwencji — używane przy diagnostyce
// ("dopasowane zdarzenia"), żeby te linie nie raportowały się jako
// niedopasowane.
func (ts *tradeState) processLine(date, rest string, stats map[string]*dayStats) bool {
	if isChatLine(rest) {
		return false
	}

	if strings.Contains(rest, tradeReceivingHint) {
		ts.active = true
		ts.date = date
		ts.section = "receiving"
		return true
	}

	if strings.Contains(rest, tradeGivingHint) {
		ts.active = true
		ts.date = date
		ts.section = "giving"
		return true
	}

	if ts.active && ts.section != "" {
		if m := tradeCashRe.FindStringSubmatch(rest); m != nil {
			if amtStr, ok := namedGroup(tradeCashRe, m, "amount"); ok {
				amount := parseMoneyAmount(amtStr)
				switch ts.section {
				case "receiving":
					ts.income += amount
				case "giving":
					ts.expense += amount
				}
			}
			return true
		}
	}

	if strings.Contains(rest, tradeSuccessHint) {
		if ts.active {
			d := dayEntry(stats, ts.date)
			d.income += ts.income
			d.expense += ts.expense
		}
		*ts = tradeState{}
		return true
	}

	return false
}
