import { test, expect } from "@playwright/test";
import { boot, SIZES } from "./helpers.mjs";

// 25の画面の大きさで：横はみ出し0・ヘッダー80px・ヒーローは画面の高さ−80px・ヒーローの中身は最初の画面に収まる・重ならない・欠けない
for (const [w, h] of SIZES) {
  test(`${w}x${h}`, async ({ page }) => {
    await page.setViewportSize({ width: w, height: h });
    await boot(page);
    const m = await page.evaluate(() => {
      const vw = innerWidth;
      const vh = innerHeight;
      const r = (el) => { const b = el.getBoundingClientRect(); return { l: b.left, t: b.top, r: b.right, b: b.bottom, w: b.width, h: b.height }; };
      const visible = (el) => { const s = getComputedStyle(el); const b = el.getBoundingClientRect(); return s.display !== "none" && s.visibility !== "hidden" && b.width > 0 && b.height > 0; };
      const header = r(document.querySelector(".site-header"));
      const hero = r(document.querySelector("#hero"));
      const parts = [".eyebrow", "#hero-title", ".lead", ".hero-actions"].map((s) => document.querySelector(s)).filter(visible);
      const partBoxes = parts.map((el) => ({ sel: el.className || el.id, ...r(el) }));
      const buttons = [...document.querySelectorAll(".hero-actions .btn")].map((el) => ({ ...r(el), clip: el.scrollWidth - el.clientWidth }));
      // 欠け：はみ出しを切る箱（overflow が visible でない）の中で、中身が箱より大きいもの。
      // ヒーローははみ出しを切るので、ヒーローの文字（実際の字形の範囲）がヒーローの中と画面の中に収まっているかも見る。
      const clipped = [];
      for (const el of document.querySelectorAll("#hero, #hero *, .btn, .tool-btn, .chip, .stat, .ls-row, .code pre")) {
        if (!visible(el) || el.matches(".sr-only")) continue;
        const s = getComputedStyle(el);
        const clips = s.overflowX !== "visible" || s.overflowY !== "visible";
        if (clips && el.matches(".hero, .monitor-bezel, .monitor-feed, .monitor")) continue; // ヒーローは下で字形の範囲を見る・モニターは飾り
        if (clips && (el.scrollWidth > el.clientWidth + 1 || el.scrollHeight > el.clientHeight + 1)) clipped.push(el.className || el.id || el.tagName);
      }
      const heroBox = document.querySelector("#hero").getBoundingClientRect();
      for (const el of document.querySelectorAll(".eyebrow, #hero-title, .lead, .hero-actions .btn")) {
        if (!visible(el)) continue;
        const range = document.createRange();
        range.selectNodeContents(el);
        for (const q of range.getClientRects()) {
          if (q.width === 0) continue;
          if (q.top < heroBox.top - 0.5 || q.bottom > Math.min(heroBox.bottom, vh) + 0.5 || q.left < -0.5 || q.right > vw + 0.5) {
            clipped.push(`${el.className || el.id} の文字が画面・ヒーローの外（${Math.round(q.top)}〜${Math.round(q.bottom)}）`);
            break;
          }
        }
      }
      // ページ全体：画面の左右からはみ出す要素（スクロールする箱の中身と、意図して画面外に置く要素は除く）
      const outside = [];
      for (const el of document.querySelectorAll("body *")) {
        if (!visible(el) || el.closest(".skip, .sr-only, svg, .boot, .crt-overlay, .log, .monitor-feed")) continue;
        const b = el.getBoundingClientRect();
        if (b.right > vw + 0.5 || b.left < -0.5) outside.push(`${el.tagName}.${el.className}`);
      }
      return {
        vw, vh, scrollW: document.documentElement.scrollWidth, bodyScrollW: document.body.scrollWidth,
        header, hero, partBoxes, buttons, clipped, outside: outside.slice(0, 8),
      };
    });
    // 横はみ出し（横スクロール）0
    expect(m.scrollW, "横スクロール").toBeLessThanOrEqual(m.vw);
    expect(m.bodyScrollW).toBeLessThanOrEqual(m.vw);
    expect(m.outside, "画面の左右からはみ出す要素").toEqual([]);
    // ヘッダー 80px、ヒーローは画面の高さ − 80px（ヘッダー＋ヒーローで画面1枚）
    expect(Math.round(m.header.h)).toBe(80);
    expect(Math.abs(m.hero.h - (m.vh - 80))).toBeLessThanOrEqual(1);
    expect(Math.round(m.hero.t)).toBe(80);
    // ヒーローの中身は最初の画面に収まる
    for (const p of m.partBoxes) {
      expect(p.t, `${p.sel} の上端`).toBeGreaterThanOrEqual(80 - 0.5);
      expect(p.b, `${p.sel} の下端`).toBeLessThanOrEqual(m.vh + 0.5);
      expect(p.l, `${p.sel} の左端`).toBeGreaterThanOrEqual(-0.5);
      expect(p.r, `${p.sel} の右端`).toBeLessThanOrEqual(m.vw + 0.5);
    }
    for (const b of m.buttons) {
      expect(b.b).toBeLessThanOrEqual(m.vh + 0.5);
      expect(b.r).toBeLessThanOrEqual(m.vw + 0.5);
      expect(b.h).toBeGreaterThanOrEqual(40); // 押しやすい大きさ
    }
    // 重ならない（縦に並ぶ部品どうし・ボタンどうし）
    const overlap = (a, b) => a.l < b.r - 0.5 && b.l < a.r - 0.5 && a.t < b.b - 0.5 && b.t < a.b - 0.5;
    for (let i = 0; i < m.partBoxes.length; i++) for (let j = i + 1; j < m.partBoxes.length; j++) {
      expect(overlap(m.partBoxes[i], m.partBoxes[j]), `${m.partBoxes[i].sel} と ${m.partBoxes[j].sel} が重なる`).toBe(false);
    }
    for (let i = 0; i < m.buttons.length; i++) for (let j = i + 1; j < m.buttons.length; j++) {
      expect(overlap(m.buttons[i], m.buttons[j]), "ボタンが重なる").toBe(false);
    }
    // 文字の欠け 0
    expect(m.clipped, "欠けている要素").toEqual([]);
  });
}
