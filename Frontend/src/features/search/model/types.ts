export interface UseSearchParamOptions {
  key: string;
  defaultValue?: string;
  replace?: boolean;
}

export interface UseDebouncedSearchParamOptions extends UseSearchParamOptions {
  debounceMs?: number;
  minLength?: number;
}

export interface UseDebouncedSearchParamResult {
  inputValue: string;
  committedValue: string;
  isPending: boolean;
  setInputValue: (next: string) => void;
  clear: () => void;
}
