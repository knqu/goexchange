package main

import (
	"flag"
	"log"

	"github.com/knqu/goexchange/internal/engine"
	"github.com/knqu/goexchange/internal/journal"
)

func main() {
	path := flag.String("journal", "exchange.jnl", "path to journal file to inspect")
	depth := flag.Int("depth", 5, "number of price levels to display per side")

	flag.Parse()

	var cmds []engine.Command

	if err := journal.Replay(*path, func(cmd engine.Command) error {
		cmds = append(cmds, cmd)
		return nil
	}); err != nil {
		log.Fatalf("replay failed: %v", err)
	}

	eng := engine.NewEngine("", 0, nil) // symbol doesn't mattter and buf/events are unused (engine never runs)
	eng.Restore(cmds)

	bids, asks := eng.Depth(*depth)
	log.Printf("journal %s contains %d commands", *path, len(cmds))
	log.Printf("bids (top %d): %+v", *depth, bids)
	log.Printf("asks (top %d): %+v", *depth, asks)
}
