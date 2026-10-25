import { defineConfig } from '@playwright/test';
export default defineConfig({
    testDir: './e2e',
    workers: 1,
    timeout: 45000,
    use: {
        baseURL: 'http://127.0.0.1:18080',
        channel: 'msedge',
        headless: true,
        viewport: { width: 1365, height: 1000 },
        colorScheme: 'dark',
    },
});
