import { defineStore } from 'pinia';

export interface AuthState {
  token: string;
  username: string;
  role: 'admin' | 'user' | 'readonly' | '';
}

export const useAuthStore = defineStore('auth', {
  state: (): AuthState => ({
    token: localStorage.getItem('token') || '',
    username: localStorage.getItem('username') || '',
    role: (localStorage.getItem('role') as AuthState['role']) || '',
  }),

  getters: {
    isLoggedIn: (state) => !!state.token,
    isAdmin: (state) => state.role === 'admin',
    isReadonly: (state) => state.role === 'readonly',
  },

  actions: {
    /**
     * 保存认证信息到 state 和 localStorage
     *
     * 校验 role 必须是 admin | user | readonly，否则清除认证并抛出错误。
     */
    setAuth(token: string, username: string, role: string) {
      const validRoles: string[] = ['admin', 'user', 'readonly'];
      if (!role || !validRoles.includes(role)) {
        this.clearAuth();
        throw new Error('登录响应无效');
      }
      this.token = token;
      this.username = username;
      this.role = role as AuthState['role'];
      localStorage.setItem('token', token);
      localStorage.setItem('username', username);
      localStorage.setItem('role', role);
    },

    /**
     * 清除认证信息：移除 localStorage 并重置 state
     */
    clearAuth() {
      this.token = '';
      this.username = '';
      this.role = '';
      localStorage.removeItem('token');
      localStorage.removeItem('username');
      localStorage.removeItem('role');
    },

    /**
     * 退出登录：清除认证信息并跳转到登录页
     * 使用动态 import 打断 store -> router 的静态循环依赖
     */
    async logout() {
      this.clearAuth();
      const { router } = await import('@/router');
      router.push('/login');
    },

    /**
     * 处理 401 未授权：清除认证信息并跳转到登录页
     * 使用动态 import 打断 store -> router 的静态循环依赖
     */
    async handleUnauthorized() {
      this.clearAuth();
      const { router } = await import('@/router');
      router.push('/login');
    },
  },
});
