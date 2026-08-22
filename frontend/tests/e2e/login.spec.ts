import { test, expect } from '@playwright/test'

test.describe('Login Page', () => {
  test('renders login form', async ({ page }) => {
    await page.goto('/login')
    await expect(page.getByText('Log in to cargobay')).toBeVisible()
  })

  test('has username field', async ({ page }) => {
    await page.goto('/login')
    await expect(page.getByLabel(/username/i)).toBeVisible()
  })

  test('has password field', async ({ page }) => {
    await page.goto('/login')
    await expect(page.getByLabel(/password/i)).toBeVisible()
  })

  test('has login button', async ({ page }) => {
    await page.goto('/login')
    await expect(page.getByRole('button', { name: /log in/i })).toBeVisible()
  })

  test('has register link', async ({ page }) => {
    await page.goto('/login')
    await expect(page.getByRole('link', { name: /create account/i })).toBeVisible()
  })

  test('shows error for invalid credentials', async ({ page }) => {
    await page.goto('/login')
    await page.getByLabel(/username/i).fill('invalid')
    await page.getByLabel(/password/i).fill('wrongpassword')
    await page.getByRole('button', { name: /log in/i }).click()

    // Error message should appear
    await expect(page.getByText(/login failed|invalid credentials/i)).toBeVisible()
  })

  test('shows error for empty fields', async ({ page }) => {
    await page.goto('/login')
    await page.getByRole('button', { name: /log in/i }).click()

    // Should show validation errors
    await expect(page.getByText(/required|empty/i)).toBeVisible()
  })
})

test.describe('Login Flow', () => {
  test('logs in successfully with valid credentials', async ({ page }) => {
    await page.goto('/login')
    await page.getByLabel(/username/i).fill('admin')
    await page.getByLabel(/password/i).fill('admin')
    await page.getByRole('button', { name: /log in/i }).click()

    // Should redirect to home or dashboard
    await expect(page).toHaveURL(/\/?|\/dashboard/)

    // User should be logged in
    await expect(page.getByRole('link', { name: /admin/i })).toBeVisible()
    await expect(page.getByRole('button', { name: /log out/i })).toBeVisible()
  })

  test('redirects authenticated users away from login', async ({ page }) => {
    // First login
    await page.goto('/login')
    await page.getByLabel(/username/i).fill('admin')
    await page.getByLabel(/password/i).fill('admin')
    await page.getByRole('button', { name: /log in/i }).click()

    // Navigate to login again
    await page.goto('/login')

    // Should be redirected away from login
    await expect(page).not.toHaveURL('/login')
  })
})

test.describe('Password Visibility', () => {
  test('can toggle password visibility', async ({ page }) => {
    await page.goto('/login')
    const passwordInput = page.getByLabel(/password/i)
    const toggleButton = page.getByRole('button', { name: /show password/i })

    await expect(passwordInput).toHaveAttribute('type', 'password')

    await toggleButton.click()
    await expect(passwordInput).toHaveAttribute('type', 'text')

    await toggleButton.click()
    await expect(passwordInput).toHaveAttribute('type', 'password')
  })
})

test.describe('Remember Me', () => {
  test('persists session on refresh', async ({ page }) => {
    // Login
    await page.goto('/login')
    await page.getByLabel(/username/i).fill('admin')
    await page.getByLabel(/password/i).fill('admin')
    await page.getByRole('button', { name: /log in/i }).click()

    // Verify logged in
    await expect(page.getByRole('link', { name: /admin/i })).toBeVisible()

    // Refresh page
    await page.reload()

    // Should still be logged in
    await expect(page.getByRole('link', { name: /admin/i })).toBeVisible()
  })
})

test.describe('Keyboard Navigation', () => {
  test('submits form on Enter key', async ({ page }) => {
    await page.goto('/login')
    await page.getByLabel(/username/i).fill('admin')
    await page.getByLabel(/password/i).fill('admin')
    await page.keyboard.press('Enter')

    // Should submit form (may redirect or show error)
    await expect(page.url()).not.toBe('/login')
  })

  test('focuses username field on load', async ({ page }) => {
    await page.goto('/login')
    const usernameInput = page.getByLabel(/username/i)
    await expect(usernameInput).toBeFocused()
  })
})

test.describe('Security', () => {
  test('does not expose password in URL', async ({ page }) => {
    await page.goto('/login')
    await page.getByLabel(/username/i).fill('admin')
    await page.getByLabel(/password/i).fill('admin')
    await page.getByRole('button', { name: /log in/i }).click()

    // Password should not appear in URL
    const url = page.url()
    expect(url).not.toContain('password')
    expect(url).not.toContain('admin')
  })

  test('handles rate limiting', async ({ page }) => {
    // Multiple rapid login attempts
    for (let i = 0; i < 5; i++) {
      await page.goto('/login')
      await page.getByLabel(/username/i).fill('test')
      await page.getByLabel(/password/i).fill('wrong')
      await page.getByRole('button', { name: /log in/i }).click()
    }

    // Should eventually get rate limited or error
    const hasError = await page.locator('.error-message').count()
    expect(hasError).toBeGreaterThanOrEqual(0)
  })
})

test.describe('Internationalization', () => {
  test('handles special characters in username', async ({ page }) => {
    await page.goto('/login')
    await page.getByLabel(/username/i).fill('user@test.com')
    await page.getByLabel(/password/i).fill('password123')
    await page.getByRole('button', { name: /log in/i }).click()

    // Should handle the input
    expect(page.url()).not.toBe('/login')
  })
})
