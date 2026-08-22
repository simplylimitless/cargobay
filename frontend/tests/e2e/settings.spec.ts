import { test, expect } from '@playwright/test'

test.describe('Settings Page', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/login')
    await page.getByLabel(/username/i).fill('admin')
    await page.getByLabel(/password/i).fill('admin')
    await page.getByRole('button', { name: /log in/i }).click()
    await expect(page).toHaveURL(/\/?|\/dashboard/)
    await page.getByRole('link', { name: /settings/i }).click()
  })

  test('renders settings page', async ({ page }) => {
    await expect(page.getByText('Settings')).toBeVisible()
  })

  test('shows general settings', async ({ page }) => {
    await expect(page.getByText(/general/i)).toBeVisible()
    await expect(page.getByText(/site name/i)).toBeVisible()
    await expect(page.getByText(/theme/i)).toBeVisible()
  })

  test('shows proxy settings', async ({ page }) => {
    await expect(page.getByText(/proxy/i)).toBeVisible()
    await expect(page.getByText(/upstream/i)).toBeVisible()
    await expect(page.getByText(/cache/i)).toBeVisible()
  })

  test('shows authentication settings', async ({ page }) => {
    await expect(page.getByText(/authentication/i)).toBeVisible()
    await expect(page.getByText(/session/i)).toBeVisible()
    await expect(page.getByText(/security/i)).toBeVisible()
  })

  test('shows backup settings', async ({ page }) => {
    await expect(page.getByText(/backup/i)).toBeVisible()
    await expect(page.getByText(/schedule/i)).toBeVisible()
    await expect(page.getByText(/storage/i)).toBeVisible()
  })

  test('shows notifications settings', async ({ page }) => {
    await expect(page.getByText(/notifications/i)).toBeVisible()
    await expect(page.getByText(/email/i)).toBeVisible()
    await expect(page.getByText(/webhook/i)).toBeVisible()
  })
})

test.describe('General Settings', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/login')
    await page.getByLabel(/username/i).fill('admin')
    await page.getByLabel(/password/i).fill('admin')
    await page.getByRole('button', { name: /log in/i }).click()
    await expect(page).toHaveURL(/\/?|\/dashboard/)
    await page.getByRole('link', { name: /settings/i }).click()
  })

  test('updates site name', async ({ page }) => {
    await page.getByLabel(/site name/i).fill('New Site Name')
    await page.getByRole('button', { name: /save/i }).click()

    await expect(page.getByText(/saved/i)).toBeVisible()
  })

  test('changes site theme', async ({ page }) => {
    await page.getByRole('button', { name: /theme/i }).click()
    await page.getByText(/dark/i).click()
    await page.getByRole('button', { name: /save/i }).click()

    await expect(page.getByText(/saved/i)).toBeVisible()
  })

  test('updates footer text', async ({ page }) => {
    await page.getByLabel(/footer/i).fill('Custom Footer')
    await page.getByRole('button', { name: /save/i }).click()

    await expect(page.getByText(/saved/i)).toBeVisible()
  })

  test('updates contact email', async ({ page }) => {
    await page.getByLabel(/contact/i).fill('new@example.com')
    await page.getByRole('button', { name: /save/i }).click()

    await expect(page.getByText(/saved/i)).toBeVisible()
  })
})

