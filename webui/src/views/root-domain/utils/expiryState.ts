/**
 * 根域名到期状态计算（纯函数，无外部依赖、无 DOM）。
 *
 * 从 `../components/RootDomainTable.vue` 抽取，供组件“剩余天数”列渲染使用；
 * 抽成独立纯函数后可在 Vitest（node 环境，见 vitest.config.ts）下直接单测，
 * 无需挂载组件（本仓库单测约定：不做整组件挂载，测纯逻辑，
 * 参见 `views/alert/utils/channelConfig.ts` 与其测试）。
 *
 * 输入字段来源与后端 `Api.RootDomain` 完全一致（对齐 Go
 * `internal/model/root_domain.go` 的 json tag）：
 *   - expiry_date:    string | null   注册到期日（RFC3339 UTC）；null = 未知
 *   - days_remaining: number | null   剩余天数（向零截断整天数）；null = 未知
 */

/** naive-ui NTag 的 type：默认 / 错误(红) / 警告(橙) / 成功(绿)。 */
export type StateType = 'default' | 'error' | 'warning' | 'success';

/** 到期状态：展示文案 + NTag 颜色类型。 */
export interface ExpiryState {
  label: string;
  type: StateType;
}

/**
 * 依据到期日与剩余天数计算展示状态：
 * - expiry_date == null || days_remaining == null → “未知”（default）
 * - days_remaining <= 0                           → “已过期”（error，红）
 * - 0 < days_remaining <= threshold               → “即将到期”（warning，橙）
 * - days_remaining > threshold                    → “正常”（success，绿）
 *
 * @param row       根域名记录（仅读取 expiry_date 与 days_remaining）
 * @param threshold 到期阈值（天）：来源 SystemConfig.domain_expiry.expiry_threshold_days，默认 14
 */
export function expiryState(row: Api.RootDomain, threshold: number): ExpiryState {
  if (row.expiry_date == null || row.days_remaining == null) {
    return { label: '未知', type: 'default' };
  }
  const days = row.days_remaining;
  if (days <= 0) return { label: '已过期', type: 'error' };
  if (days <= threshold) return { label: '即将到期', type: 'warning' };
  return { label: '正常', type: 'success' };
}
