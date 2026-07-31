/**
 * E2E tests for Root Domain (WHOIS registration expiry) monitoring page.
 *
 * Route: /root-domains  →  views/root-domain/index.vue
 *
 * Covers the end-to-end flows for task 13.1:
 *   1. 列表加载        — list loads from the mocked GET /api/root-domains
 *   2. 手动添加        — manual add via POST /api/root-domains
 *   3. 从 Cloudflare 导入 — import via POST /api/root-domains/import
 *   4. 按行手动刷新     — per-row WHOIS refresh via POST /api/root-domains/{id}/refresh
 *   5. 删除            — delete via DELETE /api/root-domains/{id}
 *
 * The Go backend is NOT running under Playwright (the dev server is a Vite-only
 * SPA host started/stopped by scripts/run-e2e.mjs). All backend calls are
 * intercepted with page.route(...) and fulfilled with the standard
 * { code, message, data } envelope, using the exact field names from
 * Api.RootDomain / the backend handler (root_domain_handler.go).
 *
 * These are behavioural flows (not responsive-layout checks), so — following the
 * established pattern in auth-redirect.spec.ts — they run on a single viewport.
 * On desktop-1366 (1366px) the table (scroll-x 1250) needs no horizontal scroll,
 * so the fixed-right action column is fully visible and clickable.
 *
 * _Requirements: 2.1, 3.1, 7.1, 8.1, 8.4_
 */
import { expect, type Page, test } from '@playwright/test';

const now = '2026-06-17T12:00:00Z';

/** GET /api/system/config payload — the page only reads domain_expiry.expiry_threshold_days. */
const systemConfig = {
  domain_expiry: {
    expiry_threshold_days: 14,
    refresh_interval_minutes: 1440,
    whois_timeout_seconds: 10,
  },
};

/** Existing Cloudflare DNS config, offered in the import dialog's "选择已有 DNS 配置" mode. */
const dnsConfig = {
  id: 'dns1',
  name: 'Cloudflare DNS',
  type: 'cloudflare',
  main_domains: ['example.com'],
  enabled: true,
  config_json: '{}',
  created_at: now,
  updated_at: now,
};

/**
 * Build a full Api.RootDomain record (field names aligned with the Go json tags
 * in internal/model/root_domain.go), allowing per-field overrides.
 */
function makeRootDomain(overrides: Record<string, unknown> = {}) {
  return {
    id: 'rd-1',
    name: 'example.com',
    source: 'manual',
    registrable_domain: 'example.com',
    expiry_date: '2026-12-31T00:00:00Z',
    days_remaining: 100,
    last_checked_at: now,
    last_status: 'success',
    last_error: '',
    monitor_enabled: true,
    alert_ignored: false,
    created_at: now,
    updated_at: now,
    ...overrides,
  };
}

/** Fresh seed list for each test: one "即将到期" (manual) + one "未知" (cloudflare). */
function seedRootDomains() {
  return [
    makeRootDomain({
      id: 'rd-expiring',
      name: 'expiring.example.com',
      registrable_domain: 'expiring.example.com',
      source: 'manual',
      expiry_date: '2026-06-27T00:00:00Z',
      days_remaining: 10, // 0 < days <= 14 → 即将到期
      last_status: 'success',
    }),
    makeRootDomain({
      id: 'rd-unknown',
      name: 'unknown.example.org',
      registrable_domain: 'unknown.example.org',
      source: 'cloudflare',
      expiry_date: null, // 未知
      days_remaining: null,
      last_checked_at: null,
      last_status: '',
    }),
  ];
}

/**
 * Install the mocked backend. Returns the mutable root-domain list so that
 * mutations (create/delete) are reflected by subsequent list reads, enabling
 * genuine end-to-end assertions (e.g. a deleted row disappears).
 */
