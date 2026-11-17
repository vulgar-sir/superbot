#!/usr/bin/env python3
"""Real-model acceptance run for the runtime features.

Runs on the server against the live deployment with the configured provider
(no mocks): it creates temporary bots and groups named "验收-…", drives real
conversations through the HTTP API, checks the outcomes, and deletes what it
created. Every scenario reports PASS/FAIL with the evidence it looked at.

Usage (on the server):
    python3 deploy/acceptance_runtime.py            # all scenarios
    python3 deploy/acceptance_runtime.py closure goal   # only these

Reads the admin credentials from /srv/superbot/.env; never prints them.
"""

import http.cookiejar
import json
import os
import subprocess
import sys
import time
import traceback
import urllib.error
import urllib.parse
import urllib.request
import uuid
from pathlib import Path

BASE = os.environ.get("SUPERBOT_ACCEPT_BASE", "http://127.0.0.1:32608")
ENV_FILE = Path(os.environ.get("SUPERBOT_ACCEPT_ENV", "/srv/superbot/.env"))
DATA_DIR = Path(os.environ.get("SUPERBOT_ACCEPT_DATA", "/srv/superbot/data"))
PREFIX = "验收-"
TAG = uuid.uuid4().hex[:6]


def read_env():
    values = {}
    for line in ENV_FILE.read_text(encoding="utf-8", errors="replace").splitlines():
        if "=" not in line or line.lstrip().startswith("#"):
            continue
        key, value = line.split("=", 1)
        values[key.strip()] = value.strip().strip('"')
    return values


env = read_env()
jar = http.cookiejar.CookieJar()
opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))


def call(method, path, payload=None, timeout=60, ok=(200, 201, 202, 204)):
    data = json.dumps(payload).encode() if payload is not None else None
    request = urllib.request.Request(
        BASE + path, data=data, method=method,
        headers={"Content-Type": "application/json", "Origin": BASE},
    )
    try:
        with opener.open(request, timeout=timeout) as response:
            body = response.read()
            status = response.status
    except urllib.error.HTTPError as error:
        body = error.read()
        status = error.code
    parsed = None
    if body:
        try:
            parsed = json.loads(body)
        except ValueError:
            parsed = {"raw": body[:300].decode("utf-8", "replace")}
    if ok is not None and status not in ok:
        raise RuntimeError(f"{method} {path} -> {status}: {json.dumps(parsed, ensure_ascii=False)[:300]}")
    return status, parsed


def sql(query):
    """Read-only peek into the database for facts the API does not expose."""
    result = subprocess.run(
        ["docker", "exec", "superbot-postgres", "psql", "-U", "superbot", "-d", "superbot", "-AtF", "\t", "-c", query],
        capture_output=True, text=True, timeout=30,
    )
    if result.returncode != 0:
        raise RuntimeError("psql failed: " + result.stderr.strip()[:300])
    return [line.split("\t") for line in result.stdout.splitlines() if line]


def sql_exec(query):
    result = subprocess.run(
        ["docker", "exec", "-i", "superbot-postgres", "psql", "-U", "superbot", "-d", "superbot", "-v", "ON_ERROR_STOP=1", "-q"],
        input=query, capture_output=True, text=True, timeout=60,
    )
    if result.returncode != 0:
        raise RuntimeError("psql failed: " + result.stderr.strip()[:300])


class World:
    def __init__(self):
        self.bots = []
        self.teams = []
        self.rules = []
        self.provider_id = None
        self.model = None

    def login(self):
        call("POST", "/api/auth/login", {
            "email": env.get("SUPERBOT_ADMIN_EMAIL", ""), "password": env.get("SUPERBOT_ADMIN_PASSWORD", ""),
        })
        _, providers = call("GET", "/api/providers")
        llm = [p for p in providers["providers"] if p.get("type") == "llm" and p.get("enabled")]
        if not llm:
            raise RuntimeError("no enabled LLM provider configured")
        self.provider_id = llm[0]["id"]
        _, models = call("GET", f"/api/providers/{self.provider_id}/models")
        self.model = models["models"][0]["name"]
        _, permissions = call("GET", "/api/permissions")
        print(f"provider={llm[0]['name']} kind={llm[0]['kind']} model={self.model} "
              f"auto_review={permissions.get('auto_review')}")

    def bot(self, name, prompt="", model=None, memory=False, profile="browser"):
        _, data = call("POST", "/api/bots", {
            "name": f"{PREFIX}{name}-{TAG}", "slug": f"accept-{TAG}-{uuid.uuid4().hex[:6]}",
            "system_prompt": prompt, "provider_id": self.provider_id,
            "model": model or self.model, "memory_enabled": memory, "computer_profile": profile,
        })
        self.bots.append(data["bot"]["id"])
        return data["bot"]

    def direct(self, bot):
        _, data = call("POST", "/api/conversations", {"kind": "direct", "bot_id": bot["id"]})
        return data["conversation"]["id"]

    def team(self, name, mode, bots, lead=None):
        config = {"max_rounds": 8}
        if lead:
            config["primary_bot_id"] = lead["id"]
        _, data = call("POST", "/api/teams", {
            "name": f"{PREFIX}{name}-{TAG}", "mode": mode, "config": config, "bot_ids": [b["id"] for b in bots],
        })
        team = data["team"]
        self.teams.append(team["id"])
        rows = sql(f"select id from conversations where team_id = '{team['id']}' order by created_at limit 1")
        if rows:
            return team, rows[0][0]
        _, conv = call("POST", "/api/conversations", {"kind": "team", "team_id": team["id"]})
        return team, conv["conversation"]["id"]

    def cleanup(self):
        for rule_id in self.rules:
            call("DELETE", f"/api/permissions/rules/{rule_id}", {}, ok=None)
        for team_id in self.teams:
            call("DELETE", f"/api/teams/{team_id}", {}, ok=None)
        for bot_id in self.bots:
            call("DELETE", f"/api/bots/{bot_id}", {}, ok=None)
        leftovers = sql(f"select count(*) from bots where name like '{PREFIX}%-{TAG}'")
        print(f"cleanup: temporary bots left = {leftovers[0][0]}")


def send(conv, content):
    _, data = call("POST", f"/api/conversations/{conv}/messages", {"content": content, "client_msg_id": uuid.uuid4().hex})
    return data


