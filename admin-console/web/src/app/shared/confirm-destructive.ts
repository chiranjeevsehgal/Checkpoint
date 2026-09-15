import { ChangeDetectionStrategy, Component, computed, input, output, signal } from '@angular/core';

@Component({
  selector: 'ck-confirm',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <div class="modal-backdrop">
      <div class="card modal" role="dialog" aria-modal="true" [attr.aria-label]="title()">
        <h2>{{ title() }}</h2>
        <p class="muted">{{ message() }}</p>
        <div class="field">
          <label for="confirm-expected">Type "{{ expected() }}" to confirm</label>
          <input
            id="confirm-expected"
            autocomplete="off"
            [value]="typed()"
            (input)="onInput($event)"
          />
        </div>
        <div class="row between">
          <button class="btn btn-ghost" type="button" (click)="cancelled.emit()">Cancel</button>
          <button class="btn btn-danger" type="button" [disabled]="!ready()" (click)="confirmed.emit()">
            {{ confirmLabel() }}
          </button>
        </div>
      </div>
    </div>
  `,
})
export class ConfirmDestructive {
  readonly title = input.required<string>();
  readonly expected = input.required<string>();
  readonly message = input('');
  readonly confirmLabel = input('Delete');
  readonly confirmed = output<void>();
  readonly cancelled = output<void>();

  protected readonly typed = signal('');
  protected readonly ready = computed(() => this.typed().trim() === this.expected());

  protected onInput(event: Event): void {
    this.typed.set((event.target as HTMLInputElement).value);
  }
}
