export interface MovieTitleQuery {
  blank(): boolean
  matches(title: string, originalTitle?: string | null): boolean
}

function normalizedSearchTitle(value: string): string {
  return value
    .normalize('NFD')
    .replace(/\p{M}/gu, '')
    .toLowerCase()
    .replace(/[^\p{L}\p{N}]+/gu, ' ')
    .trim()
}

// Compile once per search; each title must independently contain every word.
export function compileMovieTitleSearch(raw: string): MovieTitleQuery {
  const blank = raw.trim() === ''
  const normalized = normalizedSearchTitle(raw)
  const words = normalized ? normalized.split(' ') : []
  const matchesTitle = (title: string) => {
    const normalizedTitle = normalizedSearchTitle(title)
    return words.every((word) => normalizedTitle.includes(word))
  }

  return {
    blank: () => blank,
    matches: (title, originalTitle) => {
      if (blank) return true
      if (words.length === 0) return false
      return (
        matchesTitle(title) || (!!originalTitle && matchesTitle(originalTitle))
      )
    },
  }
}
