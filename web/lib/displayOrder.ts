import type { Message } from "./types";

// How far into the conversation a bot had read when it wrote this reply
// (the engine records it as metadata.seen_ordinal); 0 when unknown.
export function replySeenOrdinal(message: Message): number {
  if (message.sender_type !== "bot") return 0;
  const seen = Number(message.metadata?.seen_ordinal ?? 0);
  return Number.isFinite(seen) && seen > 0 ? seen : 0;
}

// A user message the reply never read: it arrived while the reply was being
// written. A message still on its way to the server is always one.
export function unreadByReply(message: Message, seen: number): boolean {
  if (message.sender_type !== "user") return false;
  return message.id.startsWith("pending-") || message.ordinal > seen;
}

// orderForDisplay shows each bot reply before the user messages it never
// read, the way the engine orders history for the model (orderByDelivery).
// Ordinals record when a row was stored, and a reply is stored when it is
// finished: a message sent while the bot was still answering would otherwise
// land between the question and its answer, and the answer would read as a
// reply to it. Only user messages are passed over, so notices and private
// message cards keep their place.
export function orderForDisplay(messages: Message[]): Message[] {
  const out: Message[] = [];
  for (const message of messages) {
    const seen = replySeenOrdinal(message);
    let at = out.length;
    if (seen > 0) {
      while (at > 0 && unreadByReply(out[at - 1], seen)) at -= 1;
    }
    out.splice(at, 0, message);
  }
  return out;
}

// The highest stored ordinal; messages still on their way have none yet.
export function latestOrdinal(messages: Message[]): number {
  let latest = 0;
  for (const message of messages) {
    if (!message.id.startsWith("pending-") && message.ordinal > latest) latest = message.ordinal;
  }
  return latest;
}

// The runs whose tool calls belong to a reply: its own, and the runs that
// paused for a tool approval on the way to it.
export function replyRunIDs(message: Message): string[] {
  if (!message.run_id) return [];
  const prior = message.metadata?.resumed_from;
  const ids = Array.isArray(prior) ? prior.map((id) => String(id)).filter(Boolean) : [];
  return [...ids, message.run_id];
}
