# Background jobs of a bot's computer. The worker runs this inside the
# computer as `python3 -c SCRIPT COMMAND ARGS...` (internal/sandbox/jobs.go)
# and reads the one JSON line it prints.
#
#   start ID TIMEOUT_SECONDS MAX_RUNNING NAME COMMAND
#   status [ID] [TAIL_LINES]
#   stop ID
#   live
#
# A job runs `sh -lc COMMAND` in /workspace in its own session, detached
# from the exec that started it, with its output in a size-capped log. Its
# record lives in the workspace (JOBS), so it survives the computer
# sleeping; whether it still runs is recorded on the tmpfs (LIVE), which a
# restart clears, so a job can never look alive after the computer stopped.
import json
import os
import select
import signal
import subprocess
import sys
import time

JOBS = "/workspace/.superbot/jobs"
LIVE = "/tmp/.sbjobs"
LOG_MAX = 4 << 20       # rotate the log at 4 MiB, keeping one older part
KEEP_FINISHED = 30      # finished job records kept
STOP_GRACE = 10         # seconds between SIGTERM and SIGKILL


def emit(value):
    print(json.dumps(value, ensure_ascii=False), flush=True)


def proc_start(pid):
    try:
        with open("/proc/%s/stat" % pid) as handle:
            return handle.read().rsplit(")", 1)[1].split()[19]
    except (OSError, IndexError):
        return None


def live_entry(job_id):
    """(runner pid, process group) of a running job, or None."""
    try:
        with open(os.path.join(LIVE, job_id)) as handle:
            pid, start, pgid = handle.read().split()
    except (OSError, ValueError):
        return None
    if proc_start(pid) != start:
        return None
    return int(pid), int(pgid)


def read_json(path):
    try:
        with open(path) as handle:
            return json.load(handle)
    except (OSError, ValueError):
        return None


def write_json(path, value):
    tmp = path + ".tmp"
    with open(tmp, "w") as handle:
        json.dump(value, handle, ensure_ascii=False)
    os.replace(tmp, path)


def job_ids():
    try:
        entries = os.listdir(JOBS)
    except OSError:
        return []
    jobs = []
    for entry in entries:
        meta = read_json(os.path.join(JOBS, entry, "meta.json"))
        if meta:
            jobs.append((meta.get("started_at", 0), entry))
    return [entry for _, entry in sorted(jobs, reverse=True)]


def running_ids():
    try:
        return [entry for entry in os.listdir(LIVE) if live_entry(entry)]
    except OSError:
        return []


def tail(directory, lines):
    data = b""
    for name in ("log.1", "log"):
        try:
            with open(os.path.join(directory, name), "rb") as handle:
                handle.seek(0, os.SEEK_END)
                size = handle.tell()
                handle.seek(max(0, size - 64 * 1024))
                data += handle.read()
        except OSError:
            pass
    text = data.decode("utf-8", "replace")
    parts = text.splitlines()[-lines:] if lines > 0 else []
    return "\n".join(parts)[-12000:]


def describe(job_id, tail_lines):
    directory = os.path.join(JOBS, job_id)
    meta = read_json(os.path.join(directory, "meta.json")) or {"id": job_id}
    result = read_json(os.path.join(directory, "exit.json"))
    job = dict(meta)
    if result:
        job.update(result)
        if result.get("stopped"):
            job["state"] = "stopped"
        elif result.get("timed_out"):
            job["state"] = "timed_out"
        else:
            job["state"] = "exited"
    elif live_entry(job_id):
        job["state"] = "running"
    else:
        job["state"] = "lost"
    if tail_lines:
        job["log_tail"] = tail(directory, tail_lines)
    return job


def prune():
    finished = [entry for entry in job_ids()
                if os.path.exists(os.path.join(JOBS, entry, "exit.json")) or not live_entry(entry)]
    for entry in finished[KEEP_FINISHED:]:
        directory = os.path.join(JOBS, entry)
        for name in os.listdir(directory):
            try:
                os.unlink(os.path.join(directory, name))
            except OSError:
                pass
        try:
            os.rmdir(directory)
        except OSError:
            pass


