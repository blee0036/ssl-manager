import { describe, it, expect } from 'vitest';
import { expiryState } from '../utils/expiryState';
import type { ExpiryState, StateType } from '../utils/expiryState';

/**
 * Validates: Requirements 8.1, 8.3
 *
 * 单元测试：根域名列表「剩余天数」列的到期状态渲染逻辑。
 *
 * 页面结构：`views/root-domain/index.vue` → `components/RootDomainTable.vue`，
 * 表格「剩余天数」列以 `expiryState(row, threshold)` 决定展示文案与 NTag 颜色类型。
 * 该纯决策逻辑已抽取到 `../utils/expiryState`（组件与本测试引用同一函数，
 * 保证测的就是页面实际渲染所用逻辑，无副本漂移）。
 *
 * 为何不整组件挂载：本仓库 vitest 运行在 node 环境（见 vitest.config.ts，
 * `environment: 'node'`），且未引入 @vue/test-utils，naive-ui 组件无法挂载；
 * 既有视图单测（如 views/domain/__tests__/*、views/alert/utils/__tests__/*）
 * 一律只测抽取出的纯逻辑，本测试与之保持一致。
 *
 * 到期状态规则（对齐 design.md「前端 4.3」与 RootDomainTable.vue 注释）：
 *   - expiry_date == null || days_remaining == null → “未知”（default）      需求 8.3
 *   - days_remaining <= 0                           → “已过期”（error，红）
 *   - 0 < days_remaining <= threshold               → “即将到期”（warning，橙）
 *   - days_remaining > threshold                    → “正常”（success，绿）
 *
 * 字段来源均取自后端 `Api.RootDomain`（逐字段对齐 Go
 * `internal/model/root_domain.go` 的 json tag）：expiry_date / days_remaining /
 * last_status 等。makeRootDomain 返回值受 `Api.RootDomain` 类型约束，字段名
 * 若与后端契约漂移则本文件无法通过类型检查。
 */

/** 默认到期阈值（天），对齐后端 DomainExpiryConfig.ExpiryThresholdDays 默认值 14。 */
const THRESHOLD = 14;

/** 全部合法的 NTag type，用于反向校验 expiryState 只会返回这四种之一。 */
const ALL_STATE_TYPES: StateType[] = ['default', 'error', 'warning', 'success'];

/**
 * 构造一条完整的 Api.RootDomain（字段名对齐后端 Go json tag），允许覆写。
 * 使用后端契约中的真实字段名，任何字段名/类型漂移都会被 TS 类型检查捕获。
 */
function makeRootDomain(overrides: Partial<Api.RootDomain> = {}): Api.RootDomain {
  return {
    id: 'rd-1',
    name: 'example.com',
    source: 'manual',
    registrable_domain: 'example.com',
    expiry_date: '2025-09-14T00:00:00Z',
    expiry_source: 'whois',
    days_remaining: 42,
    last_checked_at: '2025-08-01T03:00:00Z',
    last_status: 'success',
    last_error: '',
    monitor_enabled: true,
    alert_ignored: false,
    created_at: '2025-07-01T10:00:00Z',
    updated_at: '2025-08-01T03:00:00Z',
    ...overrides,
  };
}

/** 便捷包装：以默认阈值计算状态，返回类型受 ExpiryState 约束。 */
function stateOf(row: Api.RootDomain, threshold = THRESHOLD): ExpiryState {
  return expiryState(row, threshold);
}

