import { useCallback, useState } from "react";

export function useRefresh(action: () => Promise<unknown> | unknown) {
  const [refreshing, setRefreshing] = useState(false);

  const onRefresh = useCallback(async () => {
    setRefreshing(true);
    try {
      await action();
    } finally {
      setRefreshing(false);
    }
  }, [action]);

  return { refreshing, onRefresh };
}
