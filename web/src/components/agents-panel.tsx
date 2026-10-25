import { X } from 'lucide-react';
import { Button } from '@/components/ui/button';
import type { Agent, Observation } from '@/hooks/use-observation';
import { money, signed } from '@/lib/format';

const staleAfter = 5000; // ms since an agent's last published observation before marking as stale

// --- agents table ---

// AgentsTable lists each agent's position and unrealized P&L for the selected symbol.
export function AgentsTable({
    agents,
    symbol,
    selected,
    onSelect,
    stale,
    now,
}: {
    agents: Agent[];
    symbol: string;
    selected: string | null;
    onSelect: (id: string, button: HTMLButtonElement) => void;
    stale: boolean;
    now: number;
}) {
    return (
        <section className="panel agents-panel">
            <div className="panel-heading">
                <h2>Agents</h2>
                <span>{symbol} inventory &amp; performance</span>
            </div>

            <div className="table-scroll">
                <table>
                    <thead>
                        <tr>
                            <th>Agent</th>
                            <th>Strategy</th>
                            <th className="numeric">Position</th>
                            <th className="numeric">Unrealized P&amp;L</th>
                        </tr>
                    </thead>
                    <tbody>
                        {agents.map((agent) => {
                            const observation = agent.observation!;
                            const position = observation.portfolio.positions?.find(
                                (position) => position.symbol === symbol,
                            );
                            const unpriced = observation.portfolio.unpriced?.includes(symbol);
                            const isSelected = selected === observation.id;

                            const outdated = now - new Date(observation.updated_at).getTime() > staleAfter;
                            const flagged = stale || outdated || !observation.running;

                            return (
                                <tr key={observation.id} className={isSelected ? 'selected-row' : ''}>
                                    <td>
                                        <button
                                            className="agent-link"
                                            aria-expanded={isSelected}
                                            aria-controls="agent-expanded"
                                            onClick={(event) => onSelect(observation.id, event.currentTarget)}
                                        >
                                            Agent {observation.id}
                                        </button>
                                        {flagged && (
                                            <span className="stale-tag">
                                                {observation.running ? 'stale' : 'stopped'}
                                            </span>
                                        )}
                                    </td>
                                    <td>{agent.strategy}</td>
                                    <td className="numeric">{signed(position?.quantity ?? 0)}</td>
                                    <td className="numeric">{unpriced ? '—' : money(position?.unrealized ?? 0)}</td>
                                </tr>
                            );
                        })}

                        {agents.length === 0 && (
                            <tr>
                                <td colSpan={4} className="empty-cell">
                                    {stale ? 'Waiting for agent observations...' : 'No agents are reporting yet.'}
                                </td>
                            </tr>
                        )}
                    </tbody>
                </table>
            </div>
        </section>
    );
}

// --- agent details ---

// AgentDetails shows one agent's full portfolio (across all symbols) and its policy for the selected symbol.
export function AgentDetails({ agent, symbol, close }: { agent: Agent; symbol: string; close: () => void }) {
    const observation = agent.observation!;
    const portfolio = observation.portfolio;
    const policy = observation.policies[symbol];
    const partiallyValued = !!portfolio.unpriced?.length;

    return (
        <section className="agent-detail panel" aria-labelledby="agent-detail-title">
            <div className="panel-heading">
                <h2 id="agent-detail-title">
                    Agent {observation.id} <span className="muted">/ {agent.strategy}</span>
                </h2>
                <Button variant="ghost" size="icon" onClick={close} aria-label="Close agent details">
                    <X />
                </Button>
            </div>

            <dl className="detail-grid">
                <div>
                    <dt>Cash</dt>
                    <dd>{money(portfolio.cash)}</dd>
                </div>
                <div>
                    <dt>Net worth {partiallyValued ? '(partially valued)' : ''}</dt>
                    <dd>{money(portfolio.net_worth)}</dd>
                </div>
                <div>
                    <dt>Realized P&amp;L</dt>
                    <dd>{money(portfolio.total_realized)}</dd>
                </div>
                <div>
                    <dt>Unrealized P&amp;L</dt>
                    <dd>{partiallyValued ? '—' : money(portfolio.total_unrealized)}</dd>
                </div>
                <div>
                    <dt>Starting cash</dt>
                    <dd>{money(agent.starting_cash)}</dd>
                </div>
            </dl>

            {partiallyValued && (
                <p className="notice">
                    Waiting for a current mark for {portfolio.unpriced!.join(', ')}. Those positions are excluded from
                    net worth.
                </p>
            )}

            <PositionsTable observation={observation} />

            <div className="panel-heading policy-heading">
                <h3>
                    Policy <span className="muted">/ {symbol}</span>
                </h3>
                <span>Active policy · deliberated by agent</span>
            </div>

            {policy && (
                <>
                    <dl className="detail-grid policy-grid">
                        <div>
                            <dt>Participation</dt>
                            <dd>{policy.participation ? 'Enabled' : 'Standing down'}</dd>
                        </div>
                        <div>
                            <dt>Bias</dt>
                            <dd>{policy.bias.toFixed(2)}</dd>
                        </div>
                        <div>
                            <dt>Risk appetite</dt>
                            <dd>{policy.risk_appetite.toFixed(2)}</dd>
                        </div>
                        <div>
                            <dt>Aggression</dt>
                            <dd>
                                {policy.aggression.toFixed(2)} · {policy.aggression < 0.5 ? 'resting' : 'crossing'}
                            </dd>
                        </div>
                    </dl>
                    {policy.rationale && <p className="rationale">{policy.rationale}</p>}
                </>
            )}

            <p className="panel-note">
                Portfolio accounting starts with this agent session. Observed{' '}
                {new Date(observation.updated_at).toLocaleTimeString()}.
            </p>
        </section>
    );
}

// PositionsTable lists every position the agent holds or has held, across all symbols.
function PositionsTable({ observation }: { observation: Observation }) {
    const { positions, unpriced } = observation.portfolio;

    return (
        <div className="table-scroll">
            <table>
                <caption>Positions across all symbols</caption>
                <thead>
                    <tr>
                        <th>Symbol</th>
                        <th className="numeric">Position</th>
                        <th className="numeric">Average cost</th>
                        <th className="numeric">Realized P&amp;L</th>
                        <th className="numeric">Unrealized P&amp;L</th>
                    </tr>
                </thead>
                <tbody>
                    {(positions ?? []).map((position) => (
                        <tr key={position.symbol}>
                            <td>{position.symbol}</td>
                            <td className="numeric">{signed(position.quantity)}</td>
                            <td className="numeric">{money(position.average_cost)}</td>
                            <td className="numeric">{money(position.realized)}</td>
                            <td className="numeric">
                                {unpriced?.includes(position.symbol) ? '—' : money(position.unrealized)}
                            </td>
                        </tr>
                    ))}

                    {!positions?.length && (
                        <tr>
                            <td colSpan={5} className="empty-cell">
                                No fills in this session yet.
                            </td>
                        </tr>
                    )}
                </tbody>
            </table>
        </div>
    );
}
