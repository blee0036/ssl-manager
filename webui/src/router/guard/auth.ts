import type { Router } from 'vue-router';

/**
 * 认证守卫：无 token 时跳转 /login
 *
 * Flow:
 * 1. 公开路由（requiresAuth === false）→ 直接放行
 * 2. 有 token → 放行到下一个守卫（permission）
 * 3. 无 token → 跳转 /login，携带 redirect query
 */
export function createAuthGuard(router: Router) {
  router.beforeEach((to, _from, next) => {
    // 公开路由直接放行（login、init、403、404）
    if (to.meta.requiresAuth === false) {
      next();
      return;
    }

    const token = localStorage.getItem('token');
    if (!token) {
      next({ path: '/login', query: { redirect: to.fullPath } });
      return;
    }

    next();
  });
}
