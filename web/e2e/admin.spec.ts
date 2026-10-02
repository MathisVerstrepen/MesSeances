import { test, expect, openPage } from './fixtures'

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