def messages(conv):
    _, data = call("GET", f"/api/conversations/{conv}")
    return data["messages"]


def bot_texts(conv):
    return [m["content"] for m in messages(conv) if m["sender_type"] == "bot"]


def idle(conv):
    rows = sql(f"select count(*) from cycles where conversation_id = '{conv}' and status in ('queued','running')")
    return rows[0][0] == "0"


def wait(predicate, timeout, step=2.0, what=""):
    deadline = time.time() + timeout
    while time.time() < deadline:
        value = predicate()
        if value:
            return value
        time.sleep(step)
    raise TimeoutError(f"timed out after {timeout}s waiting for {what}")


def wait_idle(conv, timeout=240):
    time.sleep(1.5)
    wait(lambda: idle(conv), timeout, what="the conversation to go idle")


def cycle_rows(conv):
    return sql(f"select status, attempt, coalesce(error,'') from cycles where conversation_id = '{conv}' order by created_at")


# --- scenarios ----------------------------------------------------------------

def scenario_interjection(world):
    """A message sent while the bot is working reaches it or gets its own turn."""
    bot = world.bot("插话", "你是一个严谨的助手。按用户要求的步骤逐个调用工具，每一步都要真的调用。")
    conv = world.direct(bot)
    send(conv, "请严格按顺序做三件事，每件都要调用工具：1) workspace_list 列出工作区根目录；"
               "2) search_chat_history 搜索「预算」；3) 再次 workspace_list。全部做完后用一句话总结。")
    time.sleep(2.5)
    late = send(conv, "补充要求：最终回复的最后一行必须是 INTERJECT-OK-7731")
    was_interjection = bool(late.get("interjection"))
    wait_idle(conv)
    texts = bot_texts(conv)
    delivered = sql(
        f"select count(*) from events where conversation_id = '{conv}' and type = 'status' "
        f"and payload::text like '%interjection_delivered%'")[0][0]
    follow_up = sql(
        f"select count(*) from events where conversation_id = '{conv}' and type = 'cycle.updated' "
        f"and payload->>'reason' = 'follow_up'")[0][0]
    evidence = (f"interjection={was_interjection} delivered_events={delivered} follow_up_cycles={follow_up} "
                f"replies={len(texts)} last={texts[-1][-80:]!r}")
    assert was_interjection, "second message was not treated as an interjection: " + evidence
    assert any("INTERJECT-OK-7731" in text for text in texts), "the bot never honoured the interjection: " + evidence
    assert delivered != "0" or follow_up != "0", "neither delivered mid-turn nor followed up: " + evidence
    return evidence


def scenario_checkpoint(world):
    """Long history is folded into a real checkpoint; early facts survive."""
    bot = world.bot("检查点", "你是项目助理，回答要简短准确。")
    conv = world.direct(bot)
    rows = []
    for index in range(1, 171):
        if index == 3:
            content = "记住一件重要的事：我们项目的内部代号是「蓝鲸-7731」，以后问到要能答上来。"
            sender = "user"
        elif index % 2 == 1:
            content = f"第 {index} 条闲聊：今天继续整理第 {index} 批资料，没有特别的事。"
            sender = "user"
        else:
            content = f"好的，第 {index - 1} 批资料已记录。"
            sender = "bot"
        bot_id = f"'{bot['id']}'" if sender == "bot" else "null"
        rows.append(f"('{conv}', '{sender}', {bot_id}, '{content}', '{{}}'::jsonb)")
    sql_exec("insert into messages (conversation_id, sender_type, sender_bot_id, content, metadata) values\n"
             + ",\n".join(rows) + ";")
    send(conv, "我们项目的内部代号是什么？只回答代号。")
    wait_idle(conv, 300)
    # Booleans are computed in SQL: the checkpoint text is multi-line and would
    # not survive being read back row by row.
    summary = sql(f"select source, up_to_seq, length(content), "
                  f"(content like '%## 目标%' and content like '%## 下一步%')::int, "
                  f"(content like '%用户原话%')::int, (content like '%蓝鲸-7731%')::int "
                  f"from conversation_summaries where conversation_id = '{conv}' order by created_at desc limit 1")
    assert summary, "no checkpoint was created for a 170-message history"
    source, up_to, length = summary[0][0], summary[0][1], summary[0][2]
    structured, has_verbatim, fact_kept = summary[0][3] == "1", summary[0][4] == "1", summary[0][5] == "1"
    marker = sql(f"select count(*) from messages where conversation_id = '{conv}' "
                 f"and metadata->>'kind' = 'context_checkpoint'")[0][0]
    answer = bot_texts(conv)[-1]
    used_search = sql(f"select count(*) from tool_invocations ti join runs r on r.id = ti.run_id "
                      f"where r.conversation_id = '{conv}' and ti.name = 'search_chat_history'")[0][0]
    evidence = (f"summary source={source} up_to_seq={up_to} chars={length} marker_messages={marker} "
                f"structured={structured} verbatim_section={has_verbatim} fact_in_checkpoint={fact_kept} "
                f"search_tool_calls={used_search} answer={answer[:60]!r}")
    assert source == "auto", "the real model should produce a valid structured checkpoint: " + evidence
    assert structured and has_verbatim, "checkpoint is missing its structure: " + evidence
    assert marker == "1", "expected one checkpoint marker message: " + evidence
    assert "蓝鲸-7731" in answer, "an early fact was lost after the history was folded: " + evidence
    world.checkpoint_conv = conv
    return evidence


def scenario_side(world):
    """A side question is answered from the snapshot and leaves no trace."""
    conv = getattr(world, "checkpoint_conv", None)
    if conv is None:
        bot = world.bot("旁路", "你是项目助理。")
        conv = world.direct(bot)
        send(conv, "记住：我们项目的内部代号是「蓝鲸-7731」。只回复“好的”。")
        wait_idle(conv)
    before = len(messages(conv))
    status, data = call("POST", f"/api/conversations/{conv}/side", {"question": "项目的内部代号是什么？只回答代号。"},
                        timeout=180, ok=(201, 502))
    side = data["side_chat"]
    after = len(messages(conv))
    _, listing = call("GET", f"/api/conversations/{conv}/side")
    evidence = (f"http={status} status={side['status']} answer={side['answer'][:60]!r} "
                f"messages_before={before} after={after} listed={len(listing['side_chats'])}")
    assert side["status"] == "answered", "side question failed: " + evidence + " " + side.get("error", "")
    assert "蓝鲸-7731" in side["answer"], "side answer did not use the conversation snapshot: " + evidence
    assert before == after, "a side question must not write into the conversation: " + evidence
    call("DELETE", f"/api/conversations/{conv}/side/{side['id']}", {})
    return evidence


