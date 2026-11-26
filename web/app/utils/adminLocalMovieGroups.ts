import type {
  AdminLocalMovieGroup,
  AdminLocalMovieGroupsResponse,
  AdminLocalMovieSource,
  AdminPendingMatch,
  AdminPendingMatchesResponse,
} from '../types/api'

export const LOCAL_GROUP_CATALOG_PAGE_SIZE = 100
export const MISSED_LOCAL_GROUPING_THRESHOLDS = [0.7, 0.8, 0.9] as const
export const DEFAULT_MISSED_LOCAL_GROUPING_THRESHOLD = 0.8
// Source runtimes differ by trailers/ads; beyond this gap the admin is warned.
export const MISSED_LOCAL_GROUPING_RUNTIME_GAP_MINUTES = 15

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

// Same contract as the group catalog loader, for every film of one review status.
export async function loadPendingMatchCatalog(
  fetchPage: (
    limit: number,
    offset: number,
  ) => Promise<AdminPendingMatchesResponse>,
  isCurrent: () => boolean,
  onProgress: (count: number) => void = () => {},
): Promise<AdminPendingMatch[] | null> {
  const matches = new Map<string, AdminPendingMatch>()
  for (let offset = 0; isCurrent(); offset += LOCAL_GROUP_CATALOG_PAGE_SIZE) {
    const response = await fetchPage(LOCAL_GROUP_CATALOG_PAGE_SIZE, offset)
    if (!isCurrent()) return null
    for (const match of response.items)
      matches.set(missedSourceKey(match), match)
    onProgress(matches.size)
    if (response.items.length < LOCAL_GROUP_CATALOG_PAGE_SIZE) {
      return [...matches.values()]
    }
  }
  return null
}

export type MissedLocalGroupingSource = AdminLocalMovieSource & {
  source_title: string
  source_runtime_minutes?: number | null
}

export interface MissedLocalGroupingMember<
  T extends MissedLocalGroupingSource,
> {
  source: T
  // Best title similarity to another suggested film or to the existing group.
  similarity: number
}

export interface MissedLocalGrouping<T extends MissedLocalGroupingSource> {
  key: string
  members: MissedLocalGroupingMember<T>[]
  // Existing group the films should join, or null for a new group.
  group: AdminLocalMovieGroup | null
  groupTitle: string | null
  score: number
  runtimeGap: number | null
}

function missedSourceKey(source: AdminLocalMovieSource): string {
  return `${source.source_provider}:${source.source_movie_id}`
}

function runtimeGap(sources: readonly MissedLocalGroupingSource[]) {
  const runtimes = sources
    .map((source) => source.source_runtime_minutes ?? 0)
    .filter((runtime) => runtime > 0)
  return runtimes.length >= 2
    ? Math.max(...runtimes) - Math.min(...runtimes)
    : null
}

// Dice is bounded by the smaller bigram multiset; skip hopeless pairs cheaply.
function bestSignatureSimilarity(
  left: readonly TitleSignature[],
  right: readonly TitleSignature[],
  threshold: number,
): number {
  let best = 0
  for (const leftSignature of left) {
    if (!leftSignature.normalized) continue
    for (const rightSignature of right) {
      if (!rightSignature.normalized) continue
      const bound =
        leftSignature.normalized === rightSignature.normalized
          ? 1
          : (2 * Math.min(leftSignature.size, rightSignature.size)) /
            (leftSignature.size + rightSignature.size || 1)
      if (bound < threshold || bound <= best) continue
      best = Math.max(best, signatureSimilarity(leftSignature, rightSignature))
      if (best === 1) return best
    }
  }
  return best
}

// Connected components of items linked by a best title similarity >= threshold.
// `best[i]` is item i's strongest link; components keep input order.
function similarityComponents(
  items: readonly (readonly TitleSignature[])[],
  threshold: number,
) {
  const parent = items.map((_, index) => index)
  const find = (index: number): number => {
    while (parent[index] !== index) {
      parent[index] = parent[parent[index]!]!
      index = parent[index]!
    }
    return index
  }
  const best = items.map(() => 0)
  for (let left = 0; left < items.length; left += 1) {
    for (let right = left + 1; right < items.length; right += 1) {
      const similarity = bestSignatureSimilarity(
        items[left]!,
        items[right]!,
        threshold,
      )
      if (similarity < threshold) continue
      best[left] = Math.max(best[left]!, similarity)
      best[right] = Math.max(best[right]!, similarity)
      parent[find(left)] = find(right)
    }
  }
  const components = new Map<number, number[]>()
  items.forEach((_, index) => {
    const root = find(index)
    components.set(root, [...(components.get(root) ?? []), index])
  })
  return { components: [...components.values()], best }
}

