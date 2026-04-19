export interface PageParams {
  page: number;
  size: number;
}

export interface UsePageParamsOptions {
  defaultSize?: number;
  allowedSizes?: readonly number[];
  pageKey?: string;
  sizeKey?: string;
  replace?: boolean;
}

export interface UsePageParamsResult extends PageParams {
  setPage: (next: number) => void;
  setSize: (next: number) => void;
  setPageAndSize: (next: PageParams) => void;
  reset: () => void;
}
