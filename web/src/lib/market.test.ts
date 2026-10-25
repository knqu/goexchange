import { describe, expect, it } from 'vitest';
import { MarketState } from './market';
import { money, timeAgo } from './format';

const snapshot = '{"type":"snapshot","Seq":2,"Halted":false,"Bids":[{"Price":10000,"Quantity":10}],"Asks":null}';

function trade(price: number, quantity: number): string {
    return JSON.stringify({
        type: 'trade',
        Side: 0,
        Price: price,
        Quantity: quantity,
        MakerOrderID: 1,
        TakerOrderID: 2,
    });
}

describe('market state', () => {
    it('tracks halt state from snapshots and status messages', () => {
        const book = new MarketState();
        expect(() => book.apply('{"type":"status","Halted":true}')).toThrow('before snapshot');

        book.apply(snapshot);
        expect(book.halted).toBe(false);

        book.apply('{"type":"status","Halted":true}');
        expect(book.halted).toBe(true);

        book.apply(snapshot.replace('"Halted":false', '"Halted":true'));
        expect(book.halted).toBe(true);
    });

    it('overwrites absolute quantities and removes empty levels', () => {
        const book = new MarketState();
        book.apply(snapshot);

        book.apply('{"type":"delta","Seq":3,"Side":0,"Price":10000,"Quantity":4}');
        expect(book.bids.get(10000)).toBe(4);

        book.apply('{"type":"delta","Seq":4,"Side":0,"Price":10000,"Quantity":0}');
        expect(book.bids.size).toBe(0);
    });

    it('discards a gapped book and only resumes on a new snapshot', () => {
        const book = new MarketState();
        book.apply(snapshot);

        expect(() => book.apply('{"type":"delta","Seq":4,"Side":0,"Price":10000,"Quantity":30}')).toThrow('Book gap');
        expect(book.bids.size).toBe(0);
        expect(book.seq).toBeNull();

        book.apply(snapshot);
        expect(book.bids.get(10000)).toBe(10);
    });

    it('keeps uint64 sequence numbers exact beyond JavaScript number precision', () => {
        const book = new MarketState();
        book.apply(snapshot.replace('"Seq":2', '"Seq":9007199254740993'));
        book.apply('{"type":"delta","Seq":9007199254740994,"Side":1,"Price":10004,"Quantity":10}');

        expect(book.seq).toBe(9007199254740994n);
    });

    it('bounds trade history and coalesces chart updates in the same second', () => {
        const book = new MarketState();
        book.apply(snapshot);

        const largeTakerTrade =
            '{"type":"trade","Side":0,"Price":10002,"Quantity":5,"MakerOrderID":1,"TakerOrderID":9007199254740993}';
        for (let i = 0; i < 250; i++) {
            book.apply(largeTakerTrade, 100000 + i);
        }

        expect(book.trades).toHaveLength(100);
        expect(book.points).toHaveLength(1);
        expect(book.trades[0].taker).toBe('9007199254740993');
        expect(book.observed.volume).toBe(1250n);

        book.invalidate();
        expect(book.last).toBeNull();
        expect(book.points).toHaveLength(0);
    });

    it('rejects trade-before-snapshot and unsafe numeric market values', () => {
        const book = new MarketState();

        expect(() => book.apply('{"type":"trade"}')).toThrow('before snapshot');
        expect(() => book.apply(snapshot.replace('10000', '9007199254740993'))).toThrow('precision');
    });

    it('retains observed stats through reconnects without counting snapshots or invalid trades', () => {
        const book = new MarketState();
        book.apply(snapshot);
        book.apply(trade(10000, Number.MAX_SAFE_INTEGER), 1000);
        const previousView = book.view('live', '');

        book.invalidate();
        book.apply(snapshot);
        expect(() => book.apply(trade(10200, -5), 2000)).toThrow('Invalid trade');
        book.apply(trade(10200, 10), 3000);

        expect(book.view('live', '').observed).toEqual({
            firstPrice: 10000,
            lastPrice: 10200,
            volume: 9007199254741001n,
            lastReceived: 3000,
        });
        expect(previousView.observed.lastPrice).toBe(10000);
        expect(new MarketState().observed).toEqual({
            firstPrice: null,
            lastPrice: null,
            volume: 0n,
            lastReceived: null,
        });
    });
});

describe('formatting', () => {
    it('formats integer cents without rounding large portfolio balances', () => {
        expect(money(10002)).toBe('$100.02');
        expect(money(-4)).toBe('-$0.04');
        expect(money(9007199254740993n)).toBe('$90,071,992,547,409.93');
    });

    it('formats last-trade age across time units and clock changes', () => {
        expect(timeAgo(null, 10000)).toBe('—');
        expect(timeAgo(1000, 3999)).toBe('2s ago');
        expect(timeAgo(1000, 61000)).toBe('1m ago');
        expect(timeAgo(1000, 3601000)).toBe('1h ago');
        expect(timeAgo(1000, 86401000)).toBe('1d ago');
        expect(timeAgo(2000, 1000)).toBe('0s ago');
    });
});
