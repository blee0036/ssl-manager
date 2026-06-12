import { expect, type Page, test } from '@playwright/test';

const now = '2026-06-04T12:00:00Z';

const certificate = {
  id: 'cert1',
  name: 'example.com',
  domains: ['example.com', '*.example.com'],
  source: 'manual',
  expire_at: '2026-12-31T00:00:00Z',
  auto_renew: true,
  issuer: 'Test CA',
  fingerprint_sha256: 'abcdef1234567890',
  chain_valid: true,
  has_private_key: true,
  machine_count: 1,
  last_renew_at: null,
  renew_status: 'success',
  created_at: now,
  updated_at: now,
};

const machine = {
  id: 'm1',
  name: 'web-01',
  ip: '10.0.0.10',
  hostname: 'web-01',
  os: 'linux',
  arch: 'amd64',
  tags: ['prod', 'edge'],
  remark: 'primary web node',
  status: 'online',
  agent_version: '1.0.3',
  last_heartbeat_at: now,
  created_at: now,
  updated_at: now,
};

const machineCertificate = {
  id: 'mc1',
  machine_id: 'm1',
  certificate_id: 'cert1',
  cert_path: '/etc/ssl/certs/example.com/fullchain.pem',
  private_key_path: '/etc/ssl/private/example.com/privkey.pem',
  post_deploy_commands: 'systemctl reload nginx',
  config_revision: 1,
  last_deploy_status: 'success',
  last_deploy_at: now,
  last_deploy_message: 'ok',
  created_at: now,
  updated_at: now,
};

const domain = {
  id: 'domain1',
  name: 'example.com',
  source: 'manual',
  monitor_port: 443,
  linked_machine_id: 'm1',
  linked_certificate_id: 'cert1',
  linked_machine_certificate_id: 'mc1',
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
    domain_id: 'domain1',
    checked_port: 443,
    resolved_ips: ['10.0.0.10'],
    tls_success: true,
    certificate_fingerprint_sha256: 'abcdef1234567890',
    issuer: 'Test CA',
    expire_at: '2026-12-31T00:00:00Z',
    days_remaining: 210,
    domain_matched: true,
    chain_valid: true,
    error_message: '',
    checked_at: now,
  },
};

const thirdpartDns = {
  id: 'dns1',
  name: 'Cloudflare DNS',
  type: 'cloudflare',
  main_domains: ['example.com'],
  enabled: true,
  config_json: JSON.stringify({ api_token: 'token' }),
  created_at: now,
  updated_at: now,
};

const alertChannel = {
  id: 'alert1',
  name: 'Ops Lark',
  type: 'lark',
  enabled: true,
  config_json: JSON.stringify({ webhook_url: 'https://example.com/webhook' }),
};

const alertHistory = {
  id: 'ah1',
  level: 'warning',
  type: 'cert_expiry',
  title: '证书即将过期',
  content: 'example.com certificate expires soon',
  status: 'sent',
  target_type: 'certificate',
  target_id: 'cert1',
  sent_channels: ['Ops Lark'],
  created_at: now,
  resolved_at: null,
};

const systemConfig = {
  server: {
    external_url: 'http://localhost:8080',
    listen_addr: ':8080',
  },
  agent: {
    heartbeat_timeout_seconds: 120,
    poll_interval_seconds: 30,
  },
  alert: {
    default_before_days: 15,
  },
  certbot: {
    binary_path: '/usr/bin/certbot',
    data_dir: '/app/data/certbot',
    email: 'admin@example.com',
  },
  readonly: {
    enabled: true,
    view_password: '',
  },
  domain_monitor: {
    default_port: 443,
    interval_minutes: 60,
  },
  turnstile: {
    enabled: false,
    site_key: '',
    secret_key: '',
  },
};

const authenticatedRoutes = [
  '/dashboard',
  '/certificates',
  '/machines',
  '/machines/m1/deploy',
  '/domains',
  '/thirdpart-dns',
  '/alerts',
  '/audit-logs',
  '/system',
  '/users',
];

