import { test, expect } from '@playwright/test';

const frontendBaseUrl = process.env.FRONTEND_BASE_URL || 'http://localhost:3000';
const apiBaseUrl = process.env.API_BASE_URL || 'http://localhost:8080';

test.describe('Argon2 Browser PoW', () => {
  test('should complete voting with Argon2 PoW', async ({ page }) => {
    const uniqueSuffix = Date.now().toString(36);
    const votingName = `Argon2 Browser ${uniqueSuffix}`;

    const createResponse = await fetch(`${apiBaseUrl}/votings`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        name: votingName,
        candidates: [{ candidateId: 'c1', name: 'Alice Argon2' }],
        antiAbuse: {
          honeypotEnabled: false,
          slideVoteMode: 'off',
          interactionTelemetryEnabled: false,
          pow: {
            enabled: true,
            algorithm: 'argon2id',
            ttlSeconds: 90,
            baseDifficultyBits: 8,
            maxDifficultyBits: 8,
            adaptiveWindowSeconds: 60,
            memoryKiB: 64,
            timeCost: 1,
            parallelism: 1,
            hashLength: 16,
          },
        },
      }),
    });

    expect(createResponse.ok).toBe(true);
    const voting = await createResponse.json();

    const openResponse = await fetch(`${apiBaseUrl}/votings/${voting.votingId}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ status: 'OPEN' }),
    });
    expect(openResponse.ok).toBe(true);

    const deadline = Date.now() + 30000;
    while (Date.now() < deadline) {
      const listResponse = await fetch(`${apiBaseUrl}/votings?status=OPEN`);
      const votings = await listResponse.json();
      if (Array.isArray(votings) && votings.some((v: any) => v.votingId === voting.votingId)) {
        break;
      }
      await new Promise(resolve => setTimeout(resolve, 500));
    }

    const seen: string[] = [];

    page.on('request', request => {
      const url = request.url();
      if (
        url.includes('config.js') ||
        url.includes('vote.js') ||
        url.includes('pow-worker.js') ||
        url.includes('argon2-bundled.min.js')
      ) {
        seen.push(url);
      }
    });

    await page.goto(`${frontendBaseUrl}/vote.html`, { waitUntil: 'networkidle' });
    await page.locator('.voting-card', { hasText: votingName }).click();
    await page.locator('.candidate-option', { hasText: 'Alice Argon2' }).click();
    await page.locator('#vote-btn').click();

    await page.waitForFunction(() => {
      const text = document.querySelector('#vote-status')?.textContent || '';
      return text.includes('Vote Confirmed!') || text.includes('Vote Failed');
    }, { timeout: 60000 });

    const statusText = await page.locator('#vote-status').textContent();
    expect(statusText).toContain('Vote Confirmed!');

    const resultsDeadline = Date.now() + 15000;
    let lastResults = null;
    while (Date.now() < resultsDeadline) {
      const resultsResponse = await fetch(`${apiBaseUrl}/votings/${voting.votingId}/results`);
      lastResults = await resultsResponse.json();
      if (Number(lastResults.totalVotes || 0) >= 1) {
        break;
      }
      await new Promise(resolve => setTimeout(resolve, 250));
    }
    expect(Number(lastResults.totalVotes || 0)).toBeGreaterThanOrEqual(1);

    const fragments = ['config.js?v=', 'vote.js?v=', 'pow-worker.js?v=', 'argon2-bundled.min.js?v='];
    for (const fragment of fragments) {
      expect(seen.some(url => url.includes(fragment))).toBe(true);
    }

    const configResponse = await page.evaluate(() =>
      fetch('/config.js', { cache: 'no-store' }).then(async response => ({
        cacheControl: response.headers.get('cache-control'),
        body: await response.text(),
      }))
    );
    expect(configResponse.cacheControl).toContain('no-cache');
    expect(configResponse.body).toContain('assetVersion');

    const wasmResponse = await page.evaluate(() =>
      fetch(window.APP_CONFIG.assetVersion ? `/js/vendor/argon2.wasm?v=${window.APP_CONFIG.assetVersion}` : '/js/vendor/argon2.wasm', {
        cache: 'no-store',
      }).then(response => ({
        ok: response.ok,
        cacheControl: response.headers.get('cache-control'),
      }))
    );
    expect(wasmResponse.ok).toBe(true);
    expect(wasmResponse.cacheControl).toContain('immutable');
  });
});
