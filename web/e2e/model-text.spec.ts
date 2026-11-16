import { expect, test } from "@playwright/test";
import { stripModelArtifacts } from "../lib/modelText";

// Pure checks of the leaked control tag filter; no browser is started.

const answer = "FANOUT-OK\n\n<budget:token_budget>200000</budget:token_budget>";

test("a leaked budget tag is removed from a finished answer", () => {
  expect(stripModelArtifacts(answer).trim()).toBe("FANOUT-OK");
});

test("no prefix of a streaming answer shows the tag being written", () => {
  for (let end = "FANOUT-OK\n\n".length; end <= answer.length; end++) {
    const shown = stripModelArtifacts(answer.slice(0, end), true);
    expect(shown, `prefix ${end}`).not.toContain("<");
    expect(shown, `prefix ${end}`).not.toContain("budget");
  }
});

test("ordinary text and markup are left alone", () => {
  for (const text of ["<b>粗体</b>", "a < b", "预算 budget: 200", "<xs:element/>", "x <br/>"]) {
    expect(stripModelArtifacts(text)).toBe(text);
    expect(stripModelArtifacts(text, true)).toBe(text);
  }
});