def scenario_closure(world):
    """A teammate's private reply comes back to the asker and wakes it."""
    helper = world.bot("帮手", "你是数学助手。收到问题直接给出答案，不要反问。")
    asker = world.bot("发起人", "你是协调者。需要别人协助时用 send_to_agent 工具。")
    conv = world.direct(asker)
    send(conv, f"请用 send_to_agent 工具给「{helper['name']}」发私信，问它：17 乘以 23 等于多少？"
               f"发完后本轮先结束。等它的回复送回来以后，把它给的数字告诉我，并在最后加上 CLOSURE-OK。")

    def done():
        texts = bot_texts(conv)
        return any("CLOSURE-OK" in text and "391" in text for text in texts) and idle(conv)

    try:
        wait(done, 240, what="the asker to report the helper's answer")
    finally:
        cards = sql(f"select metadata->>'direction', metadata->>'inject', left(metadata->>'inject_text', 80) "
                    f"from messages where conversation_id = '{conv}' and metadata->>'kind' = 'agent_thread' order by ordinal")
        texts = bot_texts(conv)
        evidence = f"cards={cards} asker_replies={len(texts)} last={texts[-1][-90:] if texts else ''!r}"
        print("    " + evidence)
    received = [card for card in cards if card[0] == "received"]
    assert received and received[0][1] == "true", "the reply card does not carry the reply for the model: " + evidence
    assert len(texts) >= 2, "the asker was not woken by the reply: " + evidence
    return evidence


def scenario_fanout(world):
    """A bot that messages two teammates at once gets both replies; the cards fold per exchange."""
    first = world.bot("甲", "你是数学助手。收到问题直接给出答案，不要反问。")
    second = world.bot("乙", "你是数学助手。收到问题直接给出答案，不要反问。")
    asker = world.bot("协调", "你是协调者。需要别人协助时用 send_to_agent 工具。")
    conv = world.direct(asker)
    send(conv, f"请在同一轮里用 send_to_agent 分别给「{first['name']}」和「{second['name']}」发私信："
               f"问前者 12 乘以 12 等于多少，问后者 25 乘以 4 等于多少。发完后本轮先结束。"
               f"两个回复都送回来以后，把两个数字告诉我，并在最后加上 FANOUT-OK。")

    def done():
        texts = bot_texts(conv)
        return any("FANOUT-OK" in t and "144" in t and "100" in t for t in texts) and idle(conv)

    try:
        wait(done, 300, what="the coordinator to report both answers")
    finally:
        cards = sql(f"select metadata->>'thread_id', metadata->>'direction', metadata->>'peer_bot_name' "
                    f"from messages where conversation_id = '{conv}' and metadata->>'kind' = 'agent_thread' order by ordinal")
        evidence = f"cards={[(c[0][:8], c[1], c[2][-6:]) for c in cards]} conv={conv}"
        print("    " + evidence)
    threads = {c[0] for c in cards}
    assert len(threads) == 2, "expected two private exchanges: " + evidence
    assert all({d for t, d, _ in cards if t == thread} == {"sent", "received"} for thread in threads),         "each exchange needs a sent and a received card: " + evidence
    world.fanout_conv = conv
    return evidence


def scenario_goal(world):
    """A group works a goal through stages with evidence and stops by itself."""
    lead = world.bot("群主", "你是这个群的群主，负责组织大家完成群目标。做事干脆，不要反复确认。")
    writer = world.bot("写手", "你是文案写手。被点名后直接完成任务并用 group_goal 的 report 汇报。")
    team, conv = world.team("目标群", "group", [lead, writer], lead)
    status, data = call("POST", f"/api/conversations/{conv}/goal/actions", {
        "action": "draft",
        "objective": "为一款笔记应用写一句不超过 20 个字的中文口号",
        "acceptance": "群里给出最终口号，且不超过 20 个字",
        "constraints": ["不要使用英文"],
        "stages": [
            {"title": "起草", "acceptance": "写手给出一句口号"},
            {"title": "定稿", "acceptance": "群主确认口号不超过 20 个字并公布"},
        ],
    })
    assert data["goal"]["status"] == "drafting", data
    assert data["lead_bot_id"] == lead["id"], "configured lead was not picked"
    status, rejected = call("POST", f"/api/conversations/{conv}/goal/actions", {"action": "pass"}, ok=(422,))
    _, started = call("POST", f"/api/conversations/{conv}/goal/actions", {"action": "start"})
    assert started["goal"]["status"] == "active", started

    def settled():
        _, current = call("GET", f"/api/conversations/{conv}/goal")
        goal = current["goal"]
        return goal if (goal["status"] == "complete" or (idle(conv) and time.time() - began > 45)) else None

    began = time.time()
    try:
        goal = wait(settled, 420, step=4, what="the group to work the goal")
    except TimeoutError:
        _, current = call("GET", f"/api/conversations/{conv}/goal")
        goal = current["goal"]
    time.sleep(8)
    quiet = idle(conv)
    nudges = sql(f"select count(*) from messages where conversation_id = '{conv}' and metadata->>'kind' = 'goal_nudge'")[0][0]
    passed = [stage["title"] for stage in goal["stages"] if stage["status"] == "passed"]
    tasks = [(task["bot_name"][-12:], task["status"]) for stage in goal["stages"] for task in stage["tasks"]]
    evidence = (f"rule_rejection_http={status} status={goal['status']} passed={passed} tasks={tasks} "
                f"stage_evidence={[stage['evidence'][:30] for stage in goal['stages']]} "
                f"goal_checks={nudges} nudges_left_at={goal['nudges']} idle_after={quiet} "
                f"bot_messages={len(bot_texts(conv))} seconds={int(time.time() - began)}")
    assert rejected["error"]["code"] == "goal_rule", "rule violations must come back as readable errors"
    assert passed, "no stage was passed with evidence: " + evidence
    assert all(stage["evidence"] for stage in goal["stages"] if stage["status"] == "passed"), evidence
    assert quiet, "the group did not stop by itself: " + evidence
    assert int(nudges) <= 12, "the goal driver nudged far too often: " + evidence
    if goal["status"] != "complete":
        evidence += "  NOTE: goal not fully complete within the time limit"
    return evidence


