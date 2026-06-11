import 'axios';

declare module 'axios' {
  interface AxiosRequestConfig {
    /** When true, the global error notification interceptor will skip this request */
    skipErrorNotify?: boolean;
  }
}
