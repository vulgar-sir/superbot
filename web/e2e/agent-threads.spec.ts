import { expect, test } from "@playwright/test";
import { buildTranscript, itemMessage, type TranscriptItem } from "../lib/agentThreads";
import type { Message } from "../lib/types";

// Pure checks of how bot-to-bot private message cards fold in the transcript;
// no browser is started.

let counter = 0;
function message(sender: string, metadata: Record<string, string> = {}): Message {
  counter += 1;
  return {
    id: `m${counter}`,
    sender_type: sender,
    metadata,
    content: "",
    created_at: `2026-11-17T10:00:${String(counter).padStart(2, "0")}Z`,
  } as unknown as Message;
}

function card(thread: string, direction: "sent" | "received", peer = "测试"): Message {
  return message("system", {
    kind: "agent_thread",
    thread_id: thread,
    direction,
    peer_bot_name: peer,
    peer_bot_id: `id-${peer}`,
  });
}

function shape(items: TranscriptItem[]): string[] {
  return items.map((item) =>
    item.kind === "message"
      ? item.message.id
      : `[${item.threads.map((thread) => `${thread.threadID}:${thread.cards.length}`).join(",")}]`,
  );
}

test.beforeEach(() => {
  counter = 0;
});

test("an exchange folds into one summary at its latest card", () => {
  const items = buildTranscript([message("user"), card("T1", "sent"), message("bot"), card("T1", "received")]);
  expect(shape(items)).toEqual(["m1", "m3", "[T1:2]"]);
});

test("a message still waiting for its reply stays where it was sent", () => {
  expect(shape(buildTranscript([message("user"), card("T1", "sent"), message("bot")]))).toEqual(["m1", "[T1:1]", "m3"]);
});

test("exchanges next to each other share one row; a bubble in between splits them", () => {
  const items = buildTranscript([
    card("A", "sent", "甲"),
    card("B", "sent", "乙"),
    message("bot"),
    card("A", "received", "甲"),
    card("B", "received", "乙"),
    message("bot"),
    card("C", "sent", "丙"),
  ]);
  expect(shape(items)).toEqual(["m3", "[A:2,B:2]", "m6", "[C:1]"]);
  expect(itemMessage(items[1], "first").id).toBe("m4");
  expect(itemMessage(items[1], "last").id).toBe("m5");
});

test("cards without a thread id and other messages are left alone", () => {
  const items = buildTranscript([
    message("system", { kind: "agent_thread", direction: "sent" }),
    message("bot"),
    message("system", { kind: "agent_thread", direction: "received" }),
    message("system", { kind: "context_checkpoint" }),
  ]);
  expect(shape(items)).toEqual(["[:1]", "m2", "[:1]", "m4"]);
});
