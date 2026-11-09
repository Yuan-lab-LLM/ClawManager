// Presentation only: never use this label as an identity or statistics key.
export function ownerWelcomeName(owner: string): string {
  const local = owner.split('@')[0].trim();
  const withoutDigits = local.replace(/[0-9０-９]/g, '').replace(/^[._+\-\s]+|[._+\-\s]+$/g, '');
  return withoutDigits || '朋友';
}
