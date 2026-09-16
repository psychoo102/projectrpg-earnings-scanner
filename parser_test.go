package main

import "testing"

func TestStandardEarningsWithoutXP(t *testing.T) {
	rules := defaultMoneyRules()
	compiled := compileMoneyRules(rules)

	stats := make(map[string]*dayStats)

	matched := processLine(
		"2026-09-14",
		"Otrzymałeś 55.20$ [+119%]",
		stats,
		compiled,
		nil,
	)

	if !matched {
		t.Fatal("linia zarobku nie została wykryta")
	}

	d := stats["2026-09-14"]
	if d == nil {
		t.Fatal("nie utworzono statystyk dla dnia")
	}

	if d.income != 55.20 {
		t.Fatalf("oczekiwano income=55.20, otrzymano %.2f", d.income)
	}

	if d.xp != 0 {
		t.Fatalf("oczekiwano xp=0, otrzymano %d", d.xp)
	}
}

func TestStandardEarningsWithXP(t *testing.T) {
	rules := defaultMoneyRules()
	compiled := compileMoneyRules(rules)

	stats := make(map[string]*dayStats)

	matched := processLine(
		"2026-09-14",
		"Otrzymałeś 55.20$ +119 XP",
		stats,
		compiled,
		nil,
	)

	if !matched {
		t.Fatal("linia zarobku z XP nie została wykryta")
	}

	d := stats["2026-09-14"]
	if d == nil {
		t.Fatal("nie utworzono statystyk dla dnia")
	}

	if d.income != 55.20 {
		t.Fatalf("oczekiwano income=55.20, otrzymano %.2f", d.income)
	}

	if d.xp != 119 {
		t.Fatalf("oczekiwano xp=119, otrzymano %d", d.xp)
	}
}
