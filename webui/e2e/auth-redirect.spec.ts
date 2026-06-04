import { expect, test } from '@playwright/test';

test.describe('auth redirect behavior', () => {
  test('protected API 401 clears auth and redirects to login', async ({ page }, testInfo) => {
    test.skip(testInfo.project.name !== 'desktop-1366', 'auth redirect regression only needs one viewport');

    await page.route('**/api/auth/turnstile-config', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ code: 0, message: 'ok', data: { enabled: false, site_key: '' } }),
      });
    });

    await page.goto('/login', { waitUntil: 'domcontentloaded' });
    await page.evaluate(() => {
      window.localStorage.setItem('token', 'expired-token');
      window.localStorage.setItem('username', 'admin');
      window.localStorage.setItem('role', 'admin');
    });

    await page.route('**/api/dashboard', async (route) => {
      await route.fulfill({
        status: 401,
        contentType: 'application/json',
        body: JSON.stringify({ code: 401, message: 'invalid or expired token' }),
      });
    });

    await page.goto('/dashboard', { waitUntil: 'domcontentloaded' });

    await expect(page).toHaveURL(/\/login$/);
    await expect
      .poll(() => page.evaluate(() => window.localStorage.getItem('token')))
      .toBeNull();
  });
});
