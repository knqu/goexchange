import { useEffect, useRef, useState } from 'react';
import { Circle, Pause, Play } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { AgentDetails, AgentsTable } from '@/components/agents-panel';
import { MarketSummary } from '@/components/market-summary';
import { OrderBook } from '@/components/order-book';
import { PriceChart } from '@/components/price-chart';
import { TradeTape } from '@/components/trade-tape';
import { useHaltControl, type PendingRequest } from '@/hooks/use-halt-control';
import { useMarket } from '@/hooks/use-market';
import { useObservation } from '@/hooks/use-observation';
import type { MarketView } from '@/lib/market';

const pollStaleAfter = 5000; // ms without a successful poll before agent data is considered stale

export default function App() {
    const exchange = useObservation();
    const now = useNow();

    const [requestedSymbol, setRequestedSymbol] = useState(new URLSearchParams(location.search).get('symbol') ?? '');
    const symbol = exchange.symbols.find((candidate) => candidate === requestedSymbol) ?? exchange.symbols[0] ?? '';

    const market = useMarket(symbol, exchange.session);
    const live = market.status === 'live';
    const halted = live ? market.halted : null;
    const haltControl = useHaltControl(symbol, exchange.session, halted, now);

    const stale = !!exchange.error || !exchange.received || Date.now() - exchange.received > pollStaleAfter;

    const agents = exchange.agents
        .filter((agent) => agent.observation)
        .sort((a, b) => (BigInt(a.observation!.id) < BigInt(b.observation!.id) ? -1 : 1));

    const [selected, setSelected] = useState<string | null>(null);
    const selectedButton = useRef<HTMLButtonElement | null>(null);
    const selectedAgent = agents.find((agent) => agent.observation!.id === selected);

    useEffect(() => {
        setSelected(null);
    }, [symbol, exchange.session]);

    const selectSymbol = (next: string) => {
        setRequestedSymbol(next);

        const url = new URL(location.href);
        url.searchParams.set('symbol', next);
        history.replaceState(null, '', url);
    };

    const toggleAgent = (id: string, button: HTMLButtonElement) => {
        selectedButton.current = button;
        setSelected(selected === id ? null : id);
    };

    const closeAgent = () => {
        setSelected(null);
        selectedButton.current?.focus();
    };

    return (
        <main className="exchange">
            <header className="app-header">
                <div className="wordmark">GoExchange</div>
                <SymbolPicker symbols={exchange.symbols} selected={symbol} onSelect={selectSymbol} />
                <div className="header-actions">
                    <FeedStatus market={market} />
                    <HaltButton
                        symbol={symbol}
                        halted={halted}
                        pending={haltControl.pending}
                        onToggle={haltControl.toggle}
                    />
                </div>
            </header>

            {halted && (
                <div className="notice halt-notice" role="status">
                    {symbol} is halted. New orders are rejected, but cancellations remain available.
                </div>
            )}
            {haltControl.error && (
                <div className="notice" role="alert">
                    {haltControl.error}
                </div>
            )}
            {exchange.error && (
                <div className="notice" role="status">
                    Exchange unavailable. Agent observations may be stale. Reconnecting automatically.
                </div>
            )}
            {!live && symbol && (
                <div className="notice" role="status">
                    {market.reason}. Waiting for a synchronized book.
                </div>
            )}

            <div className="workspace">
                <MarketSummary observed={market.observed} now={now} />

                <div className="market-grid">
                    {/* keyed so the chart starts fresh for each symbol and exchange run */}
                    <PriceChart key={`${exchange.session}/${symbol}`} points={market.points} />
                    <OrderBook bids={market.bids} asks={market.asks} live={live} />
                </div>

                <div className="bottom-grid">
                    <AgentsTable
                        agents={agents}
                        symbol={symbol}
                        selected={selected}
                        onSelect={toggleAgent}
                        stale={stale}
                        now={now}
                    />
                    <TradeTape trades={market.trades} now={now} />
                    {selectedAgent && (
                        <div id="agent-expanded" className="expanded-row">
                            <AgentDetails agent={selectedAgent} symbol={symbol} close={closeAgent} />
                        </div>
                    )}
                </div>
            </div>
        </main>
    );
}

// --- header ---

function SymbolPicker({
    symbols,
    selected,
    onSelect,
}: {
    symbols: string[];
    selected: string;
    onSelect: (symbol: string) => void;
}) {
    return (
        <div className="symbol-control" role="group" aria-label="Market symbol">
            {symbols.length === 0 && <span className="symbol-placeholder">Connecting...</span>}
            {symbols.map((symbol) => (
                <button
                    key={symbol}
                    type="button"
                    className="symbol-option"
                    aria-pressed={symbol === selected}
                    onClick={() => onSelect(symbol)}
                >
                    {symbol}
                </button>
            ))}
        </div>
    );
}

function FeedStatus({ market }: { market: MarketView }) {
    const live = market.status === 'live';

    let label = 'Resynchronizing';
    if (live) {
        label = 'Book synchronized';
    } else if (market.status === 'connecting') {
        label = 'Connecting';
    }

    return (
        <div
            className={`feed-status ${live ? '' : 'warning-text'}`}
            title={`Book sequence ${market.seq}; ${market.gaps} gaps this connection session`}
        >
            <Circle size={7} fill={live ? 'currentColor' : 'none'} />
            {label}
        </div>
    );
}

function HaltButton({
    symbol,
    halted,
    pending,
    onToggle,
}: {
    symbol: string;
    halted: boolean | null;
    pending: PendingRequest | null;
    onToggle: () => Promise<void>;
}) {
    let label = `${halted ? 'Resume' : 'Halt'} ${symbol}`;
    if (pending) {
        label = `${pending.halted ? 'Halt' : 'Resume'} requested...`;
    }

    return (
        <Button variant="outline" disabled={halted === null || !!pending} onClick={() => void onToggle()}>
            {halted ? <Play /> : <Pause />}
            {label}
        </Button>
    );
}

// --- helpers ---

// useNow returns the current time, updated every second.
function useNow(): number {
    const [now, setNow] = useState(Date.now());

    useEffect(() => {
        const timer = setInterval(() => setNow(Date.now()), 1000);
        return () => clearInterval(timer);
    }, []);

    return now;
}
