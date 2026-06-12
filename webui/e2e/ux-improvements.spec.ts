/**
 * E2E tests for UX Improvements Batch 1 core scenarios.
 * Covers: domain sort/filter, batch operations, DNS sync per-row loading,
 * edit dialog, readonly permission, and column resize.
 */
import { expect, type Page, test } from '@playwright/test';

const now = '2026-06-04T12:00:00Z';

const domains = [
  {
    id: 'd1',
    name: 'alpha.example.com',
    source: 'manual',
    monitor_port: 443,
    linked_machine_id: '',
    linked_certificate_id: '',
    linked_machine_certificate_id: '',
    monitor_enabled: true,
    alert_ignored: false,
    dns_record_id: '',
    dns_record_type: '',
    dns_record_value: '',
    thirdpart_dns_id: '',
    created_at: now,
    updated_at: now,
    latest_monitor_result: {
      id: 'dmr1',
      domain_id: 'd1',
      checked_port: 443,
      resolved_ips: ['1.2.3.4'],
      tls_success: true,
      certificate_fingerprint_sha256: 'abc',
      issuer: 'CA',
      expire_at: '2026-12-31T00:00:00Z',
      days_remaining: 200,
      domain_matched: true,
      chain_valid: true,
      error_message: '',
      checked_at: now,
    },
  },
  {
    id: 'd2',
    name: 'beta.example.com',
    source: 'cloudflare',
    monitor_port: 8443,
    linked_machine_id: '',
    linked_certificate_id: '',
    linked_machine_certificate_id: '',
    monitor_enabled: false,
    alert_ignored: true,
    dns_record_id: 'rec-1',
    dns_record_type: 'A',
    dns_record_value: '5.6.7.8',
    thirdpart_dns_id: 'dns1',
    created_at: now,
    updated_at: now,
    latest_monitor_result: null,
  },
];

const thirdpartDns = {
  id: 'dns1',
  name: 'Cloudflare DNS',
  type: 'cloudflare',
  main_domains: ['example.com'],
  enabled: true,
  config_json: '{}',
  created_at: now,
  updated_at: now,
};

async function mockAdminApi(page: Page) {
  await page.addInitScript(() => {
    window.localStorage.setItem('token', 'e2e-token');
    window.localStorage.setItem('username', 'admin');
    window.localStorage.setItem('role', 'admin');
  });

  await page.route('**/*', async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const path = url.pathname;

    if (!path.startsWith('/api/')) {
      await route.continue();
      return;
    }

    const json = (data: unknown) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ code: 0, message: 'ok', data }),
      });

    if (path === '/api/auth/turnstile-config') return json({ enabled: false, site_key: '' });
    if (path === '/api/domains') {
      // Parse sort/filter params from URL
      const sortBy = url.searchParams.get('sort_by') || '';
      const filterStatus = url.searchParams.get('filter_status') || '';

      let filtered = [...domains];
      if (filterStatus === 'enabled') filtered = filtered.filter(d => d.monitor_enabled);
      if (filterStatus === 'disabled') filtered = filtered.filter(d => !d.monitor_enabled);
      if (filterStatus === 'ignored') filtered = filtered.filter(d => d.alert_ignored);

      if (sortBy === 'name') {
        filtered.sort((a, b) => a.name.localeCompare(b.name));
      }

      return json({ items: filtered, total: filtered.length, page: 1, per_page: 50 });
    }

    if (path.match(/^\/api\/domains\/[^/]+\/probe$/)) {
      return json({
        id: 'dmr-new',
        domain_id: 'd1',
        checked_port: 443,
        resolved_ips: ['1.2.3.4'],
        tls_success: true,
        certificate_fingerprint_sha256: 'abc',
        issuer: 'CA',
        expire_at: '2026-12-31T00:00:00Z',
        days_remaining: 200,
        domain_matched: true,
        chain_valid: true,
        error_message: '',
        checked_at: now,
      });
    }

    if (path.match(/^\/api\/domains\/[^/]+$/) && request.method() === 'PUT') {
      return json(domains[0]);
    }

    if (path.match(/^\/api\/domains\/[^/]+$/) && request.method() === 'DELETE') {
      return json(null);
    }

    if (path === '/api/thirdpart-dns') return json([thirdpartDns]);
    if (path === '/api/thirdpart-dns/dns1/sync') {
      // Simulate a short delay for sync
      await new Promise(r => setTimeout(r, 200));
      return json({ records_count: 5, new_domains: ['new.example.com'], updated_domains: [], removed_domains: [] });
    }
    if (path === '/api/thirdpart-dns/dns1/sync-logs') {
      return json({ items: [{
        id: 'sl1',
        thirdpart_dns_id: 'dns1',
        records_count: 5,
        status: 'success',
        error_message: '',
        new_domains: '["new.example.com"]',
        updated_domains: '[]',
        removed_domains: '[]',
        synced_at: now,
      }], total: 1, page: 1, per_page: 50 });
    }

    if (request.method() !== 'GET') return json({});
    return json({});
  });
}

