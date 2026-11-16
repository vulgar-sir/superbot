// Some models now and then echo control tags from their training format into
// an answer, e.g. "<budget:token_budget>200000</budget:token_budget>". The
// server strips them before saving (internal/llm/artifacts.go); this does the
// same while the answer is still streaming, including a tag that is only
// half written so far. Only the budget: namespace is touched.
const PAIR = /<budget:[A-Za-z_]+>[^<]{0,200}<\/budget:[A-Za-z_]+>/g;
const LONE = /<\/?budget:[A-Za-z_]+\s*\/?>/g;
const PARTIAL = /<\/?(?:b(?:u(?:d(?:g(?:e(?:t(?::[A-Za-z_]*(?:>[^<]{0,200}(?:<\/?[A-Za-z_:]*)?)?)?)?)?)?)?)?)?$/;

export function stripModelArtifacts(text: string, streaming = false): string {
  let out = text.includes("budget:") ? text.replace(PAIR, "").replace(LONE, "") : text;
  if (streaming) out = out.replace(PARTIAL, "");
  return out;
}
