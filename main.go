package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
)

func getLogPath() (string, error) {
	paths := []string{
		`C:\Program Files (x86)\MTA San Andreas 1.6\MTA\logs`,
		`C:\Program Files\MTA San Andreas 1.6\MTA\logs`,
	}

	for _, p := range paths {
		info, err := os.Stat(p)
		if err == nil && info.IsDir() {
			return p, nil
		}
	}

	return "", fmt.Errorf("nie znaleziono MTA logs")
}

func getLogFiles(dir string) ([]string, error) {
	var files []string

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	for _, e := range entries {
		name := e.Name()

		if name == "console.log" || len(name) >= 11 && name[:11] == "console.log" {
			files = append(files, filepath.Join(dir, name))
		}
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i] < files[j]
	})

	return files, nil
}

type dayStats struct {
	money float64
	xp    int
}

func parseFile(path string, stats map[string]*dayStats, re *regexp.Regexp) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()

		match := re.FindStringSubmatch(line)
		if len(match) > 3 {
			date := match[1]

			money, _ := strconv.ParseFloat(match[2], 64)
			xp, _ := strconv.Atoi(match[3])

			if _, ok := stats[date]; !ok {
				stats[date] = &dayStats{}
			}

			stats[date].money += money
			stats[date].xp += xp
		}
	}

	return scanner.Err()
}

func main() {
	logPath, err := getLogPath()
	if err != nil {
		panic(err)
	}

	files, err := getLogFiles(logPath)
	if err != nil {
		panic(err)
	}

	re := regexp.MustCompile(`\[(\d{4}-\d{2}-\d{2}) [^\]]+\].*Otrzymałeś ([0-9]+(?:\.[0-9]+)?)\$.*\+([0-9]+) XP`)

	stats := make(map[string]*dayStats)

	for _, f := range files {
		err := parseFile(f, stats, re)
		if err != nil {
			panic(err)
		}
	}

	var dates []string
	for d := range stats {
		dates = append(dates, d)
	}
	sort.Slice(dates, func(i, j int) bool {
		return dates[i] > dates[j]
	})

	out, err := os.Create("raport.txt")
	if err != nil {
		panic(err)
	}
	defer out.Close()

	out.WriteString("=== RAPORT DZIENNY ===\n")

	for _, date := range dates {
		d := stats[date]
		out.WriteString(fmt.Sprintf("%s -> %.2f$ | %d XP\n", date, d.money, d.xp))
	}

	fmt.Println("Raport wygenerowany: raport.txt")
	fmt.Println("Naciśnij Enter, aby zamknąć...")
	fmt.Scanln()
}
