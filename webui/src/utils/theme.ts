/**
 * 主题模式工具
 *
 * 注意：index.html 中有一段内联脚本在首屏渲染前做同样的解析，
 * 修改存储键名或取值时需同步修改那里，否则会出现首屏闪烁。
 */

export type ThemeMode = 'system' | 'light' | 'dark';

export const THEME_MODE_STORAGE_KEY = 'ssl-manager:theme-mode';

export const THEME_MODES: readonly ThemeMode[] = ['system', 'light', 'dark'];

const DARK_MEDIA_QUERY = '(prefers-color-scheme: dark)';

export function isThemeMode(value: unknown): value is ThemeMode {
  return typeof value === 'string' && (THEME_MODES as readonly string[]).includes(value);
}

/** 读取用户保存的主题模式，未保存或非法值时返回 system */
export function readStoredThemeMode(): ThemeMode {
  try {
    const value = localStorage.getItem(THEME_MODE_STORAGE_KEY);
    return isThemeMode(value) ? value : 'system';
  } catch {
    return 'system';
  }
}

export function writeStoredThemeMode(mode: ThemeMode): void {
  try {
    if (mode === 'system') {
      localStorage.removeItem(THEME_MODE_STORAGE_KEY);
    } else {
      localStorage.setItem(THEME_MODE_STORAGE_KEY, mode);
    }
  } catch {
    // 隐私模式等场景下 localStorage 不可用，忽略即可，仅本次会话生效
  }
}

/** 根据主题模式与系统偏好计算是否使用深色 */
export function resolveIsDark(mode: ThemeMode, systemDark: boolean): boolean {
  if (mode === 'dark') return true;
  if (mode === 'light') return false;
  return systemDark;
}

export function getSystemDarkMediaQuery(): MediaQueryList | null {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return null;
  return window.matchMedia(DARK_MEDIA_QUERY);
}

/** 把深色状态同步到 <html>，供 UnoCSS 的 dark: 变体和 theme.css 使用 */
export function applyDarkClass(isDark: boolean): void {
  const root = document.documentElement;
  root.classList.toggle('dark', isDark);
  root.style.colorScheme = isDark ? 'dark' : 'light';
}
