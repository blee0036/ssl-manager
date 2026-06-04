import type { Router } from 'vue-router';

/**
 * 权限守卫：角色不匹配时跳转
 *
 * Flow (runs after auth guard):
 * 1. 无 roles 限制 → 放行
 * 2. 用户角色在 roles 列表中 → 放行
 * 3. 角色不匹配 → 跳转 /403
 *
 * Permission Matrix:
 * - admin: all pages
 * - user: all except /system and /users
 * - readonly: all except /system, /users, /thirdpart-dns
 *   (readonly CAN access /domains, /certificates, /machines, etc. in read-only mode)
 */
export function createPermissionGuard(router: Router) {
  router.beforeEach((to, _from, next) => {
    // 公开路由不需要权限检查
    if (to.meta.requiresAuth === false) {
      next();
      return;
    }

    const roles = to.meta.roles as string[] | undefined;

    // 无角色限制，放行
    if (!roles || roles.length === 0) {
      next();
      return;
    }

    const userRole = localStorage.getItem('role') || '';

    // 未登录（无角色）不应到达这里（auth guard 已拦截），但防御性处理
    if (!userRole) {
      next({ path: '/login' });
      return;
    }

    if (roles.includes(userRole)) {
      next();
    } else {
      // 无权限：跳转 /403
      next({ path: '/403' });
    }
  });
}
