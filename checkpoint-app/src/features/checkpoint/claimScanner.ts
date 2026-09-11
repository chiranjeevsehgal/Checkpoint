import { CameraView } from "expo-camera";

export function claimScannerAvailable(): boolean {
  return CameraView.isModernBarcodeScannerAvailable === true;
}

export function startClaimScanner(
  onResult: (data: string) => void,
  onError: (error: unknown) => void,
): () => void {
  let subscription: { remove: () => void } | null = null;
  let handled = false;
  const stop = () => {
    subscription?.remove();
    subscription = null;
  };
  subscription = CameraView.onModernBarcodeScanned((event) => {
    handled = true;
    stop();
    onResult(event.data ?? "");
  });
  CameraView.launchScanner({ barcodeTypes: ["qr"] })
    .then(() => {
      if (!handled) stop();
    })
    .catch((error) => {
      stop();
      onError(error);
    });
  return stop;
}
