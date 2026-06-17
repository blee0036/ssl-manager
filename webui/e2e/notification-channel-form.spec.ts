/**
 * E2E tests for Notification Channel Form (structured form fields).
 *
 * Covers:
 * 1. 创建 Lark 渠道 — 填写 webhook_url 并提交
 * 2. 创建 Telegram 渠道 — 填写 bot_token + chat_id 并提交
 * 3. 创建时 webhook_url 校验失败阻断提交
 * 4. 切换类型清空配置字段
 * 5. 编辑模式 — 类型选择器禁用，配置留空提交不发 config_json
 * 6. 编辑模式 — 填写新配置提交带 config_json
 */
import { expect, type Page, test } from '@playwright/test';

const now = '2026-06-17T12:00:00Z';

const channels = [
  {
    id: 'ch1',
    name: 'Ops Lark',
    type: 'lark',
    enabled: true,
    config_json: '{"webhook_url":"https://******.com/hook"}',
    created_at: now,
    updated_at: now,
  },
  {
    id: 'ch2',
    name: 'Dev Telegram',
    type: 'telegram',
    enabled: true,
    config_json: '{"bot_token":"****","chat_id":"****"}',
    created_at: now,
    updated_at: now,
  },
];

/** Track intercepted requests for assertion */
interface CapturedRequest {
  method: string;
  path: string;
  body: any;
}

async function setupMockApi(page: Page, captured: CapturedRequest[]) {
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

    const json = (data: unknown, status = 200) =>
      route.fulfill({
        status,
        contentType: 'application/json',
        body: JSON.stringify({ code: 0, message: 'ok', data }),
      });

    // Auth
    if (path === '/api/auth/turnstile-config') return json({ enabled: false, site_key: '' });

    // List channels
    if (path === '/api/alerts/channels' && request.method() === 'GET') {
      return json(channels);
    }

    // Create channel
    if (path === '/api/alerts/channels' && request.method() === 'POST') {
      const body = JSON.parse(await request.postData() || '{}');
      captured.push({ method: 'POST', path, body });
      const newChannel = {
        id: 'ch-new',
        name: body.name,
        type: body.type,
        enabled: body.enabled,
        config_json: body.config_json,
        created_at: now,
        updated_at: now,
      };
      return json(newChannel);
    }

    // Update channel
    if (path.match(/^\/api\/alerts\/channels\/[^/]+$/) && request.method() === 'PUT') {
      const body = JSON.parse(await request.postData() || '{}');
      captured.push({ method: 'PUT', path, body });
      return json({ ...channels[0], ...body });
    }

    // Alerts history (needed for the tab)
    if (path === '/api/alerts' && request.method() === 'GET') {
      return json({ items: [], total: 0, page: 1, per_page: 50 });
    }

    // Fallback
    return json({});
  });
}

