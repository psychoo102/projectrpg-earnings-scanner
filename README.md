# projectrpg-earnings-scanner

Earning scanner and log reader for ProjectRPG MTA:SA server.

Program czyta logi klienta MTA:SA (`console*.log`), sumuje dzienne zarobki
i XP oraz wykrywa wybrane akcje wykonywane raz dziennie (np. odebranie
nagrody za codzienne logowanie). Wynik zapisywany jest do `raport.txt`.

## Kodowanie plików logów

Program automatycznie wykrywa i konwertuje kodowanie plików `console*.log`:
UTF-8 (z lub bez BOM), UTF-16 (LE/BE) oraz Windows-1250 ("ANSI" na polskiej
Windows). Nie trzeba nic konfigurować — jeśli plik nie jest poprawnym UTF-8,
program automatycznie zakłada Windows-1250 (najczęstsza przyczyna
"krzaczków" zamiast polskich znaków w logach z Windows) i konwertuje go
przed analizą.

## Diagnostyka

Po każdym uruchomieniu program wypisuje krótkie podsumowanie parsowania,
np.:
```
Przetworzono 1523 linii logów (rozpoznany znacznik czasu: 1518, dopasowane zdarzenia: 812)
```
Jeśli coś wygląda podejrzanie (np. zero rozpoznanych znaczników czasu —
zwykle inny format logów niż oczekiwany, mimo automatycznej konwersji
kodowania), program od razu podpowiada możliwą przyczynę i pokazuje
przykładową linię z pliku do porównania. Przydatne przy zgłoszeniach typu
"program mi nic nie pokazuje".

## Podsumowanie

Zarówno `raport.txt`, jak i `raport.csv` kończą się sekcją podsumowującą
z łącznym przychodem, wydatkami, saldem netto, XP oraz liczbą dni, w których
każdy skonfigurowany tracker wystąpił (np. `Nagroda dzienna: 12/15 dni`).

## Uruchamianie

Program można uruchomić na dwa sposoby:

**1. Bez żadnych flag** (zwykłe podwójne kliknięcie `.exe`) — po wygenerowaniu
statystyk z logów program pokaże proste menu:

```
Co chcesz zrobić?
  1. Wygeneruj raport.txt
  2. Wygeneruj tabelę CSV
  3. Uruchom w trybie obserwacji na żywo
  4. Wygeneruj raport.txt oraz CSV
  0. Wyjdź
Wybór:
```

**2. Z flagami** (np. do automatyzacji, harmonogramu zadań) — podanie flagi
`--watch` lub `--csv` **pomija menu** i program od razu robi to, co kazano:

```
scanner.exe --csv=raport.csv
scanner.exe --watch
```

## Eksport do CSV

Opcja 2/4 w menu (albo flaga `--csv`) generuje `raport.csv` gotowy do
otwarcia w Excelu lub Arkuszach Google — z separatorem `;` i przecinkiem
jako separatorem dziesiętnym (pod polskie ustawienia regionalne Excela),
z kolumnami: `Data`, `Przychod`, `Wydatki`, `Netto`, `XP`, oraz po jednej
kolumnie na każdy skonfigurowany tracker (np. `Nagroda dzienna`) z jawnym
`TAK`/`NIE` dla każdego dnia.

## Budowanie

```
go build -o scanner.exe .
```

## Pierwsze uruchomienie

Przy pierwszym starcie program:

1. Próbuje znaleźć MTA:SA pod standardowymi ścieżkami instalacji Windows.
2. Jeśli się nie uda — poprosi o ręczne podanie ścieżki do katalogu `logs`.
3. Zapisze konfigurację w `%APPDATA%\projectrpg-earnings-scanner\config.json`
   (albo w katalogu obok programu, jeśli system nie udostępnia `%APPDATA%`).

Kolejne uruchomienia korzystają już z zapisanej konfiguracji bez pytań.

Żeby zmienić zapisaną ścieżkę ręcznie, bez usuwania pliku config.json:

```
scanner.exe --set-path
```

## Przychody i wydatki

W `config.json` znajduje się sekcja `money_rules` — lista reguł rozpoznających
w logach konkretne rodzaje przychodu (`"kind": "income"`) lub wydatku
(`"kind": "expense"`). Domyślnie skonfigurowane są cztery reguły:

