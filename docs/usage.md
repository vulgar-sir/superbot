# How to use superbot

This guide is for people who already have a superbot instance running. The goal: get your first bot working in ten minutes, then learn how to write prompts that make a single bot, a bot group or a bot team finish real work.

> 简体中文：[usage.zh-CN.md](usage.zh-CN.md)

## 1. Configure a model first

1. Sign in with the admin account from `.env`.
2. Open **Settings → Providers → Add provider**.
3. Enter the base URL, API key and model ID, save, then click **Test**.
4. In the model catalog, set the context window, maximum output, price, and whether the model supports vision and tool calls. Prices feed the cost report.

Common configurations:

| Provider | Example base URL | Example model |
|---|---|---|
| DeepSeek | `https://api.deepseek.com` | `deepseek-chat`, `deepseek-reasoner` |
| OpenAI-compatible | the `/v1` endpoint from your provider | the model ID from your provider |
| Anthropic | configure the Anthropic Messages API | a Claude model ID |
| Ollama (local) | `http://host.docker.internal:11434/v1` | `llama3.1`, `qwen2.5`, … |

Notes:

- API keys are encrypted on the server, never returned to the browser, and never placed in a bot's prompt.
- On first boot, if no provider exists, `SUPERBOT_DEEPSEEK_API_KEY` seeds a DeepSeek provider.
- Set model capabilities conservatively. A wrong context window or vision flag causes confusing bot behavior.

## 2. Create your first bot

1. Click **+** in the sidebar, enter a name and pick an avatar.
2. Choose a default model, then open **Details**.
3. In the detail panel, configure:
   - **Persona / system prompt** — who it is, what it is good at, how it works, what it must not do.
   - **Tool policy** — which tools it may use, and which high-risk tools require approval.
   - **Workspace** — its private directory for files, scripts and artifacts.
   - **Memory** — enable long-term memory; pin important preferences.
   - **Browser / cloud computer** — enable only when the bot needs to browse or use a GUI.

Prefer **one bot, one job**: a researcher, an engineer, a reviewer and an operator are more reliable than one generalist. A group supports up to six bots; three or four specialists usually work best.

## 3. Three ways to work

| Mode | Best for | How to use it |
|---|---|---|
| **Single-bot room** | Q&A, writing, code changes, analyzing a file | Open the bot's room and send a message |
| **Group / team** | Work that benefits from roles, review and hand-offs | Create a group, pick 2–6 bots, address them with `@BotName` |
| **Routines** | Recurring work | Create a cron, `@every 1h` or one-shot routine in the bot panel |
| **Team orchestration** | Fixed processes: roundtable, pipeline, router–worker | Create a team and set member order, mode and maximum rounds |

In a group, **only mentioned bots speak**. If nobody is mentioned, the main bot answers as a fallback. Bots can also mention teammates or use `send_to_agent` for private messages; the main transcript shows only a folded summary.

## 4. How to write prompts

### 4.1 The eight parts of a good prompt

1. **Role** — who you are and what standard you work to.
2. **Goal** — what must be delivered. Not "take a look".
3. **Context** — files, links, constraints and known facts.
4. **Constraints** — what must not happen, what rules apply.
5. **Tools** — what may be used, and what needs approval.
6. **Deliverable** — format, length, file location, language.
7. **Acceptance criteria** — what "done" means, and who checks it.
8. **Evidence** — sources, test output, screenshots or command results.

Add a **stop condition**: when the bot should stop and ask you instead of guessing.

### 4.2 Bad vs. good

**Weak:**

```text
Research the AI phone market for me.
```

**Better:**

```text
You are my research assistant. You turn public information into verifiable conclusions.

Goal: research the 2026 Chinese AI phone market and produce a decision memo of at most 1,500 words.

Context:
- The reader is a product lead who cares about the competitive landscape, key players and differentiation opportunities.
- Use only publicly verifiable sources.

Constraints:
- Every key conclusion needs a source link and date.
- Separate "fact", "inference" and "unknown". Do not invent data.
- If you cannot find a reliable source, say so.

Deliverable:
1. A one-page summary (at most 10 bullets)
2. A comparison table of key players
3. Three opportunities with corresponding risks
4. A source list

Acceptance criteria: every number can be found in a source; unsourced inferences are labeled.

Evidence: list the links you actually opened at the end.

First give me a four-step plan and wait for my confirmation.
```

### 4.3 Template 1: research / analysis

```text
You are my research assistant. Goal: <the question to answer>.
Context: <known facts, files, links, who the reader is>.
Constraints: use verifiable sources only; separate fact/inference/unknown; never invent.
Deliverable: <summary + table + risks + sources>.
Acceptance: <what "done" means>.
First give me an execution plan and wait for confirmation.
```

### 4.4 Template 2: coding task

```text
@Engineer Complete <requirement> in /workspace.

Read the relevant code first and give me a minimal-change plan. Do not edit anything yet.
After I confirm:
- Change only what is necessary; do not refactor unrelated code.
- Run go test ./... and npm run typecheck; fix failures until they pass.
- Finally show a diff summary, the test commands and the real output.

Do not run git push, delete data, change production configuration, or send anything externally.
Stop and ask me when a product decision is needed.
```

### 4.5 Template 3: group collaboration

```text
@Planner Split the goal into 3–5 phases with deliverables and owners.
@Researcher Start phase 1; attach a source to every key conclusion.
@Reviewer After the researcher submits, give PASS/FAIL against the acceptance criteria. On FAIL, say who must fix what.

Group goal: <one-sentence goal>
Acceptance criteria: <checkable conditions>
Evidence: at the end of each phase, post command output, source links or screenshots.
```

