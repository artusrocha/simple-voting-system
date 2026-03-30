import { test, expect } from '@playwright/test';

const frontendBaseUrl = process.env.FRONTEND_BASE_URL || 'http://localhost:3000';
const apiBaseUrl = process.env.API_BASE_URL || 'http://localhost:8080';

async function fetchVotingByName(name: string) {
  const response = await fetch(`${apiBaseUrl}/votings`);
  const votings = await response.json();
  return (Array.isArray(votings) ? votings : []).find((v: any) => v.name === name);
}

test.describe('Admin Anti-Abuse Browser', () => {
  test('should create and edit voting with anti-abuse settings via admin UI', async ({ page }) => {
    const uniqueSuffix = Date.now().toString(36);
    const votingName = `Browser Admin ${uniqueSuffix}`;

    await page.goto(`${frontendBaseUrl}/admin.html`, { waitUntil: 'networkidle' });

    await page.getByRole('button', { name: '+ New Voting' }).click();
    await page.waitForSelector('#form-section', { state: 'visible' });

    await page.fill('#voting-name', votingName);
    await page.selectOption('#voting-status', 'OPEN');
    await page.fill('.candidate-row .candidate-id', `c-${uniqueSuffix}`);
    await page.fill('.candidate-row .candidate-name', 'Alice Browser');

    const honeypotCheckbox = page.locator('#antiabuse-honeypot-enabled');
    if (!(await honeypotCheckbox.isChecked())) {
      await honeypotCheckbox.click();
    }

    await page.selectOption('#antiabuse-slide-vote-mode', 'button');

    const telemetryCheckbox = page.locator('#antiabuse-interaction-telemetry-enabled');
    if (!(await telemetryCheckbox.isChecked())) {
      await telemetryCheckbox.click();
    }

    const powCheckbox = page.locator('#antiabuse-pow-enabled');
    if (!(await powCheckbox.isChecked())) {
      await powCheckbox.click();
    }

    await page.waitForFunction(() => !document.querySelector('#antiabuse-pow-ttl-seconds').disabled);
    await page.fill('#antiabuse-pow-ttl-seconds', '45');
    await page.fill('#antiabuse-pow-base-difficulty-bits', '18');
    await page.fill('#antiabuse-pow-max-difficulty-bits', '24');
    await page.fill('#antiabuse-pow-adaptive-window-seconds', '90');
    await page.getByRole('button', { name: 'Save' }).click();

    await page.waitForFunction((name) => {
      const rows = Array.from(document.querySelectorAll('#voting-list tr'));
      return rows.some(row => row.textContent && row.textContent.includes(name));
    }, votingName);

    let voting = await fetchVotingByName(votingName);
    expect(voting).toBeTruthy();
    expect(voting.antiAbuse.slideVoteMode).toBe('button');
    expect(voting.antiAbuse.pow.enabled).toBe(true);
    expect(voting.antiAbuse.pow.ttlSeconds).toBe(45);

    const targetRow = page.locator('#voting-list tr', { hasText: votingName });
    await targetRow.getByRole('button', { name: 'Edit' }).click();
    await page.waitForFunction((name) => document.querySelector('#voting-name')?.value === name, votingName);

    const honeypotCheckboxEdit = page.locator('#antiabuse-honeypot-enabled');
    if (await honeypotCheckboxEdit.isChecked()) {
      await honeypotCheckboxEdit.click();
    }

    await page.selectOption('#antiabuse-slide-vote-mode', 'full');

    const powCheckboxEdit = page.locator('#antiabuse-pow-enabled');
    if (await powCheckboxEdit.isChecked()) {
      await powCheckboxEdit.click();
    }

    await page.waitForFunction(() => document.querySelector('#antiabuse-pow-ttl-seconds').disabled);
    await page.getByRole('button', { name: 'Save' }).click();

    await page.waitForFunction(() => document.querySelector('#form-section')?.style.display === 'none');

    voting = await fetchVotingByName(votingName);
    expect(voting).toBeTruthy();
    expect(voting.antiAbuse.honeypotEnabled).toBe(false);
    expect(voting.antiAbuse.slideVoteMode).toBe('full');
    expect(voting.antiAbuse.pow.enabled).toBe(false);
    expect(voting.antiAbuse.pow.ttlSeconds).toBe(45);
  });
});
