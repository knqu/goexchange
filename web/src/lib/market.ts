import { parse } from 'lossless-json';

// --- json decoding ---

// decode parses JSON like JSON.parse, but keeps integers beyond Number.MAX_SAFE_INTEGER exact as bigints.
// uint64 values from the exchange (order ids, sequence numbers) can exceed what a JavaScript number represents.
export function decode(text: string): unknown {
    return parse(text, undefined, (value) => {
        const number = Number(value);
        const isUnsafeInteger = /^-?\d+$/.test(value) && !Number.isSafeInteger(number);

        return isUnsafeInteger ? BigInt(value) : number;
    });
}

// --- market data structures ---

// Level is one price level of the book: the total resting quantity at a price (both in integer ticks/units).
export type Level = {
    Price: number;
    Quantity: number;
};

// Trade is a single execution received from the feed, as shown in the recent trades tape.
export type Trade = {
    price: number;
    quantity: number;
    side: 0 | 1;
    maker: string; // order ids are strings because they can exceed javascript number precision
    taker: string;
    received: number; // browser receive time in milliseconds (feed carries no timestamps)
    key: number;
};

// Point is one chart point: the last observed trade price (in dollars) during one receive-time second.
export type Point = {
    time: number; // unix seconds
    value: number;
};

// Observed holds statistics over every trade received since opening a symbol (surviving reconnects).
export type Observed = {
    firstPrice: number | null;
    lastPrice: number | null;
    volume: bigint;
    lastReceived: number | null;
};

// MarketView is an immutable copy of a symbol's market state, handed to React for rendering.
export type MarketView = {
    status: 'connecting' | 'live' | 'reconnecting';
    reason: string;
    seq: string;
    bids: Level[]; // best (highest) first
    asks: Level[]; // best (lowest) first
    trades: Trade[]; // newest first
    points: Point[];
    last: number | null;
    halted: boolean;
    gaps: number;
    observed: Observed;
};

const maxTrades = 100; // trades kept for the recent trades tape
const maxPoints = 1800; // chart points kept (30 minutes at one point per second)

function emptyObserved(): Observed {
    return { firstPrice: null, lastPrice: null, volume: 0n, lastReceived: null };
}

// emptyMarket returns the view shown before a symbol's feed has delivered anything.
export function emptyMarket(): MarketView {
    return {
        status: 'connecting',
        reason: 'Waiting for a snapshot',
        seq: '—',
        bids: [],
        asks: [],
        trades: [],
        points: [],
        last: null,
        halted: false,
        gaps: 0,
        observed: emptyObserved(),
    };
}

// --- field validation ---

// note: every field from the feed is validated, so a malformed message fails loudly instead of corrupting the book

function flag(value: unknown): boolean {
    if (typeof value !== 'boolean') {
        throw Error('Invalid trading status');
    }
    return value;
}

// integer accepts prices and quantities, which must stay exact as numbers to be displayed and compared.
function integer(value: unknown): number {
    if (typeof value !== 'number' || !Number.isSafeInteger(value)) {
        throw Error('Market value exceeds display precision');
    }
    return value;
}

// sequence accepts uint64 values (sequence numbers and order ids), which may arrive as bigints.
function sequence(value: unknown): bigint {
    if (typeof value === 'bigint' && value >= 0n) {
        return value;
    }
    if (typeof value === 'number' && Number.isSafeInteger(value) && value >= 0) {
        return BigInt(value);
    }
    throw Error('Invalid sequence');
}

function side(value: unknown): 0 | 1 {
    if (value === 0 || value === 1) {
        return value;
    }
    throw Error('Invalid side');
}

// readLevels converts a snapshot's array of levels into a price to quantity mapping.
function readLevels(value: unknown): Map<number, number> {
    if (value != null && !Array.isArray(value)) {
        throw Error('Invalid depth');
    }

    const levels = new Map<number, number>();
    for (const level of (value ?? []) as Level[]) {
        const price = integer(level.Price);
        const quantity = integer(level.Quantity);
        if (price <= 0 || quantity <= 0) {
            throw Error('Invalid depth level');
        }
        levels.set(price, quantity);
    }

    return levels;
}

// --- market state ---

// MarketState mirrors one symbol's book from the feed, along with a bounded history of observed trades.
export class MarketState {
    bids = new Map<number, number>();
    asks = new Map<number, number>();
    seq: bigint | null = null;
    halted = false;
    last: number | null = null;
    trades: Trade[] = []; // oldest first
    points: Point[] = [];
    gaps = 0;
    observed = emptyObserved();
    private tradeKey = 0;

