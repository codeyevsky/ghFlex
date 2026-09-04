package engine

import "github.com/codeyevsky/ghFlex/internal/style"

type ModeSpec struct {
	Kind  string // "user" or "repo"
	Past  string // followed / unfollowed / starred / unstarred
	Mark  string
	Color string // tint code for the mark
}

// Modes is every action the engine knows. Kind decides which collector runs
// and which state bucket the result lands in.
var Modes = map[string]ModeSpec{
	"follow":   {Kind: "user", Past: "followed", Mark: "[+]", Color: style.Green},
	"unfollow": {Kind: "user", Past: "unfollowed", Mark: "[-]", Color: style.Red},
	"star":     {Kind: "repo", Past: "starred", Mark: "[*]", Color: style.Yellow},
	"unstar":   {Kind: "repo", Past: "unstarred", Mark: "[x]", Color: style.Lilac},
}
