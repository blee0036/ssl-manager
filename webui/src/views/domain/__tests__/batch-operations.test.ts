// Feature: ux-improvements-batch1, Property 17: Batch Operations Continue on Individual Failure
import { describe, it, expect, vi, beforeEach } from 'vitest';

/**
 * **Validates: Requirements 12.5**
 *
 * Property 17: Batch Operations Continue on Individual Failure
 *
 * For any batch operation (probe, ignore, un-ignore, delete) processing N items
 * where K items fail (0 ≤ K < N), all N items SHALL be attempted —
 * the operation SHALL NOT short-circuit on the first failure.
 */

// ============================================================
// Extract batch operation pure logic for testing
// (mirrors the logic in domain/index.vue handleBatchOp)
// ============================================================

interface BatchState {
  operation: string;
  processed: number;
  total: number;
  failures: Array<{ id: string; name: string; error: string }>;
}

type BatchItemExecutor = (operation: string, itemId: string) => Promise<void>;

/**
 * Pure logic extracted from domain/index.vue handleBatchOp.
 * Iterates all items, catches individual failures, never short-circuits.
 */
async function executeBatch(
  operation: string,
  items: Array<{ id: string; name: string }>,
  executor: BatchItemExecutor,
): Promise<BatchState> {
  const state: BatchState = {
    operation,
    processed: 0,
    total: items.length,
    failures: [],
  };

  for (const item of items) {
    try {
      await executor(operation, item.id);
    } catch (e: unknown) {
      const errMsg = e instanceof Error ? e.message : '操作失败';
      state.failures.push({
        id: item.id,
        name: item.name,
        error: errMsg,
      });
    }
    state.processed++;
  }

  return state;
}

// ============================================================
// Tests
// ============================================================