def scenario_pipeline_approval(world):
    """After an approval pause the remaining pipeline stages still run."""
    first = world.bot("一号", "你是流水线第一位成员。按要求用工具完成任务。")
    second = world.bot("二号", "你是流水线第二位成员。基于上一位的产出，用一句话确认收到。")
    team, conv = world.team("流水线", "pipeline", [first, second])
    # The account may allow workspace writes outright; an always-ask rule
    # scoped to this run's file name guarantees the approval path is taken.
    filename = f"accept-{TAG}.txt"
    _, rule = call("POST", "/api/permissions/rules", {"kind": "always_ask", "pattern": filename})
    world.rules.append(rule["rule"]["id"])
    send(conv, f"第一位：请用 workspace_write 工具把内容 hello 写入文件 {filename}，然后说“已写入”。")

    def pending():
        _, data = call("GET", "/api/tool-approvals")
        return [a for a in data["approvals"] if a["conversation_id"] == conv] or None

    approvals = wait(pending, 180, what="a tool approval request")
    before = len(bot_texts(conv))
    _, resolved = call("POST", f"/api/tool-approvals/{approvals[0]['id']}", {"decision": "allow"})
    time.sleep(2)
    for _ in range(4):
        wait_idle(conv, 240)
        more = pending() if not idle(conv) else None
        extra = None
        try:
            _, listing = call("GET", "/api/tool-approvals")
            extra = [a for a in listing["approvals"] if a["conversation_id"] == conv]
        except RuntimeError:
            extra = None
        if not extra:
            break
        call("POST", f"/api/tool-approvals/{extra[0]['id']}", {"decision": "allow"})
        time.sleep(2)
    speakers = sql(f"select b.name, count(*) from messages m join bots b on b.id = m.sender_bot_id "
                   f"where m.conversation_id = '{conv}' and m.sender_type = 'bot' group by b.name order by b.name")
    evidence = (f"tool={approvals[0]['tool_name']} resume={resolved.get('resume')} replies_before_approval={before} "
                f"speakers={[(name[-12:], count) for name, count in speakers]} cycles={cycle_rows(conv)}")
    names = [name for name, _ in speakers]
    assert before == 0, "stage one should have been paused before replying: " + evidence
    assert resolved.get("resume") == "started", "approval did not start the resume cycle: " + evidence
    assert any(first["name"] == name for name in names), "stage one never finished: " + evidence
    assert any(second["name"] == name for name in names), "stage two was dropped after the approval pause: " + evidence
    counts = {name: int(count) for name, count in speakers}
    assert counts.get(first["name"]) == 1 and counts.get(second["name"]) == 1,         "each stage must reply exactly once (a resumed checkpoint ran twice?): " + evidence
    return evidence


def scenario_permanent_failure(world):
    """A request the provider rejects fails once instead of retrying thrice."""
    bot = world.bot("坏模型", "x", model="model-that-does-not-exist-" + TAG)
    conv = world.direct(bot)
    began = time.time()
    send(conv, "你好")
    wait_idle(conv, 120)
    rows = cycle_rows(conv)
    evidence = f"cycles={[(r[0], r[1], r[2][:70]) for r in rows]} seconds={int(time.time() - began)}"
    assert rows and rows[-1][0] == "failed", "cycle should have failed: " + evidence
    assert rows[-1][1] == "1", "a permanent provider error must not be retried: " + evidence
    return evidence


def scenario_memory(world):
    """Dedupe, secret refusal, pin/archive/restore and tidy through the API."""
    bot = world.bot("记忆", "你是助理。", memory=True)
    fact = f"验收事实 {TAG}：发布包存放在 /srv/releases 目录"
    status_a, first = call("POST", "/api/memories", {"content": fact})
    status_b, second = call("POST", "/api/memories", {"content": "  " + fact + "。 "})
    status_c, refused = call("POST", "/api/memories", {"content": "数据库 password=Zx9!kLm2Qw8#Rt5"}, ok=(400,))
    memory_id = first["memory"]["id"]
    _, pinned = call("POST", f"/api/memories/{memory_id}/pin", {"pinned": True})
    _, listed = call("GET", "/api/memories?include_archived=1")
    mine = [m for m in listed["memories"] if m["id"] == memory_id]
    _, report = call("POST", f"/api/bots/{bot['id']}/memories/tidy", {})
    call("POST", f"/api/memories/{memory_id}/pin", {"pinned": False})
    call("DELETE", f"/api/memories/{memory_id}", {})
    evidence = (f"create={status_a} duplicate_http={status_b} duplicate={second.get('duplicate')} "
                f"same_row={second['memory']['id'] == memory_id} secret_http={status_c} "
                f"secret_code={refused['error']['code']} pinned={pinned['memory']['pinned']} "
                f"source={mine[0]['source'] if mine else None} tidy_scanned={report['report']['scanned']}")
    assert second.get("duplicate") is True and second["memory"]["id"] == memory_id, evidence
    assert refused["error"]["code"] == "secret_rejected", evidence
    assert pinned["memory"]["pinned"] is True and mine, evidence
    return evidence


