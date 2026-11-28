// Plain-language explanations for the version acronyms shown across the site.
const definitions = {
  'VF SME': 'Version française avec sous-titres pour sourds et malentendants',
  VFSTF: 'Version française sous-titrée en français',
  VOSTFR: 'Version originale sous-titrée en français',
  VOST: 'Version originale sous-titrée',
  VOF: 'Version originale française : film tourné en français',
  VF: 'Version française : film doublé en français',
  VO: 'Version originale, dans la langue de tournage',
} as const satisfies Record<string, string>

export type LanguageTerm = keyof typeof definitions

export type LanguageTextSegment =
  | { readonly kind: 'text'; readonly text: string }
  | {
      readonly kind: 'term'
      readonly text: LanguageTerm
      readonly definition: string
    }

// Longest terms first so "VOSTFR" never matches as "VO" + "STFR".
const termPattern = new RegExp(
  `(?<![\\p{L}\\p{N}_])(${Object.keys(definitions)
    .sort((left, right) => right.length - left.length)
    .map((term) => term.replace(' ', '[ _]'))
    .join('|')})(?![\\p{L}\\p{N}_])`,
  'gu',
)

const terms: readonly string[] = Object.keys(definitions)

function normalizedTerm(value: string): LanguageTerm | null {
  const term = value.replace('_', ' ')
  return isLanguageTerm(term) ? term : null
}

function isLanguageTerm(value: string): value is LanguageTerm {
  return terms.includes(value)
}

export function languageDefinition(value: string): string | null {
  const term = normalizedTerm(value.trim())
  return term ? definitions[term] : null
}

export function splitLanguageTerms(text: string): LanguageTextSegment[] {
  const segments: LanguageTextSegment[] = []
  let cursor = 0
  for (const match of text.matchAll(termPattern)) {
    const term = normalizedTerm(match[0])
    if (!term) continue
    if (match.index > cursor)
      segments.push({ kind: 'text', text: text.slice(cursor, match.index) })
    segments.push({ kind: 'term', text: term, definition: definitions[term] })
    cursor = match.index + match[0].length
  }
  if (cursor < text.length)
    segments.push({ kind: 'text', text: text.slice(cursor) })
  return segments
}

// Native <option> elements cannot host markup, so they get a plain title.
export function languageOptionTitle(label: string): string | undefined {
  const definitions = splitLanguageTerms(label).flatMap((segment) =>
    segment.kind === 'term' ? [`${segment.text} : ${segment.definition}`] : [],
  )
  return definitions.length ? definitions.join(' · ') : undefined
}
