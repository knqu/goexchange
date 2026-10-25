// Integer is a whole number from the exchange: a number if it fits safely, otherwise a bigint.
export type Integer = number | bigint;

// money formats an integer amount of cents as dollars using bigint arithmetic so large balances are never rounded.
export function money(ticks: Integer | null | undefined): string {
    if (ticks == null) {
        return '—';
    }

    const value = BigInt(ticks);
    const absolute = value < 0n ? -value : value;
    const sign = value < 0n ? '-' : '';
    const dollars = (absolute / 100n).toLocaleString('en-US');
    const cents = (absolute % 100n).toString().padStart(2, '0');

    return `${sign}$${dollars}.${cents}`;
}

// quantity formats a share count with thousands separators.
export function quantity(value: Integer | null | undefined): string {
    if (value == null) {
        return '—';
    }
    return value.toLocaleString('en-US');
}

// signed formats a share count with an explicit "+" for positive values (negative values already carry a sign).
export function signed(value: Integer): string {
    const sign = value > 0 ? '+' : '';
    return `${sign}${quantity(value)}`;
}

// timeAgo formats how long ago a moment was (both in milliseconds), such as "5s ago" or "2h ago".
export function timeAgo(received: number | null, now: number): string {
    if (received === null) {
        return '—';
    }

    const seconds = Math.max(0, Math.floor((now - received) / 1000)); // clamp in case the clock moved backwards

    if (seconds < 60) {
        return `${seconds}s ago`;
    }
    if (seconds < 3600) {
        return `${Math.floor(seconds / 60)}m ago`;
    }
    if (seconds < 86400) {
        return `${Math.floor(seconds / 3600)}h ago`;
    }
    return `${Math.floor(seconds / 86400)}d ago`;
}
