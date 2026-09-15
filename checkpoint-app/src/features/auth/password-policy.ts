export const PASSWORD_MIN_LENGTH = 12;

export const PASSWORD_STATIC_RULES = [
  'Not a password seen in known data breaches',
  'Not similar to your name or email',
] as const;

export function passwordMeetsLength(password: string): boolean {
  return password.length >= PASSWORD_MIN_LENGTH;
}
