import { test, expect } from '@playwright/test'

test.describe('Home Page', () => {
  test('has title', async ({ page }) => {
    await page.goto('/')
    await expect(page).toHaveTitle(/cargobay/)
  })

  test('renders logo', async ({ page }) => {
    await page.goto('/')
    await expect(page.getByText('cargobay')).toBeVisible()
  })

  test('has navigation', async ({ page }) => {
    await page.goto('/')
    const nav = page.locator('nav')
    await expect(nav).toBeVisible()
  })
})

test.describe('Authentication', () => {
  test('shows login button when not authenticated', async ({ page }) => {
    await page.goto('/')
    await expect(page.getByRole('link', { name: /log in/i })).toBeVisible()
  })

  test('navigates to login page', async ({ page }) => {
    await page.goto('/')
    await page.getByRole('link', { name: /log in/i }).click()
    await expect(page).toHaveURL('/login')
    await expect(page.getByText('Log in to cargobay')).toBeVisible()
  })
})

test.describe('Search', () => {
  test('has search input on home page', async ({ page }) => {
    await page.goto('/')
    await expect(page.getByPlaceholder(/Search artifacts/i)).toBeVisible()
  })

  test('navigates to search results', async ({ page }) => {
    await page.goto('/')
    await page.getByPlaceholder(/Search artifacts/i).fill('nginx')
    await page.keyboard.press('Enter')
    await expect(page).toHaveURL(/search/)
    await expect(page.getByText(/Search results/i)).toBeVisible()
  })
})

test.describe('Registry Browsing', () => {
  test('shows registries link in navigation', async ({ page }) => {
    await page.goto('/')
    // After login
    await expect(page.getByRole('link', { name: /browse/i })).toBeVisible()
  })
})

test.describe('User Profile', () => {
  test('shows profile link when logged in', async ({ page }) => {
    await page.goto('/')
    // Profile should be visible after login
    await expect(page.getByRole('link', { name: /testuser/i })).toBeVisible()
  })

  test('navigates to profile page', async ({ page }) => {
    await page.goto('/')
    await page.getByRole('link', { name: /profile/i }).click()
    await expect(page).toHaveURL('/profile')
    await expect(page.getByText('User Profile')).toBeVisible()
  })
})

test.describe('Settings', () => {
  test('shows settings link for admin', async ({ page }) => {
    await page.goto('/')
    // Settings should be visible for admin users
    await expect(page.getByRole('link', { name: /settings/i })).toBeVisible()
  })

  test('navigates to settings page', async ({ page }) => {
    await page.goto('/')
    await page.getByRole('link', { name: /settings/i }).click()
    await expect(page).toHaveURL('/settings')
    await expect(page.getByText('Settings')).toBeVisible()
  })
})

test.describe('Getting Started', () => {
  test('has guide link', async ({ page }) => {
    await page.goto('/')
    await expect(page.getByRole('link', { name: /guide/i })).toBeVisible()
  })

  test('navigates to getting started page', async ({ page }) => {
    await page.goto('/')
    await page.getByRole('link', { name: /guide/i }).click()
    await expect(page).toHaveURL('/getting-started')
    await expect(page.getByText('Getting Started')).toBeVisible()
  })
})

test.describe('Error Handling', () => {
  test('shows 404 for unknown routes', async ({ page }) => {
    await page.goto('/nonexistent-route')
    await expect(page.getByText(/not found/i)).toBeVisible()
  })
})

test.describe('Responsive Design', () => {
  test('works on mobile viewport', async ({ page }) => {
    await page.setViewportSize({ width: 375, height: 667 })
    await page.goto('/')
    await expect(page.getByText('cargobay')).toBeVisible()
  })

  test('works on tablet viewport', async ({ page }) => {
    await page.setViewportSize({ width: 768, height: 1024 })
    await page.goto('/')
    await expect(page.getByText('cargobay')).toBeVisible()
  })

  test('works on desktop viewport', async ({ page }) => {
    await page.setViewportSize({ width: 1920, height: 1080 })
    await page.goto('/')
    await expect(page.getByText('cargobay')).toBeVisible()
  })
})

test.describe('Performance', () => {
  test('loads quickly', async ({ page }) => {
    const startTime = Date.now()
    await page.goto('/')
    const loadTime = Date.now() - startTime
    // Should load within 3 seconds
    expect(loadTime).toBeLessThan(3000)
  })
})
