import 'vue-router';

declare module 'vue-router' {
  interface RouteMeta {
    /** 页面标题 */
    title?: string;
    /** 允许访问的角色列表，空数组表示公开 */
    roles?: Array<'admin' | 'user' | 'readonly'>;
    /** 是否需要认证 */
    requiresAuth?: boolean;
    /** 菜单图标 */
    icon?: string;
    /** 是否在菜单中隐藏 */
    hideInMenu?: boolean;
    /** 排序权重 */
    order?: number;
  }
}

export {};
