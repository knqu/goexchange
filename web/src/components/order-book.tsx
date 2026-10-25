import { money, quantity } from '@/lib/format';
import type { Level } from '@/lib/market';

const shownLevels = 10;

// OrderBook shows the best bid, ask, and spread, along with the top levels of each side of the book.
export function OrderBook({ bids, asks, live }: { bids: Level[]; asks: Level[]; live: boolean }) {
    const bestBid = live ? bids[0]?.Price : null;
    const bestAsk = live ? asks[0]?.Price : null;
    const spread = bestBid != null && bestAsk != null ? BigInt(bestAsk) - BigInt(bestBid) : null;

    return (
        <section className="panel book-panel">
            <div className="panel-heading">
                <h2>Order book</h2>
                <dl className="book-quotes" aria-label="Current best quotes">
                    <div>
                        <dt>Bid</dt>
                        <dd className="buy-text">{money(bestBid)}</dd>
                    </div>
                    <div>
                        <dt>Ask</dt>
                        <dd className="sell-text">{money(bestAsk)}</dd>
                    </div>
                    <div>
                        <dt>Spread</dt>
                        <dd>{money(spread)}</dd>
                    </div>
                </dl>
            </div>

            <div className="depth">
                <BookSide levels={bids} side="Bid" />
                <BookSide levels={asks} side="Ask" />
            </div>
        </section>
    );
}

// BookSide shows one side of the book, with a bar behind each level sized by its quantity.
function BookSide({ levels, side }: { levels: Level[]; side: 'Bid' | 'Ask' }) {
    const shown = levels.slice(0, shownLevels);
    const largest = Math.max(1, ...shown.map((level) => level.Quantity)); // sizings are relative to the largest level
    const name = side.toLowerCase();
    const direction = side === 'Bid' ? 'left' : 'right'; // bars grow outward from the middle of the book

    return (
        <table className={`book-side ${name}`}>
            <thead>
                <tr>
                    <th>{side} $</th>
                    <th className="numeric">Qty</th>
                </tr>
            </thead>
            <tbody>
                {shown.map((level) => {
                    const fill = (level.Quantity / largest) * 100;
                    const bar = `linear-gradient(to ${direction}, var(--depth-${name}) ${fill}%, transparent ${fill}%)`;

                    return (
                        <tr key={level.Price} style={{ backgroundImage: bar }}>
                            <td>{money(level.Price).replace('$', '')}</td>
                            <td className="numeric">{quantity(level.Quantity)}</td>
                        </tr>
                    );
                })}

                {shown.length === 0 && (
                    <tr>
                        <td colSpan={2} className="empty-cell">
                            No {name}s
                        </td>
                    </tr>
                )}
            </tbody>
        </table>
    );
}
