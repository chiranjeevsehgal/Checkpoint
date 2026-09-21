import { useCallback, useEffect, useRef, useState } from 'react';

import { mergePage } from '@/features/items/itemsView';
import type { ItemPage } from '@/lib/api/items-api';
import { describeUserError } from '@/lib/user-errors';

type LoadMode = 'first' | 'more' | 'refresh';

interface FeedState<T> {
  key: string;
  items: T[];
  nextOffset: number | null;
  loading: boolean;
  error: string | null;
}

export interface PagedItems<T> {
  items: T[];
  loading: boolean;
  refreshing: boolean;
  error: string | null;
  hasMore: boolean;
  loadMore: () => void;
  refresh: () => void;
  reload: () => void;
  replaceItems: (updater: (items: T[]) => T[]) => void;
}

/**
 * One offset-paged feed. `key` identifies the current query and resets the feed
 * when it changes; `load` must be memoized by the caller. A generation counter
 * drops responses from a superseded query.
 */
export function usePagedItems<T>(
  key: string,
  load: (offset: number) => Promise<ItemPage<T>>,
): PagedItems<T> {
  const [state, setState] = useState<FeedState<T>>({
    key,
    items: [],
    nextOffset: 0,
    loading: true,
    error: null,
  });
  const [refreshing, setRefreshing] = useState(false);
  const generation = useRef(0);
  const busy = useRef(false);

  const run = useCallback(
    async (targetKey: string, offset: number, mode: LoadMode) => {
      if (busy.current) return;
      busy.current = true;
      const token = generation.current;
      try {
        const page = await load(offset);
        if (token !== generation.current) return;
        setState((prev) => {
          const base = prev.key === targetKey ? prev.items : [];
          return {
            key: targetKey,
            items: mode === 'more' ? mergePage(base, page) : page.items,
            nextOffset: page.next_offset,
            loading: false,
            error: null,
          };
        });
      } catch (err) {
        if (token === generation.current) {
          setState((prev) => ({
            ...prev,
            key: targetKey,
            loading: false,
            error: describeUserError(err, 'Could not load your notes.'),
          }));
        }
      } finally {
        setRefreshing(false);
        if (token === generation.current) busy.current = false;
      }
    },
    [load],
  );

  useEffect(() => {
    generation.current += 1;
    busy.current = false;
    void run(key, 0, 'first');
  }, [key, run]);

  const current = state.key === key;
  const nextOffset = current ? state.nextOffset : null;

  const loadMore = useCallback(() => {
    if (nextOffset === null) return;
    void run(key, nextOffset, 'more');
  }, [key, nextOffset, run]);

  const refresh = useCallback(() => {
    if (busy.current) return;
    setRefreshing(true);
    void run(key, 0, 'refresh');
  }, [key, run]);

  const reload = useCallback(() => {
    void run(key, 0, 'first');
  }, [key, run]);

  const replaceItems = useCallback(
    (updater: (items: T[]) => T[]) => {
      setState((prev) => (prev.key === key ? { ...prev, items: updater(prev.items) } : prev));
    },
    [key],
  );

  return {
    items: current ? state.items : [],
    loading: current ? state.loading : true,
    refreshing,
    error: current ? state.error : null,
    hasMore: nextOffset !== null,
    loadMore,
    refresh,
    reload,
    replaceItems,
  };
}
