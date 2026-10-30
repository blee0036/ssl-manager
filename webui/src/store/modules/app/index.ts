import { defineStore } from 'pinia';
import {
  type ThemeMode,
  getSystemDarkMediaQuery,
  readStoredThemeMode,
  resolveIsDark,
  writeStoredThemeMode,
} from '@/utils/theme';

interface AppState {
  /** 侧边栏是否折叠 */
  siderCollapsed: boolean;
  /** 是否为移动端 */
  isMobile: boolean;
  /** 主题模式：跟随系统 / 浅色 / 深色 */
  themeMode: ThemeMode;
  /** 系统当前是否偏好深色 */
  systemDark: boolean;
}

export const useAppStore = defineStore('app', {
  state: (): AppState => ({
    siderCollapsed: false,
    isMobile: false,
    themeMode: readStoredThemeMode(),
    systemDark: getSystemDarkMediaQuery()?.matches ?? false,
  }),

  getters: {
    /** 实际是否使用深色主题 */
    isDark: (state) => resolveIsDark(state.themeMode, state.systemDark),
  },

  actions: {
    toggleSider() {
      this.siderCollapsed = !this.siderCollapsed;
    },

    setSiderCollapsed(collapsed: boolean) {
      this.siderCollapsed = collapsed;
    },

    setIsMobile(isMobile: boolean) {
      this.isMobile = isMobile;
      if (isMobile) {
        this.siderCollapsed = true;
      }
    },

    setThemeMode(mode: ThemeMode) {
      this.themeMode = mode;
      writeStoredThemeMode(mode);
    },

    setSystemDark(dark: boolean) {
      this.systemDark = dark;
    },
  },
});
