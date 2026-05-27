#!/usr/bin/env python3
"""通过 API Key 插入 10 条桌台维护记录（带通知截图）

用途：验证 API Key 行级权限改造效果——这些记录的 source_api_key_id 会指向本 key。
"""
import urllib.request
import json
import random
import struct
import zlib
from datetime import datetime, timedelta

API = "http://localhost:30081/api"
API_KEY = "opsk_tZoSjDj9pWuKTWcWzt7uZYbjByiYeM6s"
TABLE_ID = "09ccbe4d-fcce-44f2-b689-725894111e80"

HEADERS = {"X-API-Key": API_KEY}


def api_post(path, data, content_type="application/json"):
    body = json.dumps(data).encode("utf-8") if isinstance(data, dict) else data
    req = urllib.request.Request(f"{API}{path}", data=body, method="POST")
    req.add_header("X-API-Key", API_KEY)
    req.add_header("Content-Type", content_type)
    resp = urllib.request.urlopen(req)
    return json.loads(resp.read().decode("utf-8"))


def make_png(w, h, color):
    r, g, b = color
    raw = b""
    for _ in range(h):
        raw += b"\x00"
        for _ in range(w):
            raw += bytes([r, g, b])

    def chunk(ctype, data):
        c = ctype + data
        return struct.pack(">I", len(data)) + c + struct.pack(">I", zlib.crc32(c) & 0xffffffff)

    sig = b"\x89PNG\r\n\x1a\n"
    ihdr = struct.pack(">IIBBBBB", w, h, 8, 2, 0, 0, 0)
    return sig + chunk(b"IHDR", ihdr) + chunk(b"IDAT", zlib.compress(raw)) + chunk(b"IEND", b"")


def upload_screenshot(filename, content):
    boundary = "----WebKitFormBoundary7MA4YWxkTrZu0gW"
    body = (
        f"--{boundary}\r\n"
        f'Content-Disposition: form-data; name="file"; filename="{filename}"\r\n'
        f"Content-Type: image/png\r\n\r\n"
    ).encode("utf-8") + content + f"\r\n--{boundary}--\r\n".encode("utf-8")

    req = urllib.request.Request(f"{API}/storage/upload", data=body, method="POST")
    req.add_header("X-API-Key", API_KEY)
    req.add_header("Content-Type", f"multipart/form-data; boundary={boundary}")
    resp = urllib.request.urlopen(req)
    result = json.loads(resp.read().decode("utf-8"))
    return {
        "path": result.get("path") or result.get("url") or result.get("file_url", ""),
        "name": filename,
    }


# 数据模板
projects = ["G01"]
sites_tables = {
    "PT": ["P001", "P002", "P003"],
    "欧洲厅": ["E001", "E002", "E003"],
    "卡卡湾": ["C001"],
}
game_types_map = {
    "P001": ["百家乐"], "P002": ["百家乐"], "P003": ["龙虎"],
    "E001": ["龙虎"], "E002": ["轮盘"], "E003": ["百家乐", "骰宝"],
    "C001": ["百家乐"],
}
maint_types = ["紧急维护", "临时维护", "例行维护"]
durations = ["两分钟内", "五分钟内", "十分钟内", "十分钟以上"]
operations = ["维护", "维护", "维护", "取消", "重算", "漏操作"]
operators = ["张三", "李四", "王五", "赵六"]
inspectors = ["陈七", "周八"]
qc_statuses = ["正常", "正常", "正常", "异常"]
reasons = ["系统更新维护", "网络故障排查", "硬件设备更换", "软件版本升级", "数据库优化"]
remarks = ["已完成，运行正常", "恢复正常", "需要持续观察", "问题已修复"]
colors = [(41, 128, 185), (39, 174, 96), (192, 57, 43), (243, 156, 18), (22, 160, 133)]

print(f"通过 API Key {API_KEY[:13]}***{API_KEY[-6:]} 插入 10 条记录到 table {TABLE_ID}")
print("=" * 60)

success = 0
for i in range(10):
    days_ago = random.randint(0, 9)
    date = (datetime.now() - timedelta(days=days_ago)).strftime("%Y-%m-%d")
    hour = random.randint(6, 22)
    minute = random.choice([0, 15, 30, 45])
    start_time = f"{date} {hour:02d}:{minute:02d}"
    end_dt = datetime.strptime(start_time, "%Y-%m-%d %H:%M") + timedelta(minutes=random.randint(15, 90))
    end_time = end_dt.strftime("%Y-%m-%d %H:%M")

    site = random.choice(list(sites_tables.keys()))
    available = sites_tables[site]
    selected_tables = random.sample(available, random.randint(1, min(3, len(available))))
    all_gt = set()
    for t in selected_tables:
        all_gt.update(game_types_map.get(t, []))

    operation = random.choice(operations)
    is_maint = operation == "维护"

    # 上传截图
    color = random.choice(colors)
    start_shot = upload_screenshot(f"apikey_start_{i+1}_{date}.png", make_png(200, 100, color))
    end_shot = None
    if is_maint:
        end_shot = upload_screenshot(f"apikey_end_{i+1}_{date}.png", make_png(200, 100, color))

    data = {
        "date": date,
        "start_time": start_time,
        "affected_projects": projects,
        "affected_sites": [site],
        "affected_tables": selected_tables,
        "game_types": list(all_gt),
        "maintenance_type": random.choice(maint_types),
        "start_duration": random.choice(durations),
        "operation": operation,
        "operator": random.choice(operators),
        "inspector": random.choice(inspectors),
        "qc_status": random.choice(qc_statuses),
        "reason": random.choice(reasons),
        "affect_settlement": random.choice(["是", "否", "否"]),
        "remark": random.choice(remarks),
        "notify_start_screenshot": [start_shot],
    }
    if is_maint:
        data["end_time"] = end_time
        data["close_duration"] = random.choice(durations)
        data["notify_end_screenshot"] = [end_shot]

    try:
        res = api_post(f"/custom-tables/{TABLE_ID}/rows", {"data": data})
        print(f"  [{i+1:>2}/10] OK  id={res.get('id','?')[:8]}... {date} {site} {selected_tables} {operation}")
        success += 1
    except Exception as e:
        body = ""
        if hasattr(e, "read"):
            body = e.read().decode("utf-8", errors="ignore")
        print(f"  [{i+1:>2}/10] FAIL: {e}  body={body}")

print("=" * 60)
print(f"完成: {success}/10 成功")