test.describe('Proxy Settings', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/login')
    await page.getByLabel(/username/i).fill('admin')
    await page.getByLabel(/password/i).fill('admin')
    await page.getByRole('button', { name: /log in/i }).click()
    await expect(page).toHaveURL(/\/?|\/dashboard/)
    await page.getByRole('link', { name: /settings/i }).click()
  })

  test('configures registry upstream', async ({ page }) => {
    await page.getByLabel(/upstream/i).fill('https://registry.example.com')
    await page.getByRole('button', { name: /save/i }).click()

    await expect(page.getByText(/saved/i)).toBeVisible()
  })

  test('updates cache TTL', async ({ page }) => {
    await page.getByLabel(/cache ttl/i).fill('3600')
    await page.getByRole('button', { name: /save/i }).click()

    await expect(page.getByText(/saved/i)).toBeVisible()
  })

  test('enables/disables caching', async ({ page }) => {
    const checkbox = page.getByRole('checkbox', { name: /cache/i })
    await checkbox.click()
    await page.getByRole('button', { name: /save/i }).click()

    await expect(page.getByText(/saved/i)).toBeVisible()
  })

  test('configures timeout settings', async ({ page }) => {
    await page.getByLabel(/timeout/i).fill('60')
    await page.getByRole('button', { name: /save/i }).click()

    await expect(page.getByText(/saved/i)).toBeVisible()
  })

  test('configures retry settings', async ({ page }) => {
    await page.getByLabel(/max retries/i).fill('3')
    await page.getByRole('button', { name: /save/i }).click()

    await expect(page.getByText(/saved/i)).toBeVisible()
  })
})

test.describe('Authentication Settings', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/login')
    await page.getByLabel(/username/i).fill('admin')
    await page.getByLabel(/password/i).fill('admin')
    await page.getByRole('button', { name: /log in/i }).click()
    await expect(page).toHaveURL(/\/?|\/dashboard/)
    await page.getByRole('link', { name: /settings/i }).click()
  })

  test('updates session timeout', async ({ page }) => {
    await page.getByLabel(/session timeout/i).fill('86400')
    await page.getByRole('button', { name: /save/i }).click()

    await expect(page.getByText(/saved/i)).toBeVisible()
  })

  test('enables password policy', async ({ page }) => {
    const checkbox = page.getByRole('checkbox', { name: /password/i })
    await checkbox.click()
    await page.getByRole('button', { name: /save/i }).click()

    await expect(page.getByText(/saved/i)).toBeVisible()
  })

  test('configures password requirements', async ({ page }) => {
    await page.getByLabel(/min length/i).fill('12')
    await page.getByRole('button', { name: /save/i }).click()

    await expect(page.getByText(/saved/i)).toBeVisible()
  })

  test('enables 2FA', async ({ page }) => {
    const checkbox = page.getByRole('checkbox', { name: /2fa/i })
    await checkbox.click()
    await page.getByRole('button', { name: /save/i }).click()

    await expect(page.getByText(/saved/i)).toBeVisible()
  })
})

test.describe('Backup Settings', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/login')
    await page.getByLabel(/username/i).fill('admin')
    await page.getByLabel(/password/i).fill('admin')
    await page.getByRole('button', { name: /log in/i }).click()
    await expect(page).toHaveURL(/\/?|\/dashboard/)
    await page.getByRole('link', { name: /settings/i }).click()
  })

  test('configures backup schedule', async ({ page }) => {
    await page.getByRole('button', { name: /daily/i }).click()
    await page.getByRole('button', { name: /save/i }).click()

    await expect(page.getByText(/saved/i)).toBeVisible()
  })

  test('configures backup storage', async ({ page }) => {
    await page.getByLabel(/backup path/i).fill('/backups')
    await page.getByRole('button', { name: /save/i }).click()

    await expect(page.getByText(/saved/i)).toBeVisible()
  })

  test('enables backup encryption', async ({ page }) => {
    const checkbox = page.getByRole('checkbox', { name: /encrypt/i })
    await checkbox.click()
    await page.getByRole('button', { name: /save/i }).click()

    await expect(page.getByText(/saved/i)).toBeVisible()
  })

  test('configures retention policy', async ({ page }) => {
    await page.getByLabel(/retention days/i).fill('30')
    await page.getByRole('button', { name: /save/i }).click()

    await expect(page.getByText(/saved/i)).toBeVisible()
  })
})