def scenario_sandbox_mounts(world):
    """A bot's computer only mounts the shared folders of its own teams."""
    mine = world.bot("沙箱甲", "你是工程助手。用户让你运行命令时直接用 shell 工具运行，然后原样报告输出。")
    other = world.bot("沙箱乙", "你是工程助手。")
    team_a, conv_a = world.team("沙箱A", "group", [mine], mine)
    team_b, _ = world.team("沙箱B", "group", [other], other)
    send(conv_a, "请用 shell 工具运行：ls /teams ; echo MOUNT-CHECK-DONE ，然后把输出原样贴出来。")
    wait_idle(conv_a, 240)
    container = "superbot-bot-" + mine["id"].replace("-", "")[:12]
    result = subprocess.run(["docker", "inspect", container, "--format",
                             "{{range .Mounts}}{{.Destination}} {{end}}|{{index .Config.Labels \"superbot.teams\"}}"],
                            capture_output=True, text=True, timeout=30)
    mounts, _, label = result.stdout.strip().partition("|")
    outputs = sql(f"select left(ti.result_artifact, 200) from tool_invocations ti join runs r on r.id = ti.run_id "
                  f"where r.conversation_id = '{conv_a}' and ti.name = 'shell'")
    listing = ""
    if outputs and outputs[0][0]:
        listing = subprocess.run(["cat", outputs[0][0]], capture_output=True, text=True).stdout
    subprocess.run(["docker", "rm", "-f", container], capture_output=True, timeout=60)
    evidence = f"mounts=[{mounts}] label={label} shell_output={listing.strip()[:160]!r}"
    assert f"/teams/{team_a['id']}/shared" in mounts, "own team folder missing: " + evidence
    assert team_b["id"] not in mounts and team_b["id"] not in listing, "another team's folder is visible: " + evidence
    assert " /teams " not in f" {mounts} ", "the whole teams directory is still mounted: " + evidence
    return evidence


def scenario_docker_proxy(world):
    """The worker reaches Docker only through the filtering proxy."""
    mounts = subprocess.run(["docker", "inspect", "superbot-worker-1", "--format",
                             "{{range .Mounts}}{{.Source}} {{end}}|{{range .Config.Env}}{{println .}}{{end}}"],
                            capture_output=True, text=True, timeout=30).stdout
    worker_mounts, _, worker_env = mounts.partition("|")
    proxy_ip = subprocess.run(["docker", "inspect", "superbot-docker-proxy-1", "--format",
                               "{{range .NetworkSettings.Networks}}{{.IPAddress}} {{end}}"],
                              capture_output=True, text=True, timeout=30).stdout.split()
    assert proxy_ip, "docker-proxy container is not running"
    base = f"http://{proxy_ip[0]}:2375"

    def raw(method, path, body=None):
        data = json.dumps(body).encode() if body is not None else None
        request = urllib.request.Request(base + path, data=data, method=method,
                                         headers={"Content-Type": "application/json"})
        try:
            with urllib.request.urlopen(request, timeout=20) as response:
                return response.status, response.read()[:200]
        except urllib.error.HTTPError as error:
            return error.code, error.read()[:200]

    ping = raw("GET", "/_ping")
    images = raw("GET", "/v1.47/images/json")
    build = raw("POST", "/build")
    privileged = raw("POST", "/v1.47/containers/create?name=superbot-bot-evil" + TAG, {
        "Image": "superbot/computer:latest", "User": "1000:1000",
        "Labels": {"superbot.bot_id": "x"},
        "HostConfig": {"Privileged": True, "Binds": ["/:/host"], "CapDrop": ["ALL"],
                       "SecurityOpt": ["no-new-privileges"], "NetworkMode": "host"},
    })
    foreign = raw("POST", "/v1.47/containers/superbot-postgres/exec", {"Cmd": ["id"]})
    evidence = (f"worker_mounts=[{worker_mounts.strip()}] docker_host={'DOCKER_HOST=tcp://docker-proxy:2375' in worker_env} "
                f"ping={ping[0]} images={images[0]} build={build[0]} privileged_create={privileged[0]} "
                f"exec_into_postgres={foreign[0]}")
    assert "docker.sock" not in worker_mounts, "the worker still mounts the Docker socket: " + evidence
    assert ping[0] == 200, evidence
    assert images[0] == 403 and build[0] == 403, "non-sandbox endpoints must be refused: " + evidence
    assert privileged[0] == 403, "a privileged container must be refused: " + evidence
    assert foreign[0] == 403, "exec into a non-bot container must be refused: " + evidence
    return evidence


def scenario_browser(world):
    """A browser bot's computer starts through the proxy and can browse."""
    bot = world.bot("浏览器", "你会用浏览器工具。按要求打开网页后如实报告页面标题。", profile="browser")
    conv = world.direct(bot)
    send(conv, "请用 browser_open 打开 https://example.com ，然后告诉我页面标题，最后一行写 BROWSER-OK。")
    wait_idle(conv, 300)
    texts = bot_texts(conv)
    tools = sql(f"select ti.name, ti.status from tool_invocations ti join runs r on r.id = ti.run_id "
                f"where r.conversation_id = '{conv}' order by ti.created_at")
    container = "superbot-bot-" + bot["id"].replace("-", "")[:12]
    state = subprocess.run(["docker", "inspect", container, "--format",
                            "{{.State.Running}} {{index .Config.Labels \"superbot.profile\"}} {{.HostConfig.Privileged}}"],
                           capture_output=True, text=True, timeout=30).stdout.strip()
    subprocess.run(["docker", "rm", "-f", container], capture_output=True, timeout=60)
    evidence = f"container={state} tools={tools} last={texts[-1][-120:] if texts else ''!r}"
    assert state.startswith("true browser false"), "browser computer did not start: " + evidence
    assert any(name == "browser_open" and status == "done" for name, status in tools), evidence
    assert texts and "Example Domain" in texts[-1], "page title not reported: " + evidence
    return evidence


def container_state(bot_id):
    """Running flag and profile label of a bot's computer, or None if it has none."""
    name = "superbot-bot-" + bot_id.replace("-", "")[:12]
    result = subprocess.run(["docker", "inspect", name, "--format",
                             "{{.State.Running}} {{index .Config.Labels \"superbot.profile\"}}"],
                            capture_output=True, text=True, timeout=30)
    return result.stdout.strip() if result.returncode == 0 else None


