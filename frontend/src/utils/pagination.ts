export const DEFAULT_PAGE_SIZE = 50;
export const MAX_PAGE_SIZE = 200;
export const MAX_PAGE = 1_000_000;

export function parsePage(value: string | null): number {
  const page = Number(value);
  return Number.isInteger(page) && page >= 1 && page <= MAX_PAGE ? page : 1;
}

export function parsePageSize(value: string | null): number {
  const size = Number(value);
  return Number.isInteger(size) && size >= 1 && size <= MAX_PAGE_SIZE ? size : DEFAULT_PAGE_SIZE;
}

export function normalizePaginationParams(
  params: URLSearchParams,
  total?: number
): URLSearchParams {
  const next = new URLSearchParams(params);
  const pageSize = parsePageSize(params.get('page_size'));
  const requested = parsePage(params.get('page'));
  const page =
    total === undefined ? requested : getPagination(total, requested, pageSize).currentPage;
  if (page === 1) next.delete('page');
  else next.set('page', String(page));
  if (pageSize === DEFAULT_PAGE_SIZE) next.delete('page_size');
  else next.set('page_size', String(pageSize));
  return next;
}

export function getPagination(total: number, page: number, pageSize: number) {
  const totalPages = Math.max(1, Math.ceil(total / pageSize));
  const currentPage = Math.min(Math.max(1, page), totalPages);
  return { currentPage, totalPages, start: (currentPage - 1) * pageSize };
}

/** Clamp after list shrinkage so the last remaining page stays reachable. */
export function paginate<T>(items: T[], page: number, pageSize: number) {
  const { currentPage, totalPages, start } = getPagination(items.length, page, pageSize);
  return { pageItems: items.slice(start, start + pageSize), currentPage, totalPages };
}
