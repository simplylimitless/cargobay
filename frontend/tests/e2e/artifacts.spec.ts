import { test, expect } from '@playwright/test'

test.describe('Artifact Browse', () => {
  test.beforeEach(async ({ page }) => {
    // Login before each test
    await page.goto('/login')
    await page.getByLabel(/username/i).fill('admin')
    await page.getByLabel(/password/i).fill('admin')
    await page.getByRole('button', { name: /log in/i }).click()
    await expect(page).toHaveURL(/\/?|\/dashboard/)

    // Navigate to browse
    await page.getByRole('link', { name: /browse/i }).click()
  })

  test('shows artifact list', async ({ page }) => {
    await expect(page.getByText(/Artifacts/i)).toBeVisible()
  })

  test('shows artifact type filters', async ({ page }) => {
    await expect(page.getByText(/docker/i)).toBeVisible()
    await expect(page.getByText(/npm/i)).toBeVisible()
    await expect(page.getByText(/maven/i)).toBeVisible()
  })

  test('displays artifact cards', async ({ page }) => {
    await expect(page.locator('.artifact-card')).toBeVisible()
  })

  test('shows artifact details on click', async ({ page }) => {
    const firstArtifact = page.locator('.artifact-card').first()
    await firstArtifact.click()

    await expect(page).toHaveURL(/\/artifact\/.+/)
    await expect(page.getByText(/Details/i)).toBeVisible()
  })

  test('shows version history', async ({ page }) => {
    await expect(page.getByText(/version/i)).toBeVisible()
  })
})

test.describe('Artifact Search', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/login')
    await page.getByLabel(/username/i).fill('admin')
    await page.getByLabel(/password/i).fill('admin')
    await page.getByRole('button', { name: /log in/i }).click()
    await expect(page).toHaveURL(/\/?|\/dashboard/)
    await page.getByRole('link', { name: /browse/i }).click()
  })

  test('searches artifacts by name', async ({ page }) => {
    const searchInput = page.getByPlaceholder(/Search artifacts/i)
    await searchInput.fill('nginx')
    await page.keyboard.press('Enter')

    await expect(page.getByText(/nginx/i)).toBeVisible()
  })

  test('shows search results count', async ({ page }) => {
    const searchInput = page.getByPlaceholder(/Search artifacts/i)
    await searchInput.fill('test')
    await page.keyboard.press('Enter')

    await expect(page.getByText(/results/i)).toBeVisible()
  })

  test('clears search results', async ({ page }) => {
    const searchInput = page.getByPlaceholder(/Search artifacts/i)
    await searchInput.fill('nonexistent')
    await page.keyboard.press('Enter')

    await expect(page.getByText(/no results/i)).toBeVisible()
  })

  test('filters by namespace', async ({ page }) => {
    await page.getByPlaceholder(/Search artifacts/i).fill('library')
    await page.keyboard.press('Enter')

    await expect(page.getByText(/library/i)).toBeVisible()
  })
})

