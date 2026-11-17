import { expect, test } from "@playwright/test";
import { orderForDisplay, replyRunIDs, unreadByReply } from "../lib/displayOrder";
import type { Message } from "../lib/types";

// Pure checks of where replies and live streams go; no browser is started.

let next = 0;
function msg(sender: Message["sender_type"], ordinal: number, extra: Partial<Message> = {}): Message {
  next += 1;
  return {
    id: extra.id ?? `m${next}`,
    ordinal,
    cycle_id: null,
    sender_type: sender,
    sender_bot_id: sender === "bot" ? "bot-a" : null,
    content: `${sender}-${ordinal}`,
    metadata: {},
    run_id: sender === "bot" ? `run-${ordinal}` : null,
    parent_message_id: null,
    reactions: [],
    attachments: [],
    created_at: new Date(2026, 10, 18, 12, 0, ordinal).toISOString(),
    ...extra,
  };
}

const contents = (list: Message[]) => list.map((item) => item.content);

test("a reply goes before the message that arrived while it was written", () => {
  const question = msg("user", 1);
  const interjection = msg("user", 2);
  const answer = msg("bot", 3, { metadata: { seen_ordinal: 1 } });
  const followUp = msg("bot", 4, { metadata: { seen_ordinal: 3 } });
  expect(contents(orderForDisplay([question, interjection, answer, followUp]))).toEqual([
    "user-1",
    "bot-3",
    "user-2",
    "bot-4",
  ]);
});

test("a reply that read the interjection stays after it", () => {
  const list = [msg("user", 1), msg("user", 2), msg("bot", 3, { metadata: { seen_ordinal: 2 } })];
  expect(contents(orderForDisplay(list))).toEqual(["user-1", "user-2", "bot-3"]);
});

test("group: the next member read the supplement, the first did not", () => {
  const list = [
    msg("user", 1),
    msg("user", 2),
    msg("bot", 3, { metadata: { seen_ordinal: 1 } }),
    msg("bot", 4, { metadata: { seen_ordinal: 3 } }),
  ];
  expect(contents(orderForDisplay(list))).toEqual(["user-1", "bot-3", "user-2", "bot-4"]);
});

test("notices and private-message cards keep their place", () => {
  const list = [
    msg("user", 1),
    msg("system", 2),
    msg("user", 3),
    msg("bot", 4, { metadata: { seen_ordinal: 1 } }),
  ];
  expect(contents(orderForDisplay(list))).toEqual(["user-1", "system-2", "bot-4", "user-3"]);
});

test("replies without a recorded position keep the stored order", () => {
  const list = [msg("user", 1), msg("user", 2), msg("bot", 3)];
  expect(contents(orderForDisplay(list))).toEqual(["user-1", "user-2", "bot-3"]);
});

test("a message still being sent is always unread", () => {
  const pending = msg("user", 99, { id: "pending-1" });
  expect(unreadByReply(pending, 1000)).toBe(true);
  expect(unreadByReply(msg("user", 5), 5)).toBe(false);
  expect(unreadByReply(msg("system", 9), 5)).toBe(false);
});

test("a resumed reply owns the tool calls of the runs that paused before it", () => {
  const reply = msg("bot", 7, { run_id: "run-c", metadata: { resumed_from: ["run-a", "run-b"] } });
  expect(replyRunIDs(reply)).toEqual(["run-a", "run-b", "run-c"]);
  expect(replyRunIDs(msg("user", 8))).toEqual([]);
});
