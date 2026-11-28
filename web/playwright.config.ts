import { mkdirSync } from 'node:fs'
import { defineConfig } from '@playwright/test'

// Screenshots and traces may contain session data in future acceptance suites.
process.umask(0o077)
mkdirSync(new URL('../tmp/playwright', import.meta.url), {
  recursive: true,
  mode: 0o700,
})

// Each worker starts its own fixture server (e2e/fixtures.ts) from one shared
// production build (e2e/global-setup.ts), so tests run fully in parallel.
export default defineConfig({
  testDir: './e2e',
  globalSetup: './e2e/global-setup.ts',
  globalTeardown: './e2e/global-teardown.ts',
  fullyParallel: true,
  // Default: half the CPU cores. Override with --workers.
  workers: process.env.CI ? 2 : undefined,
  forbidOnly: Boolean(process.env.CI),
  retries: 0,
  timeout: 30000,
  reporter: [
    ['list'],
    ['html', { open: 'never', outputFolder: '../tmp/playwright/report' }],
  ],
  outputDir: '../tmp/playwright/results',
  use: {
    serviceWorkers: 'block',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    launchOptions: {
      args: ['--proxy-server=http://127.0.0.1:9'],
    },
  },
  projects: [
    {
      name: 'desktop',
      use: { browserName: 'chromium', viewport: { width: 1440, height: 900 } },
    },
    {
      name: 'mobile',
      use: {
        browserName: 'chromium',
        viewport: { width: 390, height: 844 },
        isMobile: true,
        hasTouch: true,
      },
    },
  ],
})
