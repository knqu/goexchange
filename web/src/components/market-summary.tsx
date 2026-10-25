import { money, quantity, timeAgo } from '@/lib/format';
import type { Observed } from '@/lib/market';

// MarketSummary shows the last observed trade price and stats over every trade received since the symbol was opened.
export function MarketSummary({ observed, now }: { observed: Observed; now: number }) {
    return (
        <section className="quote-summary" aria-label="Observed market activity">
            <div className="last-trade">
                <span className="eyebrow">Last observed trade</span>
                <strong className="last-price">{money(observed.lastPrice)}</strong>
            </div>

            <div className="session-summary">
                <dl className="session-stats">
                    <div>
                        <dt>Price change</dt>
                        <PriceChange observed={observed} />
                    </div>
                    <div>
                        <dt>Observed volume</dt>
                        <dd>
                            {quantity(observed.volume)} <span className="stat-unit">shares</span>
                        </dd>
                    </div>
                    <div>
                        <dt>Last trade</dt>
                        <dd>{timeAgo(observed.lastReceived, now)}</dd>
                    </div>
                </dl>
                <span
                    className="session-footnote"
                    title="Trades received since opening this symbol. Resets on symbol changes or exchange restarts; missed trades during disconnects are not included."
                >
                    Since opening
                </span>
            </div>
        </section>
    );
}

// PriceChange shows the change from the first to the last observed trade price, in dollars and percent.
function PriceChange({ observed }: { observed: Observed }) {
    const { firstPrice, lastPrice } = observed;
    if (firstPrice == null || lastPrice == null) {
        return <dd>—</dd>;
    }

    const change = lastPrice - firstPrice;
    const percent = (change / firstPrice) * 100;

    let color = '';
    let arrow = '';
    if (change > 0) {
        color = 'buy-text';
        arrow = '↗ ';
    } else if (change < 0) {
        color = 'sell-text';
        arrow = '↘ ';
    }

    const sign = change > 0 ? '+' : ''; // negative values already carry a sign
    const percentSign = percent > 0 ? '+' : '';

    return (
        <dd className={color}>
            <span aria-hidden="true">{arrow}</span>
            {sign}
            {money(change)}
            <span className="change-percent">
                ({percentSign}
                {percent.toFixed(2)}%)
            </span>
        </dd>
    );
}
