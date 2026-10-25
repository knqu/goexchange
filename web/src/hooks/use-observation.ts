import { useEffect, useState } from 'react';
import { decode } from '@/lib/market';
import type { Integer } from '@/lib/format';

// --- api data structures ---

// these mirror the json served by /admin/symbols and /admin/agents

export type Policy = {
    participation: boolean;
    bias: number;
    risk_appetite: number;
    aggression: number;
    rationale: string;
};

export type Position = {
    symbol: string;
    quantity: Integer;
    average_cost: Integer;
    realized: Integer;
    unrealized: Integer;
};

export type Portfolio = {
    cash: Integer;
    net_worth: Integer;
    total_realized: Integer;
    total_unrealized: Integer;
    positions: Position[] | null;
    unpriced: string[] | null;
};

export type Observation = {
    id: string;
    updated_at: string;
    running: boolean;
    policies: Record<string, Policy>;
    portfolio: Portfolio;
};

export type Agent = {
    strategy: string;
    starting_cash: Integer;
    observation: Observation | null;
};

// ExchangeView is everything the dashboard polls for, plus the state of the polling itself.
export type ExchangeView = {
    session: string; // identifies the exchange run; a change means the exchange restarted
    symbols: string[];
    agents: Agent[];
    error: string;
    received: number;
};

const pollInterval = 1000; // ms between the end of one poll and the start of the next
const pollTimeout = 3000; // ms before an unanswered poll is abandoned

// --- polling ---

async function readJSON<T>(path: string, signal: AbortSignal): Promise<T> {
    const response = await fetch(path, { signal, cache: 'no-store' });
    if (!response.ok) {
        throw Error(`Exchange returned ${response.status}`);
    }
    return decode(await response.text()) as T;
}

// useObservation polls the exchange for symbols and agent observations once a second.
export function useObservation(): ExchangeView {
    const [view, setView] = useState<ExchangeView>({ session: '', symbols: [], agents: [], error: '', received: 0 });

    useEffect(() => {
        let stopped = false; // set on cleanup, so a poll finishing afterwards neither updates state nor reschedules
        let timer: ReturnType<typeof setTimeout>;
        let controller: AbortController;

        const poll = async () => {
            controller = new AbortController();
            const timeout = setTimeout(() => controller.abort(), pollTimeout);

            try {
                const [market, fleet] = await Promise.all([
                    readJSON<{ symbols: string[] }>('/admin/symbols', controller.signal),
                    readJSON<{ session: string; agents: Agent[] }>('/admin/agents', controller.signal),
                ]);

                if (!stopped) {
                    setView({
                        session: fleet.session,
                        symbols: market.symbols,
                        agents: fleet.agents,
                        error: '',
                        received: Date.now(),
                    });
                }
            } catch (error) {
                // keep showing last successful view, flagged with the error
                if (!stopped) {
                    const message = error instanceof Error ? error.message : 'Exchange unavailable';
                    setView((previous) => ({ ...previous, error: message }));
                }
            } finally {
                clearTimeout(timeout);
                if (!stopped) {
                    timer = setTimeout(poll, pollInterval);
                }
            }
        };

        void poll();

        return () => {
            stopped = true;
            clearTimeout(timer);
            controller?.abort();
        };
    }, []);

    return view;
}
