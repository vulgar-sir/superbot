import type { Message } from "./types";

// Cards that record bot-to-bot private messages ("已发消息给 X", "来自 X").
export function isAgentThreadCard(message: Message): boolean {
  return message.sender_type === "system" && message.metadata?.kind === "agent_thread";
}

export interface AgentThreadSummary {
  threadID: string;
  conversationID: string;
  peerBotID: string;
  peerName: string;
  // Every card of the thread in this conversation, oldest first.
  cards: Message[];
  latest: Message;
}

export type TranscriptItem =
  | { kind: "message"; message: Message }
  | { kind: "agents"; key: string; threads: AgentThreadSummary[] };

function threadKey(message: Message): string {
  const id = message.metadata?.thread_id;
  return typeof id === "string" && id ? id : message.id;
}

export function summarizeThread(cards: Message[]): AgentThreadSummary {
  const latest = cards[cards.length - 1];
  const named = [...cards].reverse().find((card) => String(card.metadata?.peer_bot_name ?? "")) ?? latest;
  return {
    threadID: String(latest.metadata?.thread_id ?? ""),
    conversationID: String(latest.metadata?.conversation_id ?? ""),
    peerBotID: String(named.metadata?.peer_bot_id ?? ""),
    peerName: String(named.metadata?.peer_bot_name ?? ""),
    cards,
    latest,
  };
}

// buildTranscript folds bot-to-bot private messages. Every card of one thread
// becomes a single summary, shown where the thread's latest card is (so the
// summary sits next to whatever the exchange led to), and summaries that end
// up next to each other share one row.
export function buildTranscript(messages: Message[]): TranscriptItem[] {
  const threads = new Map<string, Message[]>();
  for (const message of messages) {
    if (!isAgentThreadCard(message)) continue;
    const key = threadKey(message);
    const cards = threads.get(key);
    if (cards) cards.push(message);
    else threads.set(key, [message]);
  }
  const items: TranscriptItem[] = [];
  for (const message of messages) {
    if (!isAgentThreadCard(message)) {
      items.push({ kind: "message", message });
      continue;
    }
    const cards = threads.get(threadKey(message))!;
    if (cards[cards.length - 1] !== message) continue;
    const summary = summarizeThread(cards);
    const previous = items[items.length - 1];
    if (previous?.kind === "agents") previous.threads.push(summary);
    else items.push({ kind: "agents", key: message.id, threads: [summary] });
  }
  return items;
}

// The message a transcript item is placed at: for a row of thread summaries,
// the first or last summary's latest card.
export function itemMessage(item: TranscriptItem, edge: "first" | "last" = "last"): Message {
  if (item.kind === "message") return item.message;
  const thread = edge === "first" ? item.threads[0] : item.threads[item.threads.length - 1];
  return thread.latest;
}
