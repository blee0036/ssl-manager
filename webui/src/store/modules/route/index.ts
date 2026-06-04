import { defineStore } from 'pinia';
import { businessRoutes } from '@/router/routes/modules';

export interface MenuItem {
  /** 路由 name 作为唯一标识 */
  key: string;
  /** 菜单显示标题 */
  label: string;
  /** 图标 class（UnoCSS icon） */
  icon?: string;
  /** 导航路径 */
  path: string;
}

interface RouteState {
  /** 当前角色可访问的菜单列表 */
  menus: MenuItem[];
}

export const useRouteStore = defineStore('route', {
  state: (): RouteState => ({
    menus: [],
  }),

  actions: {
    /**
     * 根据角色动态生成菜单
     * 1. 过滤 businessRoutes 中 meta.roles 包含当前角色的路由
     * 2. 排除 hideInMenu: true 的路由
     * 3. 按 meta.order 升序排列
     */
    generateRoutes(role: string) {
      const filtered = businessRoutes
        .filter((route) => {
          const meta = route.meta;
          if (!meta) return false;
          // 排除隐藏菜单项
          if (meta.hideInMenu) return false;
          // 如果未定义 roles，默认所有角色可访问
          if (!meta.roles || meta.roles.length === 0) return true;
          // 检查角色是否在允许列表中
          return meta.roles.includes(role as 'admin' | 'user' | 'readonly');
        })
        .sort((a, b) => {
          const orderA = a.meta?.order ?? 999;
          const orderB = b.meta?.order ?? 999;
          return orderA - orderB;
        })
        .map((route): MenuItem => ({
          key: (route.name as string) || route.path,
          label: route.meta?.title || '',
          icon: route.meta?.icon,
          path: route.path,
        }));

      this.menus = filtered;
    },
  },
});
