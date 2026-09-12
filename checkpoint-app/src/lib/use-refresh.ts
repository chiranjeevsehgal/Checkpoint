import { useCallback, useState } from 'react';

export function useRefresh(action: () => Promise<unknown> | unknown) {
  const [refreshing, setRefreshing] = useState(false);

  const onRefresh = useCallback(() => {
    setRefreshing(true);
    void Promise.resolve()
      .then(action)
      .catch(() => undefined)
      .finally(() => setRefreshing(false));
  }, [action]);

  return { refreshing, onRefresh };
}