- **Zarobek (standardowy)** — klasyczny wpis `Otrzymałeś X$ ... +Y XP`.
- **Zrzucenie towaru** — np. `Zrzucono 20.00 kg trawy na stos (+3 XP)` (samo XP, bez kwoty).
- **Sprzedaż towaru** — np. `Sprzedano 760 kg trawy za 6,748.80$` (kwoty z przecinkiem jako separatorem tysięcy są obsługiwane poprawnie).
- **Tankowanie paliwa** — np. `Pomyślnie zatankowano 9.52l LPG za kwotę 31.89$`. Nazwa paliwa (LPG, Pb 95, Pb 98, On, ...) **nie jest zaszyta na sztywno** — wzorzec złapie dowolną nazwę, więc nowe rodzaje paliwa nie wymagają zmiany wzorca.

Każda reguła musi mieć wzorzec z nazwaną grupą `(?P<amount>...)` (kwota)
i/lub `(?P<xp>...)` (zdobyte punkty doświadczenia) — możesz mieć jedną,
drugą albo obie naraz. Przykład dopisania własnej reguły, np. dla innego
rodzaju kosztu:

```json
{
  "name": "Naprawa pojazdu",
  "kind": "expense",
  "pattern": "Naprawiłeś pojazd za (?P<amount>[0-9.,]+)\\$"
}
```

Nie trzeba nic zmieniać w kodzie — wystarczy dopisać wpis do `money_rules`
w `config.json` i uruchomić program ponownie.

Raport pokazuje saldo netto oraz rozbicie na przychód/wydatki:

```
2026-08-09 -> 844.65$ netto (przychód 999.99$, wydatki 155.34$) | 0 XP
2026-08-08 -> 6899.30$ netto (przychód 6899.30$, wydatki 0.00$) | 28 XP | Nagroda dzienna: TAK (1)
```

## Trackery akcji dziennych

W `config.json` znajduje się sekcja `trackers` — lista wzorców (regex)
dopasowywanych do treści linii logów. Domyślnie skonfigurowany jest jeden
tracker: **Nagroda dzienna**, wykrywający wpis w stylu:

```
[2026-08-08 04:18:11] [Output] : ⓘ Otrzymałeś 60 $ za codzienne logowanie, oby tak dalej! (Dzień 1)
```

Możesz dopisać własne trackery, np.:

```json
{
  "name": "Dzienny checkpoint",
  "pattern": "Ukończyłeś dzienny checkpoint"
}
```

Jeśli wzorzec zawiera grupę przechwytującą (nawiasy `(...)`), jej wartość
(np. numer dnia streaka) pojawi się w raporcie obok statusu.

Przykładowy fragment `raport.txt`:

```
=== RAPORT DZIENNY ===
2026-08-08 -> 6899.30$ netto (przychód 6899.30$, wydatki 0.00$) | 28 XP | Nagroda dzienna: TAK (1)
2026-08-07 -> 200.00$ | 50 XP
```

## Tryb obserwacji na żywo (`--watch`)

```
scanner.exe --watch
```

Program po wygenerowaniu pierwszego raportu przechodzi w tryb ciągłej
obserwacji: co 2 sekundy sprawdza, czy w plikach logów pojawiły się nowe
linie (również w nowo utworzonych plikach `console_*.log`, np. po
restarcie gry), na bieżąco aktualizuje statystyki i `raport.txt`, oraz
wypisuje w konsoli powiadomienie przy każdym wykrytym trackerze, np.:

```
  🎉 Wykryto: Nagroda dzienna
[07:00:12] raport.txt zaktualizowany
```

Zatrzymanie: `Ctrl+C`.

## Pozostałe flagi

| Flaga | Opis |
|---|---|
| `--set-path` | Wymusza ponowne ustawienie ścieżki do katalogu logów MTA. |
| `--out <plik>` | Zmienia ścieżkę/nazwę pliku wynikowego raportu tekstowego (domyślnie `raport.txt`). |
| `--csv <plik>` | Generuje CSV pod podaną ścieżką (domyślnie `raport.csv`) i pomija menu. |
| `--watch` | Uruchamia tryb ciągłej obserwacji logów i pomija menu. |
