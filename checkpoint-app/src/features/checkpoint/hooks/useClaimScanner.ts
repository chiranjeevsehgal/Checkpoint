import { useCallback, useEffect, useRef } from "react";

import { claimScannerAvailable, startClaimScanner } from "../claimScanner.ts";

export function useClaimScanner(onResult: (data: string) => void) {
  const stopRef = useRef<(() => void) | null>(null);
  const onResultRef = useRef(onResult);

  useEffect(() => {
    onResultRef.current = onResult;
  }, [onResult]);

  const start = useCallback(() => {
    stopRef.current?.();
    stopRef.current = startClaimScanner((data) => onResultRef.current(data));
  }, []);

  useEffect(() => {
    return () => stopRef.current?.();
  }, []);

  return { available: claimScannerAvailable(), start };
}
