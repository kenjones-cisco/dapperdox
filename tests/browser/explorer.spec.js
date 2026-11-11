const { test, expect } = require('@playwright/test');
const fs = require('node:fs/promises');

test.beforeEach(async ({ page }) => {
  await page.route('https://**/*', async route => {
    const url = route.request().url();
    if (url === 'https://ajax.googleapis.com/ajax/libs/jquery/1.11.3/jquery.min.js') {
      await route.fulfill({ path: require.resolve('jquery/dist/jquery.min.js'), contentType: 'application/javascript' });
    } else {
      await route.fulfill({ body: '', contentType: url.endsWith('.css') ? 'text/css' : 'application/javascript' });
    }
  });
});

async function open(page, operation) {
  await page.goto(`/compatibility-api/reference/pet-operations/${operation}`);
  await expect(page.locator('#exploreButton')).toBeVisible();
}

async function submit(page, path) {
  const request = page.waitForRequest(r => new URL(r.url()).pathname === path);
  await page.locator('#exploreButton').click();
  return request;
}

test('rendered explorer preserves header key, bearer, basic, body and URL contracts', async ({ page }) => {
  await open(page, 'update-pet');
  await page.locator('#id').fill('pet-42');
  await page.locator('#filter').fill('name & type');
  await page.locator('#payload').fill('{"name":"Milo"}');
  await page.locator('#api-key-input').fill('fixture-key');
  let request = await submit(page, '/fixture-api/pets/pet-42');
  expect(new URL(request.url()).searchParams.get('filter')).toBe('name & type');
  expect(request.headers()['x-api-key']).toBe('fixture-key');
  expect(request.postDataJSON()).toEqual({ name: 'Milo' });
  await expect(page.locator('#exploreButton')).not.toHaveAttribute('disabled');

  await page.locator('#basic-username-input').fill('tester');
  await page.locator('#basic-password-input').fill('fixture-password');
  request = await submit(page, '/fixture-api/pets/pet-42');
  expect(request.headers().authorization).toBe('Basic dGVzdGVyOmZpeHR1cmUtcGFzc3dvcmQ=');
  await expect(page.locator('#exploreButton')).not.toHaveAttribute('disabled');

  await page.locator('#access-token-input').fill('fixture-token');
  request = await submit(page, '/fixture-api/pets/pet-42');
  expect(request.headers().authorization).toBe('Bearer fixture-token');
});

test('query API key and custom authentication hook remain compatible', async ({ page }) => {
  await open(page, 'query-pet');
  await page.locator('#api-key-input').fill('key with spaces');
  let request = await submit(page, '/fixture-api/query');
  expect(new URL(request.url()).searchParams.get('api_key')).toBe('key with spaces');
  await expect(page.locator('#exploreButton')).not.toHaveAttribute('disabled');
  await page.evaluate(() => apiExplorer.setBeforeSendCallback(request => {
    request.headers = { 'X-Custom-Auth': 'custom-value' };
    request.params = { custom_auth: 'query-value' };
  }));
  request = await submit(page, '/fixture-api/query');
  expect(request.headers()['x-custom-auth']).toBe('custom-value');
  expect(new URL(request.url()).searchParams.get('custom_auth')).toBe('query-value');
});

test('multipart explorer preserves filename, text field and uploaded bytes', async ({ page }) => {
  await open(page, 'upload-pet');
  await page.locator('#caption').fill('A pet attachment');
  await page.locator('input[type=file]').setInputFiles({
    name: 'pet.txt', mimeType: 'text/plain', buffer: Buffer.from('upload fixture\n'),
  });
  const request = await submit(page, '/fixture-api/upload');
  expect(request.headers()['content-type']).toMatch(/^multipart\/form-data; boundary=/);
  const body = request.postDataBuffer().toString();
  expect(body).toContain('name="caption"\r\n\r\nA pet attachment');
  expect(body).toContain('name="attachment"; filename="pet.txt"');
  expect(body).toContain('upload fixture\n');
});

test('binary response uses the browser download path with unchanged bytes', async ({ page }) => {
  await open(page, 'download-pet');
  const download = page.waitForEvent('download');
  await page.locator('#exploreButton').click();
  const result = await download;
  expect(result.suggestedFilename()).toBe('explorer_response.txt');
  expect(await fs.readFile(await result.path())).toEqual(Buffer.from('fixture\x00download\n'));
});
