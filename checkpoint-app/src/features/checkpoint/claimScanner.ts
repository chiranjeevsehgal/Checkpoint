import { CameraView } from "expo-camera";

export function claimScannerAvailable(): boolean {
  return CameraView.isModernBarcodeScannerAvailable === true;
}

export function startClaimScanner(onResult: (data: string) => void): () => void {
  let subscription: { remove: () => void } | null = null;
  const stop = () => {
    subscription?.remove();
    subscription = null;
  };
  subscription = CameraView.onModernBarcodeScanned((event) => {
    stop();
    onResult(event.data ?? "");
  });
  void CameraView.launchScanner({ barcodeTypes: ["qr"] }).catch(stop);
  return stop;
}
