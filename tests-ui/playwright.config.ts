import { defineConfig, devices } from '@playwright/test';

const frontendBaseUrl = process.env.FRONTEND_BASE_URL || 'http://localhost:3000';
const apiBaseUrl = process.env.API_BASE_URL || 'http://localhost:8080';

export default defineConfig({
  testDir: './',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  workers: process.env.CI ? 1 : undefined,
  reporter: 'list',
  use: {
    baseURL: frontendBaseUrl,
    trace: 'on-first-retry',
  },
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],
  webServer: {
    command: '',
    url: frontendBaseUrl,
    reuseExistingServer: true,
  },
});
