import { test, expect } from "@playwright/test";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { boot } from "./helpers.mjs";

const here = path.dirname(fileURLToPath(import.meta.url));
const examples = JSON.parse(fs.readFileSync(path.join(here, "../../web/examples.json"), "utf8"));
const golden = (name) => JSON.parse(fs.readFileSync(path.join(here, "../../internal/testcases/golden", `${name}.json`), "utf8"));

async function lastResponse(page, count) {
  await expect(page.locator("#log .entry.in[data-done]")).toHaveCount(count);
  const text = await page.locator("#log .entry.in[data-done]").last().locator("pre").textContent();
  return JSON.parse(text);
}

// 画面の操作で全部の例を実行し、表示された返事がゴールデン（Go の試験と同じ正解）と一致する＝誤答 0
test("全部の例を画面から実行して、返事が正解と一致する", async ({ page }) => {
  test.setTimeout(120_000);
  await page.setViewportSize({ width: 1280, height: 900 });
  await boot(page);
  let n = await page.locator("#log .entry.in[data-done]").count();
  for (const ex of examples) {
    await page.locator(`.tool-btn[data-tool="${ex.tool}"]`).click();
    await expect(page.locator("#console-title")).toHaveText(ex.tool);
    await page.locator(`.chip[data-example="${ex.name}"]`).click();
    await expect(page.locator(`.chip[data-example="${ex.name}"]`)).toHaveAttribute("aria-pressed", "true");
    expect(JSON.parse(await page.locator("#args").inputValue())).toEqual(ex.args);
    await page.locator("#run").click();
    n++;
    const res = await lastResponse(page, n);
    expect(res.result.isError, ex.name).toBeFalsy();
    expect(res.result.structuredContent, ex.name).toEqual(golden(ex.name));
    const summary = res.result.structuredContent.summary || res.result.structuredContent.output;
    if (summary) await expect(page.locator("#summary")).toHaveText(summary);
    await expect(page.locator("#summary")).not.toHaveClass(/err/);
  }
});

test("誤った入力はエラーとして見せ、続けて使える", async ({ page }) => {
  await boot(page);
  await page.locator('.tool-btn[data-tool="wareki_convert"]').click();
  await page.locator("#args").fill('{"input": "平成31年5月1日"}');
  await page.locator("#run").click();
  await expect(page.locator("#summary")).toHaveClass(/err/);
  await expect(page.locator("#summary")).toContainText("令和元年5月1日");
  // 壊れた JSON
  await page.locator("#args").fill("{ input: ");
  await page.locator("#run").click();
  await expect(page.locator("#args")).toHaveClass(/invalid/);
  await expect(page.locator("#summary")).toContainText("JSON");
  // 直せば動く（Ctrl/⌘+Enter）
  await page.locator("#args").fill('{"input": "R8.10.20"}');
  await page.locator("#args").press("ControlOrMeta+Enter");
  await expect(page.locator("#summary")).toHaveText("令和8年10月20日 = 2026年10月20日");
  await expect(page.locator("#summary")).not.toHaveClass(/err/);
});

test("道具の選択は矢印キーでも動く", async ({ page }) => {
  await boot(page);
  const first = page.locator(".tool-btn").first();
  await first.focus();
  await page.keyboard.press("ArrowDown");
  await expect(page.locator(".tool-btn").nth(1)).toHaveAttribute("aria-checked", "true");
  await expect(page.locator(".tool-btn").nth(1)).toBeFocused();
  await page.keyboard.press("ArrowUp");
  await expect(first).toHaveAttribute("aria-checked", "true");
});

test("道具・資料・依頼文の一覧は tools/list などの返事から作られる", async ({ page }) => {
  await boot(page);
  await expect(page.locator("#ls .ls-row")).toHaveCount(12);
  await expect(page.locator("#resources li")).toHaveCount(4);
  await expect(page.locator("#prompts li")).toHaveCount(3);
  await page.locator("#ls .ls-try", { hasText: "payment_due_date" }).click();
  await expect(page.locator("#console-title")).toHaveText("payment_due_date");
});

test("導入のタブ・写すボタン・色の切り替え", async ({ page, browserName, context }) => {
  if (browserName === "chromium") await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  await boot(page);
  await page.locator("#tab-code").click();
  await expect(page.locator("#panel-code")).toBeVisible();
  await expect(page.locator("#panel-desktop")).toBeHidden();
  await page.locator("#tab-code").press("ArrowRight");
  await expect(page.locator("#tab-cursor")).toBeFocused();
  await expect(page.locator("#panel-cursor")).toBeVisible();
  const copy = page.locator('[data-copy="code-install"]');
  await copy.click();
  await expect(copy).toHaveClass(/done/);
  if (browserName === "chromium") {
    const clip = await page.evaluate(() => navigator.clipboard.readText());
    expect(clip).toBe("go install github.com/Ryou-Koshimizu/japan-business-mcp/cmd/japan-business-mcp@latest");
  }
  await page.locator("#phosphor-toggle").click();
  await expect(page.locator("html")).toHaveAttribute("data-phosphor", "green");
  await page.reload();
  await page.waitForSelector("html[data-mcp=ready]");
  await expect(page.locator("html")).toHaveAttribute("data-phosphor", "green");
});

test("確かめたことの数字は実機試験の結果（stats.js）と同じ", async ({ page }) => {
  await boot(page);
  const stats = await page.evaluate(() => window.JBIZ_STATS);
  expect(stats.length).toBeGreaterThan(0);
  await page.locator("#proof").scrollIntoViewIfNeeded();
  for (const [i, s] of stats.entries()) {
    await expect(page.locator(".stat .n").nth(i)).toHaveText(s.value.toLocaleString("ja-JP"), { timeout: 5000 });
  }
});

test("動きを減らす設定では、起動の画面と打ち込みを出さない", async ({ browser }) => {
  const ctx = await browser.newContext({ reducedMotion: "reduce" });
  const page = await ctx.newPage();
  await page.goto("/");
  await expect(page.locator("html")).toHaveClass(/booted/);
  await expect(page.locator("#hero-title")).toHaveText("日本の業務を、AI の道具に。");
  await page.waitForSelector("html[data-mcp=ready]");
  await expect(page.locator("#boot")).toBeHidden();
  await ctx.close();
});

// Linux の WebKit（CI）では、モニターが動いている間 requestAnimationFrame が来ず、返事の表示が途中で止まった。
// rAF を止めた状態でも、返事が最後まで表示され、要約が出ることを確かめる（回帰試験）。
test("描画の間引きで requestAnimationFrame が来なくても、返事は最後まで出る", async ({ page }) => {
  await page.addInitScript(() => { window.requestAnimationFrame = () => 0; });
  await boot(page);
  const n = await page.locator("#log .entry.in[data-done]").count();
  await page.locator('.tool-btn[data-tool="wareki_convert"]').click();
  await page.locator('.chip[data-example="wareki_reiwa_kanji"]').click();
  await page.locator("#run").click();
  await expect(page.locator("#log .entry.in[data-done]")).toHaveCount(n + 1);
  await expect(page.locator("#summary")).toHaveText("令和8年10月20日 = 2026年10月20日");
  await page.locator("#args").fill('{"input": "平成31年5月1日"}');
  await page.locator("#run").click();
  await expect(page.locator("#summary")).toHaveClass(/err/);
});
