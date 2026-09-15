import { ChangeDetectionStrategy, Component, effect, input, signal } from '@angular/core';

@Component({
  selector: 'ck-claim-qr',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    @if (dataUrl()) {
      <div class="qr">
        <img [src]="dataUrl()" alt="Pendant enrollment QR code" />
      </div>
    } @else {
      <p class="subtle">{{ error() || 'Enter a device id and claim key to render the QR.' }}</p>
    }
  `,
})
export class ClaimQr {
  readonly value = input.required<string>();

  protected readonly dataUrl = signal('');
  protected readonly error = signal('');

  constructor() {
    effect(() => {
      void this.render(this.value());
    });
  }

  private async render(value: string): Promise<void> {
    if (!value) {
      this.dataUrl.set('');
      return;
    }
    try {
      const qrcode = await import('qrcode');
      this.dataUrl.set(await qrcode.toDataURL(value, { width: 220, margin: 1 }));
      this.error.set('');
    } catch (error) {
      this.error.set(error instanceof Error ? error.message : 'QR generation failed');
    }
  }
}
