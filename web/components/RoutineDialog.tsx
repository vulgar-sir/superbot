"use client";

import { Modal } from "./ui/Modal";
import { BotRoutineList } from "./BotRoutineList";
import type { Bot } from "@/lib/types";

export function RoutineDialog({ bot, onClose }: { bot: Bot; onClose: () => void }) {
  return (
    <Modal
      open
      onClose={onClose}
      title={`${bot.name} 的例程`}
      subtitle="到点后会在它的私聊里自动开工，并把结果写进动态。"
      size="md"
    >
      <BotRoutineList bot={bot} />
    </Modal>
  );
}