async function setupMockApi(page: Page) {
  await page.addInitScript(() => {
    window.localStorage.setItem('token', 'e2e-token');
    window.localStorage.setItem('username', 'admin');
    window.localStorage.setItem('role', 'admin');
  });

  const rootDomains: ReturnType<typeof makeRootDomain>[] = seedRootDomains();

  await page.route('**/*', async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const path = url.pathname;
    const method = request.method();

    // Let the Vite dev server serve the SPA (HTML/JS/CSS); only mock the API.
    if (!path.startsWith('/api/')) {
      await route.continue();
      return;
    }

    const json = (data: unknown, status = 200) =>
      route.fulfill({
        status,
        contentType: 'application/json',
        body: JSON.stringify({ code: 0, message: 'ok', data }),
      });

    // --- Auth / global ---
    if (path === '/api/auth/turnstile-config') return json({ enabled: false, site_key: '' });
    if (path === '/api/system/config') return json(systemConfig);

    // --- Third-party DNS list (import dialog loads Cloudflare configs) ---
    if (path === '/api/thirdpart-dns' && method === 'GET') return json([dnsConfig]);

    // --- Import (static path — must be checked before /{id}) ---
    if (path === '/api/root-domains/import' && method === 'POST') {
      return json({
        imported: ['imported-a.example.com', 'imported-b.example.net'],
        skipped: ['expiring.example.com'],
        total: 3,
      });
    }

    // --- List + Create ---
    if (path === '/api/root-domains' && method === 'GET') {
      return json({ items: rootDomains, total: rootDomains.length, page: 1, per_page: 20 });
    }
    if (path === '/api/root-domains' && method === 'POST') {
      const body = JSON.parse((await request.postData()) || '{}');
      const created = makeRootDomain({
        id: `rd-${rootDomains.length + 1}`,
        name: body.name,
        registrable_domain: body.name,
        source: 'manual',
        last_status: 'success',
      });
      rootDomains.push(created);
      return json(created, 201);
    }

    // --- Per-row refresh ---
    const refreshMatch = path.match(/^\/api\/root-domains\/([^/]+)\/refresh$/);
    if (refreshMatch && method === 'POST') {
      const id = refreshMatch[1];
      const rd = rootDomains.find((d) => d.id === id) ?? rootDomains[0];
      return json({
        ...rd,
        last_status: 'success',
        last_error: '',
        last_checked_at: now,
        expiry_date: '2027-01-01T00:00:00Z',
        days_remaining: 200,
      });
    }

    // --- {id}: GET / PUT / DELETE ---
    const idMatch = path.match(/^\/api\/root-domains\/([^/]+)$/);
    if (idMatch) {
      const id = idMatch[1];
      const idx = rootDomains.findIndex((d) => d.id === id);
      if (method === 'PUT') {
        const body = JSON.parse((await request.postData()) || '{}');
        if (idx >= 0) rootDomains[idx] = { ...rootDomains[idx], ...body };
        return json(rootDomains[idx] ?? rootDomains[0]);
      }
      if (method === 'DELETE') {
        if (idx >= 0) rootDomains.splice(idx, 1);
        return json(null);
      }
      if (method === 'GET') {
        return json(idx >= 0 ? rootDomains[idx] : null);
      }
    }

    // Fallback: benign empty payload so no request hangs.
    return json({});
  });

  return rootDomains;
}

/** Navigate to the page and wait for the initial list load to settle. */
async function gotoRootDomains(page: Page) {
  await page.goto('/root-domains', { waitUntil: 'domcontentloaded' });
  await page.waitForLoadState('networkidle');
}

