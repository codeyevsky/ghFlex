package main

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/codeyevsky/ghFlex/internal/style"
	"golang.org/x/term"
)

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z~]|\x1b.`)

// sanitizeInput strips ANSI escape sequences and control characters, so
// pressing an arrow key while typing can't smuggle garbage into a value.
func sanitizeInput(s string) string {
	s = ansiRe.ReplaceAllString(s, "")
	var b strings.Builder
	for _, r := range s {
		if r >= 32 && r != 127 {
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

var banner = []string{
	"░█▀▀░▀█▀░▀█▀░█░█░█░█░█▀▄░█▀▀░█░░░█▀▀░█░█",
	"░█░█░░█░░░█░░█▀█░█░█░█▀▄░█▀▀░█░░░█▀▀░▄▀▄",
	"░▀▀▀░▀▀▀░░▀░░▀░▀░▀▀▀░▀▀░░▀░░░▀▀▀░▀▀▀░▀░▀",
}

var bannerColors = []int{177, 135, 93}
var bannerShown bool

// animateBanner reveals the wordmark left to right in a quick sweep.
func animateBanner() {
	rows := make([][]rune, len(banner))
	width := 0
	for i, l := range banner {
		rows[i] = []rune(l)
		if len(rows[i]) > width {
			width = len(rows[i])
		}
	}
	fmt.Print("\x1b[?25l")
	defer fmt.Print("\x1b[?25h")
	for range banner {
		fmt.Println()
	}
	for k := 3; ; k += 3 {
		if k > width {
			k = width
		}
		fmt.Printf("\x1b[%dA", len(banner))
		for i, r := range rows {
			end := k
			if end > len(r) {
				end = len(r)
			}
			fmt.Printf("\r\x1b[2K  \x1b[38;5;%dm%s\x1b[0m\n", bannerColors[i%len(bannerColors)], string(r[:end]))
		}
		if k == width {
			return
		}
		time.Sleep(18 * time.Millisecond)
	}
}

// flushLine reveals one line left to right with the same sweep the banner
// uses; used for section headers so opening a section animates too.
func flushLine(indent, text, code string) {
	if !style.On {
		fmt.Println(indent + text)
		return
	}
	r := []rune(text)
	fmt.Print("\x1b[?25l")
	for k := 4; ; k += 4 {
		if k > len(r) {
			k = len(r)
		}
		fmt.Printf("\r\x1b[2K%s\x1b[%sm%s\x1b[0m", indent, code, string(r[:k]))
		if k == len(r) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	fmt.Print("\x1b[?25h\n")
}

// revealPause is the per-line delay used when the panel first appears, so the
// section below the banner drops in line by line.
const revealPause = 55 * time.Millisecond

// revealLn prints a line and, when reveal is set, waits a beat so the panel
// unrolls top to bottom.
func revealLn(s string, reveal bool) {
	fmt.Println(s)
	if reveal {
		time.Sleep(revealPause)
	}
}

func printBanner(reveal bool) {
	fmt.Println()
	if style.On && !bannerShown {
		animateBanner()
		bannerShown = true
	} else {
		for i, l := range banner {
			if style.On {
				fmt.Printf("  \x1b[38;5;%dm%s\x1b[0m\n", bannerColors[i%len(bannerColors)], l)
			} else {
				fmt.Println("  " + l)
			}
		}
	}
	fmt.Println()
	revealLn("  "+style.Tint(style.Deep, strings.Repeat("=", 60)), reveal)
}

func clearScreen(interactive bool) {
	if interactive {
		fmt.Print("\x1b[2J\x1b[H")
	}
}

type menuItem struct {
	cmd  string
	desc string
}

func menuLine(it menuItem) string {
	return fmt.Sprintf("%-9s %s", it.cmd, it.desc)
}

func renderMenu(items []menuItem, sel int, redraw bool) {
	if redraw {
		fmt.Printf("\x1b[%dA", len(items))
	}
	for i, it := range items {
		if i == sel {
			if style.On {
				fmt.Printf("\x1b[2K   \x1b[38;5;177m->\x1b[0m \x1b[48;5;93;38;5;231;1m %s \x1b[0m\r\n", menuLine(it))
			} else {
				fmt.Printf("\x1b[2K   -> \x1b[7m %s \x1b[0m\r\n", menuLine(it))
			}
		} else {
			fmt.Printf("\x1b[2K      %s\r\n", menuLine(it))
		}
	}
}

// pick shows a horizontal arrow-key chooser for one question and returns the
// selected option; defIdx is the option highlighted first. Only arrows (or
// h/l, j/k) move and Enter confirms, so no stray keystroke can pick a bad
// value.
func pick(label string, options []string, defIdx int, in *bufio.Reader, interactive bool) string {
	return pickHint(label, options, nil, defIdx, in, interactive)
}

// pickHint is pick with an optional per-option note shown live next to the
// row, so a warning (e.g. for "fast") appears while the cursor is on that
// option, before the user commits.
func pickHint(label string, options, hints []string, defIdx int, in *bufio.Reader, interactive bool) string {
	if defIdx < 0 || defIdx >= len(options) {
		defIdx = 0
	}
	if !interactive {
		fmt.Printf("  %s [%s]: ", label, options[defIdx])
		line, err := in.ReadString('\n')
		if err != nil {
			return options[defIdx]
		}
		line = sanitizeInput(line)
		for _, o := range options {
			if line == o {
				return o
			}
		}
		return options[defIdx]
	}

	fd := int(os.Stdin.Fd())
	old, err := term.MakeRaw(fd)
	if err != nil {
		return pickHint(label, options, hints, defIdx, in, false)
	}
	defer func() { _ = term.Restore(fd, old) }()
	fmt.Print("\x1b[?25l")
	defer fmt.Print("\x1b[?25h")

	sel := defIdx
	render := func() {
		fmt.Printf("\r\x1b[2K  %s:", label)
		for i, o := range options {
			if i == sel {
				if style.On {
					fmt.Printf(" \x1b[48;5;93;38;5;231;1m %s \x1b[0m", o)
				} else {
					fmt.Printf(" \x1b[7m %s \x1b[0m", o)
				}
			} else {
				fmt.Printf("  %s ", o)
			}
		}
		if sel < len(hints) && hints[sel] != "" {
			fmt.Print("   " + style.Tint(style.Yellow, "! "+hints[sel]))
		}
	}
	render()

	buf := make([]byte, 8)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil || n == 0 {
			fmt.Print("\r\n")
			return options[sel]
		}
		b := buf[:n]
		if b[0] == 0x1b && n == 1 {
			m, err := os.Stdin.Read(buf[1:3])
			if err != nil {
				fmt.Print("\r\n")
				return options[sel]
			}
			b = buf[:1+m]
		}
		switch {
		case b[0] == '\r' || b[0] == '\n' || b[0] == 3:
			// Leave a clean, permanent line so the next output can't collide
			// with a half-erased prompt.
			if style.On {
				fmt.Printf("\r\x1b[2K  %s: \x1b[%sm%s\x1b[0m\r\n", label, style.Purple, options[sel])
			} else {
				fmt.Printf("\r  %s: %s\r\n", label, options[sel])
			}
			return options[sel]
		case b[0] == 'h' || b[0] == 'k' || (len(b) >= 3 && b[0] == 0x1b && b[1] == '[' && (b[2] == 'D' || b[2] == 'A')):
			if sel > 0 {
				sel--
			}
		case b[0] == 'l' || b[0] == 'j' || (len(b) >= 3 && b[0] == 0x1b && b[1] == '[' && (b[2] == 'C' || b[2] == 'B')):
			if sel < len(options)-1 {
				sel++
			}
		}
		render()
	}
}

// waitEnter waits for the Enter key in raw mode without echoing, so pressing
// arrows or any other key can't spill escape codes onto the screen.
func waitEnter(in *bufio.Reader) {
	fmt.Print("\n  " + style.Tint(style.Dim, "[Enter] back to menu") + " ")
	fd := int(os.Stdin.Fd())
	old, err := term.MakeRaw(fd)
	if err != nil {
		_, _ = in.ReadString('\n')
		return
	}
	defer func() { _ = term.Restore(fd, old) }()
	buf := make([]byte, 8)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil {
			return
		}
		for i := 0; i < n; i++ {
			if buf[i] == '\r' || buf[i] == '\n' || buf[i] == 3 {
				fmt.Print("\r\n")
				return
			}
		}
	}
}

// selectMenu shows the menu and returns the chosen index. Only the arrow keys
// (or j/k) move the cursor and only Enter confirms; nothing happens on any
// other key, so an accidental keystroke can't trigger an action. When stdin
// is not a terminal it falls back to reading the entry name as a plain line,
// so piping input for scripts and tests keeps working.
func selectMenu(items []menuItem, in *bufio.Reader, interactive, reveal bool) (int, bool) {
	if !interactive {
		for _, it := range items {
			fmt.Println("      " + menuLine(it))
		}
		fmt.Print("  choice: ")
		line, err := in.ReadString('\n')
		if err != nil {
			return 0, false
		}
		line = strings.TrimSpace(line)
		for i, it := range items {
			if line == it.cmd {
				return i, true
			}
		}
		return 0, false
	}

	fd := int(os.Stdin.Fd())
	old, err := term.MakeRaw(fd)
	if err != nil {
		// terminal refused raw mode; behave like the piped fallback
		return selectMenu(items, in, false, false)
	}
	defer func() { _ = term.Restore(fd, old) }()
	fmt.Print("\x1b[?25l")
	defer fmt.Print("\x1b[?25h")

	sel := 0
	fmt.Print("\r\n")
	hint := "   " + style.Tint(style.Dim, "up/down (or j/k) to move, Enter to select") + "\r\n\r\n"
	if reveal {
		time.Sleep(revealPause)
		fmt.Print(hint)
		time.Sleep(revealPause)
		// Drop each entry in one at a time, then highlight the first.
		for _, it := range items {
			fmt.Printf("\x1b[2K      %s\r\n", menuLine(it))
			time.Sleep(revealPause)
		}
		renderMenu(items, sel, true)
	} else {
		fmt.Print(hint)
		renderMenu(items, sel, false)
	}

	buf := make([]byte, 8)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil || n == 0 {
			return 0, false
		}
		b := buf[:n]

		// A bare ESC may arrive before the rest of an arrow sequence.
		if b[0] == 0x1b && n == 1 {
			m, err := os.Stdin.Read(buf[1:3])
			if err != nil {
				return 0, false
			}
			b = buf[:1+m]
		}

		switch {
		case b[0] == 3: // Ctrl-C
			return 0, false
		case b[0] == '\r' || b[0] == '\n':
			return sel, true
		case b[0] == 'k' || (len(b) >= 3 && b[0] == 0x1b && b[1] == '[' && b[2] == 'A'):
			if sel > 0 {
				sel--
			}
		case b[0] == 'j' || (len(b) >= 3 && b[0] == 0x1b && b[1] == '[' && b[2] == 'B'):
			if sel < len(items)-1 {
				sel++
			}
		}
		renderMenu(items, sel, true)
	}
}
