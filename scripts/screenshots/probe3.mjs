// probe3.mjs - セッション作成ボタンとチャット送信方法の調査
import { chromium } from 'playwright';
import fs from 'fs';
import path from 'path';
import { fileURLToPath } from 'url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));

async function probe3() {
  const browser = await chromium.launch({ headless: true });
  const page = await browser.newPage();
  page.setDefaultTimeout(30000);

  // コンソールログを収集
  page.on('console', msg => console.log('BROWSER:', msg.type(), msg.text()));

  console.log('Navigating to http://localhost:8080/ui/ ...');
  await page.goto('http://localhost:8080/ui/', { waitUntil: 'networkidle', timeout: 30000 });
  await page.waitForTimeout(2000);

  // mat-selectをクリックしてsimple_helloを選択
  console.log('Selecting simple_hello...');
  await page.locator('mat-select').first().click();
  await page.waitForTimeout(500);
  await page.locator('mat-option').filter({ hasText: 'simple_hello' }).click();
  await page.waitForTimeout(2000);

  // スクリーンショット
  await page.screenshot({ path: path.join(__dirname, 'probe-05-after-select.png'), fullPage: true });

  // 全ボタンを再確認
  const allButtons = page.locator('button');
  const allBtnCount = await allButtons.count();
  console.log('\nAll buttons after agent select:', allBtnCount);
  for (let i = 0; i < allBtnCount; i++) {
    const btn = allButtons.nth(i);
    const text = await btn.textContent();
    const ariaLabel = await btn.getAttribute('aria-label');
    const title = await btn.getAttribute('title');
    const isVisible = await btn.isVisible();
    console.log(`  [${i}] text="${text?.trim()}" aria="${ariaLabel}" title="${title}" visible=${isVisible}`);
  }

  // mat-icon-button を確認
  const iconBtns = page.locator('[mat-icon-button], [mat-raised-button], [mat-flat-button]');
  const iconBtnCount = await iconBtns.count();
  console.log('\nMaterial buttons:', iconBtnCount);

  // addボタン（新しいセッション作成）を探す
  console.log('\nSearching for add/new session button...');
  const addBtn = page.locator('button').filter({ hasText: 'add' });
  const addBtnCount = await addBtn.count();
  console.log('add buttons:', addBtnCount);

  // addをクリックしてセッション作成
  if (addBtnCount > 0) {
    console.log('Clicking add button...');
    await addBtn.first().click();
    await page.waitForTimeout(2000);
    await page.screenshot({ path: path.join(__dirname, 'probe-06-session-created.png'), fullPage: true });
    console.log('Screenshot: probe-06-session-created.png');

    // セッション作成後のUI要素確認
    console.log('\nAfter session creation:');
    const textareas = page.locator('textarea');
    const taCount = await textareas.count();
    console.log('textareas:', taCount);
    for (let i = 0; i < taCount; i++) {
      const ta = textareas.nth(i);
      const ph = await ta.getAttribute('placeholder');
      const visible = await ta.isVisible();
      console.log(`  textarea[${i}]: placeholder="${ph}" visible=${visible}`);
    }

    // 全ボタン再確認
    const btns2 = page.locator('button');
    const btn2Count = await btns2.count();
    console.log('\nAll buttons after session creation:', btn2Count);
    for (let i = 0; i < btn2Count; i++) {
      const btn = btns2.nth(i);
      const text = await btn.textContent();
      const ariaLabel = await btn.getAttribute('aria-label');
      const visible = await btn.isVisible();
      console.log(`  [${i}] text="${text?.trim()}" aria="${ariaLabel}" visible=${visible}`);
    }

    // textareaに入力してみる
    const ta = page.locator('textarea[placeholder="Type a Message..."]');
    if (await ta.count() > 0) {
      console.log('\nTyping "hello" in textarea...');
      await ta.fill('hello');
      await page.waitForTimeout(500);
      await page.screenshot({ path: path.join(__dirname, 'probe-07-typed.png'), fullPage: true });
      console.log('Screenshot: probe-07-typed.png');

      // 送信方法をリスト表示
      console.log('\nButtons after typing:');
      const btns3 = page.locator('button');
      const btn3Count = await btns3.count();
      for (let i = 0; i < btn3Count; i++) {
        const btn = btns3.nth(i);
        const text = await btn.textContent();
        const ariaLabel = await btn.getAttribute('aria-label');
        const visible = await btn.isVisible();
        const disabled = await btn.isDisabled();
        if (visible) {
          console.log(`  [${i}] text="${text?.trim()}" aria="${ariaLabel}" visible=${visible} disabled=${disabled}`);
        }
      }

      // Enterキーで送信試行
      console.log('\nTrying Enter key...');
      await ta.press('Enter');
      await page.waitForTimeout(1000);
      await page.screenshot({ path: path.join(__dirname, 'probe-08-after-enter.png'), fullPage: true });
      console.log('Screenshot: probe-08-after-enter.png');
    }
  }

  await browser.close();
  console.log('\nProbe3 complete!');
}

probe3().catch(e => {
  console.error('Probe3 failed:', e);
  process.exit(1);
});
