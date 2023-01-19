package assets

import (
	"os"

	"src.goblgobl.com/utils"
	"src.goblgobl.com/utils/log"
)

var (
	Upstreams map[string]*Upstream
)

func Run() {
	Upstreams = make(map[string]*Upstream, len(Config.Upstreams))
	for name, config := range Config.Upstreams {
		upstream, err := NewUpstream(name, config)
		if err != nil {
			log.Fatal("new_upstream").Err(err).String("up", name).Log()
			os.Exit(1)
		}
		Upstreams[name] = upstream
	}
	Listen()
}

// https://www.openmymind.net/ASCII_String_To_Lowercase_in_Go/
func lowercase(input string) string {
	for i := 0; i < len(input); i++ {
		c := input[i]
		if 'A' <= c && c <= 'Z' {
			// We've found an uppercase character, we'll need to convert this string
			lower := make([]byte, len(input))

			// copy everything we've skipped over up to this point
			copy(lower, input[:i])

			// our current character needs to be uppercase (it's the reason we're
			// in this branch)
			lower[i] = c + 32

			// now iterate over the rest of the input, from where we are, knowing that
			// we need to copy/lower case into our lowercase strinr
			for j := i + 1; j < len(input); j++ {
				c := input[j]
				if 'A' <= c && c <= 'Z' {
					c += 32
				}
				lower[j] = c
			}
			return utils.B2S(lower)
		}
	}

	// input was already lowercase, return it as-is
	return input
}
