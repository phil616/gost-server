import { spawnSync } from "node:child_process";
import { createRequire } from "node:module";
import { createMemoryHistory, createPath, parsePath } from "@umijs/history";
import { describe, expect, it } from "vitest";

const require = createRequire(import.meta.url);

describe("patched query decoder", () => {
  it("preserves Umi query parsing, serialization, and navigation", () => {
    const path = createPath({
      pathname: "/login",
      query: { name: "中文 & +", tag: ["one", "two"], empty: "", flag: null },
    });
    const history = createMemoryHistory({ initialEntries: [path] });
    expect(history.location.query).toEqual({
      name: "中文 & +",
      tag: ["one", "two"],
      empty: "",
      flag: null,
    });
    history.push("/?q=hello+world&bad=%zz");
    expect(history.location.query).toEqual({ q: "hello world", bad: "%zz" });
    history.back();
    expect(history.location.pathname).toBe("/login");
    expect(parsePath("/?q=%E4%B8%AD%E6%96%87").query).toEqual({ q: "中文" });
  });

  it("handles both malformed-input DoS cases through the CommonJS caller", () => {
    // A separate process bounds regressions even if a decoder blocks its event loop.
    const result = spawnSync(
      process.execPath,
      [
        "-e",
        `const assert = require('node:assert/strict');
         const { parse } = require(${JSON.stringify(require.resolve("query-string"))});
         assert.equal(parse('q=%E4%B8%AD%E6%96%87').q, '中文');
         assert.equal(typeof parse('q=' + '%C2'.repeat(20000)).q, 'string');
         let payload = '';
         for (let i = 0; i < 32000; i++) {
           payload += '%' + Math.floor(i / 256).toString(16).padStart(2, '0')
             + '%' + (i % 256).toString(16).padStart(2, '0') + 'x';
         }
         assert.equal(typeof parse('q=' + payload + '%zz').q, 'string');`,
      ],
      { encoding: "utf8", timeout: 5000 },
    );
    expect(result.error).toBeUndefined();
    expect(result.stderr).toBe("");
    expect(result.status).toBe(0);
  }, 10000);
});
