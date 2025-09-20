package main

import (
	"os"

	"github.com/mrschyzo/animesucc/cli/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
