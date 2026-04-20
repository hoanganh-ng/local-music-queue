import { test, expect } from '@playwright/test';

test.describe('Authentication Flow', () => {
  test('should show login form and allow submitting', async ({ page }) => {
    // Navigate to root (which should be the login page)
    await page.goto('/');

    // Verify title and header
    await expect(page).toHaveTitle(/Local Music Queue/);
    await expect(page.locator('h1')).toContainText('Local Music Queue');

    // Fill in the form
    await page.fill('#displayName', 'Test User');
    await page.fill('#pin', '1234');

    // Mock the login API call
    await page.route('**/api/auth', async route => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          name: 'Test User',
          role: 'Guest',
          session_id: 'test-session'
        }),
      });
    });

    // Mock the initial queue fetch that happens on dashboard load
    await page.route('**/api/queue', async route => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          status: 'stopped',
          current_song: null,
          queue: [],
          history: []
        }),
      });
    });

    // Submit the form
    await page.click('button[type="submit"]');

    // Should redirect to root (Dashboard)
    await expect(page).toHaveURL('http://localhost:5173/');
    
    // Verify Dashboard content is visible
    await expect(page.locator('.brand h2')).toContainText('Local Music Queue');
  });

  test('should show error message on failed login', async ({ page }) => {
    await page.goto('/');

    await page.fill('#pin', 'wrong-pin');

    // Mock the login API failure
    await page.route('**/api/auth', async route => {
      await route.fulfill({
        status: 401,
        contentType: 'application/json',
        body: JSON.stringify({ error: 'Invalid PIN' }),
      });
    });

    await page.click('button[type="submit"]');

    // Verify error message appears
    await expect(page.locator('.error-message')).toBeVisible();
    await expect(page.locator('.error-message')).toContainText('Invalid PIN');
  });
});
