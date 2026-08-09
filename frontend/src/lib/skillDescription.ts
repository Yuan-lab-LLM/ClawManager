const IMPORTED_FROM = /^imported from\b/i;

export function isPlaceholderSkillDescription(description?: string | null): boolean {
  const trimmed = (description || '').trim();
  if (!trimmed) return true;
  return IMPORTED_FROM.test(trimmed);
}

export function displaySkillDescription(description?: string | null, maxRunes = 80): string {
  if (isPlaceholderSkillDescription(description)) {
    return '';
  }
  const text = (description || '').replace(/\s+/g, ' ').trim();
  const runes = Array.from(text);
  if (runes.length <= maxRunes) {
    return text;
  }
  return `${runes.slice(0, maxRunes).join('')}…`;
}
