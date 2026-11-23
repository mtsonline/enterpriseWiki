// Package csvsafe keeps values in CSV downloads from being taken as
// formulas by spreadsheet programs (CSV injection, CWE-1236): a value that
// starts with = + - @, a tab or a carriage return gets a leading apostrophe.
package csvsafe

import "strings"

// Cell returns s safe for a CSV cell.
func Cell(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

// Row applies Cell to every value of the row.
func Row(row []string) []string {
	out := make([]string, len(row))
	for i, v := range row {
		out[i] = Cell(v)
	}
	return out
}