def scenario_computer_lifecycle(world):
    """Creating a bot creates its computer (on standby); deleting it removes everything."""
    started = time.time()
    _, data = call("POST", "/api/bots", {
        "name": f"{PREFIX}电脑-{TAG}", "slug": f"accept-{TAG}-life",
        "provider_id": world.provider_id, "model": world.model,
    })
    bot = data["bot"]
    world.bots.append(bot["id"])
    created = wait(lambda: container_state(bot["id"]), 30, step=1, what="the new bot's computer")
    created_after = round(time.time() - started, 1)
    _, computer = call("GET", f"/api/bots/{bot['id']}/computer")
    # A plain chat turn must not boot the computer: the browser tools start it
    # on first use only.
    conv = world.direct(bot)
    send(conv, "你好，只回复 OK 两个字母。")
    wait_idle(conv, 120)
    after_chat = container_state(bot["id"])
    workspace = DATA_DIR / "workspaces" / bot["id"]
    had_workspace = workspace.is_dir()
    # Leave something behind in the workspace and a snapshot directory, as a
    # bot that has been used would.
    (workspace / "notes").mkdir(exist_ok=True)
    (workspace / "notes" / "todo.txt").write_text("x" * 4096)
    snapshots = DATA_DIR / "snapshots" / bot["id"]
    snapshots.mkdir(parents=True, exist_ok=True)
    (snapshots / "1.tar.gz").write_bytes(b"x" * 1024)
    # Bots and the worker write as uid 1000; so must this stand-in.
    for item in (workspace / "notes", workspace / "notes" / "todo.txt", snapshots, snapshots / "1.tar.gz"):
        os.chown(item, 1000, 1000)

    deleted_at = time.time()
    call("DELETE", f"/api/bots/{bot['id']}", {})
    world.bots.remove(bot["id"])

    def gone():
        return (container_state(bot["id"]) is None and not workspace.exists() and not snapshots.exists()
                and sql(f"select count(*) from computer_removals where bot_id = '{bot['id']}'")[0][0] == "0")

    try:
        wait(gone, 30, step=1, what="the deleted bot's computer to be removed")
    finally:
        evidence = (f"profile={bot['computer_profile']} container_after_create={created!r} in={created_after}s "
                    f"after_plain_chat={after_chat!r} "
                    f"api_status={computer['computer']['status']} workspace_dir={had_workspace} "
                    f"after_delete: container={container_state(bot['id'])!r} workspace_exists={workspace.exists()} "
                    f"snapshots_exist={snapshots.exists()} removed_in={round(time.time() - deleted_at, 1)}s")
        print("    " + evidence)
    assert bot["computer_profile"] == "browser", "a new bot must get a computer: " + evidence
    assert created.startswith("false browser"), "the computer must be created on standby, not running: " + evidence
    assert computer["computer"]["status"] == "stopped", evidence
    assert after_chat.startswith("false"), "a plain chat turn booted the computer: " + evidence
    assert had_workspace, evidence
    return evidence


def scenario_shared_drive(world):
    """A file one bot puts on the shared drive is readable by another bot and in the UI listing."""
    writer = world.bot("写盘", "你是工程助手。用户让你运行命令时直接用 shell 工具运行，然后原样报告输出。")
    reader = world.bot("读盘", "你是助手。按用户要求用工具读取文件并如实报告内容。")
    filename = f"accept-{TAG}.txt"
    secret = f"DRIVE-{TAG}-OK"
    conv_w = world.direct(writer)
    send(conv_w, f"请用 shell 工具运行：echo {secret} > /shared/{filename} && ls -l /shared/{filename} ，然后贴出输出。")
    wait_idle(conv_w, 300)
    conv_r = world.direct(reader)
    send(conv_r, f"请用 drive_read 工具读取共享盘上的文件 {filename}，把文件内容原样告诉我。")
    wait_idle(conv_r, 240)
    texts = bot_texts(conv_r)
    tools = sql(f"select ti.name, ti.status from tool_invocations ti join runs r on r.id = ti.run_id "
                f"where r.conversation_id = '{conv_r}' order by ti.created_at")
    _, listing = call("GET", "/api/drive/files")
    names = [f["name"] for f in listing["files"]]
    status, content = None, None
    request = urllib.request.Request(BASE + f"/api/drive/files/content?path={filename}", headers={"Origin": BASE})
    with opener.open(request, timeout=30) as response:
        status, content = response.status, response.read().decode().strip()
    for drive in (DATA_DIR / "shared").glob(f"*/{filename}"):
        drive.unlink()
    evidence = (f"reader_tools={tools} listed={filename in names} content_http={status} content={content!r} "
                f"reader_last={texts[-1][-80:] if texts else ''!r}")
    assert filename in names, "the drive listing does not show the file: " + evidence
    assert content == secret, evidence
    assert any(name == "drive_read" and st == "done" for name, st in tools), "the second bot did not read the drive: " + evidence
    assert texts and secret in texts[-1], "the second bot did not see the first bot's file: " + evidence
    return evidence


def scenario_builtin_skill(world):
    """Built-in skills are readable even when their files were lost."""
    bot = world.bot("技能", "")
    conv = world.direct(bot)
    send(conv, "请调用 skill_read 读取 box-desktop 这个技能，然后告诉我它的第一个标题是什么。")
    wait_idle(conv, 180)
    tools = sql(f"select ti.name, ti.status, coalesce(ti.error, '') from tool_invocations ti join runs r on r.id = ti.run_id "
                f"where r.conversation_id = '{conv}' and ti.name = 'skill_read'")
    texts = bot_texts(conv)
    evidence = f"skill_read={tools} last={texts[-1][-100:] if texts else ''!r}"
    assert tools and all(status == "done" for _, status, _ in tools), evidence
    return evidence


def tool_rows(conv):
    return sql(f"select ti.name, ti.status, translate(left(coalesce(ti.error, ''), 160), E'\\n\\r\\t', '   ') from tool_invocations ti "
               f"join runs r on r.id = ti.run_id where r.conversation_id = '{conv}' order by ti.created_at")


def upload(filename, content, mime):
    """Upload an attachment the way the composer does (multipart)."""
    boundary = "----accept" + uuid.uuid4().hex
    body = (f"--{boundary}\r\nContent-Disposition: form-data; name=\"file\"; filename=\"{filename}\"\r\n"
            f"Content-Type: {mime}\r\n\r\n").encode() + content + f"\r\n--{boundary}--\r\n".encode()
    request = urllib.request.Request(BASE + "/api/attachments", data=body, method="POST", headers={
        "Content-Type": f"multipart/form-data; boundary={boundary}", "Origin": BASE,
    })
    with opener.open(request, timeout=60) as response:
        return json.loads(response.read())["attachment"]


