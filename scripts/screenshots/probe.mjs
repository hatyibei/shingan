// probe.mjs - ADK Web UI のセレクタ特定用プローブスクリプト
import { chromium } from 'playwright';
import fs from 'fs';
import path from 'path';
import { fileURLToPath } from 'url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));

async function probe() {
  const browser = await chromium.launch({ headless: true });
  const page = await browser.newPage();

  console.log('Navigating to http://localhost:8080/ ...');
  await page.goto('http://localhost:8080/', { waitUntil: 'networkidle', timeout: 30000 });

  console.log('Current URL:', page.url());

  // スクリーンショット（初期状態）
  await page.screenshot({ path: path.join(__dirname, 'probe-01-initial.png'), fullPage: true });
  console.log('Screenshot: probe-01-initial.png');

  // HTMLをファイルに保存
  const html = await page.content();
  fs.writeFileSync(path.join(__dirname, 'probe-html.txt'), html);
  console.log('HTML saved to probe-html.txt (length:', html.length, ')');

  // アクセシビリティスナップショット
  try {
    const snapshot = await page.accessibility.snapshot({ interestingOnly: false });
    fs.writeFileSync(path.join(__dirname, 'probe-accessibility.json'), JSON.stringify(snapshot, null, 2));
    console.log('Accessibility snapshot saved to probe-accessibility.json');
  } catch(e) {
    console.log('Accessibility snapshot failed:', e.message);
  }

  // ページ内のすべてのテキストコンテンツ
  const bodyText = await page.evaluate(() => document.body.innerText);
  fs.writeFileSync(path.join(__dirname, 'probe-text.txt'), bodyText);
  console.log('Body text:', bodyText.substring(0, 500));

  // 主要なセレクタを試行
  const selectors = [
    'input', 'textarea', 'button',
    'mat-list-item', 'mat-nav-list', 'mat-select',
    '[role="listitem"]', '[role="button"]',
    '.agent', '.agent-list', '.agent-name',
    'select', 'option'
  ];

  console.log('\n--- Selector probing ---');
  for (const sel of selectors) {
    const count = await page.locator(sel).count();
    if (count > 0) {
      console.log(`${sel}: ${count} elements found`);
      const texts = await page.locator(sel).allTextContents();
      console.log('  texts:', texts.slice(0, 5).map(t => t.trim().substring(0, 80)));
    }
  }

  // 3秒待ってから再スクリーンショット
  await page.waitForTimeout(3000);
  await page.screenshot({ path: path.join(__dirname, 'probe-02-after-wait.png'), fullPage: true });
  console.log('Screenshot: probe-02-after-wait.png');

  await browser.close();
  console.log('\nProbe complete!');
}

probe().catch(e => {
  console.error('Probe failed:', e);
  process.exit(1);
});
