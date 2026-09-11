package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/knqu/goexchange/internal/agents"
	"github.com/knqu/goexchange/internal/agents/strategies"
	"github.com/knqu/goexchange/internal/engine"
	"github.com/knqu/goexchange/internal/execution"
	"github.com/knqu/goexchange/internal/feed"
	"github.com/knqu/goexchange/internal/gateway"
	"github.com/knqu/goexchange/internal/journal"
)

func main() {
	// parse command-line options into variables

	symbolsFlag := flag.String("symbols", "ACME", "comma-separated list of symbols available for trading")
	agentIDsFlag := flag.String("agentIDs", "1", "comma-separated list of agent IDs")
	gatewayAddressFlag := flag.String("gatewayAddress", "localhost:8080", "gateway listen address (HTTP)")
	feedAddressFlag := flag.String("feedAddress", "ws://localhost:8080/ws", "market data feed address (WebSocket)")
	exchangeBufFlag := flag.Int("exchangeBuf", 4096, "per-engine commands/events channel buffer size")
	fillsBufFlag := flag.Int("fillsBuf", 1024, "per-agent fills channel buffer size")
	feedBufFlag := flag.Int("feedBuf", 256, "per-subscriber market data messages channel buffer size")
	startingCashFlag := flag.Int64("startingCash", 1_000_000, "starting cash given to each agent")
	fastTickFlag := flag.Duration("fastTick", 200*time.Millisecond, "agent fast (trading) loop interval")
	slowTickFlag := flag.Duration("slowTick", 10*time.Second, "agent slow (thinking) loop interval")

	flag.Parse()

	symbols := strings.Split(*symbolsFlag, ",")

	unconverted := strings.Split(*agentIDsFlag, ",")
	agentIDs := make([]engine.AgentID, len(unconverted))
	for i, str := range unconverted {
		converted, err := strconv.Atoi(str)
		if err != nil {
			log.Fatalf("converting agent id to int: %v", err)
		}
		agentIDs[i] = engine.AgentID(converted)
	}

	gatewayAddress := *gatewayAddressFlag
	feedAddress := *feedAddressFlag

	exchangeBuf := *exchangeBufFlag
	fillsBuf := *fillsBufFlag
	feedBuf := *feedBufFlag

	startingCash := *startingCashFlag
	fastTick := *fastTickFlag
	slowTick := *slowTickFlag

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
	exchange := engine.NewExchange(symbols, exchangeBuf, writers, cancel)

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

	// run per-symbol aggregators for market data broadcasts and global distributor for maker/taker fill notifications

	aggregators := make(map[string]*feed.Aggregator)
	distributor := execution.NewDistributor()

	var fanoutGroup sync.WaitGroup
	var aggregatorGroup sync.WaitGroup

	for symbol, ch := range exchange.Events() {
		// create a new aggregator for each symbol
		aggregator := feed.NewAggregator()
		aggregators[symbol] = aggregator
		aggregatorGroup.Add(1)

		// fan-out events: send into aggregator's events channel and notify distributor if event was a trade
		aggregations := make(chan engine.Event, exchangeBuf)
		fanoutGroup.Add(1)
		go func() {
			defer fanoutGroup.Done()
			defer close(aggregations)
			for batch := range ch {
				for _, event := range batch {
					aggregations <- event
					if event.Type == engine.EventTraded {
						distributor.GenerateFill(symbol, event)
					}
				}
			}
		}()

		// run aggregator in its own goroutine (event consumption loop is blocking)
		go func() {
			defer aggregatorGroup.Done()
			aggregator.Run(aggregations)
		}()
	}

	// start the exchange (spawn all engine goroutines); then, serve the HTTP gateway from a separate goroutine

	exchange.Run(ctx)

	gw := gateway.NewGateway(exchange, aggregators, feedBuf, maxOrderID)
	go func() {
		if err := gw.Serve(gatewayAddress); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	log.Printf("gateway listening on %s", gatewayAddress)

	// create per-symbol feed clients (shared across agents to reduce load on broadcast hub; safe due to locks)

	feeds := make(map[string]*feed.Client, len(symbols))
	for _, symbol := range symbols {
		client := feed.NewClient(feedAddress+"?symbol="+symbol, feedBuf)
		go client.Run(ctx)
		feeds[symbol] = client
	}

	// initialize agents one-by-one, creating their strategies and registering them with the distributor before running

	var agentGroup sync.WaitGroup

	for _, agentID := range agentIDs {
		defaultPolicy := agents.Policy{Participation: true, Bias: 0, RiskAppetite: 0.5, Aggression: 0.25}

		agentStrategies := make(map[string]agents.Strategy, len(symbols))
		for _, symbol := range symbols {
			agentStrategies[symbol] = strategies.NewNoise(0.5, 10, 10000, 20, uint64(agentID))
		}

		fills := distributor.Register(agentID, fillsBuf)

		agent := agents.NewAgent(
			agentID, symbols, startingCash, agents.Stub{}, defaultPolicy,
			agentStrategies, feeds, fills, agents.NewGatewayClient("http://"+gatewayAddress, agentID),
		)

		agentGroup.Add(1)
		go func() {
			defer agentGroup.Done()
			agent.Run(ctx, fastTick, slowTick)
			// fastTick defaults to 5 times a second; slowTick defaults to once every 10 seconds
		}()
	}

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

	// gracefully shut down exchange, close distributor (and all fills channels), and close all journal writers

	agentGroup.Wait()      // wait for agents to finish submitting to exchange (not strictly necessary)
	<-exchange.Done()      // wait for engines to drain buffered commands and exchange to close events channels
	fanoutGroup.Wait()     // wait for fan-out to drain and process buffered events, sending them to an aggregator
	aggregatorGroup.Wait() // wait for aggregators to drain their aggregations channel and broadcast to subscribers

	distributor.Close() // closes all fills channels; fan-outs must stop calling distributor.GenerateFill()

	for _, writer := range writers {
		writer.Close()
	}

	log.Printf("graceful shutdown complete")
}
