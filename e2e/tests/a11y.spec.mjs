import { test, expect } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import { boot } from "./helpers.mjs";

for (const [w, h] of [[1440, 900], [390, 844]]) {
  for (const phosphor of ["amber", "green"]) {
    test(`axe WCAG 2.1 A/AA ${w}x${h} ${phosphor}`, async ({ page }) => {
      await page.setViewportSize({ width: w, height: h });
      await boot(page);
      if (phosphor === "green") await page.locator("#phosphor-toggle").click();
      // 出てくる動きを終わらせ、遊び場で1回実行した状態（ログ・要約が出た状態）も検査する
      await page.evaluate(() => document.querySelectorAll(".reveal").forEach((e) => e.classList.add("in")));
      await page.locator("#run").click();
      await page.waitForSelector("#log .entry.in[data-done] >> nth=1");
      await page.waitForTimeout(1200);
      const res = await new AxeBuilder({ page })
        .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"])
        .exclude(".crt-overlay")
        .analyze();
      const summary = res.violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(" ")).slice(0, 5).join(" | ")}`);
      expect(summary).toEqual([]);
    });
  }
}
