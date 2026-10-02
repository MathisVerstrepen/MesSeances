import { mkdirSync } from 'node:fs'
import { defineConfig } from '@playwright/test'

// Screenshots and traces may contain session data in future acceptance suites.
process.umask(0o077)
mkdirSync(new URL('../tmp/playwright', import.meta.url), {
  recursive: true,
  mode: 0o700,
})

const port = Number(process.env.PLAYWRIGHT_PORT || 13400)
if (!Number.isInteger(port) || port < 1024 || port > 65535)
  throw new Error('PLAYWRIGHT_PORT must be an integer from 1024 to 65535')

const baseURL = `http://127.0.0.1:${port}`

export default defineConfig({
  testDir: './e2e',
  fullyParallel: false,
  workers: 1,
  forbidOnly: Boolean(process.env.CI),
  retries: 0,
  timeout: 30000,
  reporter: [
    ['list'],
    ['html', { open: 'never', outputFolder: '../tmp/playwright/report' }],
  ],
  outputDir: '../tmp/playwright/results',
  use: {
    baseURL,
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
  webServer: {
    command: 'node tools/playwright-server.mjs',
    url: `${baseURL}/__playwright/health`,
    reuseExistingServer: false,
    timeout: 120000,
    gracefulShutdown: { signal: 'SIGTERM', timeout: 10000 },
    env: { PLAYWRIGHT_PORT: String(port), NUXT_TELEMETRY_DISABLED: '1' },
  },
})
