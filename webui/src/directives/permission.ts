import type { Directive } from 'vue';
import { useAuthStore } from '@/store';

/**
 * v-permission 指令
 * 用法：v-permission="['admin', 'user']" — 仅指定角色可见
 * 用法：v-permission:action="'write'" — 非 readonly 可见
 */
export const vPermission: Directive = {
  mounted(el: HTMLElement, binding) {
    const authStore = useAuthStore();
    const { value, arg } = binding;

    if (arg === 'action' && value === 'write') {
      if (authStore.isReadonly) {
        el.parentNode?.removeChild(el);
      }
    } else if (Array.isArray(value)) {
      if (!value.includes(authStore.role)) {
        el.parentNode?.removeChild(el);
      }
    }
  },
};
