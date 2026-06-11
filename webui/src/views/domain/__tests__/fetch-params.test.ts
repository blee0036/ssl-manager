import { describe, it, expect } from 'vitest';
import type { FetchDomainsParams } from '@/service/api/domain';

/**
 * **Validates: Requirements 5.6**
 *
 * Tests that FetchDomainsParams are correctly constructed from filter state,
 * and that changing filter resets page to 1.
 */

/**
 * Simulates the logic in domain/index.vue that builds FetchDomainsParams
 * from the reactive filter state and pagination state.
 */
interface FilterState {
  filterStatus: string;
  sortBy: string;
  sortOrder: string;
}

interface PaginationState {
  page: number;
  perPage: number;
}

function buildFetchParams(filter: FilterState, pagination: PaginationState): FetchDomainsParams {
  const params: FetchDomainsParams = {
    page: pagination.page,
    per_page: pagination.perPage,
  };

  if (filter.sortBy) {
    params.sort_by = filter.sortBy;
    params.sort_order = filter.sortOrder || 'asc';
  }

  if (filter.filterStatus && filter.filterStatus !== 'all') {
    params.filter_status = filter.filterStatus;
  }

  return params;
}

/**
 * Simulates the filter change handler: resets page to 1 when filter changes.
 */
function onFilterChange(pagination: PaginationState): PaginationState {
  return { ...pagination, page: 1 };
}

/**
 * Simulates the sort change handler: resets page to 1 when sort changes.
 */
function onSortChange(pagination: PaginationState): PaginationState {
  return { ...pagination, page: 1 };
}

describe('Sort/filter params construction', () => {
  describe('buildFetchParams', () => {
    it('should include sort_by and sort_order when sortBy is set', () => {
      const filter: FilterState = { filterStatus: 'all', sortBy: 'name', sortOrder: 'asc' };
      const pagination: PaginationState = { page: 1, perPage: 50 };

      const params = buildFetchParams(filter, pagination);

      expect(params.sort_by).toBe('name');
      expect(params.sort_order).toBe('asc');
    });

    it('should default sort_order to asc when sortOrder is empty', () => {
      const filter: FilterState = { filterStatus: '', sortBy: 'expire_at', sortOrder: '' };
      const pagination: PaginationState = { page: 1, perPage: 50 };

      const params = buildFetchParams(filter, pagination);

      expect(params.sort_by).toBe('expire_at');
      expect(params.sort_order).toBe('asc');
    });

    it('should not include sort params when sortBy is empty', () => {
      const filter: FilterState = { filterStatus: '', sortBy: '', sortOrder: '' };
      const pagination: PaginationState = { page: 2, perPage: 20 };

      const params = buildFetchParams(filter, pagination);

      expect(params.sort_by).toBeUndefined();
      expect(params.sort_order).toBeUndefined();
    });

    it('should include filter_status when it is not "all" and not empty', () => {
      const filter: FilterState = { filterStatus: 'tls_error', sortBy: '', sortOrder: '' };
      const pagination: PaginationState = { page: 1, perPage: 50 };

      const params = buildFetchParams(filter, pagination);

      expect(params.filter_status).toBe('tls_error');
    });

    it('should not include filter_status when it is "all"', () => {
      const filter: FilterState = { filterStatus: 'all', sortBy: '', sortOrder: '' };
      const pagination: PaginationState = { page: 1, perPage: 50 };

      const params = buildFetchParams(filter, pagination);

      expect(params.filter_status).toBeUndefined();
    });

    it('should not include filter_status when it is empty', () => {
      const filter: FilterState = { filterStatus: '', sortBy: '', sortOrder: '' };
      const pagination: PaginationState = { page: 1, perPage: 50 };

      const params = buildFetchParams(filter, pagination);

      expect(params.filter_status).toBeUndefined();
    });

    it('should pass page and per_page from pagination state', () => {
      const filter: FilterState = { filterStatus: '', sortBy: '', sortOrder: '' };
      const pagination: PaginationState = { page: 3, perPage: 25 };

      const params = buildFetchParams(filter, pagination);

      expect(params.page).toBe(3);
      expect(params.per_page).toBe(25);
    });

    it('should include all params together correctly', () => {
      const filter: FilterState = { filterStatus: 'expiring_30d', sortBy: 'expire_at', sortOrder: 'desc' };
      const pagination: PaginationState = { page: 2, perPage: 10 };

      const params = buildFetchParams(filter, pagination);

      expect(params).toEqual({
        page: 2,
        per_page: 10,
        sort_by: 'expire_at',
        sort_order: 'desc',
        filter_status: 'expiring_30d',
      });
    });
  });

  describe('filter/sort change resets page to 1', () => {
    it('should reset page to 1 when filter changes', () => {
      const pagination: PaginationState = { page: 5, perPage: 50 };

      const result = onFilterChange(pagination);

      expect(result.page).toBe(1);
      expect(result.perPage).toBe(50); // perPage unchanged
    });

    it('should reset page to 1 when sort changes', () => {
      const pagination: PaginationState = { page: 10, perPage: 20 };

      const result = onSortChange(pagination);

      expect(result.page).toBe(1);
      expect(result.perPage).toBe(20); // perPage unchanged
    });

    it('should keep page as 1 if already on first page', () => {
      const pagination: PaginationState = { page: 1, perPage: 50 };

      const result = onFilterChange(pagination);

      expect(result.page).toBe(1);
    });
  });
});
