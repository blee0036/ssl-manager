import { describe, it, expect, vi, beforeEach } from 'vitest';

/**
 * Validates: Requirements 9.1, 9.4, 9.5
 *
 * 单元测试：`service/api/root-domain.ts` 的 API 封装。
 *
 * 目的：逐个断言每个函数调用的 URL、HTTP 方法、请求体 / query 参数与配置，
 * 与后端契约（`internal/web/handler/root_domain_handler.go` +
 * `internal/model/{root_domain,input}.go`）逐字段一致：
 *   - URL 全部使用连字符风格 `/api/root-domains`（对齐 `/api/thirdpart-dns`）；
 *   - ID 为 string，作为路径参数拼接（`/api/root-domains/{id}` 等）；
 *   - 请求体字段名与 Go json tag 完全一致：
 *       name / api_token / config_id / monitor_enabled / alert_ignored。
 *
 * 契约对照表（design.md「API 契约」）：
 *   GET    /api/root-domains                  ← fetchRootDomains(params)  → request.get(url, { params })
 *   POST   /api/root-domains                  ← createRootDomain({name})  → request.post(url, { name }, cfg)
 *   POST   /api/root-domains/import           ← importRootDomains(body)   → request.post(url, body, cfg)
 *   PUT    /api/root-domains/{id}             ← updateRootDomain(id,data) → request.put(url, data, cfg)
 *   DELETE /api/root-domains/{id}             ← deleteRootDomain(id)      → request.delete(url, cfg)
 *   POST   /api/root-domains/{id}/refresh     ← refreshRootDomain(id)     → request.post(url, null, cfg)
 */

// ------------------------------------------------------------------
// Mock the shared axios instance BEFORE importing the module under test.
//
// The wrapper imports `{ request } from '../request'`, which resolves to
// `src/service/request`. We mock that exact module (via the `@` alias, which
// vitest.config.ts maps to `src`) so no real HTTP/axios is exercised and we
// can assert URL / method / body / params precisely. Both the wrapper's
// relative import and this alias resolve to the same absolute module, so the
// mock intercepts the wrapper's `request` reference.
// ------------------------------------------------------------------
vi.mock('@/service/request', () => ({
  request: {
    get: vi.fn(),
    post: vi.fn(),
    put: vi.fn(),
    delete: vi.fn(),
  },
}));

import { request } from '@/service/request';
import {
  fetchRootDomains,
  createRootDomain,
  importRootDomains,
  updateRootDomain,
  deleteRootDomain,
  refreshRootDomain,
} from '../root-domain';
import type { FetchRootDomainsResult } from '../root-domain';

// Cast the mocked axios-like instance to plain mock fns for ergonomic assertions.
type MockFn = ReturnType<typeof vi.fn>;
const req = request as unknown as {
  get: MockFn;
  post: MockFn;
  put: MockFn;
  delete: MockFn;
};

/**
 * Build a backend-shaped axios response.
 * `res.data` is the `{ code, message, data }` envelope; the wrapper returns
 * `res.data.data`, so the payload must be nested one level under `data`.
 */
function ok<T>(data: T) {
  return { data: { code: 200, message: 'success', data } };
}

/** A representative RootDomain record using backend json-tag field names. */
const sampleRootDomain: Api.RootDomain = {
  id: 'rd-1',
  name: 'example.com',
  source: 'manual',
  registrable_domain: 'example.com',
  expiry_date: '2025-09-14T00:00:00Z',
  expiry_source: 'whois',
  days_remaining: 42,
  last_checked_at: '2025-08-01T03:00:00Z',
  last_status: 'success',
  last_error: '',
  monitor_enabled: true,
  alert_ignored: false,
  created_at: '2025-07-01T10:00:00Z',
  updated_at: '2025-08-01T03:00:00Z',
};

beforeEach(() => {
  vi.clearAllMocks();
});

