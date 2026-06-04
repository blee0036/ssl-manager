import { describe, it, expect } from 'vitest';
import { parseJWT } from '../jwt';

/**
 * 构造一个测试用 JWT token（不含有效签名，仅用于 payload 解析测试）
 */
function makeToken(payload: Record<string, unknown>): string {
  const header = btoa(JSON.stringify({ alg: 'HS256', typ: 'JWT' }));
  const body = btoa(JSON.stringify(payload));
  const signature = 'test-signature';
  return `${header}.${body}.${signature}`;
}

describe('parseJWT', () => {
  it('should parse a valid JWT with username and role', () => {
    const token = makeToken({
      user_id: '1',
      username: 'admin',
      role: 'admin',
      exp: 9999999999,
      iat: 1700000000,
    });

    const result = parseJWT(token);
    expect(result).not.toBeNull();
    expect(result!.username).toBe('admin');
    expect(result!.role).toBe('admin');
    expect(result!.user_id).toBe('1');
  });

  it('should parse readonly role correctly', () => {
    const token = makeToken({
      user_id: 'readonly-1',
      username: 'readonly_viewer',
      role: 'readonly',
    });

    const result = parseJWT(token);
    expect(result).not.toBeNull();
    expect(result!.username).toBe('readonly_viewer');
    expect(result!.role).toBe('readonly');
  });

  it('should parse user role correctly', () => {
    const token = makeToken({
      user_id: '2',
      username: 'normal_user',
      role: 'user',
    });

    const result = parseJWT(token);
    expect(result).not.toBeNull();
    expect(result!.role).toBe('user');
  });

  it('should return null for empty string', () => {
    expect(parseJWT('')).toBeNull();
  });

  it('should return null for non-JWT string', () => {
    expect(parseJWT('not-a-jwt')).toBeNull();
  });

  it('should return null for token with only 2 parts', () => {
    expect(parseJWT('header.payload')).toBeNull();
  });

  it('should return null for token with invalid base64 payload', () => {
    expect(parseJWT('header.!!!invalid!!!.signature')).toBeNull();
  });

  it('should return null for token with non-JSON payload', () => {
    const notJson = btoa('this is not json');
    expect(parseJWT(`header.${notJson}.signature`)).toBeNull();
  });

  it('should return null when payload is missing username', () => {
    const token = makeToken({
      user_id: '1',
      role: 'admin',
    });
    expect(parseJWT(token)).toBeNull();
  });

  it('should return null when payload is missing role', () => {
    const token = makeToken({
      user_id: '1',
      username: 'admin',
    });
    expect(parseJWT(token)).toBeNull();
  });

  it('should return null when username is not a string', () => {
    const token = makeToken({
      user_id: '1',
      username: 123,
      role: 'admin',
    });
    expect(parseJWT(token)).toBeNull();
  });

  it('should return null when role is not a string', () => {
    const token = makeToken({
      user_id: '1',
      username: 'admin',
      role: true,
    });
    expect(parseJWT(token)).toBeNull();
  });

  it('should handle base64url encoded payload (with - and _)', () => {
    // Manually create a token with base64url characters
    const payload = { user_id: '1', username: 'test+user/name', role: 'admin' };
    const header = btoa(JSON.stringify({ alg: 'HS256', typ: 'JWT' }));
    // Use base64url encoding (replace + with -, / with _)
    const body = btoa(JSON.stringify(payload)).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
    const token = `${header}.${body}.signature`;

    const result = parseJWT(token);
    expect(result).not.toBeNull();
    expect(result!.username).toBe('test+user/name');
    expect(result!.role).toBe('admin');
  });
});