describe('RootDomainTable 到期状态渲染（expiryState）', () => {
  // ----------------------------------------------------------------
  // 未知（default）— 需求 8.3：尚无成功 WHOIS 结果时到期日/剩余天数为“未知”
  // ----------------------------------------------------------------
  describe('未知（default）', () => {
    it('expiry_date 与 days_remaining 均为 null → 未知', () => {
      const row = makeRootDomain({ expiry_date: null, days_remaining: null });
      expect(stateOf(row)).toEqual({ label: '未知', type: 'default' });
    });

    it('expiry_date 为 null（即使 days_remaining 有值）→ 未知', () => {
      // 覆盖 OR 条件的左侧分支
      const row = makeRootDomain({ expiry_date: null, days_remaining: 5 });
      expect(stateOf(row)).toEqual({ label: '未知', type: 'default' });
    });

    it('days_remaining 为 null（即使 expiry_date 有值）→ 未知', () => {
      // 覆盖 OR 条件的右侧分支
      const row = makeRootDomain({ expiry_date: '2025-09-14T00:00:00Z', days_remaining: null });
      expect(stateOf(row)).toEqual({ label: '未知', type: 'default' });
    });
  });

  // ----------------------------------------------------------------
  // 已过期（error）— days_remaining <= 0
  // ----------------------------------------------------------------
  describe('已过期（error）', () => {
    it('days_remaining == 0 → 已过期（边界：<= 0）', () => {
      expect(stateOf(makeRootDomain({ days_remaining: 0 }))).toEqual({ label: '已过期', type: 'error' });
    });

    it('days_remaining == -1 → 已过期', () => {
      expect(stateOf(makeRootDomain({ days_remaining: -1 }))).toEqual({ label: '已过期', type: 'error' });
    });

    it('days_remaining 大幅为负 → 已过期', () => {
      expect(stateOf(makeRootDomain({ days_remaining: -365 }))).toEqual({ label: '已过期', type: 'error' });
    });
  });

  // ----------------------------------------------------------------
  // 即将到期（warning）— 0 < days_remaining <= threshold
  // ----------------------------------------------------------------
  describe('即将到期（warning）', () => {
    it('days_remaining == 1 → 即将到期（下边界：> 0）', () => {
      expect(stateOf(makeRootDomain({ days_remaining: 1 }))).toEqual({ label: '即将到期', type: 'warning' });
    });

    it('days_remaining == threshold(14) → 即将到期（上边界：<= threshold）', () => {
      expect(stateOf(makeRootDomain({ days_remaining: THRESHOLD }))).toEqual({ label: '即将到期', type: 'warning' });
    });

    it('0 < days_remaining < threshold → 即将到期', () => {
      expect(stateOf(makeRootDomain({ days_remaining: 7 }))).toEqual({ label: '即将到期', type: 'warning' });
    });
  });

  // ----------------------------------------------------------------
  // 正常（success）— days_remaining > threshold
  // ----------------------------------------------------------------
  describe('正常（success）', () => {
    it('days_remaining == threshold + 1(15) → 正常（越过上边界）', () => {
      expect(stateOf(makeRootDomain({ days_remaining: THRESHOLD + 1 }))).toEqual({ label: '正常', type: 'success' });
    });

    it('days_remaining 远大于 threshold → 正常', () => {
      expect(stateOf(makeRootDomain({ days_remaining: 365 }))).toEqual({ label: '正常', type: 'success' });
    });
  });

  // ----------------------------------------------------------------
  // 阈值敏感性：相同剩余天数在不同阈值下的分级不同
  // ----------------------------------------------------------------
  describe('阈值（threshold）参与分级', () => {
    it('days_remaining=20：threshold=14 → 正常；threshold=30 → 即将到期', () => {
      const row = makeRootDomain({ days_remaining: 20 });
      expect(stateOf(row, 14)).toEqual({ label: '正常', type: 'success' });
      expect(stateOf(row, 30)).toEqual({ label: '即将到期', type: 'warning' });
    });
  });

  // ----------------------------------------------------------------
  // 字段来源契约（需求 8.1）：状态仅由 expiry_date / days_remaining 决定，
  // 且列表项包含后端要求的必需字段。
  // ----------------------------------------------------------------
  describe('字段来源与后端 Api.RootDomain 一致（需求 8.1）', () => {
    it('状态仅由 expiry_date 与 days_remaining 决定，与其它字段无关', () => {
      const baseState = stateOf(makeRootDomain({ expiry_date: '2025-09-14T00:00:00Z', days_remaining: 42 }));
      expect(baseState).toEqual({ label: '正常', type: 'success' });

      // 改动与到期状态无关的字段，状态不应变化
      const unrelatedVariants: Partial<Api.RootDomain>[] = [
        { last_status: 'failed', last_error: 'whois timeout' },
        { name: 'another-name.com' },
        { source: 'cloudflare' },
        { registrable_domain: 'zzz.example.org' },
        { monitor_enabled: false },
        { alert_ignored: true },
        { last_checked_at: null },
      ];
      for (const v of unrelatedVariants) {
        const row = makeRootDomain({ expiry_date: '2025-09-14T00:00:00Z', days_remaining: 42, ...v });
        expect(stateOf(row)).toEqual(baseState);
      }

      // 仅改动 days_remaining 即可改变状态（证明它是数据来源之一）
      expect(stateOf(makeRootDomain({ days_remaining: -1 }))).toEqual({ label: '已过期', type: 'error' });
      // 仅把 expiry_date 置空即变为未知（证明它是数据来源之一）
      expect(stateOf(makeRootDomain({ expiry_date: null }))).toEqual({ label: '未知', type: 'default' });
    });

    it('列表项包含需求 8.1 要求的必需字段（名称/来源/到期日/剩余天数/最近检查时间/最近状态）', () => {
      const row = makeRootDomain();
      // 字段名与后端 Go json tag 完全一致
      for (const key of ['name', 'source', 'expiry_date', 'days_remaining', 'last_checked_at', 'last_status'] as const) {
        expect(row).toHaveProperty(key);
      }
    });

    it('返回的 type 恒为四种合法 NTag 类型之一', () => {
      const daysCases: (number | null)[] = [null, -365, -1, 0, 1, 7, 14, 15, 365, 10000];
      for (const days of daysCases) {
        const row = makeRootDomain({
          days_remaining: days,
          expiry_date: days == null ? null : '2025-09-14T00:00:00Z',
        });
        expect(ALL_STATE_TYPES).toContain(stateOf(row).type);
      }
    });
  });
});