test.describe('Root Domain expiry monitor - end-to-end flows', () => {
  test.beforeEach(async ({ page }, testInfo) => {
    // Behavioural flow, not a responsive check: run once on the widest viewport
    // (no horizontal table scroll there, so the fixed-right actions are clickable).
    test.skip(
      testInfo.project.name !== 'desktop-1366',
      'root-domain functional flow only needs one viewport',
    );
    await setupMockApi(page);
  });

  test('列表加载：展示根域名及到期状态', async ({ page }) => {
    await gotoRootDomains(page);

    // Page header.
    await expect(page.getByText('域名到期监控').first()).toBeVisible();

    // Both seeded root domains render (proves the mocked list was consumed).
    // NDataTable with a fixed-right column renders each cell twice (main table +
    // fixed-column clone), so scope name checks with .first().
    await expect(page.getByText('expiring.example.com').first()).toBeVisible();
    await expect(page.getByText('unknown.example.org').first()).toBeVisible();

    // Expiry-status rendering (RootDomainTable "剩余天数" column):
    //  - the manual row is within threshold → 即将到期
    //  - the cloudflare row has no expiry → 未知 (two 未知 tags: 到期日 + 剩余天数)
    const table = page.locator('.n-data-table');
    await expect(table.getByText('即将到期').first()).toBeVisible();
    await expect(table.getByText('未知').first()).toBeVisible();

    // Source labels are localised inside the table.
    await expect(table.getByText('手动添加').first()).toBeVisible();
    await expect(table.getByText('Cloudflare').first()).toBeVisible();
  });

  test('手动添加：提交 name 并调用 POST /api/root-domains', async ({ page }) => {
    await gotoRootDomains(page);

    // Open the create dialog from the header action.
    await page.getByRole('button', { name: '手动添加' }).click();
    await expect(page.getByText('手动添加根域名')).toBeVisible();

    // Fill the domain name (must satisfy the client-side domain pattern).
    const nameInput = page.locator('.n-form-item').filter({ hasText: '域名' }).locator('input');
    await nameInput.fill('newdomain.com');

    // Submit and capture the outgoing request (dialog "添加" is exact to avoid
    // matching the header "手动添加" button).
    const createReqPromise = page.waitForRequest(
      (req) => new URL(req.url()).pathname === '/api/root-domains' && req.method() === 'POST',
    );
    await page.getByRole('button', { name: '添加', exact: true }).click();
    const createReq = await createReqPromise;

    // Body matches the backend CreateRootDomainInput contract: { name }.
    expect(createReq.postDataJSON()).toEqual({ name: 'newdomain.com' });

    // On success the dialog closes and the list refreshes to include the new row.
    await expect(page.getByText('手动添加根域名')).toBeHidden();
    await expect(page.getByText('newdomain.com').first()).toBeVisible();
  });

  test('从 Cloudflare 导入：提交 api_token 并调用 POST /api/root-domains/import', async ({ page }) => {
    await gotoRootDomains(page);

    // Open the import dialog.
    await page.getByRole('button', { name: '从 Cloudflare 导入' }).click();
    await expect(page.getByText('从 Cloudflare 导入根域名')).toBeVisible();

    // Default mode is "填写 API Token"; fill the token.
    const tokenInput = page
      .locator('.n-form-item')
      .filter({ hasText: 'Cloudflare API Token' })
      .locator('input');
    await tokenInput.fill('cf-test-token');

    // Submit and capture the import request.
    const importReqPromise = page.waitForRequest(
      (req) => new URL(req.url()).pathname === '/api/root-domains/import' && req.method() === 'POST',
    );
    await page.getByRole('button', { name: '开始导入' }).click();
    const importReq = await importReqPromise;

    // Body matches ImportRootDomainsInput: { api_token }.
    expect(importReq.postDataJSON()).toEqual({ api_token: 'cf-test-token' });

    // The dialog switches to the result view with the scan summary.
    await expect(page.getByText('共扫描 3 个 Zone')).toBeVisible();
  });

  test('按行手动刷新：调用 POST /api/root-domains/{id}/refresh', async ({ page }) => {
    await gotoRootDomains(page);

    // The first row is expiring.example.com (rd-expiring); click its 刷新 action.
    const refreshReqPromise = page.waitForRequest(
      (req) =>
        /^\/api\/root-domains\/[^/]+\/refresh$/.test(new URL(req.url()).pathname) &&
        req.method() === 'POST',
    );
    await page.getByRole('button', { name: '刷新' }).first().click();
    const refreshReq = await refreshReqPromise;

    expect(new URL(refreshReq.url()).pathname).toBe('/api/root-domains/rd-expiring/refresh');

    // A success toast confirms the refresh completed.
    await expect(page.getByText(/已刷新/).first()).toBeVisible();
  });

  test('删除：确认后调用 DELETE /api/root-domains/{id} 且行被移除', async ({ page }) => {
    await gotoRootDomains(page);

    await expect(page.getByText('expiring.example.com').first()).toBeVisible();

    // Click the first row's 删除 action → confirmation dialog opens.
    // Target the dialog title by heading role (the content paragraph also
    // contains "删除根域名", which would otherwise be an ambiguous match).
    await page.getByRole('button', { name: '删除' }).first().click();
    await expect(page.getByRole('heading', { name: '删除根域名', exact: true })).toBeVisible();

    // Confirm (the dialog's confirm button is the last "删除" button in the DOM,
    // since the modal is teleported to the end of <body>).
    const deleteReqPromise = page.waitForRequest(
      (req) =>
        /^\/api\/root-domains\/[^/]+$/.test(new URL(req.url()).pathname) &&
        req.method() === 'DELETE',
    );
    await page.getByRole('button', { name: '删除' }).last().click();
    const deleteReq = await deleteReqPromise;

    expect(new URL(deleteReq.url()).pathname).toBe('/api/root-domains/rd-expiring');

    // The deleted row disappears; the other row remains (list re-fetched).
    await expect(page.getByText('expiring.example.com')).toHaveCount(0);
    await expect(page.getByText('unknown.example.org').first()).toBeVisible();
  });
});
