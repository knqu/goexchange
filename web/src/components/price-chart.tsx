import { useEffect, useRef, useState } from 'react';
import {
    ColorType,
    LineSeries,
    createChart,
    type IChartApi,
    type ISeriesApi,
    type UTCTimestamp,
} from 'lightweight-charts';
import { Button } from '@/components/ui/button';
import type { Point } from '@/lib/market';

const followWindow = 60; // seconds of history visible while following live prices
const rightOffset = 3; // seconds of empty space kept to the right of the latest point

// themeColors reads the current theme's css variables (so the chart matches the page in light/dark mode).
function themeColors() {
    const css = getComputedStyle(document.documentElement);
    const color = (name: string) => css.getPropertyValue(name).trim();

    return {
        background: color('--background'),
        text: color('--muted-foreground'),
        grid: color('--border'),
        line: color('--gold'),
    };
}

// PriceChart plots observed trade prices over receive time.
export function PriceChart({ points }: { points: Point[] }) {
    const container = useRef<HTMLDivElement>(null);
    const chart = useRef<IChartApi | null>(null);
    const series = useRef<ISeriesApi<'Line'> | null>(null);

    const [following, setFollowing] = useState(true);
    const followingRef = useRef(true);

    useEffect(() => {
        followingRef.current = following;
        if (following) {
            chart.current?.timeScale().scrollToRealTime();
        }
    }, [following]);

    useEffect(() => {
        const host = container.current!;
        const colors = themeColors();

        const instance = createChart(host, {
            autoSize: true,
            height: 300,
            layout: {
                background: { type: ColorType.Solid, color: colors.background },
                textColor: colors.text,
                fontFamily: 'Manrope, sans-serif',
                fontSize: 11,
                attributionLogo: false,
            },
            grid: {
                vertLines: { visible: false },
                horzLines: { color: colors.grid },
            },
            rightPriceScale: {
                borderVisible: false,
                scaleMargins: { top: 0.15, bottom: 0.15 },
            },
            timeScale: {
                borderVisible: false,
                timeVisible: true,
                secondsVisible: true,
                rightOffset,
            },
            localization: {
                locale: 'en-US',
                priceFormatter: (price: number) => `$${price.toFixed(2)}`,
            },
        });

        const line = instance.addSeries(LineSeries, {
            color: colors.line,
            lineWidth: 2,
            priceLineVisible: false,
            crosshairMarkerRadius: 4,
            priceFormat: { type: 'price', precision: 2, minMove: 0.01 },
        });

        chart.current = instance;
        series.current = line;

        // any manual scrolling or dragging stops following live prices until "follow live" is clicked to resume
        const stopFollowing = () => setFollowing(false);
        host.addEventListener('wheel', stopFollowing, { passive: true });
        host.addEventListener('pointerdown', stopFollowing);

        const appearance = matchMedia('(prefers-color-scheme: dark)');
        const repaint = () => {
            const colors = themeColors();
            instance.applyOptions({
                layout: { background: { type: ColorType.Solid, color: colors.background }, textColor: colors.text },
                grid: { horzLines: { color: colors.grid } },
            });
            line.applyOptions({ color: colors.line });
        };
        appearance.addEventListener('change', repaint);

        return () => {
            host.removeEventListener('wheel', stopFollowing);
            host.removeEventListener('pointerdown', stopFollowing);
            appearance.removeEventListener('change', repaint);
            instance.remove();
            chart.current = null;
            series.current = null;
        };
    }, []);

    // redraw whenever new points arrive, keeping the latest minute in view while following
    useEffect(() => {
        series.current?.setData(points.map((point) => ({ time: point.time as UTCTimestamp, value: point.value })));

        if (followingRef.current && points.length > 0) {
            const latest = points.at(-1)!.time;
            chart.current?.timeScale().setVisibleRange({
                from: (latest - followWindow) as UTCTimestamp,
                to: (latest + rightOffset) as UTCTimestamp,
            });
        }
    }, [points]);

    return (
        <section className="panel chart-panel" aria-labelledby="chart-title">
            <div className="panel-heading">
                <h2 id="chart-title">Trade price</h2>
                <div className="chart-controls">
                    <span>Receive time · UTC</span>
                    <Button
                        variant={following ? 'default' : 'ghost'}
                        size="sm"
                        onClick={() => setFollowing(true)}
                        aria-pressed={following}
                    >
                        Follow live
                    </Button>
                </div>
            </div>

            <div className="chart-wrap">
                <div
                    ref={container}
                    className="price-chart"
                    role="img"
                    aria-label="Observed trade prices in US dollars, over browser receive time"
                />
                {points.length === 0 && (
                    <div className="chart-empty">
                        Waiting for a trade<span>Prices appear as executions arrive.</span>
                    </div>
                )}
            </div>

            <p className="panel-note">History resets after reconnect.</p>
        </section>
    );
}