async function mockApi(page: Page) {
  await page.addInitScript(() => {
    window.localStorage.setItem('token', 'playwright-token');
    window.localStorage.setItem('username', 'admin');
    window.localStorage.setItem('role', 'admin');
  });

  await page.route('**/*', async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const path = url.pathname;

    if (!path.startsWith('/api/') && !path.startsWith('/init/')) {
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
    if (path === '/init/status') return json({ phase: 'needs_admin' });
    if (path === '/api/dashboard') {
      return json({
        certificates_total: 1,
        certificates_expiring_15d: 0,
        certificates_expired: 0,
        machines_online: 1,
        machines_offline: 0,
        deploy_failures_24h: 0,
        renew_failures_24h: 0,
        domain_anomalies: 0,
      });
    }
    if (path === '/api/certificates') return json([certificate]);
    if (path === '/api/machines') return json([machine]);
    if (path === '/api/machines/m1/regenerate-token') {
      return json({
        agent_token: 'playwright-agent-token',
        install_command: 'curl -fsSL http://localhost:8080/install.sh | sh',
      });
    }
    if (path === '/api/machines/m1/certificates') return json([machineCertificate]);
    if (path === '/api/machines/m1/certificates/mc1/deployment-logs') {
      return json(['2026-06-04 12:00:00 deploy started', '2026-06-04 12:00:01 deploy completed']);
    }
    if (path === '/api/domains') return json({ items: [domain], total: 1, page: 1, per_page: 50 });
    if (path === '/api/thirdpart-dns') return json([thirdpartDns]);
    if (path === '/api/thirdpart-dns/dns1/sync-logs') {
      return json({ items: [
        {
          id: 'sl1',
          thirdpart_dns_id: 'dns1',
          records_count: 5,
          status: 'success',
          error_message: '',
          new_domains: '["new.example.com"]',
          updated_domains: '["updated.example.com"]',
          removed_domains: '[]',
          synced_at: now,
        },
      ], total: 1, page: 1, per_page: 50 });
    }
    if (path === '/api/alerts/channels') return json([alertChannel]);
    if (path === '/api/alerts') return json({ items: [alertHistory], total: 1, page: 1, per_page: 50 });
    if (path === '/api/audit-logs') {
      return json([
        {
          id: 'audit1',
          actor_type: 'user',
          actor_id: 'admin',
          action: 'create_certificate',
          target_type: 'certificate',
          target_id: 'cert1',
          ip: '127.0.0.1',
          detail: JSON.stringify({ certificate: 'example.com', operation: 'create' }),
          created_at: now,
        },
      ]);
    }
    if (path === '/api/system/config') return json(systemConfig);
    if (path === '/api/users') {
      return json([
        {
          id: 'user1',
          username: 'admin',
          role: 'admin',
          enabled: true,
          created_at: now,
          updated_at: now,
        },
      ]);
    }

    if (request.method() !== 'GET') return json({});
    return json({});
  });
}

async function expectNoPageOverflow(page: Page) {
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
      .slice(0, 8);

    return { viewportWidth, documentWidth, offenders };
  });

  expect(overflow.documentWidth, JSON.stringify(overflow.offenders, null, 2)).toBeLessThanOrEqual(
    overflow.viewportWidth + 1,
  );
}

async function openAndCheckDialog(page: Page, buttonName: string | RegExp) {
  await page.getByRole('button', { name: buttonName }).first().click();
  await expect(page.locator('.n-modal, .n-drawer').last()).toBeVisible();
  await expectNoPageOverflow(page);
  await closeOverlay(page);
}

async function closeOverlay(page: Page) {
  const overlay = page.locator('.n-modal, .n-drawer').last();
  const closeButton = overlay.locator('.n-base-close').last();
  if (await closeButton.count()) {
    await closeButton.click();
  } else {
    await page.keyboard.press('Escape');
  }
  await expect(overlay).toBeHidden({ timeout: 5000 });
}

test.describe('authenticated page responsive layout', () => {
  test.beforeEach(async ({ page }) => {
    await mockApi(page);
  });

  for (const route of authenticatedRoutes) {
    test(`${route} has no page-level horizontal overflow`, async ({ page }) => {
      await page.goto(route, { waitUntil: 'domcontentloaded' });
      await page.waitForLoadState('networkidle');
      await expectNoPageOverflow(page);
    });
  }

  test('main dialogs and drawers fit mobile viewport', async ({ page }, testInfo) => {
    test.skip(testInfo.project.name !== 'mobile-360', 'dialog smoke test only runs on the narrowest viewport');

    await page.goto('/certificates', { waitUntil: 'domcontentloaded' });
    await page.waitForLoadState('networkidle');
    await openAndCheckDialog(page, '上传证书');
    await openAndCheckDialog(page, 'Cloudflare 签发');
    await openAndCheckDialog(page, '手动 DNS 签发');

    await page.goto('/machines', { waitUntil: 'domcontentloaded' });
    await page.waitForLoadState('networkidle');
    await openAndCheckDialog(page, '创建机器');
    await page.getByRole('button', { name: '重生成 Token' }).first().click();
    await page.getByRole('button', { name: '重生成' }).last().click();
    await expect(page.getByText('安装命令')).toBeVisible();
    await expectNoPageOverflow(page);
    await closeOverlay(page);

    await page.goto('/machines/m1/deploy', { waitUntil: 'domcontentloaded' });
    await page.waitForLoadState('networkidle');
    await openAndCheckDialog(page, '新增配置');
    await page.getByRole('button', { name: '日志' }).first().click();
    await expect(page.getByText('部署日志')).toBeVisible();
    await expectNoPageOverflow(page);
    await closeOverlay(page);

    await page.goto('/domains', { waitUntil: 'domcontentloaded' });
    await page.waitForLoadState('networkidle');
    await openAndCheckDialog(page, '新增域名');
    await openAndCheckDialog(page, '批量新增');

    await page.goto('/thirdpart-dns', { waitUntil: 'domcontentloaded' });
    await page.waitForLoadState('networkidle');
    await openAndCheckDialog(page, '新增 DNS 配置');
    await page.getByRole('button', { name: '同步日志' }).first().click();
    await expect(page.getByRole('heading', { name: /同步日志/ })).toBeVisible();
    await expectNoPageOverflow(page);
    await closeOverlay(page);

    await page.goto('/alerts', { waitUntil: 'domcontentloaded' });
    await page.waitForLoadState('networkidle');
    await openAndCheckDialog(page, '创建渠道');
    await page.getByText('告警历史', { exact: true }).click();
    await expectNoPageOverflow(page);

    await page.goto('/users', { waitUntil: 'domcontentloaded' });
    await page.waitForLoadState('networkidle');
    await openAndCheckDialog(page, '创建用户');
  });
});
