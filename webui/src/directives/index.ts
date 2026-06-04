import type { App } from 'vue';
import { vPermission } from './permission';

/** 注册所有自定义指令 */
export function setupDirectives(app: App) {
  app.directive('permission', vPermission);
}