### 4.6 Template 4: long-running task

```text
This is a long task that will take several rounds.

First create a checkpoint: goal, completed, pending, key decisions, open questions.
Update the checkpoint at the end of each phase. If context is running low, use search_chat_history to retrieve the original text; do not guess.
Stop and ask me when a decision is needed. Do not invent results just to "finish".
```

### 4.7 Template 5: browser task

```text
@BrowserAssistant Open <URL> and complete <read-only task>.

Only perform read-only actions. Ask before submitting a form, purchasing, sending a message or deleting anything.
Take a screenshot at each step and explain what you clicked. If an element cannot be found, stop and report instead of clicking blindly.
```

### 4.8 Template 6: scheduled routine

```text
Every day at 09:00, summarize <content> from <source> and write it to <file/room>.

If there is nothing new, reply only "no updates". Do not invent content to fill space.
If fetching fails, report the reason and the addresses you tried.
```

### 4.9 Template 7: review / QA

```text
Act as a strict reviewer and review <file/PR/design>.

List findings by severity: blocking / important / suggestion.
For each finding give: location (file:line or section), failure scenario, and fix suggestion.
Do not edit the code. Give the review verdict and evidence first.
```

## 5. Interacting with bots

- **`@BotName`** — address a specific bot; in a group, only mentioned bots respond.
- **Direct message** — in a single-bot room the bot answers; in a group the main bot is the fallback.
- **`/side`** — ask a side question without interrupting the task or writing to the transcript. Useful for "by the way, what about…".
- **Interject** — a message sent while the bot works is delivered before its next model call; if it misses the window, it gets an extra round.
- **Stop** — stop the current run at any time; tool results and checkpoints already produced are kept.
- **Approvals** — high-risk tools raise an approval card. If you deny it, the bot receives the result and adjusts instead of silently continuing.
- **Right panel** — inspect run status, tool-call timelines, tokens and cost, memory and the library.
- **Group goal** — set a goal and acceptance criteria in the group panel; the lead bot splits phases, assigns work and verifies evidence.
- **Memory** — review, pin, archive or delete memories in the bot panel. You can also say "remember that…".
- **Library** — pages, documents, files and media are collected automatically; bots can also write there.

## 6. Safe usage

- **Never put API keys in a prompt.** Use the provider settings; use the **Vault** for login credentials.
- **Approve high-risk actions deliberately**: deletes, payments, external messages, production configuration changes, unfamiliar shell commands.
- **Local device** — every `superbot device` shell command needs native approval. Do not approve a command you do not understand.
- **Verify output** — ask for sources, test output or screenshots. An AI's confidence is not evidence.
- **Use HTTPS in production** and set `SUPERBOT_COOKIE_INSECURE` / `SUPERBOT_ALLOW_INSECURE_COOKIE` to `false`.
- **Multi-tenancy** — superbot is designed for a trusted team. Do not share an admin account with strangers; run separate instances when you need isolation.

## 7. Troubleshooting

| Symptom | Check first |
|---|---|
| The bot does not reply | Provider configured? Correct model ID? Quota exhausted? Read the error in the right panel. |
| A tool is unavailable | Tool policy, whether the approval was denied, whether the sandbox image is built, whether the container can reach the network. |
| Answers are too long / too short | Put length, format and "what not to write" in the prompt. |
| Nobody speaks in a group | Mention at least one bot; check group membership; check the main bot. |
| Memory is wrong | Open the memory panel, delete the wrong memory, and tell the bot to remember the important preference. |
| A task was interrupted | Check the worker, resume from the checkpoint, or re-send the task. |
| Browser actions fail | Is the computer running? Did the page load? Does it need a login? Ask the bot for a screenshot and a report. |
| Cost is too high | Use a smaller model, limit context, ask for a plan before execution, and cap the output length. |

## 8. Complete example: three bots produce a research report

**Create three bots:**

- **Planner** — breaks goals into phases and defines acceptance criteria.
- **Researcher** — finds sources, cites them, separates fact from inference.
- **Reviewer** — finds errors, checks the evidence chain, gives PASS/FAIL.

**Create a group**, select the three bots, and send:

```text
Group goal: produce a decision memo of at most 1,500 words on the 2026 Chinese AI phone market.

@Planner Split the work into four phases with deliverables and owners. Wait for my confirmation before starting.
@Researcher In phase 1, collect public sources; attach a source link and date to every conclusion.
@Reviewer After the researcher submits, give PASS/FAIL against "every number has a source; facts and inferences are separated".

Acceptance criteria:
1. At most 10 key conclusions.
2. A comparison table of key players.
3. Three opportunities with risks.
4. A source list.
5. Any unsourced number must be labeled as inference or removed.

Post evidence at the end of each phase. On FAIL, say who must fix what.
```

**What happens next:**

1. The planner splits the phases, the researcher executes, and the reviewer checks.
2. Bots can mention teammates or use `send_to_agent` privately, so the main transcript stays readable.
3. The reviewer returns PASS/FAIL; a failed phase goes back to the responsible bot.
4. The final report is written to the library; the right panel shows tool calls and cost.
5. If you like the result, save the workflow as a team template or routine for next time.

## 9. Share this guide with your users

If you run a superbot instance, link this guide from your help page:

```text
https://your-domain/docs/usage.md
```

Or copy "Section 4: How to write prompts" and "Section 8: Complete example" into your own onboarding.

Enjoy. / 祝你用得顺手。