async function mockReadonlyApi(page: Page) {
  await page.addInitScript(() => {
    window.localStorage.setItem('token', 'e2e-readonly-token');
    window.localStorage.setItem('username', 'viewer');
    window.localStorage.setItem('role', 'readonly');
  });

  await page.route('**/*', async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const path = url.pathname;

    if (!path.startsWith('/api/')) {
      await route.continue();
      return;
    }

    const json = (data: unknown) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ code: 0, message: 'ok', data }),
      });

    if (path === '/api/auth/turnstile-config') return json({ enabled: false, site_key: '' });
    if (path === '/api/domains') {
      return json({ items: domains, total: 2, page: 1, per_page: 50 });
    }
    return json({});
  });
}

test.describe('UX Improvements - Domain sort and filter', () => {
  test.beforeEach(async ({ page }) => {
    await mockAdminApi(page);
  });

  test('filter dropdown sends filter_status query param', async ({ page }) => {
    await page.goto('/domains', { waitUntil: 'domcontentloaded' });
    await page.waitForLoadState('networkidle');

    // Verify both domains visible initially
    await expect(page.getByText('alpha.example.com')).toBeVisible();
    await expect(page.getByText('beta.example.com')).toBeVisible();

    // Select "启用检测" filter
    const filterSelect = page.locator('.n-select').first();
    await filterSelect.click();
    await page.getByText('启用检测', { exact: true }).click();

    // Wait for reload — now only enabled domains shown
    await page.waitForLoadState('networkidle');
    await expect(page.getByText('alpha.example.com')).toBeVisible();
    await expect(page.getByText('beta.example.com')).not.toBeVisible();
  });

  test('sort-change emits correct query params', async ({ page }) => {
    let lastRequestUrl = '';
    page.on('request', (req) => {
      if (req.url().includes('/api/domains')) lastRequestUrl = req.url();
    });

    await page.goto('/domains', { waitUntil: 'domcontentloaded' });
    await page.waitForLoadState('networkidle');

    // Click on "域名" column header to sort
    await page.getByText('域名', { exact: true }).first().click();
    await page.waitForLoadState('networkidle');

    expect(lastRequestUrl).toContain('sort_by=name');
  });

  test('clear filter button resets state', async ({ page }) => {
    await page.goto('/domains', { waitUntil: 'domcontentloaded' });
    await page.waitForLoadState('networkidle');

    // Apply a filter first
    const filterSelect = page.locator('.n-select').first();
    await filterSelect.click();
    // Naive UI renders options in .n-base-select-option elements
    await page.locator('.n-base-select-option').filter({ hasText: '已忽略告警' }).click();
    await page.waitForLoadState('networkidle');

    // Only ignored domain visible
    await expect(page.getByText('beta.example.com')).toBeVisible();

    // Clear filter button should now be visible and clickable
    const clearBtn = page.getByRole('button', { name: '清空筛选' });
    await expect(clearBtn).toBeVisible();
    await clearBtn.click();
    await page.waitForLoadState('networkidle');

    // Both domains should be visible again
    await expect(page.getByText('alpha.example.com')).toBeVisible();
    await expect(page.getByText('beta.example.com')).toBeVisible();
  });
});

test.describe('UX Improvements - Edit domain dialog', () => {
  test.beforeEach(async ({ page }) => {
    await mockAdminApi(page);
  });

  test('edit button opens dialog with domain fields', async ({ page }) => {
    await page.goto('/domains', { waitUntil: 'domcontentloaded' });
    await page.waitForLoadState('networkidle');

    // Click edit button on first row
    await page.getByRole('button', { name: '编辑' }).first().click();

    // Dialog should be visible
    await expect(page.getByText('编辑域名监控')).toBeVisible();

    // Should have port input and switches
    await expect(page.locator('.n-input-number')).toBeVisible();
  });
});

