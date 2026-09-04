// Features' string map

package engine

import "github.com/codeyevsky/ghFlex/internal/style"

type ModeSpec struct {
	Kind  string 
	Past  string 
	Mark  string
	Color string 
}

var Modes = map[string]ModeSpec{
	"follow":   {Kind: "user", Past: "followed", Mark: "[+]", Color: style.Green},
	"unfollow": {Kind: "user", Past: "unfollowed", Mark: "[-]", Color: style.Red},
	"star":     {Kind: "repo", Past: "starred", Mark: "[*]", Color: style.Yellow},
	"unstar":   {Kind: "repo", Past: "unstarred", Mark: "[x]", Color: style.Lilac},
}