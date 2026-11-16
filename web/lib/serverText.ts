// Fixed texts the server writes into message metadata and cycle events. They
// are stored in Chinese; map the known ones to dictionary keys so the UI can
// show them in the reader's language. Unknown texts are shown as-is.
const SERVER_TEXT_KEYS: Record<string, string> = {
  "回复因输出长度上限被截断": "warning.outputTruncated",
  "达到工具调用步数上限": "warning.toolStepLimit",
  "流水线已完成": "warning.pipelineDone",
  "同一 Bot 连续发言达到上限": "warning.sameBotLimit",
  "协调者没有选出有效成员": "warning.noValidMember",
  "达到团队最大协作轮数": "warning.teamRoundLimit",
  "worker 多次中断，已停止自动重试": "chat.cycle.workerGaveUp",
};

export function localizeServerText(text: string, t: (key: string) => string): string {
  const key = SERVER_TEXT_KEYS[text.trim()];
  return key ? t(key) : text;
}
