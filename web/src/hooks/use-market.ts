import { useEffect, useState } from 'react';
import { emptyMarket, MarketState, type MarketView } from '@/lib/market';

const publishInterval = 100; // ms between react updates (batches bursts of feed messages into one render)
const snapshotTimeout = 10000; // ms to wait for first snapshot before reconnecting
const idleTimeout = 35000; // ms without any message before assuming dead connection (server heartbeats every 10s)

// useMarket maintains a live view of one symbol's market data feed (restarting when the symbol or session changes).
// Reconnects with backoff whenever the connection drops, goes idle, or reveals a sequence gap.
export function useMarket(symbol: string, session: string): MarketView {
    const key = `${session}/${symbol}`;
    const [result, setResult] = useState<{ key: string; view: MarketView }>({ key: '', view: emptyMarket() });

    useEffect(() => {
        if (!symbol || !session) {
            return;
        }

        const book = new MarketState();

        let socket: WebSocket | undefined;
        let retry: ReturnType<typeof setTimeout> | undefined;
        let stopped = false;
        let attempt = 0;
        let received = Date.now();
        let dirty = true; // whether the view has changed since it was last published

        let status: MarketView['status'] = 'connecting';
        let reason = 'Waiting for a snapshot';

        // publish hands react a fresh view, but only if something changed since the last publish
        const publish = () => {
            if (dirty && !stopped) {
                setResult({ key, view: book.view(status, reason) });
                dirty = false;
            }
        };

        const connect = () => {
            const url = new URL('/ws', location.href);
            url.protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
            url.searchParams.set('symbol', symbol);

            const current = new WebSocket(url);
            socket = current;
            received = Date.now();

            // fail tears down this connection, invalidates the book, and schedules a reconnect
            const fail = (message: string) => {
                if (stopped || socket !== current) {
                    return; // already replaced by a newer connection
                }

                socket = undefined;
                current.close();
                book.invalidate();

                status = 'reconnecting';
                reason = message;
                dirty = true;
                publish();

                // exponential backoff (500ms doubling to a 10s cap)
                const backoff = Math.min(10000, 500 * 2 ** Math.min(attempt, 5));
                const jitter = 0.8 + Math.random() * 0.4; // so clients don't retry in lockstep
                attempt++;
                retry = setTimeout(connect, backoff * jitter);
            };

            current.onmessage = (event) => {
                if (stopped || socket !== current) {
                    return;
                }

                received = Date.now();
                try {
                    book.apply(event.data);
                    if (book.seq !== null) {
                        status = 'live';
                        reason = 'Book synchronized';
                        attempt = 0;
                    }
                    dirty = true;
                } catch (error) {
                    fail(error instanceof Error ? error.message : 'Feed error');
                }
            };
            current.onerror = () => fail('Connection lost; reconnecting');
            current.onclose = () => fail('Connection closed; reconnecting');
        };

        connect();
        publish();

        // periodically publish changes and close connections that have gone quiet (closing triggers a reconnect)
        const timer = setInterval(() => {
            const timeout = book.seq === null ? snapshotTimeout : idleTimeout;
            if (socket && Date.now() - received > timeout) {
                socket.close();
            }
            publish();
        }, publishInterval);

        return () => {
            stopped = true;
            clearInterval(timer);
            clearTimeout(retry);
            socket?.close();
        };
    }, [key, symbol, session]);

    return result.key === key ? result.view : emptyMarket();
}
