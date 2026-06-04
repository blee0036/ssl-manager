/// <reference types="vite/client" />

interface ImportMetaEnv {
  /** API 基础 URL */
  readonly VITE_API_BASE_URL: string;
  /** 应用标题 */
  readonly VITE_APP_TITLE: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}
