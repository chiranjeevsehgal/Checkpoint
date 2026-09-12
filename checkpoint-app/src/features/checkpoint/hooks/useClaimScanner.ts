import { useCallback, useEffect, useRef } from "react";

import { claimScannerAvailable, startClaimScanner } from "../claimScanner.ts";

export function useClaimScanner(
  onResult: (data: string) => void,
  onError: (error: unknown) => void,
) {
  const stopRef = useRef<(() => void) | null>(null);
  const onResultRef = useRef(onResult);
  const onErrorRef = useRef(onError);

  useEffect(() => {
    onResultRef.current = onResult;
  }, [onResult]);

  useEffect(() => {
    onErrorRef.current = onError;
  }, [onError]);

  const start = useCallback(() => {
    stopRef.current?.();
    stopRef.current = startClaimScanner(
      (data) => onResultRef.current(data),
      (error) => onErrorRef.current(error),
    );
  }, []);

  useEffect(() => {
    return () => stopRef.current?.();
  }, []);

  return { available: claimScannerAvailable(), start };
}
