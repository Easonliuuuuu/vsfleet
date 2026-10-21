package health

import "strings"

// stateWordsByEnum is how a finding message names a vSphere state. The
// evidence keeps the raw value; the message is read by people, and
// "notResponding" or "guestToolsNeedUpgrade" is an API name, not a sentence.
var stateWordsByEnum = map[string]string{
	"notResponding":              "not responding",
	"guestToolsNotRunning":       "not running",
	"guestToolsExecutingScripts": "starting",
	"guestToolsNeedUpgrade":      "an upgrade is available",
	"guestToolsSupportedOld":     "an older supported version",
	"guestToolsTooOld":           "too old to be supported",
	"guestToolsBlacklisted":      "a version with known problems",
}

// stateWords is a vSphere state as message text: a known state by its words,
// any other camelCase state split into lower-case words, and an empty state
// as "in an unknown state".
func stateWords(state string) string {
	if words, ok := stateWordsByEnum[state]; ok {
		return words
	}
	if state == "" {
		return "in an unknown state"
	}
	var b strings.Builder
	for i, r := range state {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte(' ')
		}
		b.WriteString(strings.ToLower(string(r)))
	}
	return b.String()
}

// toolsNotRunningMessage says Tools are not running, and in what state when
// that adds something.
func toolsNotRunningMessage(state string) string {
	if words := stateWords(state); state != "" && words != "not running" {
		return "VMware Tools are not running (" + words + ")"
	}
	return "VMware Tools are not running"
}