describe('root-domain API service', () => {
  // ----------------------------------------------------------------
  // GET /api/root-domains
  // ----------------------------------------------------------------
  describe('fetchRootDomains', () => {
    it('calls GET /api/root-domains with { params } and returns unwrapped data', async () => {
      const result: FetchRootDomainsResult = {
        items: [sampleRootDomain],
        total: 1,
        page: 2,
        per_page: 50,
      };
      req.get.mockResolvedValue(ok(result));

      const params = {
        page: 2,
        per_page: 50,
        sort_by: 'expiry_date',
        sort_order: 'desc',
        filter_status: 'expiring',
        name: 'exa',
        source: 'cloudflare',
        monitor_enabled: 'true',
        alert_ignored: 'false',
      };

      const res = await fetchRootDomains(params);

      expect(req.get).toHaveBeenCalledTimes(1);
      // URL hyphenated, params passed under { params } (axios query string).
      expect(req.get).toHaveBeenCalledWith('/api/root-domains', { params });
      // Wrapper unwraps res.data.data.
      expect(res).toEqual(result);
    });

    it('defaults params to an empty object when called with no arguments', async () => {
      req.get.mockResolvedValue(ok({ items: [], total: 0, page: 1, per_page: 50 }));

      await fetchRootDomains();

      expect(req.get).toHaveBeenCalledWith('/api/root-domains', { params: {} });
    });
  });

  // ----------------------------------------------------------------
  // POST /api/root-domains
  // ----------------------------------------------------------------
  describe('createRootDomain', () => {
    it('calls POST /api/root-domains with { name } body and skipErrorNotify', async () => {
      req.post.mockResolvedValue(ok(sampleRootDomain));

      const res = await createRootDomain({ name: 'example.com' });

      expect(req.post).toHaveBeenCalledTimes(1);
      expect(req.post).toHaveBeenCalledWith(
        '/api/root-domains',
        { name: 'example.com' },
        { skipErrorNotify: true },
      );
      expect(res).toEqual(sampleRootDomain);
    });
  });

  // ----------------------------------------------------------------
  // POST /api/root-domains/import
  // ----------------------------------------------------------------
  describe('importRootDomains', () => {
    const importResult: Api.RootDomainImportResult = {
      imported: ['example.com'],
      skipped: ['existing.com'],
      total: 2,
    };

    it('calls POST /api/root-domains/import with { api_token } body and 5min timeout', async () => {
      req.post.mockResolvedValue(ok(importResult));

      const res = await importRootDomains({ api_token: 'cf-token-abc' });

      expect(req.post).toHaveBeenCalledTimes(1);
      expect(req.post).toHaveBeenCalledWith(
        '/api/root-domains/import',
        { api_token: 'cf-token-abc' },
        { timeout: 5 * 60 * 1000, skipErrorNotify: true },
      );
      expect(res).toEqual(importResult);
    });

    it('calls POST /api/root-domains/import with { config_id } body', async () => {
      req.post.mockResolvedValue(ok(importResult));

      await importRootDomains({ config_id: 'cfg-9' });

      expect(req.post).toHaveBeenCalledWith(
        '/api/root-domains/import',
        { config_id: 'cfg-9' },
        { timeout: 5 * 60 * 1000, skipErrorNotify: true },
      );
    });
  });

  // ----------------------------------------------------------------
  // PUT /api/root-domains/{id}
  // ----------------------------------------------------------------
  describe('updateRootDomain', () => {
    it('calls PUT /api/root-domains/{id} with monitor_enabled / alert_ignored body', async () => {
      const updated: Api.RootDomain = { ...sampleRootDomain, monitor_enabled: false, alert_ignored: true };
      req.put.mockResolvedValue(ok(updated));

      const res = await updateRootDomain('rd-1', { monitor_enabled: false, alert_ignored: true });

      expect(req.put).toHaveBeenCalledTimes(1);
      expect(req.put).toHaveBeenCalledWith(
        '/api/root-domains/rd-1',
        { monitor_enabled: false, alert_ignored: true },
        { skipErrorNotify: true },
      );
      expect(res).toEqual(updated);
    });

    it('interpolates the string id into the URL path', async () => {
      req.put.mockResolvedValue(ok(sampleRootDomain));

      await updateRootDomain('abc-123-xyz', { alert_ignored: true });

      expect(req.put).toHaveBeenCalledWith(
        '/api/root-domains/abc-123-xyz',
        { alert_ignored: true },
        { skipErrorNotify: true },
      );
    });

    it('calls PUT /api/root-domains/{id} with a non-empty expiry_date to set a manual override', async () => {
      const updated: Api.RootDomain = {
        ...sampleRootDomain,
        expiry_date: '2026-01-01T00:00:00.000Z',
        expiry_source: 'manual',
        last_status: 'manual',
      };
      req.put.mockResolvedValue(ok(updated));

      const res = await updateRootDomain('rd-1', { expiry_date: '2026-01-01T00:00:00.000Z' });

      expect(req.put).toHaveBeenCalledWith(
        '/api/root-domains/rd-1',
        { expiry_date: '2026-01-01T00:00:00.000Z' },
        { skipErrorNotify: true },
      );
      expect(res).toEqual(updated);
    });

    it('calls PUT /api/root-domains/{id} with an empty expiry_date string to clear a manual override', async () => {
      const cleared: Api.RootDomain = { ...sampleRootDomain, expiry_source: 'whois', last_status: '' };
      req.put.mockResolvedValue(ok(cleared));

      await updateRootDomain('rd-1', { expiry_date: '' });

      // An empty string is distinct from omitting the field entirely: it must be
      // sent verbatim (not stripped), since the backend uses it to distinguish
      // "clear override" from "no change".
      expect(req.put).toHaveBeenCalledWith(
        '/api/root-domains/rd-1',
        { expiry_date: '' },
        { skipErrorNotify: true },
      );
    });
  });

  // ----------------------------------------------------------------
  // DELETE /api/root-domains/{id}
  // ----------------------------------------------------------------
  describe('deleteRootDomain', () => {
    it('calls DELETE /api/root-domains/{id} with skipErrorNotify', async () => {
      req.delete.mockResolvedValue(ok(null));

      await deleteRootDomain('rd-1');

      expect(req.delete).toHaveBeenCalledTimes(1);
      expect(req.delete).toHaveBeenCalledWith('/api/root-domains/rd-1', { skipErrorNotify: true });
    });
  });

  // ----------------------------------------------------------------
  // POST /api/root-domains/{id}/refresh
  // ----------------------------------------------------------------
  describe('refreshRootDomain', () => {
    it('calls POST /api/root-domains/{id}/refresh with null body and 60s timeout', async () => {
      req.post.mockResolvedValue(ok(sampleRootDomain));

      const res = await refreshRootDomain('rd-1');

      expect(req.post).toHaveBeenCalledTimes(1);
      expect(req.post).toHaveBeenCalledWith(
        '/api/root-domains/rd-1/refresh',
        null,
        { timeout: 60 * 1000, skipErrorNotify: true },
      );
      expect(res).toEqual(sampleRootDomain);
    });
  });
});