def run_job(job_id, timeout, command, directory):
    """The detached runner: start the command, copy its output to the log,
    enforce the timeout and record how it ended."""
    env = dict(os.environ)
    env.pop("SUPERBOT_EXEC_MARK", None)  # a cancelled exec must not kill the job
    env["SUPERBOT_JOB_ID"] = job_id
    log_path = os.path.join(directory, "log")
    log = open(log_path, "ab")
    child = subprocess.Popen(["sh", "-lc", command], cwd="/workspace", env=env, stdin=subprocess.DEVNULL,
                             stdout=subprocess.PIPE, stderr=subprocess.STDOUT, start_new_session=True)
    with open(os.path.join(LIVE, job_id + ".tmp"), "w") as handle:
        handle.write("%d %s %d" % (os.getpid(), proc_start(os.getpid()), child.pid))
    os.replace(os.path.join(LIVE, job_id + ".tmp"), os.path.join(LIVE, job_id))

    deadline = time.time() + timeout
    timed_out = False
    term_sent = 0.0
    exited_at = None
    stop_marker = os.path.join(directory, "stop")
    fd = child.stdout.fileno()
    while True:
        ready, _, _ = select.select([fd], [], [], 1.0)
        if ready:
            chunk = os.read(fd, 65536)
            if not chunk:
                break
            log.write(chunk)
            log.flush()
            if log.tell() > LOG_MAX:
                log.close()
                os.replace(log_path, log_path + ".1")
                log = open(log_path, "ab")
        now = time.time()
        if exited_at is None and child.poll() is not None:
            exited_at = now
        if exited_at is not None and now - exited_at > 2:
            break  # the command ended; a program it left behind holds the pipe
        stop = os.path.exists(stop_marker)
        if not term_sent and (stop or now > deadline):
            timed_out = not stop
            term_sent = now
            try:
                os.killpg(child.pid, signal.SIGTERM)
            except OSError:
                pass
        elif term_sent and now - term_sent > STOP_GRACE:
            try:
                os.killpg(child.pid, signal.SIGKILL)
            except OSError:
                pass
    code = child.wait()
    if code < 0:
        code = 128 - code
    log.close()
    write_json(os.path.join(directory, "exit.json"), {
        "exit_code": code, "ended_at": time.time(),
        "timed_out": timed_out, "stopped": os.path.exists(stop_marker),
    })
    try:
        os.unlink(os.path.join(LIVE, job_id))
    except OSError:
        pass


def start(job_id, timeout, max_running, name, command):
    os.makedirs(LIVE, exist_ok=True)
    os.makedirs(JOBS, exist_ok=True)
    running = running_ids()
    if len(running) >= max_running:
        emit({"error": "too_many", "running": running})
        return
    prune()
    directory = os.path.join(JOBS, job_id)
    os.makedirs(directory)
    write_json(os.path.join(directory, "meta.json"), {
        "id": job_id, "name": name, "command": command, "started_at": time.time(), "timeout": timeout,
    })
    pid = os.fork()
    if pid == 0:
        try:
            os.setsid()
            if os.fork() != 0:
                os._exit(0)
            devnull = os.open(os.devnull, os.O_RDWR)
            for fd in (0, 1, 2):
                os.dup2(devnull, fd)
            run_job(job_id, timeout, command, directory)
        finally:
            os._exit(0)
    os.waitpid(pid, 0)
    for _ in range(50):
        if live_entry(job_id) or os.path.exists(os.path.join(directory, "exit.json")):
            break
        time.sleep(0.1)
    emit({"job": describe(job_id, 0)})


def stop(job_id):
    directory = os.path.join(JOBS, job_id)
    if not os.path.isdir(directory):
        emit({"error": "not_found"})
        return
    entry = live_entry(job_id)
    if entry is None:
        emit({"job": describe(job_id, 20)})
        return
    with open(os.path.join(directory, "stop"), "w"):
        pass
    _, pgid = entry
    try:
        os.killpg(pgid, signal.SIGTERM)
    except OSError:
        pass
    deadline = time.time() + STOP_GRACE + 5
    while time.time() < deadline and live_entry(job_id):
        time.sleep(0.2)
    emit({"job": describe(job_id, 20)})


def main(argv):
    command = argv[0] if argv else ""
    if command == "start":
        start(argv[1], int(argv[2]), int(argv[3]), argv[4], argv[5])
    elif command == "status":
        job_id = argv[1] if len(argv) > 1 else ""
        lines = int(argv[2]) if len(argv) > 2 else 0
        if job_id:
            if not os.path.isdir(os.path.join(JOBS, job_id)):
                emit({"error": "not_found"})
                return
            emit({"jobs": [describe(job_id, lines)]})
        else:
            emit({"jobs": [describe(entry, lines) for entry in job_ids()[:20]]})
    elif command == "stop":
        stop(argv[1])
    elif command == "live":
        emit({"running": len(running_ids())})
    else:
        emit({"error": "unknown command"})


main(sys.argv[1:])
