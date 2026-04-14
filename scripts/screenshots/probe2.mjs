// probe2.mjs - エージェント選択後のUIセレクタ詳細調査
import { chromium } from 'playwright';
import fs from 'fs';
import path from 'path';
import { fileURLToPath } from 'url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));

async function probe2() {
  const browser = await chromium.launch({ headless: true });
  const page = await browser.newPage();
  page.setDefaultTimeout(30000);

  console.log('Navigating to http://localhost:8080/ui/ ...');
  await page.goto('http://localhost:8080/ui/', { waitUntil: 'networkidle', timeout: 30000 });
  await page.waitForTimeout(2000);

  // mat-selectをクリック
  console.log('Clicking mat-select...');
  const matSelect = page.locator('mat-select').first();
  await matSelect.click();
  await page.waitForTimeout(1000);

  // オプション一覧を確認
  const options = page.locator('mat-option');
  const optionCount = await options.count();
  console.log('mat-option count:', optionCount);
  for (let i = 0; i < optionCount; i++) {
    const text = await options.nth(i).textContent();
    console.log(`  Option ${i}: "${text?.trim()}"`);
  }

  // スクリーンショット（ドロップダウン開いた状態）
  await page.screenshot({ path: path.join(__dirname, 'probe-03-dropdown-open.png'), fullPage: true });
  console.log('Screenshot: probe-03-dropdown-open.png');

  // simple_hello を選択
  console.log('Selecting simple_hello...');
  for (let i = 0; i < optionCount; i++) {
    const text = await options.nth(i).textContent();
    if (text?.includes('simple_hello')) {
      await options.nth(i).click();
      break;
    }
  }
  await page.waitForTimeout(2000);

  // 選択後のUIを確認
  console.log('\n--- After agent selection ---');
  await page.screenshot({ path: path.join(__dirname, 'probe-04-agent-selected.png'), fullPage: true });
  console.log('Screenshot: probe-04-agent-selected.png');

  // セレクタを探す
  const checkSelectors = [
    'input[type="text"]', 'textarea', 'input',
    '.message-input', '.chat-input', '.user-input',
    'mat-form-field input', 'mat-form-field textarea',
    'button[type="submit"]', 'button.send', '.send-button',
    '[placeholder]', '[aria-label]'
  ];

  console.log('\n--- Selector check after selection ---');
  for (const sel of checkSelectors) {
    try {
      const count = await page.locator(sel).count();
      if (count > 0) {
        const locs = page.locator(sel);
        for (let i = 0; i < Math.min(count, 3); i++) {
          const el = locs.nth(i);
          const text = await el.textContent().catch(() => '');
          const placeholder = await el.getAttribute('placeholder').catch(() => '');
          const ariaLabel = await el.getAttribute('aria-label').catch(() => '');
          const type = await el.getAttribute('type').catch(() => '');
          console.log(`${sel}[${i}]: text="${text?.trim().substring(0,50)}" placeholder="${placeholder}" aria-label="${ariaLabel}" type="${type}"`);
        }
      }
    } catch(e) {
      // ignore
    }
  }

  // HTMLを保存（エージェント選択後）
  const html = await page.content();
  fs.writeFileSync(path.join(__dirname, 'probe-html-after-select.txt'), html);
  console.log('\nHTML saved to probe-html-after-select.txt');

  // ボタン一覧
  const buttons = page.locator('button');
  const btnCount = await buttons.count();
  console.log('\nButtons found:', btnCount);
  for (let i = 0; i < btnCount; i++) {
    const text = await buttons.nth(i).textContent();
    const ariaLabel = await buttons.nth(i).getAttribute('aria-label');
    console.log(`  Button ${i}: text="${text?.trim()}" aria-label="${ariaLabel}"`);
  }

  await browser.close();
  console.log('\nProbe2 complete!');
}

probe2().catch(e => {
  console.error('Probe2 failed:', e);
  process.exit(1);
});
