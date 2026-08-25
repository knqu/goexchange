package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/knqu/goexchange/internal/engine"
	"github.com/knqu/goexchange/internal/journal"
)

const (
	symbol      = "ACME"
	journalPath = "exchange.jnl"
	bufSize     = 4096
)

func main() {
	events := make(chan []engine.Event, bufSize)
	exchange := engine.NewExchange([]string{symbol}, bufSize, events)

	if _, err := os.Stat(journalPath); err == nil {
		log.Printf("restoring journal found at %s", journalPath)

		var cmds []engine.Command

		if err := journal.Replay(journalPath, func(cmd engine.Command) error {
			cmds = append(cmds, cmd)
			return nil
		}); err != nil {
			log.Fatalf("journal replay failed: %v", err)
		}

		if err := exchange.Restore(symbol, cmds); err != nil {
			log.Fatalf("engine restore failed: %v", err)
		}

		bids, asks, err := exchange.Depth(symbol, 5)
		if err != nil {
			log.Fatalf("depth query failed: %v", err)
		}

		log.Printf("restored %d commands; initialized book with resting bids: %+v, asks: %+v", len(cmds), bids, asks)
	}

	writer, err := journal.NewWriter(journalPath)
	if err != nil {
		log.Fatalf("opening journal: %v", err)
	}
	defer writer.Close()

	drained := make(chan struct{})
	go func() {
		for batch := range events {
			for _, event := range batch {
				log.Printf("event: %+v", event)
			}
		}
		close(drained)
	}()

	ctx, cancel := context.WithCancel(context.Background())
	exchange.Run(ctx)

	for _, cmd := range generateOrders() {
		if err := writer.Append(cmd); err != nil {
			log.Fatalf("journal append: %v", err)
		}
		if err = exchange.Dispatch(symbol, cmd); err != nil {
			log.Fatalf("dispatch: %v", err)
		}
	}

	cancel()
	<-exchange.Done()
	close(events)
	<-drained

	log.Printf("exchange shut down gracefully")
}

func generateOrders() []engine.Command {
	base := engine.OrderID(time.Now().UnixNano())
	return []engine.Command{
		{Type: engine.CmdSubmit, Order: engine.Order{
			ID: base, AgentID: 1,
			Side: engine.Buy, Type: engine.Limit, TIF: engine.Day,
			Price: 10000, Quantity: 10,
		}},
		{Type: engine.CmdSubmit, Order: engine.Order{
			ID: base + 1, AgentID: 2,
			Side: engine.Sell, Type: engine.Limit, TIF: engine.Day,
			Price: 9990, Quantity: 5,
		}},
	}
}
