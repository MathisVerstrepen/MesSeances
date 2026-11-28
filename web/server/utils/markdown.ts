// Markdown link text and URL templates must not break the list syntax.
export function markdownText(value: string): string {
  return value
    .replace(/\s+/g, ' ')
    .trim()
    .replace(/([\\`*_[\]<>|])/g, '\\$1')
}

// Link destinations end at the first space or unbalanced parenthesis.
export function markdownUrl(value: string): string {
  return value
    .replaceAll(' ', '%20')
    .replaceAll('(', '%28')
    .replaceAll(')', '%29')
}