// Ungrouped films whose titles are similar enough to each other (transitively)
// or to an existing local group. Each film appears in at most one suggestion.
export function findMissedLocalGroupings<T extends MissedLocalGroupingSource>(
  sources: readonly T[],
  groups: readonly AdminLocalMovieGroup[],
  threshold = DEFAULT_MISSED_LOCAL_GROUPING_THRESHOLD,
): MissedLocalGrouping<T>[] {
  const unique = [
    ...new Map(
      sources.map((source) => [missedSourceKey(source), source]),
    ).values(),
  ]
  const signatures = unique.map((source) => titleSignature(source.source_title))
  const { components: clustered, best: internalBest } = similarityComponents(
    signatures.map((signature) => [signature]),
    threshold,
  )

  const groupSignatures = groups.map((group) => ({
    group,
    title: localMovieGroupTitle(group),
    members: group.members.map((member) => titleSignature(member.source_title)),
  }))
  const suggestions: MissedLocalGrouping<T>[] = []
  for (const indexes of clustered) {
    let bestGroup: {
      entry: (typeof groupSignatures)[number]
      score: number
      similarities: number[]
    } | null = null
    for (const entry of groupSignatures) {
      const similarities = indexes.map((index) =>
        entry.members.reduce(
          (best, member) =>
            Math.max(best, signatureSimilarity(signatures[index]!, member)),
          0,
        ),
      )
      const score =
        similarities.reduce((sum, value) => sum + value, 0) / indexes.length
      if (
        score >= threshold &&
        (!bestGroup ||
          score > bestGroup.score ||
          (score === bestGroup.score &&
            compareText(
              entry.group.local_movie_id,
              bestGroup.entry.group.local_movie_id,
            ) < 0))
      ) {
        bestGroup = { entry, score, similarities }
      }
    }
    if (indexes.length < 2 && !bestGroup) continue
    const members = indexes
      .map((index, position) => ({
        source: unique[index]!,
        similarity: Math.max(
          internalBest[index]!,
          bestGroup?.similarities[position] ?? 0,
        ),
      }))
      .sort(
        (left, right) =>
          compareText(
            normalizeLocalMovieTitle(left.source.source_title),
            normalizeLocalMovieTitle(right.source.source_title),
          ) ||
          compareText(
            missedSourceKey(left.source),
            missedSourceKey(right.source),
          ),
      )
    const score = bestGroup
      ? bestGroup.score
      : members.reduce((sum, member) => sum + member.similarity, 0) /
        members.length
    suggestions.push({
      key: [
        bestGroup?.entry.group.local_movie_id ?? 'new',
        ...members.map((member) => missedSourceKey(member.source)),
      ].join('|'),
      members,
      group: bestGroup?.entry.group ?? null,
      groupTitle: bestGroup?.entry.title ?? null,
      score,
      runtimeGap: runtimeGap(members.map((member) => member.source)),
    })
  }
  return suggestions.sort(
    (left, right) =>
      right.score - left.score ||
      right.members.length - left.members.length ||
      compareText(left.key, right.key),
  )
}

export interface MissedLocalGroupMerge {
  key: string
  groups: Array<{
    group: AdminLocalMovieGroup
    title: string
    similarity: number
  }>
  score: number
  // Oldest group: its local-film slug is the most likely to be already shared.
  suggestedTarget: AdminLocalMovieGroup
  runtimeGap: number | null
}

function localMovieGroupNumber(group: AdminLocalMovieGroup): number {
  const value = Number(group.local_movie_id.replace(/^local-film-/, ''))
  return Number.isSafeInteger(value) ? value : Number.MAX_SAFE_INTEGER
}

function compareLocalMovieGroups(
  left: AdminLocalMovieGroup,
  right: AdminLocalMovieGroup,
): number {
  return (
    localMovieGroupNumber(left) - localMovieGroupNumber(right) ||
    compareText(left.local_movie_id, right.local_movie_id)
  )
}

// Existing local groups whose member titles look like the same film.
export function findMissedLocalGroupMerges(
  groups: readonly AdminLocalMovieGroup[],
  threshold = DEFAULT_MISSED_LOCAL_GROUPING_THRESHOLD,
): MissedLocalGroupMerge[] {
  const unique = [
    ...new Map(groups.map((group) => [group.local_movie_id, group])).values(),
  ].sort(compareLocalMovieGroups)
  const { components, best } = similarityComponents(
    unique.map((group) =>
      group.members.map((member) => titleSignature(member.source_title)),
    ),
    threshold,
  )
  return components
    .filter((indexes) => indexes.length >= 2)
    .map((indexes) => {
      const entries = indexes.map((index) => ({
        group: unique[index]!,
        title: localMovieGroupTitle(unique[index]!),
        similarity: best[index]!,
      }))
      return {
        key: entries.map(({ group }) => group.local_movie_id).join('|'),
        groups: entries,
        score:
          entries.reduce((sum, entry) => sum + entry.similarity, 0) /
          entries.length,
        suggestedTarget: entries[0]!.group,
        runtimeGap: runtimeGap(
          entries.flatMap(({ group }) =>
            group.members
              .filter((member) => member.available)
              .map((member) => ({
                source_provider: member.source_provider,
                source_movie_id: member.source_movie_id,
                source_title: member.source_title ?? '',
                source_runtime_minutes: member.source_runtime_minutes,
              })),
          ),
        ),
      }
    })
    .sort(
      (left, right) =>
        right.score - left.score ||
        right.groups.length - left.groups.length ||
        compareText(left.key, right.key),
    )
}
