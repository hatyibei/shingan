/**
 * capture.mjs - Shingan ADK Web UI デモ用スクリーンショット自動取得
 *
 * 使い方:
 *   node capture.mjs
 *
 * 前提:
 *   - shingan-web が http://localhost:8080 で起動済み
 *   - playwright npm パッケージがインストール済み (npm install)
 */

import { chromium } from 'playwright';
import fs from 'fs';
import path from 'path';
import { fileURLToPath } from 'url';
import { execSync } from 'child_process';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const SCREENSHOT_DIR = __dirname;
const BASE_URL = 'http://localhost:8080';

// ページサイズ（面接デモ映えするサイズ）
const VIEWPORT = { width: 1280, height: 720 };

let shinganErrorJson = null; // 403レスポンスのJSONを保持

// ユーティリティ: スクリーンショット保存
async function ss(page, filename, fullPage = false) {
  const filepath = path.join(SCREENSHOT_DIR, filename);
  await page.screenshot({ path: filepath, fullPage });
  const size = fs.statSync(filepath).size;
  console.log(`  -> ${filename} (${Math.round(size / 1024)}KB)`);
  return filepath;
}

// ユーティリティ: 待機付きテキスト入力
async function typeAndSend(page, text) {
  const textarea = page.locator('textarea[placeholder="Type a Message..."]');
  await textarea.waitFor({ state: 'visible', timeout: 15000 });
  await textarea.click();
  await textarea.fill(text);
  await page.waitForTimeout(300);
  await textarea.press('Enter');
}

// ユーティリティ: エージェント選択
async function selectAgent(page, agentName) {
  console.log(`  エージェント選択: ${agentName}`);
  // ドロップダウンが開いていたら閉じる
  const isOpen = await page.locator('mat-option').count();
  if (isOpen > 0) {
    await page.keyboard.press('Escape');
    await page.waitForTimeout(300);
  }
  // mat-selectが見えるまで待つ
  const matSelect = page.locator('mat-select').first();
  await matSelect.waitFor({ state: 'visible', timeout: 10000 });
  await matSelect.click();
  await page.waitForTimeout(500);
  await page.locator('mat-option').filter({ hasText: agentName }).click();
  await page.waitForTimeout(2000);
}

// shinganエラーJSONをページにオーバーレイ表示してスクリーンショット
async function overlayErrorAndScreenshot(page, filename, errorJson) {
  const jsonStr = JSON.stringify(errorJson, null, 2);

  // Playwrightで一時的にエラーオーバーレイを注入
  await page.evaluate((json) => {
    // 既存のオーバーレイを削除
    const existing = document.getElementById('shingan-error-overlay');
    if (existing) existing.remove();

    const overlay = document.createElement('div');
    overlay.id = 'shingan-error-overlay';
    overlay.style.cssText = `
      position: fixed;
      top: 50%;
      left: 50%;
      transform: translate(-50%, -50%);
      background: #1a1a2e;
      border: 2px solid #e74c3c;
      border-radius: 8px;
      padding: 20px 24px;
      color: #ecf0f1;
      font-family: 'Google Sans Mono', monospace;
      font-size: 13px;
      line-height: 1.6;
      max-width: 620px;
      width: 90%;
      z-index: 99999;
      box-shadow: 0 0 30px rgba(231, 76, 60, 0.4);
      white-space: pre-wrap;
      word-break: break-all;
    `;

    const title = document.createElement('div');
    title.style.cssText = `
      color: #e74c3c;
      font-size: 15px;
      font-weight: bold;
      margin-bottom: 12px;
      display: flex;
      align-items: center;
      gap: 8px;
    `;
    title.innerHTML = '🛡️ Shingan Guard: HTTP 403 Forbidden';

    const body = document.createElement('pre');
    body.style.cssText = `
      margin: 0;
      color: #ff6b6b;
      background: #0d0d1a;
      padding: 12px;
      border-radius: 4px;
      border-left: 3px solid #e74c3c;
      font-size: 12px;
    `;
    body.textContent = json;

    overlay.appendChild(title);
    overlay.appendChild(body);
    document.body.appendChild(overlay);
  }, jsonStr);

  await page.waitForTimeout(300);
  await ss(page, filename);

  // オーバーレイを削除
  await page.evaluate(() => {
    const el = document.getElementById('shingan-error-overlay');
    if (el) el.remove();
  });
}

