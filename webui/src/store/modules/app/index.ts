import { defineStore } from 'pinia';

interface AppState {
  /** 侧边栏是否折叠 */
  siderCollapsed: boolean;
  /** 是否为移动端 */
  isMobile: boolean;
}

export const useAppStore = defineStore('app', {
  state: (): AppState => ({
    siderCollapsed: false,
    isMobile: false,
  }),

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
  },
});
