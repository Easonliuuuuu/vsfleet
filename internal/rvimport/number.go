package rvimport

import (
	"regexp"
	"strconv"
	"strings"
)

// groupedNumber matches a number with comma thousands separators, the form
// GetRows returns for a cell RVTools formats as "#,##0" or "#,##0.00".
var groupedNumber = regexp.MustCompile(`^[-+]?\d{1,3}(,\d{3})+(\.\d+)?$`)

// parseNumber reads a numeric cell as GetRows formats it. It removes commas
// only when they group thousands, so "1,5" stays unparseable rather than
// turning into 15.
func parseNumber(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	if groupedNumber.MatchString(s) {
		s = strings.ReplaceAll(s, ",", "")
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}
