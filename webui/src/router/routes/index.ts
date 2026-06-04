import type { RouteRecordRaw } from 'vue-router';
import { builtinRoutes } from './builtin';
import { businessRoutes } from './modules';

/** 所有路由（业务 + 内置） */
export const routes: RouteRecordRaw[] = [
  ...businessRoutes,
  ...builtinRoutes,
];

/** 导出业务路由供 Route Store 使用 */
export { businessRoutes };
