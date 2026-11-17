# Screen reader for the desktop tools. The worker runs it inside the bot's
# computer as `python3 -c SCRIPT OPTIONS_JSON` and parses the JSON it prints
# (internal/tools/desktop_screen.go), so it never depends on a script baked
# into an older image. It captures the screen with scrot, lists the windows
# with wmctrl/xdotool and reads the text with tesseract, returning every word
# with its box in screen coordinates.
import json
import os
import shutil
import subprocess
import sys

opts = json.loads(sys.argv[1]) if len(sys.argv) > 1 else {}
work = "/tmp/.sbscreen"
os.makedirs(work, exist_ok=True)
out = {"width": 0, "height": 0, "mouse": None, "active": "", "windows": [], "words": [], "errors": []}


def run(cmd, timeout=20):
    try:
        result = subprocess.run(cmd, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, timeout=timeout)
    except (OSError, subprocess.TimeoutExpired) as exc:
        out["errors"].append("%s: %s" % (cmd[0], exc))
        return None
    if result.returncode != 0:
        out["errors"].append("%s: %s" % (cmd[0], (result.stderr or "").strip()[:300]))
        return None
    return result.stdout


def windows():
    listing = run(["wmctrl", "-lG"]) if shutil.which("wmctrl") else None
    if listing is None:
        return
    for line in listing.splitlines():
        parts = line.split(None, 7)
        if len(parts) < 7:
            continue
        try:
            desktop, x, y, w, h = (int(v) for v in parts[1:6])
        except ValueError:
            continue
        title = parts[7] if len(parts) > 7 else ""
        out["windows"].append({"id": parts[0].lower(), "desktop": desktop, "x": x, "y": y, "w": w, "h": h, "title": title})
    active = run(["xdotool", "getactivewindow"])
    if active and active.strip().isdigit():
        out["active"] = "0x%08x" % int(active.strip())
    location = run(["xdotool", "getmouselocation", "--shell"])
    if location:
        values = dict(line.split("=", 1) for line in location.splitlines() if "=" in line)
        try:
            out["mouse"] = [int(values["X"]), int(values["Y"])]
        except (KeyError, ValueError):
            pass


def capture():
    raw = os.path.join(work, "screen.png")
    if run(["scrot", "-o", raw], timeout=30) is None:
        return None
    from PIL import Image

    image = Image.open(raw)
    image.load()
    out["width"], out["height"] = image.size
    if opts.get("save"):
        os.makedirs(os.path.dirname(opts["save"]), exist_ok=True)
        shutil.copyfile(raw, opts["save"])
    if opts.get("jpeg"):
        os.makedirs(os.path.dirname(opts["jpeg"]), exist_ok=True)
        image.convert("RGB").save(opts["jpeg"], "JPEG", quality=int(opts.get("quality", 70)), optimize=True)
    return image


def ocr(image):
    from PIL import Image

    left, top, right, bottom = 0, 0, image.width, image.height
    region = opts.get("region")
    if region:
        x, y, w, h = (int(v) for v in region)
        left, top = max(0, x), max(0, y)
        right, bottom = min(image.width, x + w), min(image.height, y + h)
        if right - left < 4 or bottom - top < 4:
            out["errors"].append("region is outside the screen")
            return
    part = image.crop((left, top, right, bottom)).convert("L")
    scale = float(opts.get("scale", 1.5))
    if scale != 1:
        part = part.resize((max(1, int(part.width * scale)), max(1, int(part.height * scale))), Image.LANCZOS)
    source = os.path.join(work, "ocr.png")
    part.save(source)
    tsv = run(["tesseract", source, "stdout", "-l", opts.get("lang", "chi_sim+eng"),
               "--psm", str(opts.get("psm", 11)), "tsv"], timeout=90)
    if tsv is None:
        return
    for line in tsv.splitlines()[1:]:
        cols = line.split("\t")
        if len(cols) != 12 or not cols[11].strip():
            continue
        try:
            conf = float(cols[10])
            bx, by, bw, bh = (int(v) for v in cols[6:10])
            block, par, row = int(cols[2]), int(cols[3]), int(cols[4])
        except ValueError:
            continue
        if conf < 0:
            continue
        out["words"].append({
            "t": cols[11].strip(), "c": round(conf),
            "x": left + int(bx / scale), "y": top + int(by / scale),
            "w": max(1, int(bw / scale)), "h": max(1, int(bh / scale)),
            "b": block, "p": par, "l": row,
        })
        if len(out["words"]) >= 4000:
            break


