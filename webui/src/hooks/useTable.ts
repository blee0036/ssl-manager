import { ref, reactive, computed } from 'vue';
import type { Ref } from 'vue';

export interface FetchParams {
  page: number;
  pageSize: number;
}

export interface FetchResult<T> {
  items: T[];
  total: number;
}

export interface UseTableOptions<T> {
  /** 数据获取函数，接收分页参数 */
  fetchFn: (params: FetchParams) => Promise<FetchResult<T>>;
  /** 默认每页条数，默认 20 */
  defaultPageSize?: number;
  /** 是否立即加载，默认 false */
  immediate?: boolean;
}

/**
 * 表格通用逻辑 Hook
 * 提供 loading、pagination、refresh 等表格常用功能
 * 支持服务端分页（通过 fetchFn 传递 page/pageSize）
 */
export function useTable<T>(options: UseTableOptions<T>) {
  const { fetchFn, defaultPageSize = 20, immediate = false } = options;

  const loading: Ref<boolean> = ref(false);
  const data: Ref<T[]> = ref([]) as Ref<T[]>;
  const total: Ref<number> = ref(0);
  const error: Ref<string> = ref('');

  const page = ref(1);
  const pageSize = ref(defaultPageSize);

  const pageCount = computed(() => Math.ceil(total.value / pageSize.value));
  const itemCount = computed(() => total.value);

  function handlePageChange(newPage: number) {
    page.value = newPage;
    refresh();
  }

  function handlePageSizeChange(newPageSize: number) {
    pageSize.value = newPageSize;
    page.value = 1;
    refresh();
  }

  const pagination = reactive({
    page,
    pageSize,
    pageCount,
    itemCount,
    showSizePicker: true,
    pageSizes: [10, 20, 50, 100],
    onChange: handlePageChange,
    onUpdatePageSize: handlePageSizeChange,
  });

  async function refresh() {
    loading.value = true;
    error.value = '';
    try {
      const result = await fetchFn({
        page: page.value,
        pageSize: pageSize.value,
      });
      data.value = result.items;
      total.value = result.total;
    } catch (err: unknown) {
      error.value = err instanceof Error ? err.message : '加载失败';
      data.value = [];
      total.value = 0;
    } finally {
      loading.value = false;
    }
  }

  if (immediate) {
    refresh();
  }

  return {
    loading,
    data,
    total,
    error,
    pagination,
    refresh,
    handlePageChange,
    handlePageSizeChange,
  };
}
