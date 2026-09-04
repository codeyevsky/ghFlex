// 256-colour SGR codes used across the UI.

package style

import (
	"os"
	"golang.org/x/term"
)

var On = term.IsTerminal(int(os.Stdout.Fd()))

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

func Tint(code, s string) string {
	if !On {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}