# Roles worth clicking or typing into; the rest of the accessibility tree
# (panels, fillers, labels inside buttons) only adds noise.
CONTROL_ROLES = {
    "push button", "toggle button", "check box", "radio button", "menu item",
    "check menu item", "radio menu item", "menu", "combo box", "text", "entry",
    "password text", "spin button", "slider", "link", "page tab", "list item",
    "tree item", "icon", "tool bar button", "split button", "table cell",
}


def session_bus():
    """The desktop session's D-Bus address, which the accessibility bus is
    found through. This script runs from an exec, outside the session."""
    addr = os.environ.get("DBUS_SESSION_BUS_ADDRESS", "")
    if addr:
        return addr
    try:
        with open("/tmp/dbus.env") as handle:
            for line in handle:
                if line.startswith("DBUS_SESSION_BUS_ADDRESS="):
                    addr = line.split("=", 1)[1].strip().rstrip(";").strip("'\"")
    except OSError:
        return ""
    if addr:
        os.environ["DBUS_SESSION_BUS_ADDRESS"] = addr
    return addr


def controls():
    """Buttons, menus and fields the programs on screen publish over the
    accessibility bus, with their names and screen boxes. OCR misses single
    symbols such as "+" on a button; the accessible name does not."""
    if not session_bus():
        return
    os.environ.pop("NO_AT_BRIDGE", None)
    try:
        import gi

        gi.require_version("Atspi", "2.0")
        from gi.repository import Atspi
    except (ImportError, ValueError) as exc:
        out["errors"].append("accessibility unavailable: %s" % exc)
        return
    import time

    deadline = time.monotonic() + float(opts.get("controls_seconds", 4))
    Atspi.set_timeout(800, 1500)
    found = []
    visited = [0]

    def walk(node, app, depth):
        if depth > 40 or visited[0] > 6000 or time.monotonic() > deadline or len(found) >= 400:
            return
        visited[0] += 1
        try:
            states = node.get_state_set()
            # The application itself has no SHOWING state; its windows do.
            if depth > 0 and not states.contains(Atspi.StateType.SHOWING):
                return
            role = node.get_role_name() or ""
            if role in CONTROL_ROLES:
                box = node.get_extents(Atspi.CoordType.SCREEN)
                if box.width > 0 and box.height > 0 and box.x >= 0 and box.y >= 0:
                    name = (node.get_name() or "").strip()
                    if not name and role in ("text", "entry", "password text"):
                        name = "(input)"
                    if name:
                        found.append({
                            "r": role, "n": name[:80], "a": app,
                            "x": box.x, "y": box.y, "w": box.width, "h": box.height,
                            "off": not states.contains(Atspi.StateType.SENSITIVE),
                            "act": states.contains(Atspi.StateType.ACTIVE),
                        })
            count = node.get_child_count()
        except Exception:  # an app that stopped answering: skip it
            return
        for index in range(min(count, 500)):
            try:
                child = node.get_child_at_index(index)
            except Exception:
                continue
            if child is not None:
                walk(child, app, depth + 1)

    try:
        desktop = Atspi.get_desktop(0)
        apps = [desktop.get_child_at_index(i) for i in range(desktop.get_child_count())]
    except Exception as exc:
        out["errors"].append("accessibility: %s" % exc)
        return
    for app in apps:
        if app is None:
            continue
        try:
            name = app.get_name() or ""
        except Exception:
            continue
        # The desktop's own furniture is already in the window list.
        if name in ("xfce4-panel", "xfdesktop", "xfwm4", "xfce4-session", "xfsettingsd"):
            continue
        walk(app, name, 0)
    out["controls"] = found


if opts.get("windows", True):
    windows()
if opts.get("controls", True):
    controls()
if opts.get("capture", True):
    screen = capture()
    if screen is not None and opts.get("ocr", True):
        ocr(screen)
print(json.dumps(out, ensure_ascii=False))
