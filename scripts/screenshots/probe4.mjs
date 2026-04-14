// probe4.mjs - メッセージ送信テストと応答/エラー確認
import { chromium } from 'playwright';
import fs from 'fs';
import path from 'path';
import { fileURLToPath } from 'url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));

async function probe4() {
  const browser = await chromium.launch({ headless: true });
  const page = await browser.newPage();
  page.setDefaultTimeout(30000);

  // ネットワークリクエストをログ
  page.on('request', req => {
    if (!req.url().includes('assets') && !req.url().includes('.js') && !req.url().includes('.css')) {
      console.log('REQ:', req.method(), req.url());
    }
  });
  page.on('response', res => {
    if (!res.url().includes('assets') && !res.url().includes('.js') && !res.url().includes('.css')) {
      console.log('RES:', res.status(), res.url());
    }
  });

  console.log('Navigating to http://localhost:8080/ui/ ...');
  await page.goto('http://localhost:8080/ui/', { waitUntil: 'networkidle', timeout: 30000 });
  await page.waitForTimeout(2000);

  // infinite_loop_unbounded を選択（Shinganがブロックするはず）
  console.log('\n=== Testing infinite_loop_unbounded ===');
  await page.locator('mat-select').first().click();
  await page.waitForTimeout(500);
  await page.locator('mat-option').filter({ hasText: 'infinite_loop_unbounded' }).click();
  await page.waitForTimeout(2000);

  // テキスト入力して送信
  const ta = page.locator('textarea[placeholder="Type a Message..."]');
  await ta.waitFor({ state: 'visible', timeout: 10000 });
  await ta.fill('hello');
  await page.waitForTimeout(300);

  console.log('Sending message with Enter...');
  await ta.press('Enter');

  // 応答またはエラーを待つ（最大15秒）
  await page.waitForTimeout(5000);
  await page.screenshot({ path: path.join(__dirname, 'probe-09-unbounded-response.png'), fullPage: true });
  console.log('Screenshot: probe-09-unbounded-response.png');

  // ページのテキストを確認
  const bodyText = await page.evaluate(() => document.body.innerText);
  console.log('\nPage text after send:', bodyText.substring(0, 1000));

  // エラー要素を探す
  const errorSelectors = [
    '.error', '.mat-error', 'mat-error',
    '[class*="error"]', '[class*="Error"]',
    '.snackbar', 'mat-snack-bar', '.mat-snack-bar-container',
    '.message', '.chat-message', '.user-message', '.model-message',
    '.event-container', '.chat-card'
  ];

  console.log('\n--- Error/message selectors ---');
  for (const sel of errorSelectors) {
    try {
      const count = await page.locator(sel).count();
      if (count > 0) {
        for (let i = 0; i < Math.min(count, 3); i++) {
          const text = await page.locator(sel).nth(i).textContent();
          if (text?.trim()) {
            console.log(`${sel}[${i}]: "${text.trim().substring(0, 200)}"`);
          }
        }
      }
    } catch(e) {}
  }

  await browser.close();
  console.log('\nProbe4 complete!');
}

probe4().catch(e => {
  console.error('Probe4 failed:', e);
  process.exit(1);
});
