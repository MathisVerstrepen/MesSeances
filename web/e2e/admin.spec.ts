import { test, expect, openPage } from './fixtures'
import type { Page } from '@playwright/test'
import type { AdminTMDBRerunJob } from '../app/types/api'

async function mockRerun(page: Page, loseStartResponse = false) {
  let job: AdminTMDBRerunJob | null = null
  let statusGets = 0
  let matchGets = 0
  let starts = 0
  await page.route('**/api/v1/admin/**', async (route) => {
    const path = new URL(route.request().url()).pathname
    if (path.endsWith('/session'))
      return route.fulfill({ json: { authenticated: true } })
    if (path.endsWith('/tmdb-matches/rerun')) {
      const starting = route.request().method() === 'POST'
      if (starting) {
        starts++
        job = {
          state: 'running',
          started_at: '2026-11-06T12:00:00Z',
          finished_at: null,
          summary: null,
        }
        if (loseStartResponse) return route.abort('failed')
      } else statusGets++
      return route.fulfill({ status: starting ? 202 : 200, json: { job } })
    }
    if (path.endsWith('/tmdb-matches/refresh-metadata'))
      return route.fulfill({ json: { job: null } })
    if (
      path.endsWith('/tmdb-matches') ||
      path.endsWith('/local-movie-groups')
    ) {
      if (path.endsWith('/tmdb-matches')) matchGets++
      return route.fulfill({ json: { items: [], limit: 20, offset: 0 } })
    }
    throw new Error(`Unexpected admin endpoint: ${path}`)
  })
  return {
    setJob: (next: AdminTMDBRerunJob) => {
      job = next
    },
    statusGets: () => statusGets,
    matchGets: () => matchGets,
    starts: () => starts,
  }
}

test('unresolved rerun recovers a lost start response, resumes polling after reload and shows completion', async ({
  page,
}) => {
  const mock = await mockRerun(page, true)
  await openPage(page, '/admin/tmdb-matches')
  await page
    .getByRole('button', {
      name: 'Relancer les films non résolus',
      exact: true,
    })
    .click()
  const running = page.getByRole('button', {
    name: 'Relance en cours…',
    exact: true,
  })
  await expect(running).toBeDisabled()
  await expect.poll(mock.statusGets).toBeGreaterThan(1)
  await openPage(page, '/admin/tmdb-matches')
  await expect(running).toBeDisabled()
  await expect(
    page.getByRole('button', {
      name: 'Actualiser toutes les métadonnées TMDB',
      exact: true,
    }),
  ).toBeDisabled()
  const before = mock.matchGets()
  mock.setJob({
    state: 'succeeded',
    started_at: '2026-11-06T12:00:00Z',
    finished_at: '2026-11-06T12:05:00Z',
    summary: {
      processed: 5,
      matched: 2,
      review_required: 1,
      unmatched: 1,
      reused: 0,
      failed: 1,
    },
  })
  await expect(
    page.getByText('Relance terminée avec des erreurs.', { exact: true }),
  ).toBeVisible()
  await expect(page.getByText(/5 traité\(s\), 2 associé\(s\)/)).toBeVisible()
  await expect.poll(mock.matchGets).toBe(before + 3)
  await expect(
    page.getByRole('button', {
      name: 'Relancer les films non résolus',
      exact: true,
    }),
  ).toBeEnabled()
  expect(mock.starts()).toBe(1)
})

test('unresolved rerun shows a safe failure and reloads partial matches', async ({
  page,
}) => {
  const mock = await mockRerun(page)
  await openPage(page, '/admin/tmdb-matches')
  await page
    .getByRole('button', {
      name: 'Relancer les films non résolus',
      exact: true,
    })
    .click()
  await expect(
    page.getByRole('button', { name: 'Relance en cours…', exact: true }),
  ).toBeDisabled()
  const before = mock.matchGets()
  mock.setJob({
    state: 'failed',
    started_at: '2026-11-06T12:00:00Z',
    finished_at: '2026-11-06T12:05:00Z',
    summary: null,
    error_code: 'rerun_failed',
  })
  await expect(
    page.getByText('La relance TMDB a échoué. Réessayez plus tard.', {
      exact: true,
    }),
  ).toBeVisible()
  await expect.poll(mock.matchGets).toBe(before + 3)
  await expect(
    page.getByRole('button', {
      name: 'Relancer les films non résolus',
      exact: true,
    }),
  ).toBeEnabled()
  await expect(
    page.getByText('Relance terminée.', { exact: true }),
  ).toHaveCount(0)
})

test('admin login rejects synthetic credentials and stays anonymous', async ({
  page,
}) => {
  await openPage(page, '/admin/login')
  await expect(page.getByRole('heading', { level: 1 })).toContainText(
    'administrateur',
  )
  const submit = page.getByRole('button', { name: 'Se connecter', exact: true })
  await expect(submit).toBeDisabled()
  await page
    .getByLabel('Mot de passe', { exact: true })
    .fill('synthetic-invalid-password')
  await submit.click()
  await expect(page.getByRole('alert')).toBeVisible()
  await expect(submit).toBeEnabled()
  await expect(page).toHaveURL(/\/admin\/login$/)
})