test.describe('Artifact Upload', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/login')
    await page.getByLabel(/username/i).fill('admin')
    await page.getByLabel(/password/i).fill('admin')
    await page.getByRole('button', { name: /log in/i }).click()
    await expect(page).toHaveURL(/\/?|\/dashboard/)
  })

  test('navigates to upload page', async ({ page }) => {
    await page.getByRole('link', { name: /upload/i }).click()
    await expect(page.getByText(/Upload Artifact/i)).toBeVisible()
  })

  test('has upload form', async ({ page }) => {
    await page.getByRole('link', { name: /upload/i }).click()

    await expect(page.getByLabel(/artifact type/i)).toBeVisible()
    await expect(page.getByLabel(/artifact name/i)).toBeVisible()
    await expect(page.getByLabel(/version/i)).toBeVisible()
    await expect(page.getByLabel(/file/i)).toBeVisible()
  })

  test('validates required fields', async ({ page }) => {
    await page.getByRole('link', { name: /upload/i }).click()
    await page.getByRole('button', { name: /upload/i }).click()

    await expect(page.getByText(/required/i)).toBeVisible()
  })

  test('shows upload progress', async ({ page }) => {
    await page.getByRole('link', { name: /upload/i }).click()

    const fileInput = page.getByLabel(/file/i)
    const fileInputHandle = await fileInput.elementHandle()
    await fileInputHandle!.setInputFiles('./tests/fixtures/test-artifact.tar.gz')

    await expect(page.getByText(/uploading/i)).toBeVisible()
  })

  test('handles upload errors', async ({ page }) => {
    await page.getByRole('link', { name: /upload/i }).click()

    // Try to upload invalid file
    const fileInput = page.getByLabel(/file/i)
    const fileInputHandle = await fileInput.elementHandle()
    await fileInputHandle!.setInputFiles('./tests/fixtures/invalid-file.txt')

    await expect(page.getByText(/error|failed/i)).toBeVisible()
  })

  test('shows success message after upload', async ({ page }) => {
    await page.getByRole('link', { name: /upload/i }).click()

    const fileInput = page.getByLabel(/file/i)
    const fileInputHandle = await fileInput.elementHandle()
    await fileInputHandle!.setInputFiles('./tests/fixtures/test-artifact.tar.gz')

    await expect(page.getByText(/success/i)).toBeVisible()
  })
})

test.describe('Artifact Versions', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/login')
    await page.getByLabel(/username/i).fill('admin')
    await page.getByLabel(/password/i).fill('admin')
    await page.getByRole('button', { name: /log in/i }).click()
    await expect(page).toHaveURL(/\/?|\/dashboard/)
  })

  test('shows all versions for artifact', async ({ page }) => {
    await page.getByRole('link', { name: /browse/i }).click()

    // Click on an artifact with multiple versions
    const artifactCard = page.locator('.artifact-card').first()
    await artifactCard.click()

    await expect(page.getByText(/versions/i)).toBeVisible()
  })

  test('displays version metadata', async ({ page }) => {
    await page.getByRole('link', { name: /browse/i }).click()
    const artifactCard = page.locator('.artifact-card').first()
    await artifactCard.click()

    await expect(page.getByText(/size/i)).toBeVisible()
    await expect(page.getByText(/digest/i)).toBeVisible()
    await expect(page.getByText(/created/i)).toBeVisible()
  })

  test('allows version deletion', async ({ page }) => {
    await page.getByRole('link', { name: /browse/i }).click()
    const artifactCard = page.locator('.artifact-card').first()
    await artifactCard.click()

    await expect(page.getByRole('button', { name: /delete/i })).toBeVisible()
  })

  test('shows confirmation before deletion', async ({ page }) => {
    await page.getByRole('link', { name: /browse/i }).click()
    const artifactCard = page.locator('.artifact-card').first()
    await artifactCard.click()

    await page.getByRole('button', { name: /delete/i }).click()
    await expect(page.getByText(/confirm|delete/i)).toBeVisible()
  })

  test('cancels deletion', async ({ page }) => {
    await page.getByRole('link', { name: /browse/i }).click()
    const artifactCard = page.locator('.artifact-card').first()
    await artifactCard.click()

    await page.getByRole('button', { name: /delete/i }).click()
    await page.getByRole('button', { name: /cancel/i }).click()
    await expect(page.getByText(/confirm/i)).not.toBeVisible()
  })
})

