package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"

	"github.com/knqu/goexchange/internal/engine"
	"github.com/knqu/goexchange/internal/feed"
	"github.com/knqu/goexchange/internal/gateway"
	"github.com/knqu/goexchange/internal/journal"
	"github.com/knqu/goexchange/internal/marketdata"
)

func main() {
	// parse command-line options into variables (symbols, server listen address, buffer size, debug mode)

	symbolsFlag := flag.String("symbols", "ACME", "comma-separated list of symbols available for trading")
	addressFlag := flag.String("address", "localhost:8080", "HTTP listen address")
	bufSizeFlag := flag.Int("bufSize", 4096, "per-engine command buffer size")
	debugFlag := flag.Bool("debug", false, "toggle debug mode")

	flag.Parse()

	symbols := strings.Split(*symbolsFlag, ",")
	address := *addressFlag
	bufSize := *bufSizeFlag
	debug := *debugFlag

	// create /journals directory if it doesn't already exist

	if err := os.MkdirAll("journals", 0o755); err != nil {
		log.Fatalf("creating journals dir: %v", err)
	}

	// collect command history from existing journals via replay and initialize new journal writers for each symbol

	restoredCmds := make(map[string][]engine.Command)
	writers := make(map[string]engine.JournalWriter)
	var maxOrderID uint64 // track max OrderID across all journals to initialize gateway's OrderID counter

	for _, symbol := range symbols {
		path := "journals/" + symbol + ".jnl"

		if _, err := os.Stat(path); err == nil {
			if err := journal.Replay(path, func(cmd engine.Command) error {
				restoredCmds[symbol] = append(restoredCmds[symbol], cmd)
				if cmd.Type == engine.CmdSubmit {
					maxOrderID = max(uint64(cmd.Order.ID), maxOrderID)
				}
				return nil
			}); err != nil {
				log.Fatalf("%s - journal replay failed: %v", symbol, err)
			}
		}

		writer, err := journal.NewWriter(path)
		if err != nil {
			log.Fatalf("opening journal: %v", err)
		}
		writers[symbol] = writer
	}

	// initialize a new exchange (pass in previously initialized map of symbols to writers)

	ctx, cancel := context.WithCancel(context.Background())
	exchange := engine.NewExchange(symbols, bufSize, writers, cancel)

	// restore each (just-initialized) engine's book state from recorded commands

	for symbol, cmds := range restoredCmds {
		if err := exchange.Restore(symbol, cmds); err != nil {
			log.Fatalf("%s restore failed: %v", symbol, err)
		}

		depth, err := exchange.Depth(symbol, 1)
		if err != nil {
			log.Fatalf("%s depth query failed: %v", symbol, err)
		}

		bestBid, bestBidQuantity := int64(0), int64(0)
		if len(depth.Bids) > 0 {
			bestBid = depth.Bids[0].Price
			bestBidQuantity = depth.Bids[0].Quantity
		}

		bestAsk, bestAskQuantity := int64(0), int64(0)
		if len(depth.Asks) > 0 {
			bestAsk = depth.Asks[0].Price
			bestAskQuantity = depth.Asks[0].Quantity
		}

		log.Printf("%s restored (%d commands): initialized book with best bid/ask $%dx%d/$%dx%d",
			symbol, len(cmds), bestBid, bestBidQuantity, bestAsk, bestAskQuantity)
	}

	// aggregate and publish each symbol's events in a separate goroutine and log if debug mode is enabled

	hub := feed.NewHub() // all aggregators share a single hub
	var aggregators sync.WaitGroup

	for symbol, ch := range exchange.Events() {
		aggregator := marketdata.NewAggregator(hub)
		aggregators.Add(1)

		go func() {
			defer aggregators.Done()
			for batch := range ch {
				for _, event := range batch {
					aggregator.Consume(event)
					if debug {
						log.Printf("%s event: %+v", symbol, event)
					}
				}
			}
		}()
	}

	// start the exchange (spawn all engine goroutines); then, serve the HTTP gateway from a separate goroutine

	exchange.Run(ctx)

	gateway := gateway.NewGateway(exchange, maxOrderID)
	go func() {
		if err := gateway.Serve(address); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	log.Printf("gateway listening on %s", address)

	// wait for interrupt signal (ctrl+c) or kill switch (cancel() called from within a running engine)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)

	select {
	case <-sig:
		log.Printf("exchange shutting down (interrupt received)")
		cancel()
	case <-exchange.Done():
		log.Printf("exchange shutting down (kill switch; check for journal failure?)")
	}

	// gracefully shut down the exchange and close all journal writers

	<-exchange.Done() // ensure exchange is fully stopped (no-op if already closed)
	aggregators.Wait()

	for _, writer := range writers {
		writer.Close()
	}

	log.Printf("graceful shutdown complete")
}
