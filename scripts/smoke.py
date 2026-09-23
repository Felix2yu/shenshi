#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""「慎始」端到端冒烟测试。

用法:
    python3 scripts/smoke.py [--base http://127.0.0.1:8787]

脚本会自动启动一个独立的服务实例（临时数据库、随机端口），逐条验证核心接口，
最后清理临时数据。退出码 0 表示全部通过。
"""

from __future__ import annotations

import argparse
import csv
import io
import json
import os
import shutil
import signal
import socket
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request
import zipfile
from datetime import date, timedelta

PASSED: list[str] = []
FAILED: list[str] = []


def call(base: str, method: str, path: str, body: dict | None = None):
    data = None
    headers = {"Accept": "application/json"}
    if body is not None:
        data = json.dumps(body).encode("utf-8")
        headers["Content-Type"] = "application/json"
    # 查询串里可能出现中文（如关键词搜索），必须先做百分号编码。
    if "?" in path:
        head, _, query = path.partition("?")
        path = head + "?" + urllib.parse.quote(query, safe="=&")
    req = urllib.request.Request(base + path, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=10) as resp:
            raw = resp.read().decode("utf-8")
            return resp.status, (json.loads(raw) if raw else None)
    except urllib.error.HTTPError as e:
        raw = e.read().decode("utf-8")
        try:
            return e.code, json.loads(raw)
        except json.JSONDecodeError:
            return e.code, {"raw": raw}


def call_full(base: str, method: str, path: str, body: dict | None = None, headers: dict | None = None):
    """返回 (status, headers, json)。鉴权检查需要读 Set-Cookie，故不能只回状态码。"""
    hdrs = {"Accept": "application/json"}
    if headers:
        hdrs.update(headers)
    data = None
    if body is not None:
        data = json.dumps(body).encode("utf-8")
        hdrs["Content-Type"] = "application/json"
    req = urllib.request.Request(base + path, data=data, headers=hdrs, method=method)
    try:
        with urllib.request.urlopen(req, timeout=10) as resp:
            raw = resp.read().decode("utf-8")
            try:
                parsed = json.loads(raw) if raw else None
            except json.JSONDecodeError:
                parsed = None
            return resp.status, dict(resp.headers), parsed
    except urllib.error.HTTPError as e:
        raw = e.read().decode("utf-8", "ignore")
        try:
            parsed = json.loads(raw) if raw else None
        except json.JSONDecodeError:
            parsed = None
        return e.code, dict(e.headers), parsed


def call_text(base: str, method: str, path: str) -> tuple[int, str]:
    """取原始响应文本，用于 CSV 等非 JSON 接口。"""
    req = urllib.request.Request(base + path, headers={"Accept": "*/*"}, method=method)
    try:
        with urllib.request.urlopen(req, timeout=10) as resp:
            return resp.status, resp.read().decode("utf-8")
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode("utf-8", "ignore")


def check(name: str, cond: bool, detail: str = "") -> None:
    if cond:
        PASSED.append(name)
        print(f"  \033[32m✓\033[0m {name}")
    else:
        FAILED.append(f"{name} — {detail}")
        print(f"  \033[31m✗\033[0m {name}  {detail}")


def section(title: str) -> None:
    print(f"\n\033[1m{title}\033[0m")


def free_port() -> int:
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


def wait_ready(base: str, timeout: float = 20.0) -> bool:
    deadline = time.time() + timeout
    while time.time() < deadline:
        try:
            status, _ = call(base, "GET", "/api/health")
            if status == 200:
                return True
        except Exception:
            pass
        time.sleep(0.2)
    return False


def go_bin() -> str:
    """定位 go：优先 GO_BIN / PATH，其次常见安装位置（GUI 终端常缺 PATH）。"""
    explicit = os.environ.get("GO_BIN")
    if explicit:
        return explicit
    found = shutil.which("go")
    if found:
        return found
    for cand in ("/opt/homebrew/bin/go", "/usr/local/go/bin/go"):
        if os.path.exists(cand):
            return cand
    raise SystemExit("找不到 go，可显式指定：GO_BIN=/path/to/go python3 scripts/smoke.py")


def build_binary(repo_root: str, tmp: str) -> str:
    binary = os.path.join(tmp, "shenshi")
    env = dict(os.environ)
    env.setdefault("GOPROXY", "https://goproxy.cn,direct")
    subprocess.run(
        [go_bin(), "build", "-o", binary, "."],
        cwd=os.path.join(repo_root, "server"),
        check=True,
        env=env,
    )
    return binary


def spawn_instance(binary: str, repo_root: str, tmp: str, token: str | None = None):
    """按给定二进制起一个临时实例，返回 (进程, base_url)。"""
    port = free_port()
    web = os.path.join(repo_root, "web", "dist")
    if not os.path.exists(os.path.join(web, "index.html")):
        web = os.path.join(repo_root, "web")
    args = [binary, "-addr", f":{port}", "-db", os.path.join(tmp, f"shenshi-{port}.db"), "-web", web]
    if token is not None:
        args += ["-token", token]
    # 服务日志写入文件而非管道：管道不被读取时会被写满并阻塞服务进程。
    log_path = os.path.join(tmp, f"server-{port}.log")
    log_file = open(log_path, "w", encoding="utf-8")
    proc = subprocess.Popen(args, stdout=log_file, stderr=subprocess.STDOUT, text=True)
    proc._shenshi_log = log_file  # type: ignore[attr-defined]
    proc._shenshi_log_path = log_path  # type: ignore[attr-defined]
    return proc, f"http://127.0.0.1:{port}"


def shutdown(proc: subprocess.Popen) -> None:
    """优雅关停实例并关闭日志句柄。"""
    if proc is None:
        return
    proc.send_signal(signal.SIGINT)
    try:
        proc.wait(timeout=5)
    except subprocess.TimeoutExpired:
        proc.kill()
    log_file = getattr(proc, "_shenshi_log", None)
    if log_file:
        log_file.close()


def launch(repo_root: str) -> tuple[subprocess.Popen, str, str]:
    """编译并启动一个临时服务实例，返回 (进程, base_url, 数据目录)。"""
    tmp = tempfile.mkdtemp(prefix="shenshi-smoke-")
    binary = build_binary(repo_root, tmp)
    proc, base = spawn_instance(binary, repo_root, tmp)
    return proc, base, tmp


def run(base: str) -> None:
    today = date.today()
    tomorrow = today + timedelta(days=1)

    section("① 基础与启动数据")
    status, health = call(base, "GET", "/api/health")
    check("GET /api/health", status == 200 and health.get("app") == "慎始", str(health))
    status, boot = call(base, "GET", "/api/bootstrap")
    check("GET /api/bootstrap 返回分组树", status == 200 and len(boot["folders"]) >= 2, str(status))
    check("bootstrap 含收集箱", boot.get("inboxListId", 0) > 0, str(boot.get("inboxListId")))
    check("bootstrap 含智能清单角标", boot["counts"]["today"] >= 1, str(boot["counts"]))
    inbox_id = boot["inboxListId"]
    first_list = next(l for l in boot["lists"] if l["id"] != inbox_id)["id"]

    section("② 任务创建与编辑")
    status, t = call(base, "POST", "/api/tasks", {
        "title": "写「慎始」的验收清单 #验收",
        "notes": "《礼记·中庸》：凡事豫则立，不豫则废。",
        "listId": first_list,
        "priority": 3,
        "dueDate": today.isoformat(),
        "dueTime": "08:30",
        "reminders": [0, 15],
    })
    check("POST /api/tasks 创建任务", status == 201 and t["id"] > 0, str(t))
    check("优先级写入正确", t["priority"] == 3, str(t["priority"]))
    check("到期日与时间写入正确", t["dueDate"] == today.isoformat() and t["dueTime"] == "08:30", str(t))
    check("标题中的 #标签 自动建档", any(g["name"] == "验收" for g in t["tags"]), str(t["tags"]))
    check("多个提醒被保存", sorted(t["reminders"]) == [0, 15], str(t["reminders"]))
    check("高优先级自动置为「重要」", t["important"] is True, str(t["important"]))
    check("当日到期自动置为「紧急」", t["urgent"] is True, str(t["urgent"]))
    tid = t["id"]

    status, t2 = call(base, "PATCH", f"/api/tasks/{tid}", {"title": "写「慎始」的验收清单（已修订）", "notes": "慎始而敬终。"})
    check("PATCH /api/tasks/{id} 局部更新", status == 200 and t2["title"].endswith("（已修订）"), str(t2.get("title")))
    check("未传字段保持原值", t2["priority"] == 3 and t2["dueTime"] == "08:30", str(t2))

    status, t3 = call(base, "PATCH", f"/api/tasks/{tid}", {"dueDate": None})
    check("显式 null 可清空到期日", status == 200 and t3["dueDate"] is None, str(t3.get("dueDate")))
    status, t4 = call(base, "PATCH", f"/api/tasks/{tid}", {"dueDate": today.isoformat(), "dueTime": "08:30"})
    check("到期日可重新设置", t4["dueDate"] == today.isoformat(), str(t4.get("dueDate")))

    status, bad = call(base, "POST", "/api/tasks", {"title": "   "})
    check("空标题被拒绝（400）", status == 400, f"{status} {bad}")

    section("③ 子任务")
    status, sub = call(base, "POST", f"/api/tasks/{tid}/subtasks", {"title": "确认三条验收标准"})
    check("新增子任务", status == 201 and sub["id"] > 0, str(sub))
    status, _ = call(base, "PATCH", f"/api/subtasks/{sub['id']}", {"done": True})
    check("勾选子任务", status == 200, str(status))
    status, sub2 = call(base, "POST", f"/api/tasks/{tid}/subtasks", {"title": "补充一条未完成子任务"})
    status, t5 = call(base, "GET", f"/api/tasks/{tid}")
    check("子任务进度回传父任务", t5["subtaskDone"] == 1 and t5["subtaskOpen"] == 1, str((t5["subtaskDone"], t5["subtaskOpen"])))
    status, _ = call(base, "DELETE", f"/api/subtasks/{sub['id']}")
    status, _ = call(base, "DELETE", f"/api/subtasks/{sub2['id']}")
    check("删除子任务", status == 200, str(status))

    section("④ 完成 / 恢复 与重复任务续期")
    status, res = call(base, "POST", f"/api/tasks/{tid}/toggle")
    check("切换为已完成", status == 200 and res["completed"] and res["task"]["status"] == "done", str(res.get("completed")))
    check("完成时间被记录", bool(res["task"].get("completedAt")), str(res["task"].get("completedAt")))
    status, res2 = call(base, "POST", f"/api/tasks/{tid}/toggle")
    check("可恢复为未完成", res2["task"]["status"] == "todo" and res2["task"]["completedAt"] is None, str(res2["task"]["status"]))

    status, rep = call(base, "POST", "/api/tasks", {
        "title": "每日晨省：规划今日三件事",
        "listId": inbox_id,
        "dueDate": today.isoformat(),
        "repeatRule": "daily",
    })
    check("创建每日重复任务", status == 201 and rep.get("repeatRule") == "daily", str(rep.get("repeatRule")))
    status, res3 = call(base, "POST", f"/api/tasks/{rep['id']}/toggle")
    check("完成后自动续期下一次", status == 200 and res3.get("nextTask") is not None, str(res3.get("nextTask")))
    if res3.get("nextTask"):
        check("续期任务日期为明天", res3["nextTask"]["dueDate"] == tomorrow.isoformat(), str(res3["nextTask"]["dueDate"]))
        check("续期任务继承重复规则", res3["nextTask"]["repeatRule"] == "daily", str(res3["nextTask"]["repeatRule"]))
        call(base, "DELETE", f"/api/tasks/{res3['nextTask']['id']}")

    status, eb = call(base, "POST", "/api/tasks", {
        "title": "复习《礼记·学记》笔记",
        "listId": inbox_id,
        "dueDate": today.isoformat(),
        "repeatRule": "ebbinghaus:0",
    })
    status, res4 = call(base, "POST", f"/api/tasks/{eb['id']}/toggle")
    nt = res4.get("nextTask") or {}
    check("艾宾浩斯首轮间隔为 1 天", nt.get("dueDate") == tomorrow.isoformat(), str(nt.get("dueDate")))
    check("艾宾浩斯下标自动推进", nt.get("repeatRule") == "ebbinghaus:1", str(nt.get("repeatRule")))
    call(base, "DELETE", f"/api/tasks/{nt.get('id', 0)}")

    # 跳过本次：这次不做了，日期直接推到下一次，但不记为完成
    status, sk = call(base, "POST", "/api/tasks", {
        "title": "每周复盘与归档",
        "listId": inbox_id,
        "dueDate": today.isoformat(),
        "repeatRule": "weekly:1",
    })
    check("创建每周重复任务", status == 201, str(status))

    status, jumped = call(base, "POST", f"/api/tasks/{sk['id']}/skip")
    check("跳过本次返回任务", status == 200, str(status))
    check(
        "跳过本次不记为完成",
        jumped["status"] == "todo" and jumped["completedAt"] is None,
        str(jumped.get("status")),
    )
    check("跳过本次保留原任务", jumped["id"] == sk["id"] and jumped["title"] == "每周复盘与归档", jumped["title"])
    check(
        "跳过本次把日期推到下一次",
        jumped["dueDate"] is not None and jumped["dueDate"] > today.isoformat(),
        f"{today.isoformat()} → {jumped.get('dueDate')}",
    )
    check("跳过本次保留重复规则", jumped.get("repeatRule") == "weekly:1", str(jumped.get("repeatRule")))

    status, all_after = call(base, "GET", "/api/tasks?smart=all")
    titles = [t["title"] for t in all_after["tasks"]]
    check("跳过未产生重复条目", titles.count("每周复盘与归档") == 1, str(titles.count("每周复盘与归档")))

    status, _ = call(base, "POST", f"/api/tasks/{tid}/skip")
    check("非重复任务跳过被拒绝（400）", status == 400, str(status))

    status, _ = call(base, "POST", "/api/tasks/999999/skip")
    check("跳过不存在的任务返回 404", status == 404, str(status))

    call(base, "DELETE", f"/api/tasks/{sk['id']}")

    section("⑤ 视图查询：今天 / 最近7天 / 收集箱 / 逾期 / 搜索")
    for smart in ["today", "next7", "inbox", "overdue", "nodate", "all", "done"]:
        status, r = call(base, "GET", f"/api/tasks?smart={smart}")
        check(f"智能清单 {smart}", status == 200 and isinstance(r.get("tasks"), list), str(status))
    status, r = call(base, "GET", f"/api/tasks?q=验收")
    check("关键词搜索命中标题", status == 200 and any("验收" in x["title"] for x in r["tasks"]), str(r.get("count")))
    status, r = call(base, "GET", f"/api/tasks?date={today.isoformat()}")
    check("按日期筛选（日历视图）", status == 200 and all(x["dueDate"] == today.isoformat() for x in r["tasks"]), str(r.get("count")))
    status, r = call(base, "GET", "/api/tasks?quadrant=1")
    check("四象限筛选", status == 200 and all(x["important"] and x["urgent"] for x in r["tasks"]), str(r.get("count")))

    section("⑥ 拖拽改期与批量操作")
    status, moved = call(base, "POST", f"/api/tasks/{tid}/move", {"dueDate": tomorrow.isoformat(), "listId": inbox_id})
    check("拖拽改期 + 改清单", status == 200 and moved["dueDate"] == tomorrow.isoformat() and moved["listId"] == inbox_id, str(moved.get("dueDate")))
    status, moved2 = call(base, "POST", f"/api/tasks/{tid}/move", {"dueDate": None})
    check("拖拽清空日期", status == 200 and moved2["dueDate"] is None, str(moved2.get("dueDate")))
    status, r = call(base, "POST", "/api/tasks/batch", {"ids": [tid, rep["id"]], "action": "complete"})
    check("批量完成", status == 200 and r["affected"] == 2, str(r))
    status, r = call(base, "POST", "/api/tasks/batch", {"ids": [tid, rep["id"]], "action": "reopen"})
    check("批量恢复", status == 200 and r["affected"] == 2, str(r))

    section("⑦ 清单分组与标签")
    status, folder = call(base, "POST", "/api/folders", {"name": "学习", "color": "#5c6b8a", "icon": "book"})
    check("新建分组", status == 201 and folder["id"] > 0, str(folder))
    status, lst = call(base, "POST", "/api/lists", {"name": "读书笔记", "folderId": folder["id"], "color": "#5c6b8a"})
    check("在分组下新建清单", status == 201 and lst["folderId"] == folder["id"], str(lst))
    status, boot2 = call(base, "GET", "/api/bootstrap")
    node = next((f for f in boot2["folders"] if f["id"] == folder["id"]), None)
    check("清单正确归入分组", node is not None and any(l["id"] == lst["id"] for l in node["lists"]), str(node))
    status, _ = call(base, "PATCH", f"/api/folders/{folder['id']}", {"collapsed": True})
    status, boot3 = call(base, "GET", "/api/bootstrap")
    check("分组折叠状态持久化", next(f for f in boot3["folders"] if f["id"] == folder["id"])["collapsed"] is True)
    status, _ = call(base, "DELETE", f"/api/folders/{folder['id']}")
    status, boot4 = call(base, "GET", "/api/bootstrap")
    check("删除分组后清单回到顶层", any(l["id"] == lst["id"] and l["folderId"] is None for l in boot4["lists"]), "清单应保留")
    status, err = call(base, "DELETE", f"/api/lists/{inbox_id}")
    check("收集箱不可删除（403）", status == 403, f"{status} {err}")

    status, tag = call(base, "POST", "/api/tags", {"name": "深度工作", "color": "#3f7a7a"})
    check("新建标签", status == 201 and tag["id"] > 0, str(tag))
    status, r = call(base, "POST", "/api/tags/ensure", {"names": ["深度工作", "复盘"]})
    check("ensure 幂等（不重复建档）", status == 200 and len(r["ids"]) == 2, str(r.get("ids")))
    status, r = call(base, "POST", "/api/tags", {"name": "深度工作"})
    check("同名标签复用既有记录", r["id"] == tag["id"], f"{r.get('id')} vs {tag['id']}")

    section("⑧ 提醒调度")
    status, rem = call(base, "POST", "/api/tasks", {
        "title": "提醒联调：马上到期",
        "listId": inbox_id,
        "dueDate": today.isoformat(),
        "dueTime": (today.strftime("%H:%M")),
        "reminders": [0],
    })
    status, due = call(base, "GET", "/api/reminders/due?lookahead=1")
    hit = next((h for h in due["reminders"] if h["task"]["id"] == rem["id"]), None)
    check("到期任务出现在提醒队列", hit is not None, f"reminders={len(due['reminders'])}")
    if hit:
        status, _ = call(base, "POST", "/api/reminders/ack", {"taskId": rem["id"], "fireAt": hit["fireAt"]})
        check("确认投递", status == 200, str(status))
        status, due2 = call(base, "GET", "/api/reminders/due?lookahead=1")
        check("已确认的提醒不再重复投递", all(h["task"]["id"] != rem["id"] for h in due2["reminders"]), str(len(due2["reminders"])))
    status, _ = call(base, "POST", "/api/reminders/reset", {"taskId": rem["id"]})
    check("重置提醒台账", status == 200, str(status))

    section("⑨ 设置 / 统计 / 日省 / 专注")
    status, kv = call(base, "PUT", "/api/settings", {"theme": "dark", "weekStart": "1"})
    check("写入设置", status == 200 and kv["theme"] == "dark", str(kv))
    status, kv2 = call(base, "GET", "/api/settings")
    check("读取设置", kv2.get("theme") == "dark", str(kv2))

    status, st = call(base, "GET", "/api/stats?days=14")
    check("统计接口可用", status == 200 and len(st["trend"]) == 14, str(len(st.get("trend", []))))
    check("统计含未完成总数", st["totalOpen"] >= 1, str(st.get("totalOpen")))
    check("统计含分组分布", len(st["byList"]) >= 1, str(st.get("byList")))
    check("统计含四象限分布", len(st["byQuadrant"]) >= 1, str(st.get("byQuadrant")))

    status, rev = call(base, "PUT", "/api/reviews", {"date": today.isoformat(), "mood": "稳", "wins": "完成验收清单", "tomorrow": "继续推进"})
    check("写入日省复盘", status == 200 and rev["mood"] == "稳", str(rev))
    status, rev2 = call(base, "PUT", "/api/reviews", {"date": today.isoformat(), "mood": "踏实", "wins": "完成验收清单", "tomorrow": "继续推进"})
    check("同日期复盘为覆盖写", status == 200 and rev2["mood"] == "踏实", str(rev2.get("mood")))
    status, rev3 = call(base, "GET", f"/api/reviews/{today.isoformat()}")
    check("按日期读取复盘", status == 200 and rev3["mood"] == "踏实", str(rev3))
    status, revs = call(base, "GET", "/api/reviews")
    check("复盘列表", status == 200 and len(revs["reviews"]) == 1, str(len(revs.get("reviews", []))))

    status, sess = call(base, "POST", "/api/focus", {"taskId": tid, "minutes": 25})
    check("记录专注时段", status == 201 and sess["minutes"] == 25, str(sess))
    status, _ = call(base, "POST", "/api/focus", {"minutes": 999})
    check("非法专注时长被拒绝（400）", status == 400, str(status))
    status, st2 = call(base, "GET", "/api/stats?days=14")
    check("专注时长计入统计", st2["focusMinutes"] >= 25, str(st2.get("focusMinutes")))

    section("⑩ 手动拖拽排序")
    # 造三个任务再倒序重排：sortBy=manual 时才由 sort_order 说话
    sort_ids = []
    for name in ("晨间例会", "午间复盘", "晚间归档"):
        status, t = call(base, "POST", "/api/tasks", {"title": name, "listId": inbox_id})
        if status == 201:
            sort_ids.append(t["id"])
    check("排序用例：三个任务已创建", len(sort_ids) == 3, str(sort_ids))

    rev = list(reversed(sort_ids))
    status, r = call(base, "POST", "/api/tasks/reorder", {"ids": rev})
    check("POST /api/tasks/reorder", status == 200 and r.get("count") == 3, str(r))

    status, r = call(base, "GET", "/api/tasks?smart=inbox&sortBy=manual&limit=100")
    got = [t["id"] for t in r["tasks"] if t["id"] in sort_ids]
    check("手动排序按 sort_order 返回", got == rev, f"期望 {rev} 实际 {got}")

    status, _ = call(base, "GET", "/api/tasks?smart=inbox&sortBy=smart&limit=100")
    check("智能排序仍然可用", status == 200, str(status))

    status, _ = call(base, "GET", "/api/tasks?smart=inbox&sortBy=due&limit=100")
    check("按到期排序仍然可用", status == 200, str(status))

    status, _ = call(base, "POST", "/api/tasks/reorder", {"ids": []})
    check("空排序序列被拒绝（400）", status == 400, str(status))

    # 清单排序：收件箱恒在最前（is_inbox DESC），故只比较其余清单
    status, r = call(base, "GET", "/api/lists")
    normal = [l["id"] for l in r["lists"] if l["id"] != inbox_id]
    if len(normal) >= 2:
        rev_lists = list(reversed(normal))
        status, _ = call(base, "PUT", "/api/lists/reorder", {"ids": rev_lists})
        check("PUT /api/lists/reorder", status == 200, str(status))
        status, r = call(base, "GET", "/api/lists")
        after = [l["id"] for l in r["lists"] if l["id"] != inbox_id]
        check("清单顺序已按新序列", after == rev_lists, f"期望 {rev_lists} 实际 {after}")

    status, r = call(base, "GET", "/api/folders")
    fids = [f["id"] for f in r["folders"]]
    if len(fids) >= 2:
        rev_folders = list(reversed(fids))
        status, _ = call(base, "PUT", "/api/folders/reorder", {"ids": rev_folders})
        check("PUT /api/folders/reorder", status == 200, str(status))
        status, r = call(base, "GET", "/api/folders")
        after = [f["id"] for f in r["folders"]]
        check("分组顺序已按新序列", after == rev_folders, f"期望 {rev_folders} 实际 {after}")

    call(base, "POST", "/api/tasks/batch", {"ids": sort_ids, "action": "delete"})

    section("⑪ 导出与导入")
    status, bundle = call(base, "GET", "/api/export")
    check("GET /api/export 返回备份", status == 200 and bundle.get("version") == 1, str(status))
    check("备份记录了收件箱", bundle.get("inboxListId", 0) > 0, str(bundle.get("inboxListId")))
    base_tasks = len(bundle["tasks"])
    check("备份包含任务", base_tasks > 0, str(base_tasks))
    check("任务自带子任务与标签", all("subtasks" in t and "tags" in t for t in bundle["tasks"]))
    check("备份包含复盘与专注", isinstance(bundle["reviews"], list) and isinstance(bundle["focus"], list))
    check("备份包含设置", isinstance(bundle["settings"], dict))

    status, csv_text = call_text(base, "GET", "/api/export/csv")
    check("GET /api/export/csv 返回表格", status == 200 and csv_text.startswith("\ufeff"), str(status))
    # 备注里可能含换行，必须交给 CSV 解析器处理，按行切分会误判
    csv_rows = list(csv.reader(io.StringIO(csv_text.lstrip("\ufeff"))))
    check("CSV 行数 = 任务数 + 表头", len(csv_rows) == base_tasks + 1, f"{len(csv_rows)} vs {base_tasks + 1}")
    check("CSV 表头可读", "标题" in csv_rows[0] and "优先级" in csv_rows[0], ",".join(csv_rows[0])[:60])
    check("CSV 每行列数与表头一致", all(len(r) == len(csv_rows[0]) for r in csv_rows), "列数不一致")

    # merge：原样再导入一份，数据作为副本追加
    status, res = call(base, "POST", "/api/import?mode=merge", bundle)
    check("merge 导入返回统计", status == 200 and res.get("tasks") == base_tasks, str(res))
    status, after = call(base, "GET", "/api/export")
    check("merge 后任务数翻倍", len(after["tasks"]) == base_tasks * 2, f"{len(after['tasks'])} vs {base_tasks * 2}")
    check("merge 未重复创建收件箱", sum(1 for l in after["lists"] if l["id"] == after["inboxListId"]) == 1)
    check(
        "merge 按名称合并分组而非重复",
        len(after["folders"]) == len(bundle["folders"]),
        f"{len(after['folders'])} vs {len(bundle['folders'])}",
    )

    # replace：先删掉一个任务，再用最早的备份精确还原
    victim = after["tasks"][0]["id"]
    call(base, "DELETE", f"/api/tasks/{victim}")
    status, cur = call(base, "GET", "/api/export")
    check("删除后任务数减少", len(cur["tasks"]) == base_tasks * 2 - 1, str(len(cur["tasks"])))

    status, res = call(base, "POST", "/api/import?mode=replace", bundle)
    check("replace 导入返回统计", status == 200 and res.get("tasks") == base_tasks, str(res))
    status, restored = call(base, "GET", "/api/export")
    check("replace 后任务数回到备份值", len(restored["tasks"]) == base_tasks, str(len(restored["tasks"])))

    def fingerprint(b):
        return sorted((t["id"], t["title"], t["dueDate"], t["sortOrder"], t["status"]) for t in b["tasks"])

    check("replace 后任务指纹与备份一致", fingerprint(restored) == fingerprint(bundle))
    check(
        "replace 后清单与分组也复原",
        [l["id"] for l in restored["lists"]] == [l["id"] for l in bundle["lists"]]
        and [f["id"] for f in restored["folders"]] == [f["id"] for f in bundle["folders"]],
    )
    check("replace 后收件箱仍唯一", sum(1 for l in restored["lists"] if l["id"] == restored["inboxListId"]) == 1)

    status, _ = call(base, "POST", "/api/import?mode=nonsense", bundle)
    check("未知导入模式被拒绝（400）", status == 400, str(status))
    status, _ = call(base, "POST", "/api/import", {"version": 1, "lists": [], "tasks": []})
    check("空备份被拒绝（400）", status == 400, str(status))

    section("⑫ 习惯打卡")
    # 后端星期口径与 Go 一致：0=周日；Python 的 weekday() 是 1=周一..7=周日。
    go_wd = (date.today().weekday() + 1) % 7
    td = today.isoformat()

    status, h1 = call(base, "POST", "/api/habits", {"name": "晨起临帖", "color": "#6b7f6e"})
    check("POST /api/habits 新建习惯", status == 201 and h1["id"] > 0, str(status))
    check("新习惯默认每天、目标 1 次", h1["cadence"] == "daily" and h1["target"] == 1, str(h1))
    check("新习惯起始日默认为今天", h1["startDate"] == td, h1["startDate"])

    status, _ = call(base, "POST", "/api/habits", {"name": "  "})
    check("空名称被拒绝（400）", status == 400, str(status))
    status, _ = call(base, "POST", "/api/habits", {"name": "节奏非法", "cadence": "hourly"})
    check("非法节奏被拒绝（400）", status == 400, str(status))
    status, _ = call(base, "POST", "/api/habits", {"name": "目标非法", "target": 0})
    check("目标次数 0 被拒绝（400）", status == 400, str(status))

    hid = h1["id"]

    def stat_of(habit_id: int) -> dict:
        _, board = call(base, "GET", "/api/habits")
        return next(s for s in board["stats"] if s["habitId"] == habit_id)

    status, board = call(base, "GET", "/api/habits")
    check("GET /api/habits 返回看板", status == 200 and {"from", "to", "today", "habits", "logs", "stats"} <= set(board), str(status))
    check("看板默认区间铺满 12 周", (date.fromisoformat(board["to"]) - date.fromisoformat(board["from"])).days == 83, board["from"])

    # 连续打卡
    status, r = call(base, "POST", f"/api/habits/{hid}/check", {"day": td})
    check("今日打卡成功", status == 200 and r["log"]["count"] == 1, str(r))
    status, r = call(base, "POST", f"/api/habits/{hid}/check", {"day": td})
    check("同日再打卡累计次数而非新增行", status == 200 and r["log"]["count"] == 2, str(r))
    status, r = call(base, "POST", f"/api/habits/{hid}/check", {"day": td, "count": 0})
    check("次数归零即撤销当天记录", status == 200 and r["log"] is None, str(r))

    # 缺省 day 视为今天
    status, r = call(base, "POST", f"/api/habits/{hid}/check", None)
    check("缺省日期按今天计", status == 200 and r["log"]["day"] == td, str(r))
    st = stat_of(hid)
    check("今日已达标", st["today"] is True and st["todayAt"] == 1, str(st))
    check("连续 1 天", st["streak"] == 1, str(st["streak"]))

    # 补打前几天（都在起始日之前）：应计入连续天数，但不虚增「应做」分母
    d1 = (date.today() - timedelta(days=1)).isoformat()
    d2 = (date.today() - timedelta(days=2)).isoformat()
    for d in (d1, d2):
        call(base, "POST", f"/api/habits/{hid}/check", {"day": d})
    st = stat_of(hid)
    check("补记的打卡计入连续天数", st["streak"] == 3, str(st["streak"]))
    check("历史最长连续同步更新", st["best"] == 3, str(st["best"]))
    check("补记不虚增应做次数（起始日前不算欠账）", st["due"] == 1 and st["done"] == 1, str(st))
    check("区间达标率按应做日计算", st["rate"] == 1.0, str(st))

    # 撤掉今天：连续天数应退回昨天起算的 2，而不是归零
    status, _ = call(base, "DELETE", f"/api/habits/{hid}/check?day={td}")
    check("DELETE check 撤销打卡", status == 200, str(status))
    st = stat_of(hid)
    check("今天未打卡不计为断档（从昨天回数）", st["streak"] == 2, str(st["streak"]))
    check("今日状态回到未达标", st["today"] is False and st["todayAt"] == 0, str(st))
    call(base, "POST", f"/api/habits/{hid}/check", {"day": td})

    # 目标次数：未达 target 不算达标
    status, h2 = call(base, "POST", "/api/habits", {"name": "日饮八杯", "target": 3})
    check("新建多次达标的习惯", status == 201 and h2["target"] == 3, str(status))
    call(base, "POST", f"/api/habits/{h2['id']}/check")
    st = stat_of(h2["id"])
    check("打卡不足目标不算今日达标", st["today"] is False and st["todayAt"] == 1, str(st))
    call(base, "POST", f"/api/habits/{h2['id']}/check")
    call(base, "POST", f"/api/habits/{h2['id']}/check")
    st = stat_of(h2["id"])
    check("达到目标次数即算达标", st["today"] is True and st["todayAt"] == 3, str(st))

    # 按周重复：只在指定星期排期
    status, h3 = call(base, "POST", "/api/habits", {"name": "今日读书", "cadence": "weekly", "weekdays": str(go_wd)})
    check("新建按周习惯", status == 201 and h3["weekdays"] == str(go_wd), str(h3))
    call(base, "POST", f"/api/habits/{h3['id']}/check")
    st = stat_of(h3["id"])
    check("按周习惯命中今天即已达标", st["today"] is True, str(st))

    other_wd = (go_wd + 3) % 7
    status, h4 = call(base, "POST", "/api/habits", {"name": "非今日例行", "cadence": "weekly", "weekdays": str(other_wd)})
    check("按周习惯可指定其它星期", status == 201 and h4["weekdays"] == str(other_wd), str(h4))
    call(base, "POST", f"/api/habits/{h4['id']}/check")
    st = stat_of(h4["id"])
    check("今天非排期日，不计入今日应做", st["today"] is False, str(st))
    check("今天非排期日，不计入区间欠账", st["due"] == 0 and st["rate"] == 0, str(st))

    status, _ = call(base, "POST", "/api/habits", {"name": "星期非法", "cadence": "weekly", "weekdays": "9"})
    check("星期取值越界被拒绝（400）", status == 400, str(status))
    status, _ = call(base, "POST", f"/api/habits/{hid}/check", {"day": "2026/01/01"})
    check("日期格式非法被拒绝（400）", status == 400, str(status))
    status, _ = call(base, "DELETE", f"/api/habits/{hid}/check?day=bad-day")
    check("撤销打卡校验日期格式（400）", status == 400, str(status))

    # 更新与排序
    status, r = call(base, "PATCH", f"/api/habits/{h2['id']}", {"name": "日饮八杯水", "target": 4, "note": "小口慢饮"})
    check("PATCH 更新习惯字段", status == 200 and r["name"] == "日饮八杯水" and r["target"] == 4, str(r))
    status, r = call(base, "PUT", "/api/habits/reorder", {"ids": [h2["id"], hid, h3["id"], h4["id"]]})
    check("PUT /api/habits/reorder", status == 200, str(status))
    _, board = call(base, "GET", "/api/habits")
    check("习惯顺序已按新序列", [h["id"] for h in board["habits"]][:4] == [h2["id"], hid, h3["id"], h4["id"]], str([h["id"] for h in board["habits"]]))

    # 导出/导入带上习惯与流水
    status, bundle2 = call(base, "GET", "/api/export")
    check("备份包含习惯", isinstance(bundle2.get("habits"), list) and len(bundle2["habits"]) == 4, str(len(bundle2.get("habits", []))))
    check("备份包含打卡流水", isinstance(bundle2.get("habitLogs"), list) and len(bundle2["habitLogs"]) > 0, str(len(bundle2.get("habitLogs", []))))
    habit_log_count = len(bundle2["habitLogs"])
    status, res = call(base, "POST", "/api/import?mode=merge", bundle2)
    check("merge 导入统计含习惯", status == 200 and res.get("habits") == 4, str(res))
    check("merge 导入统计含流水", res.get("habitLogs") == habit_log_count, str(res.get("habitLogs")))
    _, board = call(base, "GET", "/api/habits")
    check("merge 后习惯数量翻倍", len(board["habits"]) == 8, str(len(board["habits"])))
    # 副本的流水必须挂到新 id 上，而不是残留指向原习惯
    copied = [h for h in board["habits"] if h["id"] not in {h1["id"], h2["id"], h3["id"], h4["id"]}]
    copied_ids = {h["id"] for h in copied}
    logged_ids = {l["habitId"] for l in board["logs"]}
    check(
        "副本习惯各自带上了流水",
        copied_ids <= logged_ids,
        f"副本 {sorted(copied_ids)} 有流水 {sorted(copied_ids & logged_ids)}；全部流水 {sorted(logged_ids)}",
    )

    status, _ = call(base, "POST", "/api/import?mode=replace", bundle2)
    _, board = call(base, "GET", "/api/habits")
    check("replace 后习惯数回到备份值", len(board["habits"]) == 4, str(len(board["habits"])))
    check("replace 后流水数回到备份值", len(board["logs"]) == habit_log_count, f"{len(board['logs'])} vs {habit_log_count}")

    # 删除级联
    status, _ = call(base, "DELETE", f"/api/habits/{h2['id']}")
    check("删除习惯", status == 200, str(status))
    status, _ = call(base, "DELETE", f"/api/habits/{h2['id']}")
    check("重复删除返回 404", status == 404, str(status))
    _, board = call(base, "GET", "/api/habits")
    check("删除后习惯不再出现", all(h["id"] != h2["id"] for h in board["habits"]))
    check("打卡流水随习惯级联删除", all(l["habitId"] != h2["id"] for l in board["logs"]))

    section("⑬ 删除与级联")
    status, r = call(base, "DELETE", f"/api/lists/{lst['id']}")
    check("删除清单", status == 200, str(status))
    status, _ = call(base, "DELETE", "/api/tasks/999999")
    check("删除不存在的任务返回 404", status == 404, str(status))
    status, r = call(base, "DELETE", f"/api/tasks/{tid}")
    check("删除任务", status == 200, str(status))
    status, r = call(base, "GET", f"/api/tasks/{tid}")
    check("删除后不可读取", status == 404, str(status))


class _NoRedirect(urllib.request.HTTPRedirectHandler):
    """well-known 的重定向要自己看，别让 urllib 替我们跟到下一个地址去。"""

    def redirect_request(self, req, fp, code, msg, headers, newurl):  # noqa: D102
        return None


_NO_REDIRECT = urllib.request.build_opener(_NoRedirect)


def call_raw(
    base: str,
    method: str,
    path: str,
    body: bytes | None = None,
    headers: dict | None = None,
    follow: bool = True,
) -> tuple[int, dict, str]:
    """发一个原始请求，返回 (status, headers, text)。CalDAV 与上传都要用它。"""
    hdrs = {"Accept": "*/*"}
    if headers:
        hdrs.update(headers)
    req = urllib.request.Request(base + path, data=body, headers=hdrs, method=method)
    opener = urllib.request.build_opener() if follow else _NO_REDIRECT
    try:
        with opener.open(req, timeout=10) as resp:
            return resp.status, dict(resp.headers), resp.read().decode("utf-8", "ignore")
    except urllib.error.HTTPError as e:
        return e.code, dict(e.headers or {}), e.read().decode("utf-8", "ignore")


def hdr(headers: dict, name: str) -> str:
    """按名字取响应头，忽略大小写——Go 会把 DAV 规范成 Dav。"""
    for k, v in headers.items():
        if k.lower() == name.lower():
            return v
    return ""


def call_bytes(
    base: str, method: str, path: str, body: bytes | None = None, headers: dict | None = None
) -> tuple[int, dict, bytes]:
    """与 call_raw 相同，但原样返回字节——ZIP 备份是二进制，解码成文本就坏了。"""
    hdrs = {"Accept": "*/*"}
    if headers:
        hdrs.update(headers)
    req = urllib.request.Request(base + path, data=body, headers=hdrs, method=method)
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            return resp.status, dict(resp.headers), resp.read()
    except urllib.error.HTTPError as e:
        return e.code, dict(e.headers or {}), e.read()


def call_multipart(base: str, path: str, filename: str, content: str, field: str = "file"):
    """手工拼一个 multipart/form-data：为了不给测试脚本引入外部依赖。"""
    return call_multipart_blob(base, path, filename, content.encode("utf-8"), field)


def call_multipart_blob(base: str, path: str, filename: str, data: bytes, field: str = "file"):
    """上传二进制内容（压缩包、图片等）。"""
    boundary = "----shenshi-smoke-boundary"
    head = (
        f"--{boundary}\r\n"
        f'Content-Disposition: form-data; name="{field}"; filename="{filename}"\r\n'
        "Content-Type: application/octet-stream\r\n\r\n"
    ).encode("utf-8")
    tail = f"\r\n--{boundary}--\r\n".encode("utf-8")
    status, _, text = call_raw(
        base,
        "POST",
        path,
        head + data + tail,
        {"Content-Type": f"multipart/form-data; boundary={boundary}"},
    )
    try:
        return status, json.loads(text)
    except json.JSONDecodeError:
        return status, {"raw": text}


def run_extras(base: str) -> None:
    """附件、Webhook、模板、自动备份与 CalDAV —— 后加的几组扩展能力。"""

    # ---------- 附件 ----------
    section("⑭ 任务附件")
    status, task = call(base, "POST", "/api/tasks", {"title": "带附件的任务", "notes": "用于验证附件"})
    check("准备一个任务", status in (200, 201) and task.get("id"), str(status))
    tid = task["id"]

    status, lst = call(base, "GET", f"/api/tasks/{tid}/attachments")
    check("新任务没有附件", status == 200 and lst == [], str(lst))

    status, att = call_multipart(base, f"/api/tasks/{tid}/attachments", "纪要.txt", "这是附件的正文。")
    check("上传附件返回 201", status == 201, str(status))
    check("附件记录了原始文件名", att.get("name") == "纪要.txt", str(att.get("name")))
    check("附件带大小", isinstance(att.get("size"), int) and att["size"] > 0, str(att.get("size")))
    aid = att.get("id")

    # 存储名必须脱离原始文件名，避免同名覆盖与路径穿越
    check("存储名是随机串而非原名", att.get("file") != "纪要.txt" and str(att.get("file")).endswith(".txt"), str(att.get("file")))

    status, lst = call(base, "GET", f"/api/tasks/{tid}/attachments")
    check("附件出现在列表里", status == 200 and len(lst) == 1, str(len(lst or [])))

    status, headers, text = call_raw(base, "GET", f"/api/attachments/{aid}")
    check("下载附件返回内容", status == 200 and "这是附件的正文。" in text, str(status))
    check("下载带 Content-Disposition", "attachment" in hdr(headers, "Content-Disposition"), hdr(headers, "Content-Disposition"))

    status, _, _ = call_raw(base, "GET", "/api/attachments/999999")
    check("下载不存在的附件返回 404", status == 404, str(status))

    # 列表接口带上附件：详情面板不必再单独拉一次
    status, got = call(base, "GET", f"/api/tasks/{tid}")
    check("任务详情自带附件列表", status == 200 and len(got.get("attachments") or []) == 1, str(got.get("attachments")))

    status, _ = call(base, "DELETE", f"/api/attachments/{aid}")
    check("删除附件", status == 200, str(status))
    status, lst = call(base, "GET", f"/api/tasks/{tid}/attachments")
    check("删除后列表为空", status == 200 and lst == [], str(lst))

    # 上传一个超大附件应当被拒，而不是把磁盘写满
    status, body = call_multipart(base, f"/api/tasks/{tid}/attachments", "big.txt", "x" * (33 * 1024 * 1024))
    check("超过 32MB 的附件被拒绝", status == 400, f"{status} {str(body)[:60]}")

    # ---------- 模板任务 ----------
    section("⑮ 模板任务")
    status, tpl = call(
        base,
        "POST",
        "/api/templates",
        {
            "name": "每周例会",
            "title": "周会前同步本周计划",
            "notes": "先写议程再开会",
            "priority": 3,
            "dueOffset": 0,
            "dueTime": "09:30",
            "reminders": [0, 30],
            "subtasks": ["整理议程", "同步进度"],
        },
    )
    check("新建模板返回 201", status == 201, str(status))
    check("模板保留子任务", len(tpl.get("subtasks") or []) == 2, str(tpl.get("subtasks")))
    tpl_id = tpl["id"]

    status, tpls = call(base, "GET", "/api/templates")
    check("模板列表可读", status == 200 and any(t["id"] == tpl_id for t in tpls), str(len(tpls or [])))

    status, made = call(base, "POST", f"/api/templates/{tpl_id}/instantiate")
    check("按模板生成任务", status == 201 and made.get("title") == "周会前同步本周计划", str(status))
    check("生成物沿用模板的优先级", made.get("priority") == 3, str(made.get("priority")))
    check("生成物带上子任务", len(made.get("subtasks") or []) == 2, str(made.get("subtasks")))
    check("偏移 0 天时日期落在今天", made.get("dueDate") == date.today().isoformat(), str(made.get("dueDate")))
    check("生成物带时间", made.get("dueTime") == "09:30", str(made.get("dueTime")))
    made_id = made["id"]

    # 指定日期可以覆盖模板的偏移
    future = (date.today() + timedelta(days=5)).isoformat()
    status, made2 = call(base, "POST", f"/api/templates/{tpl_id}/instantiate", {"dueDate": future})
    check("可覆盖生成日期", status == 201 and made2.get("dueDate") == future, str(made2.get("dueDate")))

    status, _ = call(base, "PATCH", f"/api/templates/{tpl_id}", {"name": "每周例会（改）"})
    status, tpls = call(base, "GET", "/api/templates")
    check("改模板名称", any(t.get("name") == "每周例会（改）" for t in tpls), str(tpls))

    status, _ = call(base, "POST", "/api/templates", {"name": "缺标题"})
    check("缺标题的模板被拒", status == 400, str(status))

    status, _ = call(base, "DELETE", f"/api/templates/{tpl_id}")
    check("删除模板", status == 200, str(status))
    for t in (made_id, made2["id"]):
        call(base, "DELETE", f"/api/tasks/{t}")

    # ---------- 出站 Webhook ----------
    section("⑯ 出站 Webhook")
    status, hook = call(
        base,
        "POST",
        "/api/webhooks",
        {"name": "测试回调", "url": "http://127.0.0.1:9/hook", "secret": "s3cret", "events": ["task.created", "task.completed"]},
    )
    check("新建 Webhook 返回 201", status == 201, str(status))
    check("密钥不回显", hook.get("secret") == "" and hook.get("hasSecret") is True, str(hook))
    check("订阅事件被保留", sorted(hook.get("events") or []) == ["task.completed", "task.created"], str(hook.get("events")))
    hid = hook["id"]

    status, _ = call(base, "POST", "/api/webhooks", {"url": "ftp://example.com/hook"})
    check("非 http 地址被拒", status == 400, str(status))

    status, hooks = call(base, "GET", "/api/webhooks")
    check("Webhook 列表可读", status == 200 and any(h["id"] == hid for h in hooks), str(len(hooks or [])))

    # 投递到一个必然失败的地址，验证失败被记进台账而不是抛出去
    status, fired = call(base, "POST", "/api/tasks", {"title": "触发一次事件"})
    check("新建任务仍成功（Webhook 失败不影响写入）", status in (200, 201), str(status))
    time.sleep(1.2)
    status, _ = call(base, "POST", f"/api/webhooks/{hid}/test")
    check("测试投递接口可用", status == 200, str(status))
    time.sleep(1.5)
    status, logs = call(base, "GET", f"/api/webhooks/{hid}/deliveries")
    check("投递结果进了台账", status == 200 and isinstance(logs, list), str(status))
    check("失败的投递被记为未送达", any(d.get("ok") is False for d in logs or []), str(logs[:1]))

    status, _ = call(base, "PATCH", f"/api/webhooks/{hid}", {"enabled": False})
    status, hooks = call(base, "GET", "/api/webhooks")
    check("可停用 Webhook", any(h["id"] == hid and h["enabled"] is False for h in hooks), str(hooks))

    status, _ = call(base, "DELETE", f"/api/webhooks/{hid}")
    check("删除 Webhook", status == 200, str(status))
    call(base, "DELETE", f"/api/tasks/{fired['id']}")

    # ---------- 自动备份 ----------
    section("⑰ 自动备份")
    status, bk = call(base, "GET", "/api/backups")
    check("可读取自动备份状态", status == 200 and "enabled" in bk, str(status))
    check("默认未启用自动备份", bk.get("enabled") is False, str(bk.get("enabled")))

    status, res = call(base, "POST", "/api/backups/run")
    check("可立即备份一次", status == 200 and res.get("file"), str(status))
    check("备份文件名带时间戳", "shenshi-backup-" in str(res.get("file")), str(res.get("file")))

    status, bk = call(base, "GET", "/api/backups")
    check("备份后文件列表非空", len(bk.get("files") or []) >= 1, str(bk.get("files")))
    check("记录了上次备份时间", bool(bk.get("lastAt")), str(bk.get("lastAt")))

    # 保留份数：连做三次只留两份，验证裁剪逻辑真的会删
    for _ in range(2):
        call(base, "POST", "/api/backups/run?keep=2")
    status, bk = call(base, "GET", "/api/backups")
    check("按保留份数裁剪旧备份", len(bk.get("files") or []) <= 2, str(len(bk.get("files") or [])))

    status, _ = call(base, "PUT", "/api/settings", {"autoBackup": "1", "autoBackupHour": "4", "autoBackupKeep": "7"})
    status, bk = call(base, "GET", "/api/backups")
    check("自动备份开关可写入设置", bk.get("enabled") is True and bk.get("hour") == "4", str(bk))

    # 备份出来的文件必须能喂回导入接口，否则「自动备份」没有意义
    status, bundle = call(base, "GET", "/api/export")
    check("手动导出仍是自洽的备份", status == 200 and bundle.get("app") == "慎始", str(status))

    # ---------- CalDAV ----------
    section("⑱ CalDAV 同步")

    # Apple 客户端的死穴：任何 501 都会让它放弃整个账户
    status, headers, _ = call_raw(base, "OPTIONS", "/caldav/user/")
    check("OPTIONS 返回 204", status == 204, str(status))
    check("Dav 头含 calendar-access", "calendar-access" in hdr(headers, "DAV"), hdr(headers, "DAV"))
    check("Allow 头含 PROPPATCH", "PROPPATCH" in hdr(headers, "Allow"), hdr(headers, "Allow"))
    check("Allow 头含 PUT（提醒事项勾选要靠它）", "PUT" in hdr(headers, "Allow"), hdr(headers, "Allow"))

    status, headers, _ = call_raw(base, "GET", "/.well-known/caldav", follow=False)
    check("well-known 用 302 重定向", status == 302, str(status))
    check("well-known 指向账户根", "/caldav/user/" in hdr(headers, "Location"), hdr(headers, "Location"))

    propfind = (
        '<?xml version="1.0" encoding="utf-8"?>'
        '<D:propfind xmlns:D="DAV:"><D:prop><D:displayname/><D:getetag/></D:prop></D:propfind>'
    ).encode("utf-8")
    status, _, text = call_raw(base, "PROPFIND", "/caldav/user/", propfind, {"Depth": "0", "Content-Type": "application/xml"})
    check("PROPFIND 账户根返回 207", status == 207, str(status))
    check("PROPFIND 不是 501", status != 501, str(status))

    status, _, text = call_raw(base, "PROPFIND", "/caldav/user/calendars/shenshi-tasks/", propfind, {"Depth": "1", "Content-Type": "application/xml"})
    check("PROPFIND 提醒事项集合返回 207", status == 207, str(status))
    check("集合里列出了对象", ".ics" in text, text[:80])

    # 尾斜杠差异必须被容忍：Apple 刷新账户时会自己补上
    status2, _, _ = call_raw(base, "PROPFIND", "/caldav/user/calendars/shenshi-tasks", propfind, {"Depth": "0", "Content-Type": "application/xml"})
    check("集合路径带不带尾斜杠都认", status2 == 207, str(status2))

    status, r = call(base, "POST", "/api/tasks", {"title": "CalDAV 同步用任务", "dueDate": date.today().isoformat(), "dueTime": "10:00"})
    check("准备一个带日期的任务", status in (200, 201) and r.get("id"), str(status))
    cid = r["id"]

    status, _, text = call_raw(base, "GET", f"/caldav/user/calendars/shenshi-tasks/shenshi-td-{cid}.ics")
    check("可读取单个 VTODO", status == 200 and "BEGIN:VTODO" in text, str(status))
    check("VTODO 带标题", "CalDAV 同步用任务" in text, text[:80])

    status, _, text = call_raw(base, "GET", f"/caldav/user/calendars/shenshi/shenshi-ev-{cid}.ics")
    check("带日期的任务也进日历集合", status == 200 and "BEGIN:VEVENT" in text, str(status))

    # RFC 6578 增量同步
    sync_body = (
        '<?xml version="1.0" encoding="utf-8"?>'
        '<D:sync-collection xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav">'
        "<D:sync-level>1</D:sync-level><D:prop><D:getetag/><C:calendar-data/></D:prop>"
        "</D:sync-collection>"
    ).encode("utf-8")
    status, _, text = call_raw(base, "REPORT", "/caldav/user/calendars/shenshi-tasks/", sync_body, {"Content-Type": "application/xml"})
    check("sync-collection 返回 207", status == 207, str(status))
    check("响应带回 sync-token", "sync-token" in text, text[:120])

    # 取一个可用令牌，再用它做一次增量同步（应当只报变化，且令牌单调前进）
    token = ""
    if "<D:sync-token>" in text:
        token = text.split("<D:sync-token>")[1].split("</D:sync-token>")[0].strip()
    status, _, text2 = call_raw(
        base,
        "REPORT",
        "/caldav/user/calendars/shenshi-tasks/",
        (
            '<?xml version="1.0" encoding="utf-8"?>'
            '<D:sync-collection xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav">'
            f"<D:sync-token>{token}</D:sync-token><D:sync-level>1</D:sync-level>"
            "<D:prop><D:getetag/><C:calendar-data/></D:prop></D:sync-collection>"
        ).encode("utf-8"),
        {"Content-Type": "application/xml"},
    )
    check("带令牌的增量同步成功", status == 207, str(status))
    check("增量结果不再包含全部历史", "BEGIN:VTODO" not in text2 or text2.count("BEGIN:VTODO") <= 1, str(text2.count("BEGIN:VTODO")))

    status, _, _ = call_raw(
        base,
        "REPORT",
        "/caldav/user/calendars/shenshi-tasks/",
        (
            '<?xml version="1.0" encoding="utf-8"?>'
            '<D:sync-collection xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav">'
            "<D:sync-token>not-a-valid-token</D:sync-token><D:sync-level>1</D:sync-level>"
            "<D:prop><D:getetag/></D:prop></D:sync-collection>"
        ).encode("utf-8"),
        {"Content-Type": "application/xml"},
    )
    check("无效令牌返回 403 而非 501", status == 403, str(status))

    # PROPPATCH：库会返回 501，这里是重点回归项
    proppatch = (
        '<?xml version="1.0" encoding="utf-8"?>'
        '<D:propertyupdate xmlns:D="DAV:"><D:set><D:prop><D:displayname>改名</D:displayname></D:prop></D:set></D:propertyupdate>'
    ).encode("utf-8")
    status, _, _ = call_raw(base, "PROPPATCH", "/caldav/user/calendars/shenshi-tasks/shenshi-td-%d.ics" % cid, proppatch, {"Content-Type": "application/xml"})
    check("PROPPATCH 返回 207 而不是 501", status == 207, str(status))

    for m in ("COPY", "MOVE", "MKCOL", "LOCK"):
        status, _, _ = call_raw(base, m, "/caldav/user/calendars/shenshi-tasks/", None, {"Destination": "/caldav/user/other/"})
        check(f"{m} 返回 403 而不是 501", status == 403, str(status))

    # 写回：在提醒事项里勾掉一个任务
    ics = (
        "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//test//test//EN\r\n"
        "BEGIN:VTODO\r\nUID:shenshi-td-%d\r\nSUMMARY:CalDAV 同步用任务\r\nSTATUS:COMPLETED\r\nEND:VTODO\r\nEND:VCALENDAR\r\n" % cid
    ).encode("utf-8")
    status, _, _ = call_raw(base, "PUT", f"/caldav/user/calendars/shenshi-tasks/shenshi-td-{cid}.ics", ics, {"Content-Type": "text/calendar"})
    check("PUT 完成状态被接受", status in (200, 201, 204), str(status))
    status, after = call(base, "GET", f"/api/tasks/{cid}")
    check("勾选后任务在服务端变为完成", after.get("status") == "done", str(after.get("status")))

    # 日历集合是只读的：往里 PUT 事件应被拒，且不能是 501
    vevent = (
        "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//test//test//EN\r\n"
        "BEGIN:VEVENT\r\nUID:shenshi-ev-%d\r\nSUMMARY:改标题\r\nDTSTAMP:20260101T000000Z\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n" % cid
    ).encode("utf-8")
    status, _, _ = call_raw(base, "PUT", f"/caldav/user/calendars/shenshi/shenshi-ev-{cid}.ics", vevent, {"Content-Type": "text/calendar"})
    check("日历集合拒绝写入且不是 501", status == 403, str(status))

    status, _, _ = call_raw(base, "DELETE", f"/caldav/user/calendars/shenshi-tasks/shenshi-td-{cid}.ics")
    check("不允许从客户端删除任务", status == 403, str(status))

    call(base, "DELETE", f"/api/tasks/{cid}")
    call(base, "DELETE", f"/api/tasks/{tid}")

    # ---------- 完整备份（ZIP） ----------
    # 这一节刻意放在最后：它会用覆盖导入重建整库，前面的断言都跑完了才动。
    section("⑲ 完整备份：附件随 JSON 一起打包")

    status, ztask = call(base, "POST", "/api/tasks", {"title": "备份往返用的任务", "notes": "验证附件跟着备份走"})
    check("准备一个待备份的任务", status in (200, 201) and ztask.get("id"), str(status))
    zid = ztask["id"]

    payload = "附件正文：跟着备份一起走。"
    status, zatt = call_multipart(base, f"/api/tasks/{zid}/attachments", "随行.txt", payload)
    check("给任务挂一个附件", status == 201 and zatt.get("id"), str(status))
    zstored = zatt.get("file")

    status, zheaders, zdata = call_bytes(base, "GET", "/api/export/zip")
    check("导出 ZIP 返回 200", status == 200, str(status))
    check("响应类型是 zip", "zip" in hdr(zheaders, "Content-Type"), hdr(zheaders, "Content-Type"))
    check("文件名带时间戳", "shenshi-backup-" in hdr(zheaders, "Content-Disposition"), hdr(zheaders, "Content-Disposition"))
    check("响应头报告打包的附件数", hdr(zheaders, "X-Shenshi-Attachments") == "1", hdr(zheaders, "X-Shenshi-Attachments"))

    with zipfile.ZipFile(io.BytesIO(zdata)) as zf:
        names = zf.namelist()
        check("包根上是 JSON 备份", "shenshi-backup.json" in names, str(names[:4]))
        check("附件按存储名放进 attachments/", f"attachments/{zstored}" in names, str([n for n in names if n.startswith("attachments/")]))
        check("包内附件内容与原件一致", zf.read(f"attachments/{zstored}").decode("utf-8") == payload, "内容不符")
        zbundle = json.loads(zf.read("shenshi-backup.json"))
    check("包内 JSON 是自洽备份", zbundle.get("app") == "慎始" and zbundle.get("tasks"), str(zbundle.get("app")))

    # 只含 JSON 的包：用来验证「没带来文件」会被如实报出来，而不是假装恢复成功。
    # 注意：删除任务现在走「可撤销删除」，附件文件会在磁盘上保留十分钟供撤销恢复，
    # 所以不能再靠「删除即消失」来制造缺失；这里直接把存储名换成绝不可能存在的哨兵名。
    for t in zbundle.get("tasks", []):
        for a in (t.get("attachments") or []):
            a["file"] = "__missing_on_purpose__.bin"
    json_only = io.BytesIO()
    with zipfile.ZipFile(json_only, "w", zipfile.ZIP_DEFLATED) as zf:
        zf.writestr("shenshi-backup.json", json.dumps(zbundle, ensure_ascii=False))

    # 删掉原任务：之后恢复出来的附件只可能来自压缩包，不会是磁盘上的残留
    status, _ = call(base, "DELETE", f"/api/tasks/{zid}")
    check("删掉原任务（连带清掉附件记录）", status == 200, str(status))

    status, res = call_multipart_blob(base, "/api/import/file?mode=merge", "json-only.zip", json_only.getvalue())
    check("只有 JSON 的压缩包也能导入", status == 200 and res.get("mode") == "merge", str(res))
    check("没带来文件的附件被记为缺失", (res.get("attachmentsMissed") or 0) >= 1, str(res))
    check("缺失时不会凭空造出附件记录", (res.get("attachments") or 0) == 0, str(res))

    status, res = call_multipart_blob(base, "/api/import/file?mode=merge", "backup.zip", zdata)
    check("上传完整压缩包导入成功", status == 200 and (res.get("tasks") or 0) >= 1, str(res))
    check("导入报告恢复了附件", (res.get("attachments") or 0) >= 1, str(res))
    check("完整备份没有缺失附件", (res.get("attachmentsMissed") or 0) == 0, str(res))

    # 找回恢复出来的那一份，确认文件真能下载
    status, found = call(base, "GET", "/api/tasks?q=备份往返用的任务&status=all")
    rows = (found or {}).get("tasks") or []
    check("恢复出的任务可被检索到", len(rows) >= 1, str(len(rows)))
    restored = None
    for t in rows:
        if t.get("attachments"):
            restored = t
            break
    check("恢复出的任务带着附件记录", restored is not None, str([t.get("id") for t in rows]))
    if restored:
        new_att = restored["attachments"][0]
        status, _, body = call_raw(base, "GET", f"/api/attachments/{new_att['id']}")
        check("恢复出的附件可以下载", status == 200, str(status))
        check("下载的附件内容与原件一致", payload in body, body[:40])
        check("merge 模式给附件换了存储名", new_att.get("file") != zstored, str(new_att.get("file")))

    # 覆盖导入：任务 id 复原，附件沿用原存储名
    status, res = call_multipart_blob(base, "/api/import/file?mode=replace", "backup.zip", zdata)
    check("覆盖导入压缩包成功", status == 200 and res.get("mode") == "replace", str(res))
    check("覆盖导入同样恢复了附件", (res.get("attachments") or 0) >= 1, str(res))
    status, after = call(base, "GET", f"/api/tasks/{zid}")
    check("覆盖导入后任务按原 id 复原", status == 200 and after.get("title") == "备份往返用的任务", str(status))
    back = (after.get("attachments") or [])
    check("覆盖导入后任务带着附件", len(back) == 1, str(back))
    if back:
        status, _, body = call_raw(base, "GET", f"/api/attachments/{back[0]['id']}")
        check("覆盖导入后的附件可以下载", status == 200 and payload in body, str(status))

    # 不是备份的压缩包要被明确拒绝，而不是当成空备份把库清掉
    junk = io.BytesIO()
    with zipfile.ZipFile(junk, "w") as zf:
        zf.writestr("readme.txt", "这不是备份")
    status, res = call_multipart_blob(base, "/api/import/file?mode=merge", "junk.zip", junk.getvalue())
    check("不含 JSON 的压缩包被拒", status == 400, f"{status} {str(res)[:60]}")


def run_new_features(base: str) -> None:
    """补全特性（一/二/三/五）的端到端验证：智能清单新键、置顶/收藏、链接/开始日期、
    克隆、清空已完成、撤销删除、操作历史、保存筛选、归档清单。"""
    section("㉑ 补全特性：智能清单新键 / 置顶收藏 / 克隆 / 清空 / 撤销 / 历史 / 筛选 / 归档")

    # 智能清单角标新增 tomorrow / week / high / starred
    status, boot = call(base, "GET", "/api/bootstrap")
    counts = (boot or {}).get("counts") or {}
    for k in ("tomorrow", "week", "high", "starred"):
        check(f"智能清单角标含「{k}」", k in counts, str(list(counts.keys())))

    # 带新字段建任务
    status, t = call(base, "POST", "/api/tasks", {
        "title": "带链接与开始日期的任务",
        "priority": 3,
        "startDate": "2026-10-01",
        "url": "https://example.com/ref",
        "pinned": True,
        "starred": True,
    })
    check("创建带置顶/收藏/开始日期/链接的任务", status in (200, 201) and t.get("id"), str(status))
    tid = t.get("id")
    check("置顶标记已保存", t.get("pinned") is True, str(t.get("pinned")))
    check("收藏标记已保存", t.get("starred") is True, str(t.get("starred")))
    check("开始日期已保存", t.get("startDate") == "2026-10-01", str(t.get("startDate")))
    check("关联链接已保存", t.get("url") == "https://example.com/ref", str(t.get("url")))

    # 收藏后 starred 角标至少为 1
    status, boot2 = call(base, "GET", "/api/bootstrap")
    check("收藏后 starred 计数 ≥ 1", (boot2 or {}).get("counts", {}).get("starred", 0) >= 1, str((boot2 or {}).get("counts")))

    # 克隆任务
    status, dup = call(base, "POST", f"/api/tasks/{tid}/duplicate")
    check("克隆任务返回新记录", status in (200, 201) and dup.get("id") and dup.get("id") != tid, str(status))
    check("克隆副本标题带「副本」", "副本" in (dup.get("title") or ""), str(dup.get("title")))
    check("克隆副本不继承置顶", dup.get("pinned") is False, str(dup.get("pinned")))
    check("克隆副本状态归零为未完成", dup.get("status") == "todo", str(dup.get("status")))
    dup_id = dup.get("id")

    # 完成再清空已完成
    status, _ = call(base, "POST", f"/api/tasks/{dup_id}/toggle")
    check("克隆副本可完成", status in (200, 201), str(status))
    before_purge = (call(base, "GET", "/api/bootstrap")[1] or {}).get("counts", {}).get("done", 0)
    status, purged = call(base, "POST", "/api/tasks/purge", {})
    check("清空已完成返回受影响件数", status == 200 and (purged.get("affected") or 0) >= 1, str(purged))
    after_purge = (call(base, "GET", "/api/bootstrap")[1] or {}).get("counts", {}).get("done", 0)
    check("清空后已完成数量下降", after_purge < before_purge, f"{before_purge} -> {after_purge}")

    # 保存筛选 CRUD
    status, sf = call(base, "POST", "/api/saved-filters", {
        "name": "高优先级",
        "query": '{"priority":3,"tagIds":[],"tagMode":"any","status":"all","from":null,"to":null,"pinned":false,"starred":false}',
    })
    check("保存筛选可创建", status in (200, 201) and sf.get("id"), str(status))
    sfid = sf.get("id")
    status, sfs = call(base, "GET", "/api/saved-filters")
    sfs_list = (sfs or {}).get("savedFilters") if isinstance(sfs, dict) else sfs
    check("保存的筛选可被列出", status == 200 and isinstance(sfs_list, list) and any(x.get("id") == sfid for x in sfs_list), str(sfs))
    status, _ = call(base, "PATCH", f"/api/saved-filters/{sfid}", {"name": "高优先级（改）"})
    check("保存的筛选可改名", status in (200, 201), str(status))
    status, _ = call(base, "DELETE", f"/api/saved-filters/{sfid}")
    check("保存的筛选可删除", status in (200, 201), str(status))

    # 撤销删除
    status, t2 = call(base, "POST", "/api/tasks", {"title": "待撤销的任务"})
    t2id = t2.get("id")
    status, _ = call(base, "DELETE", f"/api/tasks/{t2id}")
    check("删除任务返回成功", status == 200, str(status))
    status, undo = call(base, "GET", "/api/undo")
    check("撤销槽位标记为可用", (undo or {}).get("available") is True, str(undo))
    check("撤销槽位记录被删任务数", (undo or {}).get("count", 0) >= 1, str(undo))
    status, undo_resp = call(base, "POST", "/api/undo")
    check("撤销删除使任务恢复", status == 200 and (undo_resp or {}).get("ok") is True and (undo_resp or {}).get("restored", 0) >= 1, str(undo_resp))
    status, undo_after = call(base, "GET", "/api/undo")
    check("撤销后槽位已清空", (undo_after or {}).get("available") is False, str(undo_after))
    # Undo 恢复为「新记录」（id 会变），按标题找回新 id 并清理。
    status, found_list = call(base, "GET", "/api/tasks?q=待撤销的任务")
    new_t2id = None
    if isinstance(found_list, dict):
        for tt in (found_list.get("tasks") or []):
            if tt.get("title") == "待撤销的任务":
                new_t2id = tt.get("id")
                break
    check("撤销后任务重新可查", new_t2id is not None, str(new_t2id))
    if new_t2id is not None:
        call(base, "DELETE", f"/api/tasks/{new_t2id}")

    # 操作历史
    status, acts = call(base, "GET", "/api/activities")
    check("操作历史可列出", status == 200 and isinstance((acts or {}).get("activities"), list), str(status))

    # 归档清单：archived 标记翻转且不计入普通列表
    status, lst = call(base, "POST", "/api/lists", {"name": "待归档清单"})
    lid = lst.get("id")
    check("创建清单成功", status in (200, 201) and lid, str(status))
    status, _ = call(base, "PATCH", f"/api/lists/{lid}", {"archived": True})
    check("清单可归档", status in (200, 201), str(status))
    status, bl = call(base, "GET", "/api/bootstrap")
    archived_list = next((x for x in (bl or {}).get("lists", []) if x.get("id") == lid), None)
    check("归档清单 archived 为真", archived_list is not None and archived_list.get("archived") is True, str(archived_list))
    status, _ = call(base, "PATCH", f"/api/lists/{lid}", {"archived": False})
    check("清单可取消归档", status in (200, 201), str(status))


def run_auth(base: str, token: str) -> None:
    """访问口令鉴权：单开一个带 -token 的实例，验证这道门该拦的拦住、该放的放行。"""
    section("⑳ 访问口令鉴权")

    # 首页未登录时应当直接返回登录页，而不是把整个前端加载进来
    status, headers, _ = call_full(base, "GET", "/")
    check("未登录访问首页返回 401", status == 401, str(status))
    check("首页返回登录页而非应用", "text/html" in headers.get("Content-Type", ""), headers.get("Content-Type", ""))
    status, html = call_text(base, "GET", "/")
    check("登录页含口令输入框", "访问口令" in html and "password" in html, html[:80])
    status, _ = call_text(base, "GET", "/assets/whatever.js")
    check("未登录时静态资源同样不放行", status == 401, str(status))

    # 健康检查与鉴权元信息必须免鉴权，否则容器探活与登录页都无法工作
    status, _, health = call_full(base, "GET", "/api/health")
    check("健康检查免鉴权", status == 200 and health.get("app") == "慎始", str(status))
    status, _, st = call_full(base, "GET", "/api/auth/status")
    check("鉴权状态接口免鉴权", status == 200, str(status))
    check("已启用口令时 required 为真", st.get("required") is True and st.get("authenticated") is False, str(st))

    status, _, _ = call_full(base, "GET", "/api/tasks")
    check("未登录访问接口返回 401", status == 401, str(status))
    status, _, _ = call_full(base, "GET", "/api/export")
    check("未登录无法导出数据", status == 401, str(status))
    status, _, _ = call_full(base, "POST", "/api/tasks", {"title": "偷偷写入"})
    check("未登录无法写入", status == 401, str(status))

    status, _, body = call_full(base, "POST", "/api/auth/login", {"token": "wrong-token"})
    check("口令错误返回 401", status == 401, str(status))
    check("口令错误给出可读提示", body.get("error") == "口令不正确", str(body))

    status, headers, body = call_full(base, "POST", "/api/auth/login", {"token": token})
    check("口令正确即可登录", status == 200 and body.get("ok") is True, str(status))
    cookie = headers.get("Set-Cookie", "")
    check("登录下发 HttpOnly Cookie", "shenshi_token=" in cookie and "HttpOnly" in cookie, cookie[:60])
    session = cookie.split(";")[0]

    status, _, _ = call_full(base, "GET", "/api/tasks", headers={"Cookie": session})
    check("带 Cookie 可访问接口", status == 200, str(status))
    status, _, _ = call_full(base, "GET", "/api/tasks", headers={"Authorization": f"Bearer {token}"})
    check("带 Bearer 头可访问接口", status == 200, str(status))
    status, headers, _ = call_full(base, "GET", f"/api/tasks?token={token}")
    check("查询串带口令可访问接口", status == 200, str(status))
    check("查询串通过后回种 Cookie", "shenshi_token=" in headers.get("Set-Cookie", ""), headers.get("Set-Cookie", "")[:60])
    status, _, _ = call_full(base, "GET", "/api/auth/status", headers={"Cookie": session})
    check("已登录时 authenticated 为真", status == 200, str(status))

    # 退出后凭据立即失效
    status, headers, _ = call_full(base, "POST", "/api/auth/logout", headers={"Cookie": session})
    check("退出登录返回成功", status == 200, str(status))
    check("退出时清空 Cookie", "Max-Age=0" in headers.get("Set-Cookie", "") or "Max-Age=-1" in headers.get("Set-Cookie", ""), headers.get("Set-Cookie", "")[:60])

    # 连续错误口令要触发节流 —— 放最后，因为它会把该来源锁上一段时间
    codes = []
    for _ in range(12):
        status, _, _ = call_full(base, "POST", "/api/auth/login", {"token": "definitely-wrong"})
        codes.append(status)
    check("连续错误口令触发节流（429）", 429 in codes, f"状态序列 {codes}")
    check("节流后正确口令同样被拦", codes[-1] == 429, str(codes[-1]))


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--base", default="", help="对已运行的服务做测试，不自动启动实例")
    args = ap.parse_args()

    repo_root = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
    proc = None
    tmp = None
    binary = None
    base = args.base
    if not base:
        print("正在编译并启动临时服务实例…")
        tmp = tempfile.mkdtemp(prefix="shenshi-smoke-")
        binary = build_binary(repo_root, tmp)
        proc, base = spawn_instance(binary, repo_root, tmp)
        if not wait_ready(base):
            print("服务启动失败，输出如下：")
            try:
                with open(getattr(proc, "_shenshi_log_path", ""), encoding="utf-8") as fh:
                    print(fh.read())
            except OSError:
                pass
            return 1

    print(f"测试目标: {base}", flush=True)
    try:
        run(base)
        run_extras(base)
        run_new_features(base)
        if binary and tmp:
            # 鉴权需要独立实例：主实例是不带口令的，用来验证「不设口令时一切照旧」
            auth_proc, auth_base = spawn_instance(binary, repo_root, tmp, "test-token-9f3a2b7c")
            try:
                if wait_ready(auth_base):
                    run_auth(auth_base, "test-token-9f3a2b7c")
                else:
                    check("带口令的实例可启动", False, "未在预期时间内就绪")
            finally:
                shutdown(auth_proc)
    finally:
        if proc:
            shutdown(proc)
        if tmp:
            shutil.rmtree(tmp, ignore_errors=True)

    total = len(PASSED) + len(FAILED)
    print(f"\n{'=' * 56}")
    print(f"通过 {len(PASSED)}/{total}")
    if FAILED:
        print("\n失败项:")
        for f in FAILED:
            print(f"  - {f}")
        return 1
    print("\033[32m全部通过 ✓\033[0m")
    return 0


if __name__ == "__main__":
    sys.exit(main())
