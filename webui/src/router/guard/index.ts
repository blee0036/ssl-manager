import type { Router } from 'vue-router';
import { createAuthGuard } from './auth';
import { createPermissionGuard } from './permission';
import { cancelAllPendingRequests } from '@/service/request/pending';

/** 注册所有路由守卫 */
export function setupRouterGuards(router: Router) {
  // Cancel pending GET requests on navigation to prevent stale responses
  router.beforeEach(() => {
    cancelAllPendingRequests();
  });

  createAuthGuard(router);
  createPermissionGuard(router);
}
