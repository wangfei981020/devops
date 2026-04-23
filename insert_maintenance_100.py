#!/usr/bin/env python3
"""Insert 100 maintenance records, half in Chinese and half in English, to verify bilingual stats."""
import urllib.request
import json
import random
from datetime import datetime, timedelta

API = "http://localhost:30081/api"

def api_post(path, data, token):
    body = json.dumps(data).encode("utf-8")
    req = urllib.request.Request(f"{API}{path}", data=body, method="POST")
    req.add_header("Authorization", f"Bearer {token}")
    req.add_header("Content-Type", "application/json")
    return json.loads(urllib.request.urlopen(req).read())

def api_get(path, token):
    req = urllib.request.Request(f"{API}{path}")
    req.add_header("Authorization", f"Bearer {token}")
    return json.loads(urllib.request.urlopen(req).read())

# Login
login_body = json.dumps({"username": "admin", "password": "ABCabc-123"}).encode("utf-8")
req = urllib.request.Request(f"{API}/login", data=login_body, method="POST")
req.add_header("Content-Type", "application/json")
token = json.loads(urllib.request.urlopen(req).read())["token"]
print(f"login ok, token={token[:20]}...")

# Find the table_maintenance custom table
tables = api_get("/custom-tables", token)
tlist = tables if isinstance(tables, list) else tables.get("tables", [])
maint_table = next((t for t in tlist if "维护" in t.get("name", "") or "maintenance" in t.get("name", "").lower()), None)
if not maint_table:
    maint_table = tlist[0] if tlist else None
table_id = maint_table["id"]
print(f"table: {maint_table['name']} (id={table_id})")

# Label pairs: (zh, en)
OPS = [
    ("维护", "Maintenance"),
    ("取消", "Cancel"),
    ("重算", "Recalculate"),
    ("重派彩", "Re-payout"),
    ("包桌T人", "VIP Table"),
]
DURATIONS = [
    ("两分钟内", "Within 2 min"),
    ("五分钟内", "Within 5 min"),
    ("十分钟内", "Within 10 min"),
    ("十分钟以上", "Over 10 min"),
]
MAINT_TYPES = [
    ("例行维护", "Routine"),
    ("临时维护", "Temporary"),
    ("紧急维护", "Emergency"),
]
QC_STATUS = [
    ("正常", "Normal"),
    ("正常", "Normal"),
    ("正常", "Normal"),
    ("异常", "Abnormal"),
]

PROJECTS = ["G01", "G33"]
SITES = ["PT", "欧洲厅", "卡卡湾"]
SITES_EN = ["PT", "European Hall", "Kakawan"]
TABLES_BY_SITE = {
    "PT": ["P001", "P002", "P003"],
    "欧洲厅": ["E001", "E002", "E003"],
    "卡卡湾": ["C001"],
}
GAME_TYPES = {
    "P001": ["百家乐"], "P002": ["百家乐"], "P003": ["龙虎"],
    "E001": ["龙虎"], "E002": ["轮盘"], "E003": ["百家乐", "骰宝"],
    "C001": ["百家乐"],
}
OPERATORS = ["张三", "李四", "王五", "赵六", "陈七", "周八"]
INSPECTORS = ["李四", "赵六", "周八"]
REASONS_ZH = ["系统更新维护", "网络故障排查", "硬件设备更换", "数据库优化", "客户反馈问题"]
REASONS_EN = ["System update", "Network issue", "Hardware replacement", "DB optimization", "Customer report"]

def make_record(lang):
    """lang: 'zh' or 'en' — pick that language's labels"""
    idx = 0 if lang == "zh" else 1

    days_ago = random.randint(0, 20)
    date = (datetime.now() - timedelta(days=days_ago)).strftime("%Y-%m-%d")
    hour = random.randint(6, 22)
    minute = random.choice([0, 15, 30, 45])
    start_time = f"{date} {hour:02d}:{minute:02d}"

    # 1~3 tables, chosen from any site — cross-site possible to match real world
    site = random.choice(SITES)
    available_tables = TABLES_BY_SITE[site]
    num_tables = random.randint(1, min(3, len(available_tables)))
    selected_tables = random.sample(available_tables, num_tables)
    all_game_types = set()
    for t in selected_tables:
        all_game_types.update(GAME_TYPES.get(t, []))

    # 1~2 projects
    num_projects = random.randint(1, 2)
    selected_projects = random.sample(PROJECTS, num_projects)

    op_pair = random.choice(OPS)
    operation = op_pair[idx]
    is_maintenance = op_pair[0] == "维护"

    start_dur_pair = random.choice(DURATIONS)
    start_duration = start_dur_pair[idx]

    # Maintenance type: if maintenance, pick a real type; else blank or none
    if is_maintenance:
        mt_pair = random.choice(MAINT_TYPES)
        maint_type = mt_pair[idx]
    else:
        maint_type = "无" if lang == "zh" else "None"

    qc_pair = random.choice(QC_STATUS)
    qc_status = qc_pair[idx]

    record = {
        "date": date,
        "start_time": start_time,
        "affected_projects": selected_projects,
        "affected_sites": [site if lang == "zh" else SITES_EN[SITES.index(site)]],
        "affected_tables": selected_tables,
        "game_types": list(all_game_types),
        "maintenance_type": maint_type,
        "start_duration": start_duration,
        "operation": operation,
        "operator": random.choice(OPERATORS),
        "inspector": random.choice(INSPECTORS),
        "qc_status": qc_status,
        "reason": random.choice(REASONS_ZH if lang == "zh" else REASONS_EN),
        "affect_settlement": ("是" if random.random() < 0.2 else "否") if lang == "zh" else ("Yes" if random.random() < 0.2 else "No"),
        "remark": f"test-{lang}-{random.randint(1000,9999)}",
    }

    if is_maintenance:
        close_dur_pair = random.choice(DURATIONS)
        record["close_duration"] = close_dur_pair[idx]
        end_dt = datetime.strptime(start_time, "%Y-%m-%d %H:%M") + timedelta(minutes=random.randint(15, 120))
        record["end_time"] = end_dt.strftime("%Y-%m-%d %H:%M")

    return record

# Insert 50 Chinese + 50 English, interleaved so date distribution is similar
success = 0
fail = 0
for i in range(100):
    lang = "zh" if i % 2 == 0 else "en"
    record = make_record(lang)
    try:
        api_post(f"/custom-tables/{table_id}/rows", {"data": record}, token)
        success += 1
        if (i + 1) % 20 == 0:
            print(f"  [{i+1}/100] ok (last: {lang} {record['operation']} {record['affected_tables']})")
    except Exception as e:
        fail += 1
        print(f"  [{i+1}/100] FAIL ({lang}): {e}")

print(f"\ndone: {success} inserted, {fail} failed")