def docx_bytes(paragraphs):
    """A minimal valid .docx with the given paragraphs."""
    import io
    import zipfile
    from xml.sax.saxutils import escape
    body = "".join(f"<w:p><w:r><w:t>{escape(text)}</w:t></w:r></w:p>" for text in paragraphs)
    parts = {
        "[Content_Types].xml": '<?xml version="1.0" encoding="UTF-8" standalone="yes"?>'
            '<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">'
            '<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>'
            '<Default Extension="xml" ContentType="application/xml"/>'
            '<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>'
            '</Types>',
        "_rels/.rels": '<?xml version="1.0" encoding="UTF-8" standalone="yes"?>'
            '<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">'
            '<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>'
            '</Relationships>',
        "word/document.xml": '<?xml version="1.0" encoding="UTF-8" standalone="yes"?>'
            '<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>'
            + body + '</w:body></w:document>',
    }
    buffer = io.BytesIO()
    with zipfile.ZipFile(buffer, "w", zipfile.ZIP_DEFLATED) as archive:
        for name, text in parts.items():
            archive.writestr(name, text)
    return buffer.getvalue()


def scenario_documents(world):
    """A document a bot writes lands in its Library, and the id it reports is real."""
    bot = world.bot("文档", "你是写作助手。")
    title = f"验收文档-{TAG}"
    conv = world.direct(bot)
    try:
        send(conv, f"请用 create_document 写一份标题为「{title}」的文档，内容是三条开题答辩注意事项。写好后告诉我文档 ID。")
        wait_idle(conv, 240)
        docs = sql(f"select id from documents where title = '{title}'")
        items = sql(f"select kind, metadata->>'source_id', length(content) from library_items "
                    f"where bot_id = '{bot['id']}' and metadata->>'source' = 'document'")
        texts = bot_texts(conv)
        evidence = f"documents={docs} library={items} last={texts[-1][-160:] if texts else ''!r}"
        assert len(docs) == 1, "the bot did not create the document: " + evidence
        doc_id = docs[0][0]
        assert any(row[1] == doc_id and int(row[2]) > 20 for row in items), "the document is not in the bot's Library: " + evidence
        assert texts and doc_id in texts[-1], "the reply does not carry the real document id: " + evidence
        return evidence
    finally:
        sql_exec(f"delete from documents where title = '{title}';")