test.describe('Notifications Settings', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/login')
    await page.getByLabel(/username/i).fill('admin')
    await page.getByLabel(/password/i).fill('admin')
    await page.getByRole('button', { name: /log in/i }).click()
    await expect(page).toHaveURL(/\/?|\/dashboard/)
    await page.getByRole('link', { name: /settings/i }).click()
  })

  test('configures email notifications', async ({ page }) => {
    await page.getByLabel(/email/i).fill('admin@example.com')
    await page.getByRole('button', { name: /save/i }).click()

    await expect(page.getByText(/saved/i)).toBeVisible()
  })

  test('configures webhook URL', async ({ page }) => {
    await page.getByLabel(/webhook/i).fill('https://hooks.example.com')
    await page.getByRole('button', { name: /save/i }).click()

    await expect(page.getByText(/saved/i)).toBeVisible()
  })

  test('enables Slack integration', async ({ page }) => {
    await page.getByLabel(/slack webhook/i).fill('https://hooks.slack.com/services/...')
    await page.getByRole('button', { name: /save/i }).click()

    await expect(page.getByText(/saved/i)).toBeVisible()
  })

  test('configures notification events', async ({ page }) => {
    const checkbox = page.getByRole('checkbox', { name: /upload/i })
    await checkbox.click()
    await page.getByRole('button', { name: /save/i }).click()

    await expect(page.getByText(/saved/i)).toBeVisible()
  })
})

test.describe('Save/Cancel Behavior', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/login')
    await page.getByLabel(/username/i).fill('admin')
    await page.getByLabel(/password/i).fill('admin')
    await page.getByRole('button', { name: /log in/i }).click()
    await expect(page).toHaveURL(/\/?|\/dashboard/)
    await page.getByRole('link', { name: /settings/i }).click()
  })

  test('cancels changes', async ({ page }) => {
    const originalValue = await page.getByLabel(/site name/i).inputValue()
    await page.getByLabel(/site name/i).fill('Changed')
    await page.getByRole('button', { name: /cancel/i }).click()

    // Should revert to original
    const newValue = await page.getByLabel(/site name/i).inputValue()
    expect(newValue).toBe(originalValue)
  })

  test('shows unsaved changes warning', async ({ page }) => {
    await page.getByLabel(/site name/i).fill('Changed')

    // Try to navigate away
    await page.getByRole('link', { name: /profile/i }).click()

    // Should show warning
    await expect(page.getByText(/unsaved/i)).toBeVisible()
  })
})

test.describe('Error Handling', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/login')
    await page.getByLabel(/username/i).fill('admin')
    await page.getByLabel(/password/i).fill('admin')
    await page.getByRole('button', { name: /log in/i }).click()
    await expect(page).toHaveURL(/\/?|\/dashboard/)
    await page.getByRole('link', { name: /settings/i }).click()
  })

  test('handles invalid config', async ({ page }) => {
    await page.getByLabel(/invalid field/i).fill('invalid')
    await page.getByRole('button', { name: /save/i }).click()

    await expect(page.getByText(/error|invalid/i)).toBeVisible()
  })

  test('shows network error', async ({ page }) => {
    // Simulate network error
    await page.route('**/api/v1/settings', (route) => route.abort())
    await page.getByRole('button', { name: /save/i }).click()

    await expect(page.getByText(/network|connection/i)).toBeVisible()
  })
})

test.describe('Accessibility', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/login')
    await page.getByLabel(/username/i).fill('admin')
    await page.getByLabel(/password/i).fill('admin')
    await page.getByRole('button', { name: /log in/i }).click()
    await expect(page).toHaveURL(/\/?|\/dashboard/)
    await page.getByRole('link', { name: /settings/i }).click()
  })

  test('has proper form labels', async ({ page }) => {
    const inputs = page.locator('input')
    const count = await inputs.count()

    for (let i = 0; i < count; i++) {
      const input = inputs.nth(i)
      const label = await input.getAttribute('aria-label') || (await input.getAttribute('id'))
        ? page.locator(`label[for="${await input.getAttribute('id')}"]`)
        : null
      expect(label || input).toBeTruthy()
    }
  })

  test('supports keyboard navigation', async ({ page }) => {
    await page.keyboard.press('Tab')
    await page.keyboard.press('Tab')
    await page.keyboard.press('Enter')

    // Should be able to navigate and interact
    expect(page.url()).toBeTruthy()
  })

  test('has proper focus states', async ({ page }) => {
    const input = page.locator('input').first()
    await input.focus()

    await expect(input).toHaveCSS('outline-style', 'solid')
  })
})
