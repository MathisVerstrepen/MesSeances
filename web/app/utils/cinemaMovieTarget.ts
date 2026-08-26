import { withSharedTheaterSelection } from './sharedTheaterSelection.ts'

export function cinemaMovieTarget(movieSlug: string, theaterId: string): string {
  const target = withSharedTheaterSelection(`/film/${encodeURIComponent(movieSlug)}`, [theaterId])
  if (target === null) throw new Error('Invalid cinema movie target')
  return target
}
