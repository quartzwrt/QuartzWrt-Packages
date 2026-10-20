#!/usr/bin/env python3
"""契约校验：AGGREGATE-API.md 冻结 v1 ⇄ quartz-daemon 产出的 status.json。

用法:
    python3 dev/contract-check.py /tmp/status.json      # 校验本地文件
    cat status.json | python3 dev/contract-check.py -   # 从 stdin

退出码 0 = 全部通过；1 = 存在违例（逐条打印）。
"""
import json
import sys

MAX_BYTES = 32 * 1024
MAX_CLIENTS = 32
MAX_SERIES = 60
MAX_UNHEALTHY = 8
MAX_BANDIX_TOP = 8
MAX_CELLULAR_MODEMS = 4
MAX_CELLULAR_SERIES = 60
MAX_SERVING_CELLS = 4

errors: list[str] = []


def err(msg: str) -> None:
    errors.append(msg)


def is_int(v) -> bool:
    return isinstance(v, int) and not isinstance(v, bool)


def is_num(v) -> bool:
    return isinstance(v, (int, float)) and not isinstance(v, bool)


def check_bytes(name: str, v) -> None:
    if not isinstance(v, dict):
        err(f"{name}: not an object")
        return
    for k in ("total", "used"):
        if not is_int(v.get(k)):
            err(f"{name}.{k}: not int")
    p = v.get("pct")
    if not is_num(p) or not (0 <= p <= 100):
        err(f"{name}.pct: not in 0..100 ({p!r})")


