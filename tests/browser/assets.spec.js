const { test, expect } = require('@playwright/test');
const path = require('node:path');
const fs = require('node:fs/promises');

test('pages render offline with local styles, fonts, icons and no script errors', async ({ page }, testInfo) => {
  const external = [], errors = [], failed = [];
  const baseline = process.env.BROWSER_TEST_CONTRACTS
    ? new Map(JSON.parse(await fs.readFile(process.env.BROWSER_TEST_CONTRACTS, 'utf8'))) : null;
  page.on('pageerror', error => errors.push(error.message));
  page.on('response', response => {
    if (response.status() >= 400 && !response.url().endsWith('/favicon.ico')) failed.push(`${response.status()} ${response.url()}`);
  });
  await page.route('**/*', route => {
    const url = new URL(route.request().url());
    if (url.hostname !== '127.0.0.1') {
      external.push(url.href);
      return route.abort();
    }
    return route.continue();
  });
  await page.goto('/guides');
  const guides = await page.locator('#navigation a[href^="/guides/"]').evaluateAll(links => [...new Set(links.map(link => link.getAttribute('href')))]);
  expect(guides.length).toBeGreaterThan(0);
  for (const pathname of ['/', '/compatibility-api/reference', ...guides, '/compatibility-api/reference/pet-operations/update-pet']) {
    const response = await page.goto(pathname);
    expect(response.status(), pathname).toBe(200);
    await expect(page.locator('h1, h2').first()).toBeVisible();
    await expect(page.locator('.navbar')).toBeVisible();
    if (baseline && pathname.startsWith('/guides/')) {
      expect(baseline.has(pathname), `Missing baseline for ${pathname}`).toBe(true);
      const contract = await page.locator('.main').evaluate(main => ({
        anchors: [...main.querySelectorAll('a[name]')].map(a => [a.name, a.getAttribute('href')]),
        links: [...main.querySelectorAll('a[href]')].map(a => [a.textContent.trim(), a.getAttribute('href')]),
        images: [...main.querySelectorAll('img')].map(img => [img.getAttribute('src'), img.getAttribute('alt')]),
        code: [...main.querySelectorAll('pre')].map(node => node.textContent),
      }));
      expect(contract, pathname).toEqual(baseline.get(pathname));
    }
    await page.evaluate(() => document.fonts.ready);
    expect(await page.evaluate(() => document.fonts.check('15px "Open Sans"'))).toBe(true);
    const overflow = await page.evaluate(() => ({
      width: innerWidth, actual: document.documentElement.scrollWidth,
      elements: [...document.querySelectorAll('body *')].filter(element => {
        const box = element.getBoundingClientRect();
        return box.width && (box.right > innerWidth + 1 || box.left < -1);
      }).map(element => `${element.tagName}.${element.className}`).slice(0, 10),
    }));
    expect(overflow.actual, `${pathname}: ${JSON.stringify(overflow)}`).toBeLessThanOrEqual(overflow.width + 1);
    if (process.env.BROWSER_SCREENSHOTS) {
      await fs.mkdir(process.env.BROWSER_SCREENSHOTS, { recursive: true });
      await page.screenshot({
        path: path.join(process.env.BROWSER_SCREENSHOTS, `${process.env.BROWSER_TEST_THEME || 'default'}-${testInfo.project.name}-${pathname.replaceAll('/', '_') || 'root'}.png`),
        fullPage: true,
      });
    }
  }
  expect(external).toEqual([]);
  const knownMissing = (process.env.BROWSER_TEST_EXPECTED_404 || '').split('\n').filter(Boolean)
    .map(pathname => `404 http://127.0.0.1:32179${pathname}`);
  expect([...new Set(failed)].sort()).toEqual(knownMissing.sort());
  expect(errors).toEqual([]);
  expect(await page.evaluate(() => $.fn.jquery)).toBe('4.0.0');
  expect(await page.evaluate(() => bootstrap.Collapse.VERSION)).toBe('5.3.9');
  expect(await page.evaluate(() => hljs.versionString)).toBe('11.12.0');
});

test('navigation collapses with Bootstrap and retains the current page', async ({ page, isMobile }) => {
  await page.goto('/compatibility-api/reference/pet-operations/update-pet');
  const group = page.locator('#ulpet-operations');
  await expect(group).toBeVisible();
  await expect(group.locator('.nav-selected')).toHaveAttribute('href', '/compatibility-api/reference/pet-operations/update-pet');
  const toggler = page.locator('#togglepet-operations');
  await toggler.click();
  await expect(group).not.toBeVisible();
  await toggler.click();
  await expect(group).toBeVisible();
  if (isMobile) {
    const navigation = page.locator('#header-navigation');
    await expect(navigation).not.toBeVisible();
    await page.getByRole('button', { name: 'Toggle navigation' }).click();
    await expect(navigation).toHaveClass(/show/);
    await expect(page.getByRole('button', { name: 'Toggle navigation' })).toHaveAttribute('aria-expanded', 'true');
    await page.getByRole('button', { name: 'Toggle navigation' }).click();
    await expect(navigation).not.toBeVisible();
  }
});

test('required fields show validation without the removed wiggle plugin', async ({ page }) => {
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  await page.goto('/compatibility-api/reference/pet-operations/update-pet');
  await page.locator('#exploreButton').click();
  await expect(page.locator('#id')).toHaveClass(/has-error/);
  await expect(page.locator('#results')).not.toBeVisible();
  expect(errors).toEqual([]);
});
