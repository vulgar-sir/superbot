type Hunk = { start: number; end: number; lines: string[] };

function diffHunks(base: string[], other: string[]): Hunk[] {
  const n = base.length;
  const m = other.length;
  const dp: number[][] = Array.from({ length: n + 1 }, () => new Array<number>(m + 1).fill(0));
  for (let i = n - 1; i >= 0; i--) {
    for (let j = m - 1; j >= 0; j--) {
      dp[i][j] = base[i] === other[j] ? dp[i + 1][j + 1] + 1 : Math.max(dp[i + 1][j], dp[i][j + 1]);
    }
  }
  const hunks: Hunk[] = [];
  let i = 0;
  let j = 0;
  while (i < n || j < m) {
    if (i < n && j < m && base[i] === other[j]) {
      i++;
      j++;
      continue;
    }
    const start = i;
    const lines: string[] = [];
    while (i < n || j < m) {
      if (i < n && j < m && base[i] === other[j]) break;
      if (j < m && (i >= n || dp[i][j + 1] >= dp[i + 1][j])) {
        lines.push(other[j]);
        j++;
      } else if (i < n) {
        i++;
      }
    }
    hunks.push({ start, end: i, lines });
  }
  return hunks;
}

function overlaps(a: Hunk, b: Hunk): boolean {
  const aInsert = a.start === a.end;
  const bInsert = b.start === b.end;
  if (aInsert && bInsert) return a.start === b.start;
  if (aInsert) return b.start < a.start && a.start < b.end;
  if (bInsert) return a.start < b.start && b.start < a.end;
  return a.start < b.end && b.start < a.end;
}

export function mergeDocumentContent(baseText: string, localText: string, remoteText: string): { text: string; conflicts: number } {
  if (baseText === localText) return { text: remoteText, conflicts: 0 };
  if (baseText === remoteText) return { text: localText, conflicts: 0 };
  if (localText === remoteText) return { text: localText, conflicts: 0 };
  if (localText.trim().startsWith("{") || remoteText.trim().startsWith("{")) {
    return { text: remoteText, conflicts: 1 };
  }

  const base = baseText.split("\n");
  const local = diffHunks(base, localText.split("\n"));
  const remote = diffHunks(base, remoteText.split("\n"));
  const output: string[] = [];
  let basePos = 0;
  let li = 0;
  let ri = 0;
  let conflicts = 0;

  const applyHunk = (hunk: Hunk) => {
    output.push(...base.slice(basePos, hunk.start));
    output.push(...hunk.lines);
    basePos = hunk.end;
  };

  while (li < local.length || ri < remote.length) {
    if (li >= local.length) {
      applyHunk(remote[ri++]);
      continue;
    }
    if (ri >= remote.length) {
      applyHunk(local[li++]);
      continue;
    }
    const a = local[li];
    const b = remote[ri];
    if (a.start === b.start && a.end === b.end && a.lines.join("\n") === b.lines.join("\n")) {
      applyHunk(a);
      li++;
      ri++;
      continue;
    }
    if (!overlaps(a, b)) {
      if (a.start <= b.start) applyHunk(a), li++;
      else applyHunk(b), ri++;
      continue;
    }
    output.push(...base.slice(basePos, Math.min(a.start, b.start)));
    output.push("<<<<<<< LOCAL");
    output.push(...a.lines);
    output.push("=======");
    output.push(...b.lines);
    output.push(">>>>>>> REMOTE");
    basePos = Math.max(a.end, b.end);
    conflicts++;
    li++;
    ri++;
  }
  output.push(...base.slice(basePos));
  return { text: output.join("\n"), conflicts };
}