def main() -> int:
    path = sys.argv[1] if len(sys.argv) > 1 else "-"
    raw = sys.stdin.buffer.read() if path == "-" else open(path, "rb").read()

    if len(raw) > MAX_BYTES:
        err(f"file size {len(raw)} > {MAX_BYTES}")
    try:
        s = json.loads(raw)
    except Exception as e:  # noqa: BLE001
        print(f"FAIL: not valid JSON: {e}")
        return 1

    if s.get("v") != 1:
        err(f"v = {s.get('v')!r}, want 1")
    if not is_int(s.get("ts")) or s.get("ts", 0) <= 0:
        err("ts: not positive int")
    iv = s.get("interval")
    if not is_int(iv) or not (1 <= iv <= 300):
        err(f"interval: bad ({iv!r})")
    cm = s.get("collect_ms")
    if cm is not None and not is_int(cm):
        err("collect_ms: not int/null")

    # host
    h = s.get("host")
    if not isinstance(h, dict):
        err("host: missing/not object")
    else:
        for k in ("hostname", "model", "release", "kernel"):
            if not isinstance(h.get(k), str):
                err(f"host.{k}: not string")
        if not is_int(h.get("uptime")):
            err("host.uptime: not int")

    # system
    sy = s.get("system")
    if not isinstance(sy, dict):
        err("system: missing/not object")
    else:
        ld = sy.get("load")
        if not isinstance(ld, list) or len(ld) != 3 or not all(is_num(x) for x in ld):
            err(f"system.load: bad ({ld!r})")
        cp = sy.get("cpu_pct")
        if not is_num(cp) or not (0 <= cp <= 100):
            err(f"system.cpu_pct: bad ({cp!r})")
        for k in ("mem", "swap", "rootfs"):
            check_bytes(f"system.{k}", sy.get(k))
        tc = sy.get("temp_c")
        if tc is not None and not is_num(tc):
            err("system.temp_c: not number/null")

    # net
    n = s.get("net")
    if not isinstance(n, dict):
        err("net: missing/not object")
    else:
        w = n.get("wan")
        if not isinstance(w, dict):
            err("net.wan: not object")
        else:
            if not isinstance(w.get("up"), bool):
                err("net.wan.up: not bool")
            for k in ("ifname", "proto", "ipv4"):
                if not isinstance(w.get(k), str):
                    err(f"net.wan.{k}: not string")
            for k in ("rx_bps", "tx_bps"):
                if not is_int(w.get(k)) or w.get(k, -1) < 0:
                    err(f"net.wan.{k}: not non-negative int")
        ser = n.get("series")
        if ser is not None:
            if not isinstance(ser, dict):
                err("net.series: not object")
            else:
                if ser.get("step") != iv:
                    err(f"net.series.step = {ser.get('step')!r}, want interval {iv!r}")
                for k in ("rx", "tx"):
                    arr = ser.get(k)
                    if not isinstance(arr, list) or len(arr) > MAX_SERIES or not all(is_int(x) for x in arr):
                        err(f"net.series.{k}: bad (len={len(arr) if isinstance(arr, list) else 'n/a'})")

    # docker
    d = s.get("docker")
    if d is not None:
        if not isinstance(d, dict):
            err("docker: not object/null")
        else:
            if not isinstance(d.get("available"), bool):
                err("docker.available: not bool")
            for k in ("running", "total", "images"):
                if not is_int(d.get(k)):
                    err(f"docker.{k}: not int")
            uh = d.get("unhealthy")
            if not isinstance(uh, list) or len(uh) > MAX_UNHEALTHY or not all(isinstance(x, str) for x in uh):
                err(f"docker.unhealthy: bad ({uh!r})")

    # clients
    c = s.get("clients")
    if c is not None:
        if not isinstance(c, dict):
            err("clients: not object/null")
        else:
            items = c.get("items")
            if not is_int(c.get("total")):
                err("clients.total: not int")
            if not isinstance(items, list):
                err("clients.items: not list")
            elif len(items) > MAX_CLIENTS:
                err(f"clients.items: {len(items)} > {MAX_CLIENTS}")
            else:
                prev_seen = None
                for i, it in enumerate(items):
                    if not isinstance(it, dict):
                        err(f"clients.items[{i}]: not object")
                        continue
                    for k in ("mac", "ip", "host", "iface"):
                        if not isinstance(it.get(k), str):
                            err(f"clients.items[{i}].{k}: not string")
                    if not is_int(it.get("last_seen")):
                        err(f"clients.items[{i}].last_seen: not int")
                    elif prev_seen is not None and it["last_seen"] > prev_seen:
                        err(f"clients.items[{i}]: not sorted by last_seen desc")
                    else:
                        prev_seen = it["last_seen"]

    # services（开放映射）
    sv = s.get("services")
    if sv is not None:
        if not isinstance(sv, dict):
            err("services: not object/null")
        else:
            for k, v in sv.items():
                if v is None:
                    continue
                if not isinstance(v, dict) or not isinstance(v.get("running"), bool):
                    err(f"services.{k}: missing bool running")

    # bandix（契约 §3.7 加法；可空）
    bx = s.get("bandix")
    if bx is not None:
        if not isinstance(bx, dict):
            err("bandix: not object/null")
        elif not isinstance(bx.get("available"), bool):
            err("bandix.available: not bool")
        elif bx["available"]:
            for k in ("devices", "online"):
                if not is_int(bx.get(k)) or bx.get(k, -1) < 0:
                    err(f"bandix.{k}: not non-negative int")
            if is_int(bx.get("devices")) and is_int(bx.get("online")) and bx["online"] > bx["devices"]:
                err(f"bandix.online {bx['online']} > devices {bx['devices']}")
            lm = bx.get("limits")
            if lm is not None:
                if not isinstance(lm, dict):
                    err("bandix.limits: not object")
                else:
                    for k in ("default_rx_bps", "default_tx_bps"):
                        if not is_int(lm.get(k)) or lm.get(k, -1) < 0:
                            err(f"bandix.limits.{k}: not non-negative int")
                    if not isinstance(lm.get("whitelist_enabled"), bool):
                        err("bandix.limits.whitelist_enabled: not bool")
                    for k in ("whitelist_count", "schedule_count", "quota_count"):
                        v = lm.get(k)
                        if v is not None and (not is_int(v) or v < 0):
                            err(f"bandix.limits.{k}: not non-negative int/absent")
            w = bx.get("wan")
            if w is not None:
                if not isinstance(w, dict):
                    err("bandix.wan: not object")
                else:
                    for k in ("rx_bps", "tx_bps"):
                        if not is_int(w.get(k)) or w.get(k, -1) < 0:
                            err(f"bandix.wan.{k}: not non-negative int")
            top = bx.get("top")
            if top is not None:
                if not isinstance(top, list):
                    err("bandix.top: not list")
                elif len(top) > MAX_BANDIX_TOP:
                    err(f"bandix.top: {len(top)} > {MAX_BANDIX_TOP}")
                else:
                    prev = None
                    for i, it in enumerate(top):
                        if not isinstance(it, dict):
                            err(f"bandix.top[{i}]: not object")
                            continue
                        for k in ("mac", "host", "ip"):
                            if not isinstance(it.get(k), str):
                                err(f"bandix.top[{i}].{k}: not string")
                        for k in ("rx_b", "tx_b"):
                            if not is_int(it.get(k)) or it.get(k, -1) < 0:
                                err(f"bandix.top[{i}].{k}: not non-negative int")
                        pc = it.get("pct")
                        if not is_num(pc) or not (0 <= pc <= 100):
                            err(f"bandix.top[{i}].pct: not in 0..100 ({pc!r})")
                        for k in ("rx_r", "tx_r"):
                            if not is_int(it.get(k)) or it.get(k, -1) < 0:
                                err(f"bandix.top[{i}].{k}: not non-negative int")
                        if it.get("wl") is not None and not isinstance(it.get("wl"), bool):
                            err(f"bandix.top[{i}].wl: not bool/absent")
                        if it.get("sch") is not None and (not is_int(it.get("sch")) or it["sch"] < 0):
                            err(f"bandix.top[{i}].sch: not non-negative int/absent")
                        if it.get("q") is not None and not isinstance(it.get("q"), bool):
                            err(f"bandix.top[{i}].q: not bool/absent")
                        cur = (it.get("rx_b") or 0) + (it.get("tx_b") or 0)
                        if prev is not None and cur > prev:
                            err(f"bandix.top[{i}]: not sorted by cumulative bytes desc")
                        prev = cur
            cn = bx.get("conn")
            if cn is not None:
                if not isinstance(cn, dict) or not all(is_int(cn.get(k)) and cn.get(k, -1) >= 0 for k in ("total", "tcp", "udp")):
                    err(f"bandix.conn: bad ({cn!r})")
            dn = bx.get("dns")
            if dn is not None:
                if not isinstance(dn, dict) or not is_int(dn.get("queries")) or dn.get("queries", -1) < 0:
                    err(f"bandix.dns: bad ({dn!r})")

    # cellular（契约 §3.9 加法；可空）
    cl = s.get("cellular")
    if cl is not None:
        if not isinstance(cl, dict):
            err("cellular: not object/null")
        elif not isinstance(cl.get("available"), bool):
            err("cellular.available: not bool")
        elif cl["available"]:
            md = cl.get("modems")
            if not isinstance(md, list) or not md:
                err(f"cellular.modems: not non-empty list ({md!r})")
            elif len(md) > MAX_CELLULAR_MODEMS:
                err(f"cellular.modems: {len(md)} > {MAX_CELLULAR_MODEMS}")
            else:
                for i, m in enumerate(md):
                    if not isinstance(m, dict):
                        err(f"cellular.modems[{i}]: not object")
                        continue
                    if not isinstance(m.get("id"), str) or not m["id"]:
                        err(f"cellular.modems[{i}].id: not non-empty string")
                    if not isinstance(m.get("registered"), bool):
                        err(f"cellular.modems[{i}].registered: not bool")
                    sg = m.get("signal")
                    if sg is not None:
                        if not isinstance(sg, dict):
                            err(f"cellular.modems[{i}].signal: not object")
                        else:
                            for k in ("rsrp", "rssi"):
                                v = sg.get(k)
                                if v is not None and not is_int(v):
                                    err(f"cellular.modems[{i}].signal.{k}: not int/absent")
                            for k in ("rsrq", "sinr", "quality_pct"):
                                v = sg.get(k)
                                if v is not None and not is_num(v):
                                    err(f"cellular.modems[{i}].signal.{k}: not number/absent")
                            q = sg.get("quality_pct")
                            if q is not None and not (0 <= q <= 100):
                                err(f"cellular.modems[{i}].signal.quality_pct: not in 0..100 ({q!r})")
                            se = sg.get("series")
                            if se is not None:
                                if not isinstance(se, dict) or not is_int(se.get("step")) or not isinstance(se.get("rsrp"), list):
                                    err(f"cellular.modems[{i}].signal.series: bad shape")
                                elif len(se["rsrp"]) > MAX_CELLULAR_SERIES:
                                    err(f"cellular.modems[{i}].signal.series.rsrp: {len(se['rsrp'])} > {MAX_CELLULAR_SERIES}")
                                elif not all(is_int(x) for x in se["rsrp"]):
                                    err(f"cellular.modems[{i}].signal.series.rsrp: non-int point")
                    cells = m.get("serving_cells")
                    if cells is not None and (not isinstance(cells, list) or len(cells) > MAX_SERVING_CELLS):
                        err(f"cellular.modems[{i}].serving_cells: not list/≤{MAX_SERVING_CELLS}")
                    dl = m.get("dial")
                    if dl is not None:
                        if not isinstance(dl, dict) or not isinstance(dl.get("running"), bool):
                            err(f"cellular.modems[{i}].dial: missing bool running")
                        else:
                            for k in ("rx_bps", "tx_bps"):
                                v = dl.get(k)
                                if v is not None and (not is_int(v) or v < 0):
                                    err(f"cellular.modems[{i}].dial.{k}: not non-negative int/absent")
                    tr = m.get("traffic")
                    if tr is not None:
                        if not isinstance(tr, dict) or not isinstance(tr.get("available"), bool):
                            err(f"cellular.modems[{i}].traffic: missing bool available")
                        else:
                            for k in ("updated_at", "rx_bytes", "tx_bytes"):
                                v = tr.get(k)
                                if v is not None and (not is_int(v) or v < 0):
                                    err(f"cellular.modems[{i}].traffic.{k}: not non-negative int/absent")
                    us = m.get("usb_speed_mbps")
                    if us is not None and (not is_int(us) or us < 0):
                        err(f"cellular.modems[{i}].usb_speed_mbps: not non-negative int/absent")
                    uv = m.get("usb_version")
                    if uv is not None and not isinstance(uv, str):
                        err(f"cellular.modems[{i}].usb_version: not string/absent")

    # errors
    er = s.get("errors")
    if not isinstance(er, list) or not all(isinstance(x, str) for x in er):
        err(f"errors: not array of strings ({er!r})")

    if errors:
        print(f"FAIL: {len(errors)} violation(s) in {path}:")
        for e in errors:
            print(f"  - {e}")
        return 1
    print(f"OK: {path} conforms to AGGREGATE-API v1 ({len(raw)} bytes)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
