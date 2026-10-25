import { money, quantity } from '@/lib/format';
import type { Trade } from '@/lib/market';

// TradeTape lists recently observed trades newest first and colored by the aggressor (taker) side.
export function TradeTape({ trades, now }: { trades: Trade[]; now: number }) {
    return (
        <section className="panel tape-panel">
            <div className="panel-heading">
                <h2>Recent trades</h2>
                <span>Aggressor side</span>
            </div>

            <div className="tape-scroll">
                <table>
                    <thead>
                        <tr>
                            <th>Received</th>
                            <th>Side</th>
                            <th className="numeric">Price $</th>
                            <th className="numeric">Qty</th>
                        </tr>
                    </thead>
                    <tbody>
                        {trades.map((trade) => {
                            const secondsAgo = Math.max(0, Math.floor((now - trade.received) / 1000));
                            const isBuy = trade.side === 0;

                            return (
                                <tr key={trade.key} title={`Maker order ${trade.maker} · taker order ${trade.taker}`}>
                                    <td>{secondsAgo}s ago</td>
                                    <td className={isBuy ? 'buy-text' : 'sell-text'}>{isBuy ? 'Buy' : 'Sell'}</td>
                                    <td className="numeric">{money(trade.price).replace('$', '')}</td>
                                    <td className="numeric">{quantity(trade.quantity)}</td>
                                </tr>
                            );
                        })}

                        {trades.length === 0 && (
                            <tr>
                                <td colSpan={4} className="empty-cell">
                                    No trades observed yet.
                                </td>
                            </tr>
                        )}
                    </tbody>
                </table>
            </div>
        </section>
    );
}