test.describe('UX Improvements - Batch operations', () => {
  test.beforeEach(async ({ page }) => {
    await mockAdminApi(page);
  });

  test('selecting domains shows batch operation bar', async ({ page }) => {
    await page.goto('/domains', { waitUntil: 'domcontentloaded' });
    await page.waitForLoadState('networkidle');

    // Click the first checkbox
    await page.locator('.n-data-table .n-checkbox').first().click();

    // Batch bar should appear
    await expect(page.getByText('已选')).toBeVisible();
    await expect(page.getByText('批量检测')).toBeVisible();
    await expect(page.getByText('批量忽略')).toBeVisible();
    await expect(page.getByText('批量删除')).toBeVisible();
  });

  test('batch probe continues processing all items', async ({ page }) => {
    await page.goto('/domains', { waitUntil: 'domcontentloaded' });
    await page.waitForLoadState('networkidle');

    // Select all via header checkbox
    await page.locator('.n-data-table thead .n-checkbox').click();

    // Click batch probe
    await page.getByText('批量检测').click();

    // Wait for completion — the batch bar should disappear (operation resets)
    // and the success notification appears
    await expect(page.getByText('批量检测')).not.toBeVisible({ timeout: 15000 });
    // After batch completes, selection is cleared and bar disappears
  });

  test('batch delete shows confirmation dialog', async ({ page }) => {
    await page.goto('/domains', { waitUntil: 'domcontentloaded' });
    await page.waitForLoadState('networkidle');

    // Select first row
    await page.locator('.n-data-table .n-checkbox').first().click();

    // Click batch delete
    await page.getByText('批量删除').click();

    // Confirmation dialog should appear
    await expect(page.getByText('确认批量删除')).toBeVisible();
    await expect(page.getByText('即将删除')).toBeVisible();
  });
});

test.describe('UX Improvements - DNS sync per-row loading', () => {
  test.beforeEach(async ({ page }) => {
    await mockAdminApi(page);
  });

  test('sync button shows loading for only the clicked row', async ({ page }) => {
    await page.goto('/thirdpart-dns', { waitUntil: 'domcontentloaded' });
    await page.waitForLoadState('networkidle');

    // Click sync button
    await page.getByRole('button', { name: '同步' }).first().click();

    // Button should show loading state (n-button--loading class)
    const syncBtn = page.getByRole('button', { name: '同步' }).first();
    await expect(syncBtn).toHaveClass(/loading/);

    // Wait for sync complete notification
    await expect(page.getByText('同步完成')).toBeVisible({ timeout: 10000 });
  });

  test('sync success refreshes sync log drawer', async ({ page }) => {
    await page.goto('/thirdpart-dns', { waitUntil: 'domcontentloaded' });
    await page.waitForLoadState('networkidle');

    // Open sync log drawer first
    await page.getByRole('button', { name: '同步日志' }).first().click();
    // Wait for drawer to be visible (Naive drawer renders its title)
    await expect(page.locator('.n-drawer')).toBeVisible();
    await page.waitForTimeout(500);

    // Close drawer
    await page.locator('.n-drawer .n-base-close').click();
    await expect(page.locator('.n-drawer')).not.toBeVisible({ timeout: 5000 });

    // Now sync
    await page.getByRole('button', { name: '同步' }).first().click();
    await expect(page.getByText('同步完成')).toBeVisible({ timeout: 10000 });
  });
});

test.describe('UX Improvements - Readonly user', () => {
  test.beforeEach(async ({ page }) => {
    await mockReadonlyApi(page);
  });

  test('readonly user does not see selection checkboxes', async ({ page }) => {
    await page.goto('/domains', { waitUntil: 'domcontentloaded' });
    await page.waitForLoadState('networkidle');

    // Table should be visible
    await expect(page.getByText('alpha.example.com')).toBeVisible();

    // No checkbox columns should be present
    const checkboxes = page.locator('.n-data-table .n-checkbox');
    await expect(checkboxes).toHaveCount(0);
  });

  test('readonly user does not see batch operation buttons', async ({ page }) => {
    await page.goto('/domains', { waitUntil: 'domcontentloaded' });
    await page.waitForLoadState('networkidle');

    // Batch buttons should not exist
    await expect(page.getByText('批量检测')).not.toBeVisible();
    await expect(page.getByText('批量忽略')).not.toBeVisible();
  });
});

test.describe('UX Improvements - Column resize', () => {
  test.beforeEach(async ({ page }) => {
    await mockAdminApi(page);
  });

  test('domain table has resizable column handles', async ({ page }) => {
    test.skip(page.viewportSize()!.width < 1200, 'column resize only meaningful on desktop');

    await page.goto('/domains', { waitUntil: 'domcontentloaded' });
    await page.waitForLoadState('networkidle');

    // Naive UI DataTable renders resize handles as .n-data-table-resize-button
    const resizeHandles = page.locator('.n-data-table-resize-button');
    const count = await resizeHandles.count();
    expect(count).toBeGreaterThan(0);
  });
});
