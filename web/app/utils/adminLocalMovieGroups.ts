import type {
  AdminLocalMovieGroup,
  AdminLocalMovieGroupsResponse,
} from '../types/api'

export const LOCAL_GROUP_CATALOG_PAGE_SIZE = 100

export function normalizeLocalMovieTitle(
  title: string | null | undefined,
): string {
  return (title ?? '')
    .normalize('NFD')
    .replace(/\p{Diacritic}/gu, '')
    .toLowerCase()
    .replace(/œ/g, 'oe')
    .replace(/æ/g, 'ae')
    .replace(/[^\p{L}\p{N}]+/gu, ' ')
    .trim()
}

function titleSignature(title: string | null | undefined) {
  const normalized = normalizeLocalMovieTitle(title)
  const characters = Array.from(normalized)
  const pairs = new Map<string, number>()
  for (let index = 1; index < characters.length; index += 1) {
    const pair = characters[index - 1]! + characters[index]!
    pairs.set(pair, (pairs.get(pair) ?? 0) + 1)
  }
  return { normalized, pairs, size: Math.max(0, characters.length - 1) }
}

type TitleSignature = ReturnType<typeof titleSignature>

// Multiset Sørensen-Dice similarity. Empty titles never count as a match.
function signatureSimilarity(
  left: TitleSignature,
  right: TitleSignature,
): number {
  if (!left.normalized || !right.normalized) return 0
  if (left.normalized === right.normalized) return 1
  if (!left.size || !right.size) return 0
  let shared = 0
  for (const [pair, count] of left.pairs) {
    shared += Math.min(count, right.pairs.get(pair) ?? 0)
  }
  return (2 * shared) / (left.size + right.size)
}

export function localMovieTitleSimilarity(
  left: string | null | undefined,
  right: string | null | undefined,
): number {
  return signatureSimilarity(titleSignature(left), titleSignature(right))
}

export function localMovieGroupTitle(group: AdminLocalMovieGroup): string {
  const preferred = group.metadata_source ?? group.primary
  return (
    group.members.find(
      (member) =>
        member.source_provider === preferred.source_provider &&
        member.source_movie_id === preferred.source_movie_id &&
        member.source_title?.trim(),
    )?.source_title ??
    group.members.find((member) => member.source_title?.trim())?.source_title ??
    group.local_movie_id
  )
}

function compareText(left: string, right: string): number {
  return left < right ? -1 : left > right ? 1 : 0
}

export function rankLocalMovieGroups(
  groups: readonly AdminLocalMovieGroup[],
  selectedTitles: readonly string[],
) {
  const selected = selectedTitles.map(titleSignature)
  return groups
    .map((group) => {
      const members = group.members.map((member) =>
        titleSignature(member.source_title),
      )
      const title = localMovieGroupTitle(group)
      const score = selected.length
        ? selected.reduce(
            (sum, selection) =>
              sum +
              members.reduce(
                (best, member) =>
                  Math.max(best, signatureSimilarity(selection, member)),
                0,
              ),
            0,
          ) / selected.length
        : 0
      return { group, title, score, sortTitle: normalizeLocalMovieTitle(title) }
    })
    .sort(
      (left, right) =>
        right.score - left.score ||
        compareText(left.sortTitle, right.sortTitle) ||
        compareText(left.group.local_movie_id, right.group.local_movie_id),
    )
}

// No total is available: exact multiples require an empty terminal page.
// Cancellation checks both sides of every await, preventing stale publication
// and further pagination after refresh or unmount. Errors discard partial data.
export async function loadLocalMovieGroupCatalog(
  fetchPage: (
    limit: number,
    offset: number,
  ) => Promise<AdminLocalMovieGroupsResponse>,
  isCurrent: () => boolean,
  onProgress: (count: number) => void = () => {},
): Promise<AdminLocalMovieGroup[] | null> {
  const groups = new Map<string, AdminLocalMovieGroup>()
  for (let offset = 0; isCurrent(); offset += LOCAL_GROUP_CATALOG_PAGE_SIZE) {
    const response = await fetchPage(LOCAL_GROUP_CATALOG_PAGE_SIZE, offset)
    if (!isCurrent()) return null
    for (const group of response.items) groups.set(group.local_movie_id, group)
    onProgress(groups.size)
    if (response.items.length < LOCAL_GROUP_CATALOG_PAGE_SIZE) {
      return [...groups.values()]
    }
  }
  return null
}
