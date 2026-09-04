// Package style holds the terminal colour helpers shared by the panel and the
// engine, so both tint their output the same way and neither imports the other.
package style

import (
	"os"

	"golang.org/x/term"
)

// On gates every escape sequence that is purely cosmetic: piped output stays
// plain so scripts and logs see clean text.
var On = term.IsTerminal(int(os.Stdout.Fd()))

// 256-colour SGR codes used across the UI.
const (
	Lilac  = "38;5;177"
	Purple = "38;5;135"
	Deep   = "38;5;93"
	Green  = "38;5;114"
	Red    = "38;5;203"
	Yellow = "38;5;221"
	Cyan   = "38;5;117"
	Dim    = "2"
)

// Tint wraps s in the given SGR code when colour is on, otherwise returns s.
func Tint(code, s string) string {
	if !On {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}
