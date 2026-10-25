import { test, expect } from '@playwright/test';
import { spawn, type ChildProcess } from 'node:child_process';
import { mkdtempSync } from 'node:fs';
import path from 'node:path';

const address = '127.0.0.1:18080';

let exchange: ChildProcess;

test.beforeAll(async () => {
    const root = path.resolve('..');
    const workingDirectory = mkdtempSync(path.join(root, '.cache', 'ui-smoke-'));

    exchange = spawn(
        path.join(root, '.cache', 'goexchange-ui.exe'),
        [
            '-symbols',
            'ACME,BETA',
            '-fleet',
            path.join(root, 'configs', 'agents.yaml'),
            '-gatewayAddress',
            address,
            '-feedAddress',
            `ws://${address}/ws`,
        ],
        { cwd: workingDirectory, windowsHide: true, stdio: 'ignore' },
    );

    const status = async () => {
        try {
            return (await fetch(`http://${address}/admin/symbols`)).status;
        } catch {
            return 0;
        }
    };
    await expect.poll(status, { timeout: 20000 }).toBe(200);
});

test.afterAll(() => exchange?.kill());

function snapshot(seq: number, bids: { Price: number; Quantity: number }[] = []): string {
    return JSON.stringify({ type: 'snapshot', Seq: seq, Halted: false, Bids: bids, Asks: [] });
}

test('live market, full-width agent details, confirmed controls, symbols and responsive layout', async ({ page }) => {
    const errors: string[] = [];
    page.on('pageerror', (error) => errors.push(error.message));

    await page.goto('/');
    await expect(page.getByText('Book synchronized', { exact: true })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Agent 4', exact: true })).toBeVisible();
    await expect(page.locator('.last-price')).not.toHaveText('—', { timeout: 20000 });

    await page.getByRole('button', { name: 'Agent 4', exact: true }).click();
    await expect(page.getByRole('button', { name: 'Close agent details' })).toBeVisible();

    const expanded = await page.locator('#agent-expanded').boundingBox();
    const bottom = await page.locator('.bottom-grid').boundingBox();
    expect(Math.abs(expanded!.width - bottom!.width)).toBeLessThan(2);

    await page.screenshot({ path: 'test-results/dashboard-dark.png', fullPage: true });
    await page.getByRole('button', { name: 'Close agent details' }).click();
    await expect(page.locator('#agent-expanded')).toHaveCount(0);

    await page.getByRole('button', { name: 'Halt ACME' }).click();
    await expect(page.getByText('ACME is halted.', { exact: false })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Resume ACME' })).toBeEnabled();

    await page.getByRole('button', { name: 'Resume ACME' }).click();
    await expect(page.getByRole('button', { name: 'Halt ACME' })).toBeEnabled();

    await page.getByRole('button', { name: 'BETA', exact: true }).click();
    await expect(page.getByRole('button', { name: 'BETA', exact: true })).toHaveAttribute('aria-pressed', 'true');
    await expect(page).toHaveURL(/symbol=BETA/);
    await expect(page.getByRole('button', { name: 'Halt BETA' })).toBeVisible();
    await expect(page.getByText('Book synchronized', { exact: true })).toBeVisible();

    await page.emulateMedia({ colorScheme: 'light' });
    await page.screenshot({ path: 'test-results/dashboard-light.png', fullPage: true });

    await page.setViewportSize({ width: 390, height: 844 });
    await page.screenshot({ path: 'test-results/dashboard-mobile.png', fullPage: true });
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);

    expect(errors).toEqual([]);
});

test('observed stats use received trades and reset when switching symbols', async ({ page }) => {
    await page.routeWebSocket('**/ws?symbol=*', (socket) => {
        socket.send(snapshot(1));
        if (!socket.url().includes('symbol=ACME')) {
            return;
        }

        const trades = [
            { Price: 10000, Quantity: 10 },
            { Price: 10200, Quantity: 25 },
        ];
        for (const { Price, Quantity } of trades) {
            socket.send(JSON.stringify({ type: 'trade', Price, Quantity, Side: 0, MakerOrderID: 1, TakerOrderID: 2 }));
        }
    });

    await page.goto('/');
    await expect(page.locator('.last-price')).toHaveText('$102.00');
    await expect(page.locator('.session-stats')).toContainText('+$2.00');
    await expect(page.locator('.session-stats')).toContainText('(+2.00%)');
    await expect(page.locator('.session-stats')).toContainText('35 shares');
    await expect(page.locator('.session-stats')).toContainText(/\ds ago/);

    await page.getByRole('button', { name: 'BETA', exact: true }).click();
    await expect(page.locator('.last-price')).toHaveText('—');
    await expect(page.locator('.session-stats dd').nth(0)).toHaveText('—');
    await expect(page.locator('.session-stats')).toContainText('0 shares');
    await expect(page.locator('.session-stats dd').nth(2)).toHaveText('—');
});

test('a WebSocket delta gap causes a fresh connection and snapshot', async ({ page }) => {
    let connections = 0;
    await page.routeWebSocket('**/ws?symbol=*', (socket) => {
        connections++;
        const firstConnection = connections === 1;

        socket.send(snapshot(10, [{ Price: firstConnection ? 10000 : 12345, Quantity: 10 }]));

        if (firstConnection) {
            const gappedDelta = JSON.stringify({ type: 'delta', Seq: 12, Side: 0, Price: 99999, Quantity: 100 });
            setTimeout(() => socket.send(gappedDelta), 200);
        }
    });

    await page.goto('/');
    await expect.poll(() => connections).toBeGreaterThanOrEqual(2);
    await expect(page.locator('.book-quotes')).toContainText('$123.45');
    await expect(page.getByText('Book synchronized', { exact: true })).toBeVisible();
    await expect(page.locator('.depth')).not.toContainText('999.99');
});
