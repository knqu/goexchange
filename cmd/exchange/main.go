package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"

	"github.com/knqu/goexchange/internal/engine"
	"github.com/knqu/goexchange/internal/gateway"
	"github.com/knqu/goexchange/internal/journal"
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

	// initialize a new exchange and create a map storing each symbol's assigned journal writer

	events := make(chan []engine.Event, bufSize)
	exchange := engine.NewExchange(symbols, bufSize, events)
	writers := make(map[string]*journal.Writer)

	// create /journals directory if it doesn't already exist

	if err := os.MkdirAll("journals", 0o755); err != nil {
		log.Fatalf("creating journals dir: %v", err)
	}

	// replay each symbol's journal if it exists (restoring engine state); then, initialize a new journal writer

	for _, symbol := range symbols {
		path := "journals/" + symbol + ".jnl"

		if _, err := os.Stat(path); err == nil {
			var cmds []engine.Command

			if err := journal.Replay(path, func(cmd engine.Command) error {
				cmds = append(cmds, cmd)
				return nil
			}); err != nil {
				log.Fatalf("%s - journal replay failed: %v", symbol, err)
			}

			if err := exchange.Restore(symbol, cmds); err != nil {
				log.Fatalf("%s - engine restore failed: %v", symbol, err)
			}

			bids, asks, err := exchange.Depth(symbol, 1)
			if err != nil {
				log.Fatalf("%s - depth query failed: %v", symbol, err)
			}

			bestBid, bestBidQuantity := int64(0), int64(0)
			if len(bids) > 0 {
				bestBid = bids[0].Price
				bestBidQuantity = bids[0].Quantity
			}

			bestAsk, bestAskQuantity := int64(0), int64(0)
			if len(asks) > 0 {
				bestAsk = asks[0].Price
				bestAskQuantity = asks[0].Quantity
			}

			log.Printf("%s - restored %d commands, initialized book with best bid/ask: $%d @ %d / $%d @ %d", symbol, len(cmds), bestBid, bestBidQuantity, bestAsk, bestAskQuantity)
		}

		writer, err := journal.NewWriter(path)
		if err != nil {
			log.Fatalf("opening journal: %v", err)
		}
		writers[symbol] = writer
	}

	// drain events channel in a separate goroutine, logging each event if debug mode is enabled

	drained := make(chan struct{})
	go func() {
		for batch := range events {
			if debug {
				for _, event := range batch {
					log.Printf("event: %+v", event)
				}
			}
		}
		close(drained)
	}()

	// start the exchange (spawn all engine goroutines); then, serve the HTTP gateway in a separate goroutine

	ctx, cancel := context.WithCancel(context.Background())
	exchange.Run(ctx)

	gateway := gateway.NewGateway(exchange)
	go func() {
		if err := gateway.Serve(address); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	log.Printf("gateway listening on %s", address)

	// wait for interrupt (ctrl+c) before gracefully shutting down the exchange and closing all journal writers

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	<-sig
	log.Printf("exchange shutting down")

	cancel()
	<-exchange.Done()
	close(events)
	<-drained

	for _, writer := range writers {
		writer.Close()
	}

	log.Printf("graceful shutdown complete")
}
