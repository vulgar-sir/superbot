type CanvasObject = Record<string, unknown> & { id: string; name?: string };

type CanvasDocument = { version: 1; objects: CanvasObject[]; background?: string };

function parse(text: string): CanvasDocument | null {
  try {
    const value = JSON.parse(text) as CanvasDocument;
    if (value && Array.isArray(value.objects)) return value;
  } catch {
    // not JSON
  }
  return null;
}

function same(a: unknown, b: unknown) {
  return JSON.stringify(a) === JSON.stringify(b);
}

function cloneWithID(object: CanvasObject, suffix: string): CanvasObject {
  return {
    ...object,
    id: `conflict-${Date.now()}-${Math.random().toString(36).slice(2)}`,
    name: `${object.name || object.type || "对象"}（冲突${suffix}）`,
  };
}

export function mergeCanvasContent(baseText: string, localText: string, remoteText: string): { text: string; conflicts: number } | null {
  const baseDoc = parse(baseText);
  const localDoc = parse(localText);
  const remoteDoc = parse(remoteText);
  if (!localDoc || !remoteDoc) return null;
  if (!baseDoc) return { text: JSON.stringify(remoteDoc), conflicts: 1 };

  const base = new Map(baseDoc.objects.map((object) => [object.id, object]));
  const local = new Map(localDoc.objects.map((object) => [object.id, object]));
  const remote = new Map(remoteDoc.objects.map((object) => [object.id, object]));
  const order: string[] = [];
  for (const object of localDoc.objects) order.push(object.id);
  for (const object of remoteDoc.objects) {
    if (!order.includes(object.id)) order.push(object.id);
  }
  for (const id of base.keys()) {
    if (!order.includes(id)) order.push(id);
  }

  const objects: CanvasObject[] = [];
  let conflicts = 0;
  const seen = new Set<string>();
  for (const id of order) {
    if (seen.has(id)) continue;
    seen.add(id);
    const b = base.get(id);
    const l = local.get(id);
    const r = remote.get(id);
    if (same(l, b)) {
      if (r) objects.push(r);
      continue;
    }
    if (same(r, b)) {
      if (l) objects.push(l);
      continue;
    }
    if (same(l, r)) {
      if (l) objects.push(l);
      continue;
    }
    if (!l && r) {
      objects.push(r);
      conflicts++;
      continue;
    }
    if (l && !r) {
      objects.push(l);
      conflicts++;
      continue;
    }
    if (l && r) {
      objects.push(l);
      objects.push(cloneWithID(r, "副本"));
      conflicts++;
    }
  }
  return {
    text: JSON.stringify({ ...localDoc, version: 1, objects }),
    conflicts,
  };
}
