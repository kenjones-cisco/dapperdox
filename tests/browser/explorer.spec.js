const { test, expect } = require('@playwright/test');
const fs = require('node:fs/promises');

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
  test.skip(process.env.BROWSER_TEST_THEME === 'cisco', 'Consumer intentionally uses bearer-only authentication');
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
  test.skip(process.env.BROWSER_TEST_THEME === 'cisco', 'Consumer intentionally uses bearer-only authentication');
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

test('consumer bearer authentication hook remains compatible', async ({ page }) => {
  test.skip(process.env.BROWSER_TEST_THEME !== 'cisco', 'Consumer-specific authentication hook');
  await open(page, 'update-pet');
  await page.locator('#id').fill('pet-42');
  await page.locator('#access-token-input').fill('fixture-token');
  const request = await submit(page, '/fixture-api/pets/pet-42');
  expect(request.headers().authorization).toBe('Bearer fixture-token');
  await expect(page.locator('#response_body')).toContainText('"accepted": true');
});

test('multipart explorer preserves filename, text field and uploaded bytes', async ({ page }) => {
  await open(page, 'upload-pet');
  await page.locator('#caption').fill('A pet attachment');
  await page.locator('input[type=file]').setInputFiles({
    name: 'pet.txt', mimeType: 'text/plain', buffer: Buffer.from('upload fixture\n'),
  });
  const response = page.waitForResponse(r => new URL(r.url()).pathname === '/fixture-api/upload');
  const request = await submit(page, '/fixture-api/upload');
  expect(request.headers()['content-type']).toMatch(/^multipart\/form-data; boundary=/);
  expect(await (await response).json()).toEqual({
    caption: 'A pet attachment', filename: 'pet.txt', body: Buffer.from('upload fixture\n').toString('base64'),
  });
});

test('binary response uses the browser download path with unchanged bytes', async ({ page }) => {
  await open(page, 'download-pet');
  const download = page.waitForEvent('download');
  await page.locator('#exploreButton').click();
  const result = await download;
  expect(result.suggestedFilename()).toBe('explorer_response.txt');
  expect(await fs.readFile(await result.path())).toEqual(Buffer.from([102, 105, 120, 116, 117, 114, 101, 0, 255, 128, 100, 111, 119, 110, 108, 111, 97, 100, 10]));
});

test('HTTP errors still render the response and re-enable the explorer', async ({ page }) => {
  await page.route('**/fixture-api/query*', route => route.fulfill({
    status: 422, contentType: 'application/json', body: '{"error":"invalid request"}',
  }));
  await open(page, 'query-pet');
  await page.locator('#exploreButton').click();
  await expect(page.locator('#response_code')).toContainText('422');
  await expect(page.locator('#response_body')).toContainText('"error": "invalid request"');
  await expect(page.locator('#exploreButton')).not.toHaveAttribute('disabled');
});

test('text responses honor their declared charset', async ({ page }) => {
  await open(page, 'query-pet');
  for (const [contentType, body, text, status = 200] of [
    ['text/plain; charset=windows-1252', Buffer.from([0x63, 0x61, 0x66, 0xe9]), 'café'],
    ['text/plain; Charset = "windows-1252"', Buffer.from([0x80]), '€', 422],
    ["application/json; charset='windows-1252'", Buffer.from('{"name":"caf\xe9"}', 'latin1'), '{\n  "name": "café"\n}'],
    ['application/xml; charset=utf-16le', Buffer.from('<name>café</name>', 'utf16le'), '<name>café</name>'],
    ['application/yaml; charset=windows-1252', Buffer.from('name: caf\xe9', 'latin1'), 'name: café'],
    ['text/plain', Buffer.from('café'), 'café'],
  ]) {
    await page.route('**/fixture-api/query*', route => route.fulfill({ contentType, body, status }));
    await page.locator('#exploreButton').click();
    await expect(page.locator('#response_body')).toHaveText(text);
    await expect(page.locator('#response_code')).toContainText(String(status));
    await expect(page.locator('#exploreButton')).not.toHaveAttribute('disabled');
    await page.unroute('**/fixture-api/query*');
  }
});

test('unsupported text encodings show an error without disabling the explorer', async ({ page }) => {
  await open(page, 'query-pet');
  await page.route('**/fixture-api/query*', route => route.fulfill({
    contentType: 'text/plain; charset=unsupported-encoding', body: Buffer.from([0xe9]),
  }));
  await page.locator('#exploreButton').click();
  await expect(page.locator('#response_body')).toContainText('Unexpected error:');
  await expect(page.locator('#response_code')).toContainText('200');
  await expect(page.locator('#exploreButton')).not.toHaveAttribute('disabled');
});

test('response highlighting supports JSON, XML, YAML and HTTP headers', async ({ page }) => {
  await open(page, 'query-pet');
  for (const [mime, body, text] of [
    ['application/json', '{"pet":"Milo"}', '"pet": "Milo"'],
    ['application/xml', '<pet>Milo</pet>', '<pet>Milo</pet>'],
    ['application/yaml', 'pet: Milo\n', 'pet: Milo'],
  ]) {
    await page.route('**/fixture-api/query*', route => route.fulfill({ contentType: mime, body }));
    await page.locator('#exploreButton').click();
    await expect(page.locator('#response_body')).toContainText(text);
    await expect(page.locator('#response_headers')).toContainText(mime);
    await expect(page.locator('#response_body span')).not.toHaveCount(0);
    await expect(page.locator('#exploreButton')).not.toHaveAttribute('disabled');
    await page.unroute('**/fixture-api/query*');
  }
});