    // invalidate discards the book (and chart/tape history) after a gap/disconnect until the next snapshot.
    invalidate() {
        this.seq = null;
        this.bids.clear();
        this.asks.clear();
        this.last = null;
        this.points = [];
        this.trades = [];
        // observed statistics are kept since they count every trade actually received
    }

    // apply processes one feed message, throwing an error if invalid or a sequence gap is revealed.
    apply(text: string, now = Date.now()) {
        const message = decode(text) as Record<string, unknown>;
        if (!message || typeof message !== 'object') {
            throw Error('Invalid feed message');
        }

        switch (message.type) {
            case 'heartbeat':
                return;
            case 'snapshot':
                this.applySnapshot(message);
                return;
            case 'status':
                this.applyStatus(message);
                return;
            case 'delta':
                this.applyDelta(message);
                return;
            case 'trade':
                this.applyTrade(message, now);
                return;
            default:
                throw Error('Unknown feed message');
        }
    }

    private applySnapshot(message: Record<string, unknown>) {
        // validate every field before replacing any state, so a bad snapshot leaves the book untouched
        const bids = readLevels(message.Bids);
        const asks = readLevels(message.Asks);
        const seq = sequence(message.Seq);
        const halted = flag(message.Halted);

        this.bids = bids;
        this.asks = asks;
        this.seq = seq;
        this.halted = halted;
    }

    private applyStatus(message: Record<string, unknown>) {
        if (this.seq === null) {
            throw Error('Status before snapshot');
        }
        this.halted = flag(message.Halted);
    }

    private applyDelta(message: Record<string, unknown>) {
        // each delta must follow the previous one; otherwise, a message was dropped (meaning the book is stale)
        if (this.seq === null || sequence(message.Seq) !== this.seq + 1n) {
            this.gaps++;
            this.invalidate();
            throw Error('Book gap detected; getting a fresh snapshot');
        }

        const price = integer(message.Price);
        const quantity = integer(message.Quantity);
        const book = side(message.Side) === 0 ? this.bids : this.asks;
        if (price <= 0 || quantity < 0) {
            throw Error('Invalid depth update');
        }

        if (quantity === 0) {
            book.delete(price);
        } else {
            book.set(price, quantity);
        }

        this.seq++;
    }

    private applyTrade(message: Record<string, unknown>, now: number) {
        if (this.seq === null) {
            throw Error('Trade before snapshot');
        }

        const price = integer(message.Price);
        const quantity = integer(message.Quantity);
        if (price <= 0 || quantity <= 0) {
            throw Error('Invalid trade');
        }

        const trade: Trade = {
            price,
            quantity,
            side: side(message.Side),
            maker: sequence(message.MakerOrderID).toString(),
            taker: sequence(message.TakerOrderID).toString(),
            received: now,
            key: ++this.tradeKey,
        };

        this.last = price;

        this.observed.firstPrice ??= price;
        this.observed.lastPrice = price;
        this.observed.volume += BigInt(quantity);
        this.observed.lastReceived = now;

        this.trades.push(trade);
        if (this.trades.length > maxTrades) {
            this.trades.shift();
        }

        this.addPoint(now, price);
    }

    // addPoint records a trade price on the chart, keeping one point per receive-time second.
    private addPoint(now: number, price: number) {
        const tail = this.points.at(-1);
        const time = Math.max(Math.floor(now / 1000), tail?.time ?? 0); // never step backwards if the clock does
        const value = price / 100;

        if (tail?.time === time) {
            tail.value = value; // later trades in the same second replace that second's price
        } else {
            this.points.push({ time, value });
        }

        if (this.points.length > maxPoints) {
            this.points.splice(0, this.points.length - maxPoints);
        }
    }

    // view returns an immutable copy of the current state for rendering.
    view(status: MarketView['status'], reason: string): MarketView {
        return {
            status,
            reason,
            seq: this.seq?.toString() ?? '—',
            bids: sortedLevels(this.bids, 'descending'),
            asks: sortedLevels(this.asks, 'ascending'),
            trades: [...this.trades].reverse(),
            points: this.points.map((point) => ({ ...point })),
            last: this.last,
            halted: this.halted,
            gaps: this.gaps,
            observed: { ...this.observed },
        };
    }
}

function sortedLevels(book: Map<number, number>, order: 'ascending' | 'descending'): Level[] {
    const levels = [...book].map(([Price, Quantity]) => ({ Price, Quantity }));

    if (order === 'ascending') {
        return levels.sort((x, y) => x.Price - y.Price);
    }
    return levels.sort((x, y) => y.Price - x.Price);
}