async function main() {
  console.log('=== Shingan デモ スクリーンショット取得開始 ===\n');

  // shingan-web 起動確認
  try {
    const res = await fetch(`${BASE_URL}/`);
    console.log(`shingan-web 接続確認: ${res.status} ${res.url}`);
  } catch (e) {
    console.error('ERROR: shingan-web が起動していません。先に起動してください。');
    console.error('  GOOGLE_CLOUD_PROJECT=axial-mercury-486503-j5 \\');
    console.error('  GOOGLE_CLOUD_LOCATION=us-central1 \\');
    console.error('  GOOGLE_GENAI_USE_VERTEXAI=true \\');
    console.error('  /home/hatyibei/Claude/shingan/shingan-web &');
    process.exit(1);
  }

  const browser = await chromium.launch({
    headless: true,
    args: ['--disable-web-security'],
  });

  const page = await browser.newPage();
  await page.setViewportSize(VIEWPORT);
  page.setDefaultTimeout(30000);

  // ネットワークインターセプト: 403レスポンスをキャプチャ
  page.on('response', async (response) => {
    if (response.url().includes('/api/run_sse') && response.status() === 403) {
      try {
        shinganErrorJson = await response.json();
        console.log(`  [Shingan] 403ブロック検出: ${JSON.stringify(shinganErrorJson).substring(0, 100)}`);
      } catch (e) {
        // ignore
      }
    }
  });

  try {
    // =========================================================
    // Scenario 1: トップページ（ADK Web UI 初期画面）
    // =========================================================
    console.log('\n--- Scenario 1: トップページ ---');
    await page.goto(`${BASE_URL}/ui/`, { waitUntil: 'networkidle', timeout: 30000 });
    await page.waitForTimeout(2000);
    await ss(page, '01-home-select-agent.png');
    console.log('  トップページのスクリーンショット取得完了');

    // ドロップダウンを開いて3エージェント一覧を表示
    console.log('  エージェントドロップダウンを開く...');
    await page.locator('mat-select').first().click();
    await page.waitForTimeout(800);
    await ss(page, '02-home-3-agents-list.png');
    console.log('  3エージェント一覧のスクリーンショット取得完了');

    // ドロップダウンを閉じる（Escape）
    await page.keyboard.press('Escape');
    await page.waitForTimeout(500);

    // =========================================================
    // Scenario 2: infinite_loop_unbounded → Shingan ブロック
    // =========================================================
    console.log('\n--- Scenario 2: infinite_loop_unbounded (Shingan ブロック) ---');
    // 新鮮な状態でリロード
    await page.goto(`${BASE_URL}/ui/`, { waitUntil: 'networkidle', timeout: 30000 });
    await page.waitForTimeout(2000);
    await selectAgent(page, 'infinite_loop_unbounded');

    // チャット入力を送信前のスクリーンショット
    await ss(page, '03-unbounded-agent-selected.png');
    console.log('  エージェント選択後のスクリーンショット取得完了');

    // メッセージを送信（Shinganがブロックするはず）
    console.log('  "hello" を送信中...');
    shinganErrorJson = null;
    await typeAndSend(page, 'hello');

    // 403が来るまで最大10秒待機
    let waited = 0;
    while (!shinganErrorJson && waited < 10000) {
      await page.waitForTimeout(500);
      waited += 500;
    }
    await page.waitForTimeout(1000); // UI更新を待つ

    // Web UIのスクリーンショット（ユーザーメッセージが見える）
    await ss(page, '04-shingan-blocks-buggy-agent.png');
    console.log('  Shinganブロック後のWeb UIスクリーンショット取得完了');

    // Shinganエラー詳細をオーバーレイで表示
    if (shinganErrorJson) {
      await overlayErrorAndScreenshot(page, '05-shingan-error-detail.png', shinganErrorJson);
      console.log('  Shinganエラー詳細（403 JSON）スクリーンショット取得完了');
    } else {
      console.log('  WARNING: 403レスポンスが取得できませんでした');
      await ss(page, '05-shingan-error-detail.png');
    }

    // =========================================================
    // Scenario 3: simple_hello → Gemini応答
    // =========================================================
    console.log('\n--- Scenario 3: simple_hello (Gemini応答) ---');

    // 新しいページに戻って simple_hello を選択
    await page.goto(`${BASE_URL}/ui/`, { waitUntil: 'networkidle', timeout: 30000 });
    await page.waitForTimeout(2000);
    await selectAgent(page, 'simple_hello');

    // エージェント選択後のスクリーンショット
    await ss(page, '06-simple-hello-selected.png');
    console.log('  simple_hello選択後スクリーンショット取得完了');

    // メッセージを送信
    console.log('  "こんにちは！" を送信中... (Vertex AI応答待ち)');
    await typeAndSend(page, 'こんにちは！');

    // Vertex AI応答を最大45秒待つ
    let responseReceived = false;
    const startTime = Date.now();
    while (!responseReceived && Date.now() - startTime < 45000) {
      await page.waitForTimeout(2000);
      const pageText = await page.evaluate(() => document.body.innerText);
      // ユーザーメッセージの後に何か応答があれば完了
      if (pageText.includes('こんにちは') && pageText.includes('model')) {
        responseReceived = true;
      }
      // .model-message要素が存在するか確認
      const modelMsgCount = await page.locator('.model-message').count();
      if (modelMsgCount > 0) {
        responseReceived = true;
      }
      // イベントコンテナに何か表示されたら完了
      const eventCount = await page.locator('.event-container').count();
      if (eventCount > 0) {
        responseReceived = true;
      }
      if (!responseReceived) {
        const elapsed = Math.round((Date.now() - startTime) / 1000);
        console.log(`  待機中... ${elapsed}秒`);
      }
    }

    await page.waitForTimeout(1000);

    // 応答後のスクリーンショット
    await ss(page, '07-clean-agent-executes.png');
    console.log(`  Gemini応答スクリーンショット取得完了 (応答受信: ${responseReceived})`);

    // 応答メッセージにスクロール・詳細スクリーンショット
    await page.evaluate(() => window.scrollTo(0, document.body.scrollHeight));
    await page.waitForTimeout(500);
    await ss(page, '08-gemini-response-detail.png');
    console.log('  Gemini応答詳細スクリーンショット取得完了');

    // =========================================================
    // Scenario 4: infinite_loop_bounded → Gemini応答
    // =========================================================
    console.log('\n--- Scenario 4: infinite_loop_bounded (Shinganパス→Gemini応答) ---');

    await page.goto(`${BASE_URL}/ui/`, { waitUntil: 'networkidle', timeout: 30000 });
    await page.waitForTimeout(2000);
    await selectAgent(page, 'infinite_loop_bounded');

    await ss(page, '09-bounded-agent-selected.png');
    console.log('  infinite_loop_bounded選択後スクリーンショット取得完了');

    console.log('  "count to 3" を送信中... (Vertex AI応答待ち)');
    await typeAndSend(page, 'count to 3');

    // 応答を最大60秒待つ（boundedは複数ループするので長め）
    responseReceived = false;
    const startTime2 = Date.now();
    while (!responseReceived && Date.now() - startTime2 < 60000) {
      await page.waitForTimeout(2000);
      const modelMsgCount = await page.locator('.model-message').count();
      if (modelMsgCount > 0) {
        responseReceived = true;
      }
      const eventCount = await page.locator('.event-container').count();
      if (eventCount > 0) {
        responseReceived = true;
      }
      if (!responseReceived) {
        const elapsed = Math.round((Date.now() - startTime2) / 1000);
        console.log(`  待機中... ${elapsed}秒`);
      }
    }

    await page.waitForTimeout(1000);

    await ss(page, '10-bounded-agent-executes.png');
    console.log(`  bounded agent応答スクリーンショット取得完了 (応答受信: ${responseReceived})`);

  } finally {
    await browser.close();
  }

  // =========================================================
  // 完了サマリー
  // =========================================================
  console.log('\n=== スクリーンショット取得完了 ===\n');
  const pngs = fs.readdirSync(SCREENSHOT_DIR).filter(f => f.endsWith('.png') && !f.startsWith('probe'));
  console.log(`取得ファイル数: ${pngs.length}`);
  for (const f of pngs.sort()) {
    const size = fs.statSync(path.join(SCREENSHOT_DIR, f)).size;
    console.log(`  ${f} (${Math.round(size / 1024)}KB)`);
  }

  // shingan-webログの最後の部分を保存（ブロックの証拠として）
  try {
    const log = execSync('tail -30 /tmp/shingan-web.log 2>/dev/null || echo "(log not found)"').toString();
    fs.writeFileSync(path.join(SCREENSHOT_DIR, 'shingan-web-log-tail.txt'), log);
    console.log('\nshingan-webログ（末尾30行）を shingan-web-log-tail.txt に保存');
  } catch (e) {
    // ignore
  }
}

main().catch(e => {
  console.error('\nFATAL ERROR:', e.message);
  process.exit(1);
});