def scenario_attachment_template(world):
    """A Word template sent in chat reaches the bot's computer and is filled in with python-docx."""
    bot = world.bot("模板", "你是论文助手，会用电脑处理 Word 文件。")
    headings = ["一、选题依据", "二、研究内容", "三、进度安排"]
    template = f"1.验收开题模板-{TAG}.docx"
    output = f"验收报告-{TAG}.docx"
    attachment = upload(template, docx_bytes(["本科毕业论文开题报告"] + headings),
                        "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
    conv = world.direct(bot)
    call("POST", f"/api/conversations/{conv}/messages", {
        "content": (f"附件是开题报告模板。先告诉我模板里有哪几个一级标题。然后在你的电脑里用 python-docx 以这个模板为底稿，"
                    f"在每个标题下面各加一句话，保存为 /workspace/{output}。"),
        "client_msg_id": uuid.uuid4().hex, "attachment_ids": [attachment["id"]],
    })
    wait_idle(conv, 480)
    workspace = DATA_DIR / "workspaces" / bot["id"]
    uploaded = workspace / "uploads" / template
    produced = workspace / output
    tools = tool_rows(conv)
    library = sql(f"select kind, title, length(content), position('选题依据' in content) > 0 from library_items "
                  f"where bot_id = '{bot['id']}' and title = '{output}'")
    texts = bot_texts(conv)
    mount = subprocess.run(["docker", "exec", "superbot-bot-" + bot["id"].replace("-", "")[:12], "sh", "-c",
                            "mount | grep ' /tmp '; python --version"], capture_output=True, text=True, timeout=30)
    evidence = (f"uploaded={uploaded.exists()} produced={produced.exists()} library={library} "
                f"tmp={mount.stdout.strip()!r} tools={tools} last={texts[-1][-200:] if texts else ''!r}")
    assert uploaded.exists(), "the attachment was not placed in the computer: " + evidence
    assert texts and all(h.split("、")[1] in texts[-1] for h in headings), "the reply does not list the template headings: " + evidence
    assert produced.exists(), "the filled-in report was not written: " + evidence
    assert library and library[0][3] == "t", "the report is not in the Library with its text: " + evidence
    assert not any("failed to map segment" in row[2] or "unknown tool" in row[2] for row in tools), evidence
    assert "noexec" not in mount.stdout and "Python 3" in mount.stdout, evidence
    return evidence


def scenario_search_fallback(world):
    """Without a search provider, web_search still answers through Bing."""
    providers = sql("select count(*) from providers where type = 'search' and enabled")
    bot = world.bot("搜索", "你是研究助手。")
    conv = world.direct(bot)
    send(conv, "请用 web_search 搜索「CNNIC 中国互联网络发展状况统计报告」，告诉我前两个结果的标题和网址。")
    wait_idle(conv, 240)
    tools = tool_rows(conv)
    texts = bot_texts(conv)
    evidence = f"search_providers={providers[0][0]} tools={tools} last={texts[-1][-200:] if texts else ''!r}"
    assert any(name == "web_search" and status == "done" for name, status, _ in tools), "web_search did not return results: " + evidence
    assert texts and "http" in texts[-1], "the reply carries no result link: " + evidence
    return evidence


def scenario_step_limit(world):
    """A turn that runs out of tool steps ends with a progress report, not a placeholder."""
    bot = world.bot("步数", "你是工程助手，用户让你运行命令时用 shell 工具逐条运行。")
    sql_exec(f"update bots set params = '{{\"max_steps\": 2}}'::jsonb where id = '{bot['id']}';")
    conv = world.direct(bot)
    send(conv, "请逐条运行这五个命令，每条单独调用一次 shell 工具，不要合并：date；uname -a；whoami；pwd；ls /。全部跑完后汇总。")
    wait_idle(conv, 300)
    texts = bot_texts(conv)
    runs = sql(f"select warning from runs where conversation_id = '{conv}' order by created_at")
    evidence = f"runs={runs} last={texts[-1][-240:] if texts else ''!r}"
    assert any(row[0] == "达到工具调用步数上限" for row in runs), "the step limit was not reached: " + evidence
    assert texts and "模型未生成最终回复" not in texts[-1], "the turn ended on the placeholder: " + evidence
    assert "继续" in texts[-1], "the report does not tell the user how to go on: " + evidence
    return evidence


def fetch_raw(path):
    """GET a file the way the browser downloads it: status, headers and bytes."""
    try:
        with opener.open(urllib.request.Request(BASE + path), timeout=60) as response:
            return response.status, response.headers, response.read()
    except urllib.error.HTTPError as error:
        return error.code, error.headers, error.read()


def disposition_name(header):
    """The UTF-8 file name of a Content-Disposition header."""
    marker = "filename*=UTF-8''"
    if marker not in header:
        return ""
    return urllib.parse.unquote(header.split(marker, 1)[1].split(";", 1)[0])


def scenario_send_file(world):
    """A bot hands over a Word file it made; the user downloads it from the chat and from the Library."""
    import io
    import zipfile
    bot = world.bot("发文件", "你是办公助手，会用电脑生成 Office 文件。")
    output = f"验收交付-{TAG}.docx"
    conv = world.direct(bot)
    send(conv, f"在你的电脑里用 python-docx 新建一个 Word 文件，正文写一句「验收交付 {TAG}」，"
               f"保存为 /workspace/out/{output}，然后把这个文件发给我。")
    wait_idle(conv, 360)
    tools = tool_rows(conv)
    files = [a for m in messages(conv) if m["sender_type"] == "bot" for a in (m.get("attachments") or [])]
    texts = bot_texts(conv)
    evidence = (f"tools={[(n, s) for n, s, _ in tools]} files={[(a['filename'], a['size_bytes']) for a in files]} "
                f"last={texts[-1][-120:] if texts else ''!r}")
    assert any(name == "send_file" and status == "done" for name, status, _ in tools), "send_file was not used: " + evidence
    sent = [a for a in files if a["filename"] == output]
    assert sent, "the file is not attached to a bot message: " + evidence

    status, headers, body = fetch_raw(sent[0]["url"] + "?download=1")
    disposition = headers.get("Content-Disposition", "")
    evidence += f" chat_download={status} name={disposition_name(disposition)!r} {len(body)}B"
    assert status == 200 and disposition.startswith("attachment") and disposition_name(disposition) == output, evidence
    with zipfile.ZipFile(io.BytesIO(body)) as archive:
        assert TAG in archive.read("word/document.xml").decode("utf-8"), "the downloaded file lacks the text: " + evidence

    item = wait(lambda: sql(f"select id from library_items where bot_id = '{bot['id']}' and title = '{output}'"),
                60, what="the file in the Library")
    status, headers, library_body = fetch_raw(f"/api/library/{item[0][0]}/download")
    evidence += f" library_download={status} name={disposition_name(headers.get('Content-Disposition', ''))!r}"
    assert status == 200 and library_body == body, "the Library download differs: " + evidence

    document = sql("select id from library_items where file_path = '' and content <> '' "
                   "and mime_type like 'text/markdown%' order by created_at desc limit 1")
    if document:
        status, headers, text = fetch_raw(f"/api/library/{document[0][0]}/download")
        name = disposition_name(headers.get("Content-Disposition", ""))
        evidence += f" document_download={status} name={name!r} {len(text)}B"
        assert status == 200 and name.endswith(".md") and text, "a document without a file did not download as .md: " + evidence

    send(conv, "你刚才发给我的文件叫什么名字？只回答文件名。")
    wait_idle(conv, 120)
    texts = bot_texts(conv)
    evidence += f" recall={texts[-1][-80:] if texts else ''!r}"
    assert texts and output in texts[-1], "the bot does not know which file it sent: " + evidence
    return evidence


SCENARIOS = {
    "sendfile": scenario_send_file,
    "documents": scenario_documents,
    "attachment": scenario_attachment_template,
    "search": scenario_search_fallback,
    "steplimit": scenario_step_limit,
    "interjection": scenario_interjection,
    "checkpoint": scenario_checkpoint,
    "side": scenario_side,
    "closure": scenario_closure,
    "fanout": scenario_fanout,
    "pipeline": scenario_pipeline_approval,
    "failure": scenario_permanent_failure,
    "memory": scenario_memory,
    "goal": scenario_goal,
    "sandbox": scenario_sandbox_mounts,
    "proxy": scenario_docker_proxy,
    "browser": scenario_browser,
    "lifecycle": scenario_computer_lifecycle,
    "drive": scenario_shared_drive,
    "skill": scenario_builtin_skill,
}


def main():
    selected = sys.argv[1:] or list(SCENARIOS)
    world = World()
    world.login()
    results = {}
    try:
        for name in selected:
            started = time.time()
            print(f"\n=== {name}: {SCENARIOS[name].__doc__.strip()}", flush=True)
            try:
                evidence = SCENARIOS[name](world)
                results[name] = "PASS"
                print(f"PASS ({int(time.time() - started)}s)  {evidence}", flush=True)
            except Exception as error:  # noqa: BLE001 - report and continue with the next scenario
                results[name] = "FAIL"
                print(f"FAIL ({int(time.time() - started)}s)  {error}", flush=True)
                if not isinstance(error, (AssertionError, TimeoutError)):
                    traceback.print_exc()
    finally:
        print()
        if os.environ.get("SUPERBOT_ACCEPT_KEEP"):
            # Leave the data for a follow-up browser check; it is removed by
            # deleting the bots named in this line.
            print("KEPT bots=" + ",".join(world.bots) + " teams=" + ",".join(world.teams)
                  + " checkpoint_conv=" + str(getattr(world, "checkpoint_conv", ""))
                  + " fanout_conv=" + str(getattr(world, "fanout_conv", "")))
        else:
            world.cleanup()
    print("\nSUMMARY: " + "  ".join(f"{name}={result}" for name, result in results.items()))
    sys.exit(0 if all(result == "PASS" for result in results.values()) else 1)


if __name__ == "__main__":
    main()