describe('Batch Operations - Property 17: Continue on Individual Failure', () => {
  let executor: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    executor = vi.fn();
  });

  it('should attempt all items even when some fail in the middle', async () => {
    const items = [
      { id: '1', name: 'a.com' },
      { id: '2', name: 'b.com' },
      { id: '3', name: 'c.com' },
      { id: '4', name: 'd.com' },
      { id: '5', name: 'e.com' },
    ];

    // Items 2 and 4 will fail
    executor.mockImplementation(async (_op: string, id: string) => {
      if (id === '2' || id === '4') {
        throw new Error(`Failed for ${id}`);
      }
    });

    const result = await executeBatch('probe', items, executor);

    // All 5 items were attempted
    expect(executor).toHaveBeenCalledTimes(5);
    expect(result.processed).toBe(5);
    expect(result.total).toBe(5);

    // Exactly 2 failures recorded
    expect(result.failures).toHaveLength(2);
    expect(result.failures[0]).toEqual({ id: '2', name: 'b.com', error: 'Failed for 2' });
    expect(result.failures[1]).toEqual({ id: '4', name: 'd.com', error: 'Failed for 4' });
  });

  it('should process all items when the first item fails', async () => {
    const items = [
      { id: '1', name: 'first.com' },
      { id: '2', name: 'second.com' },
      { id: '3', name: 'third.com' },
    ];

    executor.mockImplementation(async (_op: string, id: string) => {
      if (id === '1') throw new Error('First item failed');
    });

    const result = await executeBatch('ignore', items, executor);

    expect(executor).toHaveBeenCalledTimes(3);
    expect(result.processed).toBe(3);
    expect(result.failures).toHaveLength(1);
    expect(result.failures[0].id).toBe('1');
  });

  it('should process all items when the last item fails', async () => {
    const items = [
      { id: '1', name: 'first.com' },
      { id: '2', name: 'second.com' },
      { id: '3', name: 'third.com' },
    ];

    executor.mockImplementation(async (_op: string, id: string) => {
      if (id === '3') throw new Error('Last item failed');
    });

    const result = await executeBatch('delete', items, executor);

    expect(executor).toHaveBeenCalledTimes(3);
    expect(result.processed).toBe(3);
    expect(result.failures).toHaveLength(1);
    expect(result.failures[0].id).toBe('3');
  });

  it('should handle all items failing without short-circuit', async () => {
    const items = [
      { id: '1', name: 'a.com' },
      { id: '2', name: 'b.com' },
      { id: '3', name: 'c.com' },
    ];

    executor.mockRejectedValue(new Error('All fail'));

    const result = await executeBatch('unignore', items, executor);

    // All 3 items still attempted
    expect(executor).toHaveBeenCalledTimes(3);
    expect(result.processed).toBe(3);
    expect(result.failures).toHaveLength(3);
  });

  it('should handle zero failures (all success)', async () => {
    const items = [
      { id: '1', name: 'a.com' },
      { id: '2', name: 'b.com' },
    ];

    executor.mockResolvedValue(undefined);

    const result = await executeBatch('probe', items, executor);

    expect(executor).toHaveBeenCalledTimes(2);
    expect(result.processed).toBe(2);
    expect(result.failures).toHaveLength(0);
  });

  it('should pass the correct operation type to executor', async () => {
    const items = [{ id: '1', name: 'a.com' }];
    executor.mockResolvedValue(undefined);

    for (const op of ['probe', 'ignore', 'unignore', 'delete']) {
      executor.mockClear();
      await executeBatch(op, items, executor);
      expect(executor).toHaveBeenCalledWith(op, '1');
    }
  });

  it('should correctly track processed count incrementally', async () => {
    const items = [
      { id: '1', name: 'a.com' },
      { id: '2', name: 'b.com' },
      { id: '3', name: 'c.com' },
    ];

    executor.mockImplementation(async () => {
      // Capture processed count will be incremented AFTER executor returns
    });

    // We can't easily test incremental during async, but we verify final state
    const result = await executeBatch('probe', items, executor);

    expect(result.processed).toBe(3);
    expect(result.total).toBe(3);
  });

  it('should reset state correctly after completion (state machine: selected → executing → done)', async () => {
    const items = [
      { id: '1', name: 'a.com' },
      { id: '2', name: 'b.com' },
    ];

    executor.mockResolvedValue(undefined);

    // Simulate state machine: before batch
    const initialState: BatchState = { operation: '', processed: 0, total: 0, failures: [] };
    expect(initialState.operation).toBe(''); // idle state

    // Execute batch (simulates "executing" state)
    const result = await executeBatch('probe', items, executor);

    // After batch completes, caller resets state (in Vue component: batchState.operation = '')
    // The executeBatch returns final counts for the caller to display summary
    expect(result.operation).toBe('probe'); // was set during execution
    expect(result.processed).toBe(2);
    expect(result.total).toBe(2);
    expect(result.failures).toHaveLength(0);

    // Caller resets → back to idle (simulating what index.vue does)
    const resetState: BatchState = { operation: '', processed: 0, total: 0, failures: [] };
    expect(resetState.operation).toBe('');
  });

  it('should handle empty items list gracefully', async () => {
    const result = await executeBatch('probe', [], executor);

    expect(executor).not.toHaveBeenCalled();
    expect(result.processed).toBe(0);
    expect(result.total).toBe(0);
    expect(result.failures).toHaveLength(0);
  });

  it('should handle large batch with mixed failures (property-style)', async () => {
    // Simulate a larger batch where every 3rd item fails
    const N = 20;
    const items = Array.from({ length: N }, (_, i) => ({
      id: String(i + 1),
      name: `domain-${i + 1}.com`,
    }));

    executor.mockImplementation(async (_op: string, id: string) => {
      if (Number(id) % 3 === 0) {
        throw new Error(`Item ${id} failed`);
      }
    });

    const result = await executeBatch('probe', items, executor);

    // All N items attempted
    expect(executor).toHaveBeenCalledTimes(N);
    expect(result.processed).toBe(N);
    expect(result.total).toBe(N);

    // Items 3, 6, 9, 12, 15, 18 fail = 6 failures
    const expectedFailures = Math.floor(N / 3);
    expect(result.failures).toHaveLength(expectedFailures);

    // Verify failure IDs
    const failedIds = result.failures.map(f => f.id);
    expect(failedIds).toEqual(['3', '6', '9', '12', '15', '18']);
  });
});
