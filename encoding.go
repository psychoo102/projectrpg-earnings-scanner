package main

import (
	"bytes"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// cp1250Table mapuje bajty 0x80-0xFF kodowania Windows-1250 (popularne
// "ANSI" na polskiej wersji Windows) na Unicode. Bajty 0x00-0x7F pokrywają
// się z ASCII, więc nie są tu wymienione.
//
// Źródło: oficjalna tabela Microsoft/Unicode Consortium
// https://www.unicode.org/Public/MAPPINGS/VENDORS/MICSFT/WINDOWS/CP1250.TXT
//
// 0xFFFD (znak zastępczy) oznacza pozycję nieprzypisaną w CP1250.
var cp1250Table = [128]rune{
	0x20AC, 0xFFFD, 0x201A, 0xFFFD, 0x201E, 0x2026, 0x2020, 0x2021, // 0x80-0x87
	0xFFFD, 0x2030, 0x0160, 0x2039, 0x015A, 0x0164, 0x017D, 0x0179, // 0x88-0x8F
	0xFFFD, 0x2018, 0x2019, 0x201C, 0x201D, 0x2022, 0x2013, 0x2014, // 0x90-0x97
	0xFFFD, 0x2122, 0x0161, 0x203A, 0x015B, 0x0165, 0x017E, 0x017A, // 0x98-0x9F
	0x00A0, 0x02C7, 0x02D8, 0x0141, 0x00A4, 0x0104, 0x00A6, 0x00A7, // 0xA0-0xA7
	0x00A8, 0x00A9, 0x015E, 0x00AB, 0x00AC, 0x00AD, 0x00AE, 0x017B, // 0xA8-0xAF
	0x00B0, 0x00B1, 0x02DB, 0x0142, 0x00B4, 0x00B5, 0x00B6, 0x00B7, // 0xB0-0xB7
	0x00B8, 0x0105, 0x015F, 0x00BB, 0x013D, 0x02DD, 0x013E, 0x017C, // 0xB8-0xBF
	0x0154, 0x00C1, 0x00C2, 0x0102, 0x00C4, 0x0139, 0x0106, 0x00C7, // 0xC0-0xC7
	0x010C, 0x00C9, 0x0118, 0x00CB, 0x011A, 0x00CD, 0x00CE, 0x010E, // 0xC8-0xCF
	0x0110, 0x0143, 0x0147, 0x00D3, 0x00D4, 0x0150, 0x00D6, 0x00D7, // 0xD0-0xD7
	0x0158, 0x016E, 0x00DA, 0x0170, 0x00DC, 0x00DD, 0x0162, 0x00DF, // 0xD8-0xDF
	0x0155, 0x00E1, 0x00E2, 0x0103, 0x00E4, 0x013A, 0x0107, 0x00E7, // 0xE0-0xE7
	0x010D, 0x00E9, 0x0119, 0x00EB, 0x011B, 0x00ED, 0x00EE, 0x010F, // 0xE8-0xEF
	0x0111, 0x0144, 0x0148, 0x00F3, 0x00F4, 0x0151, 0x00F6, 0x00F7, // 0xF0-0xF7
	0x0159, 0x016F, 0x00FA, 0x0171, 0x00FC, 0x00FD, 0x0163, 0x02D9, // 0xF8-0xFF
}

// decodeCP1250 dekoduje bajty w kodowaniu Windows-1250 do stringa UTF-8.
func decodeCP1250(data []byte) string {
	var b strings.Builder
	b.Grow(len(data))
	for _, c := range data {
		if c < 0x80 {
			b.WriteByte(c)
		} else {
			b.WriteRune(cp1250Table[c-0x80])
		}
	}
	return b.String()
}

// decodeFileBytes wykrywa kodowanie na poziomie CAŁEGO pliku, ale TYLKO
// pod kątem UTF-16 (rozpoznawalne po BOM na początku pliku — to kodowanie
// jest zawsze jednolite dla całego pliku). W pozostałych przypadkach zwraca
// surowe bajty bez zmian.
//
// Świadomie NIE rozstrzygamy tu UTF-8 vs Windows-1250 dla całego pliku —
// w praktyce jeden console.log potrafi mieszać kodowania: komunikaty
// systemowe bywają w Windows-1250, a czat wpisany przez graczy bywa
// zapisany w UTF-8 (zależnie od systemowych ustawień regionalnych
// każdego gracza). Próba zgadnięcia jednego kodowania dla całego pliku
// prowadzi do "krzaczków" na liniach, które akurat miały inne kodowanie
// niż większość pliku. Zamiast tego decyzję UTF-8/CP1250 podejmujemy
// OSOBNO DLA KAŻDEJ LINII w decodeLine — Windows-1250 jest kodowaniem
// jednobajtowym, więc jest to bezpieczne bez znajomości kontekstu.
func decodeFileBytes(raw []byte) []byte {
	switch {
	case len(raw) >= 2 && raw[0] == 0xFF && raw[1] == 0xFE:
		return []byte(utf16ToString(raw[2:], false))
	case len(raw) >= 2 && raw[0] == 0xFE && raw[1] == 0xFF:
		return []byte(utf16ToString(raw[2:], true))
	}
	return bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})
}

// decodeLine dekoduje pojedynczą linię (surowe bajty) do UTF-8. Jeśli linia
// jest już poprawnym UTF-8 — zwraca ją bez zmian. W przeciwnym razie zakłada
// Windows-1250. Działanie linia-po-linii (zamiast całego pliku naraz)
// poprawnie obsługuje pliki logów o mieszanym kodowaniu.
func decodeLine(line []byte) string {
	if utf8.Valid(line) {
		return string(line)
	}
	return decodeCP1250(line)
}

func utf16ToString(b []byte, bigEndian bool) string {
	if len(b)%2 != 0 {
		b = b[:len(b)-1]
	}
	u16 := make([]uint16, len(b)/2)
	for i := range u16 {
		if bigEndian {
			u16[i] = uint16(b[2*i])<<8 | uint16(b[2*i+1])
		} else {
			u16[i] = uint16(b[2*i]) | uint16(b[2*i+1])<<8
		}
	}
	return string(utf16.Decode(u16))
}
