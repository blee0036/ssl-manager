import { describe, it, expect, vi, beforeEach } from 'vitest';

// Mock the request module
vi.mock('@/service/request', () => ({
  request: {
    get: vi.fn(),
    post: vi.fn(),
  },
}));

import { request } from '@/service/request';
import { createAdmin, saveConfig } from '@/service/api/init';

describe('Init Token API Layer', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('createAdmin should return response with init_token field', async () => {
    const mockResponse = {
      data: {
        code: 201,
        message: 'admin user created',
        data: {
          id: 'user-123',
          username: 'admin',
          role: 'admin',
          init_token: 'abc123hextoken',
        },
      },
    };
    vi.mocked(request.post).mockResolvedValue(mockResponse);

    const res = await createAdmin({ username: 'admin', password: 'pass123' });

    expect(request.post).toHaveBeenCalledWith('/init/admin', { username: 'admin', password: 'pass123' });
    expect(res.data.data!.init_token).toBe('abc123hextoken');
  });

  it('saveConfig should include X-Init-Token header', async () => {
    const mockResponse = { data: { code: 200, message: 'ok' } };
    vi.mocked(request.post).mockResolvedValue(mockResponse);

    const configData = { server: { external_url: 'http://localhost', listen_addr: ':8080' } } as any;
    const token = 'my-secret-init-token';

    await saveConfig(configData, token);

    expect(request.post).toHaveBeenCalledWith(
      '/init/config',
      configData,
      { headers: { 'X-Init-Token': token } },
    );
  });

  it('saveConfig should pass empty string token when no token available', async () => {
    const mockResponse = { data: { code: 200, message: 'ok' } };
    vi.mocked(request.post).mockResolvedValue(mockResponse);

    await saveConfig({} as any, '');

    expect(request.post).toHaveBeenCalledWith(
      '/init/config',
      {},
      { headers: { 'X-Init-Token': '' } },
    );
  });
});

describe('Init Token Loss Detection', () => {
  it('should detect token loss when phase is needs_config but token is empty', () => {
    // Pure logic test: simulates the condition in index.vue
    const phase = 'needs_config';
    const initToken = '';
    const tokenLost = phase === 'needs_config' && !initToken;
    expect(tokenLost).toBe(true);
  });

  it('should not detect token loss when token is present', () => {
    const phase = 'needs_config';
    const initToken = 'valid-token-123';
    const tokenLost = phase === 'needs_config' && !initToken;
    expect(tokenLost).toBe(false);
  });

  it('should not detect token loss in needs_admin phase', () => {
    const phase: string = 'needs_admin';
    const initToken = '';
    const tokenLost = phase === 'needs_config' && !initToken;
    expect(tokenLost).toBe(false);
  });
});
