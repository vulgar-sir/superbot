import type { Bot } from "./types";

/**
 * The profile PATCH endpoint overwrites every profile field it receives,
 * including title/description/avatar. Always send the full existing profile
 * when changing one field (pin, section, position, rename).
 */
export function botProfilePayload(bot: Bot) {
  return {
    name: bot.name,
    title: bot.title,
    description: bot.description,
    avatar: bot.avatar,
    avatar_shape: bot.avatar_shape,
    avatar_color: bot.avatar_color,
    section_id: bot.section_id,
    position: bot.position,
    pinned: bot.pinned,
  };
}
