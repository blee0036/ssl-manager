import { expect, test } from '@playwright/test';

const publicRoutes = ['/login', '/init', '/403', '/404'];

test.describe('public page responsive layout', () => {
  test.beforeEach(async ({ page }) => {
    await page.route('**/api/auth/turnstile-config', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ code: 0, message: 'ok', data: { enabled: false, site_key: '' } }),
      });
    });
    await page.route('**/init/status', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ code: 0, message: 'ok', data: { phase: 'needs_admin' } }),
      });
    });
  });

  for (const route of publicRoutes) {
    test(`${route} has no page-level horizontal overflow`, async ({ page }) => {
      await page.goto(route, { waitUntil: 'domcontentloaded' });
      await page.waitForTimeout(300);

      const overflow = await page.evaluate(() => {
        const viewportWidth = window.innerWidth;
        const documentWidth = Math.max(
          document.documentElement.scrollWidth,
          document.body.scrollWidth,
        );
        const offenders = Array.from(document.querySelectorAll<HTMLElement>('body *'))
          .map((element) => {
            const rect = element.getBoundingClientRect();
            return {
              tag: element.tagName.toLowerCase(),
              className: element.className?.toString() || '',
              left: Math.round(rect.left),
              right: Math.round(rect.right),
              width: Math.round(rect.width),
            };
          })
          .filter((item) => item.width > 0 && (item.left < -1 || item.right > viewportWidth + 1))
          .slice(0, 5);

        return {
          viewportWidth,
          documentWidth,
          offenders,
        };
      });

      expect(overflow, JSON.stringify(overflow.offenders, null, 2)).toMatchObject({
        documentWidth: expect.any(Number),
      });
      expect(overflow.documentWidth).toBeLessThanOrEqual(overflow.viewportWidth + 1);
    });
  }
});
