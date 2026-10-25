import { useEffect, useRef, useState } from 'react';

const requestTimeout = 5000; // ms before an unanswered halt/resume request is abandoned
const confirmTimeout = 10000; // ms to wait for the feed to confirm the engine applied the request

// PendingRequest is a halt/resume request that has been sent but not yet confirmed by the market feed.
export type PendingRequest = {
    symbol: string;
    session: string;
    halted: boolean;
    sent: number;
};

// useHaltControl sends halt/resume requests for a symbol and tracks them until confirmed by the market feed.
export function useHaltControl(symbol: string, session: string, halted: boolean | null, now: number) {
    const [pending, setPending] = useState<PendingRequest | null>(null);
    const [error, setError] = useState('');

    const context = `${session}/${symbol}`;
    const currentContext = useRef(context);
    currentContext.current = context;

    useEffect(() => {
        setPending(null);
        setError('');
    }, [symbol, session]);

    useEffect(() => {
        if (!pending) {
            return;
        }

        if (halted === pending.halted) {
            setPending(null);
            return;
        }

        if (now - pending.sent > confirmTimeout) {
            setPending(null);
            setError('Confirmation timed out. Check the current trading state before trying again.');
        }
    }, [pending, halted, now]);

    const toggle = async () => {
        if (halted === null || pending) {
            return;
        }

        const request: PendingRequest = { symbol, session, halted: !halted, sent: Date.now() };
        const action = request.halted ? 'halt' : 'resume';

        setError('');
        setPending(request);

        try {
            const response = await fetch(`/admin/symbols/${encodeURIComponent(symbol)}/${action}`, {
                method: 'POST',
                signal: AbortSignal.timeout(requestTimeout),
            });

            if (!response.ok) {
                const message =
                    response.status === 503 ? 'Exchange is busy; try again.' : `Request failed (${response.status})`;
                throw Error(message);
            }
        } catch (error) {
            if (currentContext.current !== `${request.session}/${request.symbol}`) {
                return;
            }

            setPending((current) => (current?.sent === request.sent ? null : current));
            setError(error instanceof Error ? error.message : 'Request failed');
        }
    };

    return { pending, error, toggle };
}
