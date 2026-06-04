import { describe, it, expect, beforeEach, vi } from 'vitest';
import { setActivePinia, createPinia } from 'pinia';
import { useAuthStore } from '../index';

// Mock vue-router
vi.mock('@/router', () => ({
  router: {
    push: vi.fn(),
  },
}));

// Mock localStorage
const localStorageMock = (() => {
  let store: Record<string, string> = {};
  return {
    getItem: (key: string) => store[key] ?? null,
    setItem: (key: string, value: string) => { store[key] = value; },
    removeItem: (key: string) => { delete store[key]; },
    clear: () => { store = {}; },
  };
})();

Object.defineProperty(globalThis, 'localStorage', { value: localStorageMock });

describe('AuthStore.setAuth', () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    localStorageMock.clear();
  });

  it('should accept valid admin role', () => {
    const store = useAuthStore();
    store.setAuth('token123', 'admin_user', 'admin');

    expect(store.token).toBe('token123');
    expect(store.username).toBe('admin_user');
    expect(store.role).toBe('admin');
    expect(localStorage.getItem('token')).toBe('token123');
    expect(localStorage.getItem('username')).toBe('admin_user');
    expect(localStorage.getItem('role')).toBe('admin');
  });

  it('should accept valid user role', () => {
    const store = useAuthStore();
    store.setAuth('token456', 'normal_user', 'user');

    expect(store.role).toBe('user');
  });

  it('should accept valid readonly role', () => {
    const store = useAuthStore();
    store.setAuth('token789', 'viewer', 'readonly');

    expect(store.role).toBe('readonly');
  });

  it('should reject empty role and clear auth', () => {
    const store = useAuthStore();
    // Pre-set some auth data
    store.token = 'existing';
    store.username = 'existing';
    store.role = 'admin';

    expect(() => store.setAuth('token', 'user', '')).toThrow('登录响应无效');
    expect(store.token).toBe('');
    expect(store.username).toBe('');
    expect(store.role).toBe('');
    expect(localStorage.getItem('token')).toBeNull();
  });

  it('should reject invalid role "superadmin"', () => {
    const store = useAuthStore();
    expect(() => store.setAuth('token', 'user', 'superadmin')).toThrow('登录响应无效');
    expect(store.token).toBe('');
  });

  it('should reject invalid role "editor"', () => {
    const store = useAuthStore();
    expect(() => store.setAuth('token', 'user', 'editor')).toThrow('登录响应无效');
    expect(store.token).toBe('');
  });

  it('should reject undefined-like role (cast to string)', () => {
    const store = useAuthStore();
    // Simulate what happens when undefined is passed as string
    expect(() => store.setAuth('token', 'user', undefined as unknown as string)).toThrow('登录响应无效');
    expect(store.token).toBe('');
  });
});

describe('AuthStore.clearAuth', () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    localStorageMock.clear();
  });

  it('should clear all auth state and localStorage', () => {
    const store = useAuthStore();
    store.setAuth('token', 'user', 'admin');

    store.clearAuth();

    expect(store.token).toBe('');
    expect(store.username).toBe('');
    expect(store.role).toBe('');
    expect(localStorage.getItem('token')).toBeNull();
    expect(localStorage.getItem('username')).toBeNull();
    expect(localStorage.getItem('role')).toBeNull();
  });
});
