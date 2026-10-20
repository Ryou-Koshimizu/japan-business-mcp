import { expect } from "@playwright/test";

// 起動（WebAssembly の読み込み・MCP の初期化）と、見出しの打ち込みが終わるまで待つ。
export async function boot(page, { path = "/" } = {}) {
  await page.goto(path);
  await page.waitForSelector("html.booted", { timeout: 30_000 });
  await page.waitForSelector("html[data-mcp=ready]", { timeout: 30_000 });
  await page.waitForSelector("#hero-title.done", { timeout: 15_000 });
  await page.evaluate(() => document.fonts.ready);
  // 起動の画面が消える動き（0.5秒）を待つ
  await expect(page.locator("#boot")).toBeHidden();
}

export const SIZES = [
  [280, 653], [320, 568], [360, 640], [375, 667], [390, 844], [393, 852], [412, 915], [430, 932],
  [568, 320], [667, 375], [844, 390], [932, 430],
  [600, 960], [768, 1024], [820, 1180], [1024, 768], [1024, 1366], [1180, 820],
  [1280, 720], [1366, 768], [1440, 900], [1536, 864], [1920, 1080], [2560, 1440], [3840, 2160],
];