test.describe('Artifact Details', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/login')
    await page.getByLabel(/username/i).fill('admin')
    await page.getByLabel(/password/i).fill('admin')
    await page.getByRole('button', { name: /log in/i }).click()
    await expect(page).toHaveURL(/\/?|\/dashboard/)
    await page.getByRole('link', { name: /browse/i }).click()
  })

  test('shows artifact metadata', async ({ page }) => {
    const artifactCard = page.locator('.artifact-card').first()
    await artifactCard.click()

    await expect(page.getByText(/registry/i)).toBeVisible()
    await expect(page.getByText(/namespace/i)).toBeVisible()
    await expect(page.getByText(/artifact/i)).toBeVisible()
  })

  test('displays pull command', async ({ page }) => {
    const artifactCard = page.locator('.artifact-card').first()
    await artifactCard.click()

    await expect(page.getByText(/docker pull/i)).toBeVisible()
  })

  test('allows copying pull command', async ({ page }) => {
    const artifactCard = page.locator('.artifact-card').first()
    await artifactCard.click()

    await page.getByRole('button', { name: /copy/i }).click()
    // Clipboard should have the pull command
  })

  test('shows artifact signatures', async ({ page }) => {
    const artifactCard = page.locator('.artifact-card').first()
    await artifactCard.click()

    await expect(page.getByText(/signature/i)).toBeVisible()
  })

  test('shows multi-arch manifests', async ({ page }) => {
    const artifactCard = page.locator('.artifact-card').first()
    await artifactCard.click()

    await expect(page.getByText(/os/i)).toBeVisible()
    await expect(page.getByText(/arch/i)).toBeVisible()
  })
})

test.describe('Pagination', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/login')
    await page.getByLabel(/username/i).fill('admin')
    await page.getByLabel(/password/i).fill('admin')
    await page.getByRole('button', { name: /log in/i }).click()
    await expect(page).toHaveURL(/\/?|\/dashboard/)
    await page.getByRole('link', { name: /browse/i }).click()
  })

  test('shows pagination controls', async ({ page }) => {
    await expect(page.getByRole('button', { name: /previous/i })).toBeVisible()
    await expect(page.getByRole('button', { name: /next/i })).toBeVisible()
  })

  test('navigates between pages', async ({ page }) => {
    const nextButton = page.getByRole('button', { name: /next/i })
    const currentPage = await page.getByText(/page 1/i).count()

    await nextButton.click()

    // Should show page 2 content
    await expect(page.getByText(/page 2/i)).toBeVisible()
  })

  test('shows total count', async ({ page }) => {
    await expect(page.getByText(/of [0-9]+/i)).toBeVisible()
  })
})

test.describe('Sorting', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/login')
    await page.getByLabel(/username/i).fill('admin')
    await page.getByLabel(/password/i).fill('admin')
    await page.getByRole('button', { name: /log in/i }).click()
    await expect(page).toHaveURL(/\/?|\/dashboard/)
    await page.getByRole('link', { name: /browse/i }).click()
  })

  test('sorts by size', async ({ page }) => {
    await page.getByRole('button', { name: /size/i }).click()

    const sizes = await page.locator('.artifact-size').allTextContents()
    const parsedSizes = sizes.map((s) => parseFloat(s))
    expect(parsedSizes).toBeSorted()
  })

  test('sorts by date', async ({ page }) => {
    await page.getByRole('button', { name: /date/i }).click()

    const dates = await page.locator('.artifact-date').allTextContents()
    expect(dates).toHaveLength.greaterThan(0)
  })
})

test.describe('Filters', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/login')
    await page.getByLabel(/username/i).fill('admin')
    await page.getByLabel(/password/i).fill('admin')
    await page.getByRole('button', { name: /log in/i }).click()
    await expect(page).toHaveURL(/\/?|\/dashboard/)
    await page.getByRole('link', { name: /browse/i }).click()
  })

  test('filters by artifact type', async ({ page }) => {
    await page.getByRole('button', { name: /docker/i }).click()

    await expect(page.getByText(/docker/i)).toBeVisible()
  })

  test('filters by namespace', async ({ page }) => {
    await page.getByRole('button', { name: /library/i }).click()

    await expect(page.getByText(/library/i)).toBeVisible()
  })

  test('clears all filters', async ({ page }) => {
    await page.getByRole('button', { name: /docker/i }).click()
    await page.getByRole('button', { name: /library/i }).click()

    await page.getByRole('button', { name: /clear/i }).click()

    await expect(page.getByText(/All/i)).toBeVisible()
  })
})
