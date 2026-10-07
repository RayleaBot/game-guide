package main

import (
	"context"
	"fmt"
	"os"

	"github.com/RayleaBot/game-guide/internal/plugin"
)

func main() {
	if err := plugin.Run(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
