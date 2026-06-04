import { ref } from 'vue';
import type { Ref } from 'vue';

export interface BatchResult<R> {
  success: R[];
  failed: Array<{ index: number; error: Error }>;
}

/**
 * 并发限制的批量请求 Hook
 * 固定并发数为 5，不可配置（需求 12.3 约束）
 * 使用 Promise 池模式，同时最多 5 个请求在执行
 */
export function useBatchRequest<T, R>(
  requestFn: (item: T) => Promise<R>
): {
  execute: (items: T[]) => Promise<BatchResult<R>>;
  progress: Ref<number>;
  isRunning: Ref<boolean>;
} {
  const CONCURRENCY_LIMIT = 5;
  const progress: Ref<number> = ref(0);
  const isRunning: Ref<boolean> = ref(false);

  async function execute(items: T[]): Promise<BatchResult<R>> {
    if (items.length === 0) {
      return { success: [], failed: [] };
    }

    isRunning.value = true;
    progress.value = 0;

    const success: R[] = [];
    const failed: Array<{ index: number; error: Error }> = [];
    let currentIndex = 0;
    const totalCount = items.length;

    async function runNext(): Promise<void> {
      const index = currentIndex++;
      if (index >= totalCount) return;

      try {
        const result = await requestFn(items[index]);
        success.push(result);
      } catch (err) {
        failed.push({
          index,
          error: err instanceof Error ? err : new Error(String(err)),
        });
      }

      progress.value = Math.round(((success.length + failed.length) / totalCount) * 100);
      await runNext();
    }

    // 启动并发池：最多 CONCURRENCY_LIMIT 个 worker 并行
    const workerCount = Math.min(CONCURRENCY_LIMIT, totalCount);
    const workers = Array.from({ length: workerCount }, () => runNext());
    await Promise.all(workers);

    isRunning.value = false;
    return { success, failed };
  }

  return {
    execute,
    progress,
    isRunning,
  };
}