test.describe('Notification Channel Form - Create Lark', () => {
  let captured: CapturedRequest[];

  test.beforeEach(async ({ page }) => {
    captured = [];
    await setupMockApi(page, captured);
  });

  test('填写合法 Webhook URL 创建 Lark 渠道成功', async ({ page }) => {
    await page.goto('/alerts', { waitUntil: 'domcontentloaded' });
    await page.waitForLoadState('networkidle');

    // Click create button
    await page.getByRole('button', { name: '创建渠道' }).click();

    // Dialog should appear
    await expect(page.getByText('创建通知渠道')).toBeVisible();

    // Fill name
    await page.locator('input').filter({ hasText: '' }).first().click();
    const nameInput = page.locator('.n-form-item').filter({ hasText: '名称' }).locator('input');
    await nameInput.fill('New Lark Channel');

    // Type should default to Lark, verify Webhook URL field is visible
    await expect(page.getByText('Webhook URL')).toBeVisible();

    // Fill webhook URL
    const webhookInput = page.locator('.n-form-item').filter({ hasText: 'Webhook URL' }).locator('input');
    await webhookInput.fill('https://open.feishu.cn/open-apis/bot/v2/hook/test123');

    // Submit
    await page.getByRole('button', { name: '创建', exact: true }).click();

    // Wait for API call
    await page.waitForTimeout(500);

    // Verify the POST was sent with correct payload
    expect(captured.length).toBeGreaterThan(0);
    const createReq = captured.find(r => r.method === 'POST');
    expect(createReq).toBeDefined();
    expect(createReq!.body.name).toBe('New Lark Channel');
    expect(createReq!.body.type).toBe('lark');
    expect(createReq!.body.enabled).toBe(true);
    const configParsed = JSON.parse(createReq!.body.config_json);
    expect(configParsed).toEqual({ webhook_url: 'https://open.feishu.cn/open-apis/bot/v2/hook/test123' });
  });

  test('非 https:// Webhook URL 阻断提交', async ({ page }) => {
    await page.goto('/alerts', { waitUntil: 'domcontentloaded' });
    await page.waitForLoadState('networkidle');

    await page.getByRole('button', { name: '创建渠道' }).click();
    await expect(page.getByText('创建通知渠道')).toBeVisible();

    // Fill name
    const nameInput = page.locator('.n-form-item').filter({ hasText: '名称' }).locator('input');
    await nameInput.fill('Bad URL Channel');

    // Fill invalid webhook URL
    const webhookInput = page.locator('.n-form-item').filter({ hasText: 'Webhook URL' }).locator('input');
    await webhookInput.fill('http://insecure.example.com/hook');
    await webhookInput.blur();

    // Submit attempt
    await page.getByRole('button', { name: '创建', exact: true }).click();
    await page.waitForTimeout(500);

    // Should show validation error and NOT send request
    await expect(page.getByText('https://')).toBeVisible();
    const createReq = captured.find(r => r.method === 'POST');
    expect(createReq).toBeUndefined();
  });
});

test.describe('Notification Channel Form - Create Telegram', () => {
  let captured: CapturedRequest[];

  test.beforeEach(async ({ page }) => {
    captured = [];
    await setupMockApi(page, captured);
  });

  test('填写 Bot Token + Chat ID 创建 Telegram 渠道成功', async ({ page }) => {
    await page.goto('/alerts', { waitUntil: 'domcontentloaded' });
    await page.waitForLoadState('networkidle');

    await page.getByRole('button', { name: '创建渠道' }).click();
    await expect(page.getByText('创建通知渠道')).toBeVisible();

    // Fill name
    const nameInput = page.locator('.n-form-item').filter({ hasText: '名称' }).locator('input');
    await nameInput.fill('TG Channel');

    // Switch type to Telegram
    await page.locator('.n-form-item').filter({ hasText: '类型' }).locator('.n-select').click();
    await page.locator('.n-base-select-option').filter({ hasText: 'Telegram' }).click();

    // Telegram fields should appear
    await expect(page.getByText('Bot Token', { exact: true })).toBeVisible();
    await expect(page.getByText('Chat ID', { exact: true })).toBeVisible();

    // Fill bot_token and chat_id
    const botTokenInput = page.locator('.n-form-item').filter({ hasText: 'Bot Token' }).locator('input');
    await botTokenInput.fill('123456:ABC-DEF-token');

    const chatIdInput = page.locator('.n-form-item').filter({ hasText: 'Chat ID' }).locator('input');
    await chatIdInput.fill('-1001234567890');

    // Submit
    await page.getByRole('button', { name: '创建', exact: true }).click();
    await page.waitForTimeout(500);

    // Verify POST
    const createReq = captured.find(r => r.method === 'POST');
    expect(createReq).toBeDefined();
    expect(createReq!.body.type).toBe('telegram');
    const configParsed = JSON.parse(createReq!.body.config_json);
    expect(configParsed).toEqual({ bot_token: '123456:ABC-DEF-token', chat_id: '-1001234567890' });
  });
});

