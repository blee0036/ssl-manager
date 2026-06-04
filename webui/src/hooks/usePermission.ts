import { useAuthStore } from '@/store';

/**
 * 组合式权限检查函数
 * 提供响应式权限判断方法，供组件内使用
 */
export function usePermission() {
  const authStore = useAuthStore();

  /** 检查当前角色是否在允许列表中 */
  function hasPermission(roles: string[]): boolean {
    return roles.includes(authStore.role);
  }

  /** 当前用户是否可执行写操作（非 readonly） */
  function canWrite(): boolean {
    return !authStore.isReadonly;
  }

  /** 当前用户是否为管理员 */
  function isAdmin(): boolean {
    return authStore.isAdmin;
  }

  return { hasPermission, canWrite, isAdmin };
}