test.describe('Notification Channel Form - Type Switch', () => {
  let captured: CapturedRequest[];

  test.beforeEach(async ({ page }) => {
    captured = [];
    await setupMockApi(page, captured);
  });

  test('切换类型从 Lark 到 Telegram 清空已填写的字段', async ({ page }) => {
    await page.goto('/alerts', { waitUntil: 'domcontentloaded' });
    await page.waitForLoadState('networkidle');

    await page.getByRole('button', { name: '创建渠道' }).click();
    await expect(page.getByText('创建通知渠道')).toBeVisible();

    // Fill Lark webhook
    const webhookInput = page.locator('.n-form-item').filter({ hasText: 'Webhook URL' }).locator('input');
    await webhookInput.fill('https://will-be-cleared.com');

    // Switch to Telegram
    await page.locator('.n-form-item').filter({ hasText: '类型' }).locator('.n-select').click();
    await page.locator('.n-base-select-option').filter({ hasText: 'Telegram' }).click();

    // Webhook URL field should be gone, Telegram fields should be empty
    await expect(page.locator('.n-form-item').filter({ hasText: 'Webhook URL' })).not.toBeVisible();
    const botTokenInput = page.locator('.n-form-item').filter({ hasText: 'Bot Token' }).locator('input');
    await expect(botTokenInput).toHaveValue('');
    const chatIdInput = page.locator('.n-form-item').filter({ hasText: 'Chat ID' }).locator('input');
    await expect(chatIdInput).toHaveValue('');

    // Switch back to Lark
    await page.locator('.n-form-item').filter({ hasText: '类型' }).locator('.n-select').click();
    await page.locator('.n-base-select-option').filter({ hasText: 'Lark' }).click();

    // Webhook should be empty (not restored)
    const webhookInputNew = page.locator('.n-form-item').filter({ hasText: 'Webhook URL' }).locator('input');
    await expect(webhookInputNew).toHaveValue('');
  });
});

test.describe('Notification Channel Form - Edit Mode', () => {
  let captured: CapturedRequest[];

  test.beforeEach(async ({ page }) => {
    captured = [];
    await setupMockApi(page, captured);
  });

  test('编辑模式类型选择器禁用', async ({ page }) => {
    await page.goto('/alerts', { waitUntil: 'domcontentloaded' });
    await page.waitForLoadState('networkidle');

    // Click edit on first channel
    await page.getByRole('button', { name: '编辑' }).first().click();
    await expect(page.getByText('编辑通知渠道')).toBeVisible();

    // Type selector should be disabled
    const typeSelect = page.locator('.n-form-item').filter({ hasText: '类型' }).locator('.n-base-selection');
    await expect(typeSelect).toHaveClass(/disabled/);
  });

  test('编辑模式配置留空提交不发 config_json', async ({ page }) => {
    await page.goto('/alerts', { waitUntil: 'domcontentloaded' });
    await page.waitForLoadState('networkidle');

    // Click edit on first channel (Lark)
    await page.getByRole('button', { name: '编辑' }).first().click();
    await expect(page.getByText('编辑通知渠道')).toBeVisible();

    // Verify hint text
    await expect(page.getByText('留空则保持原值')).toBeVisible();

    // Webhook field should be empty
    const webhookInput = page.locator('.n-form-item').filter({ hasText: 'Webhook URL' }).locator('input');
    await expect(webhookInput).toHaveValue('');

    // Submit without filling anything
    await page.getByRole('button', { name: '保存' }).click();
    await page.waitForTimeout(500);

    // Verify PUT was sent WITHOUT config_json
    const updateReq = captured.find(r => r.method === 'PUT');
    expect(updateReq).toBeDefined();
    expect(updateReq!.body.config_json).toBeUndefined();
    expect(updateReq!.body.name).toBe('Ops Lark');
    expect(updateReq!.body.enabled).toBe(true);
  });

  test('编辑模式填写新配置提交带 config_json', async ({ page }) => {
    await page.goto('/alerts', { waitUntil: 'domcontentloaded' });
    await page.waitForLoadState('networkidle');

    // Click edit on first channel (Lark)
    await page.getByRole('button', { name: '编辑' }).first().click();
    await expect(page.getByText('编辑通知渠道')).toBeVisible();

    // Fill new webhook URL
    const webhookInput = page.locator('.n-form-item').filter({ hasText: 'Webhook URL' }).locator('input');
    await webhookInput.fill('https://new-webhook.example.com/hook');

    // Submit
    await page.getByRole('button', { name: '保存' }).click();
    await page.waitForTimeout(500);

    // Verify PUT was sent WITH config_json
    const updateReq = captured.find(r => r.method === 'PUT');
    expect(updateReq).toBeDefined();
    expect(updateReq!.body.config_json).toBeDefined();
    const configParsed = JSON.parse(updateReq!.body.config_json);
    expect(configParsed).toEqual({ webhook_url: 'https://new-webhook.example.com/hook' });
  });
});
