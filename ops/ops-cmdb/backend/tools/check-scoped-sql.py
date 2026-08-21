#!/usr/bin/env python3
"""检查 Scoped 调用里的 SQL 是否带租户条件。

# 为什么需要这个

store.Scoped 会在**运行时**拒绝没有 `tenant_id = ?` 的语句 —— 这是最后一道
防线，但它的代价是：漏了的话编译、单测全绿，直到有人在线上点开那个页面
才 500。而且报错信息落在服务端日志里，用户看到的只是"加载失败"。

所以在这里把同一条规则前移到静态检查：能编译就能跑，不用等线上。

# 规则

sc.Query / sc.QueryRow / sc.Exec 的 SQL 里必须出现 tenant_id（Scoped 会
自动前置租户参数）；sc.Insert 的列名里必须出现 tenant_id。

误报处理：确有跨租户必要的，走 Platform()，不要在这里开豁免 ——
豁免名单是会长大的，长大之后就没人看了。
"""
import os
import re
import sys

CALL = re.compile(r'\bsc\.(Query|QueryRow|Exec|Insert)\s*\(')

def _one_literal(src, i):
    """从 i 处读一个字符串字面量，返回 (内容, 结束位置)；不是字面量则 (None, i)。"""
    if i < len(src) and src[i] == '`':
        j = src.find('`', i + 1)
        if j > 0:
            return src[i + 1:j], j + 1
        return None, i
    if i < len(src) and src[i] == '"':
        j = i + 1
        while j < len(src):
            if src[j] == '\\':
                j += 2
                continue
            if src[j] == '"':
                return src[i + 1:j], j + 1
            j += 1
    return None, i

def sql_literal_after(src, pos):
    """取调用括号后的 SQL。

    ⚠️ 必须处理**拼接**：`"DELETE FROM " + t + " WHERE tenant_id = ?"`
    这种写法里，只读第一段字面量会得到 "DELETE FROM "，然后误报"缺租户条件"。
    第一版就是这么写的，当场制造了一个假阳性 —— 而假阳性会训练人忽略这个检查，
    比漏报还危险。所以把 + 连起来的字面量全部收集，中间的变量当通配跳过。
    """
    i = pos
    parts = []
    while True:
        while i < len(src) and src[i] in ' \t\n':
            i += 1
        lit, ni = _one_literal(src, i)
        if lit is not None:
            parts.append(lit)
            i = ni
        else:
            # 变量：跳过一个标识符（含 . 和 ()）继续看有没有 +
            m = re.match(r'[A-Za-z_][\w.]*(\([^()]*\))?', src[i:])
            if not m:
                break
            i += m.end()
        while i < len(src) and src[i] in ' \t\n':
            i += 1
        if i < len(src) and src[i] == '+':
            i += 1
            continue
        break
    return ' '.join(parts) if parts else None

def main():
    bad = []
    for root, dirs, files in os.walk('.'):
        dirs[:] = [d for d in dirs if d not in ('.git', 'vendor', 'node_modules')]
        for fn in files:
            if not fn.endswith('.go') or fn.endswith('_test.go'):
                continue
            p = os.path.join(root, fn)
            src = open(p, encoding='utf-8').read()
            for m in CALL.finditer(src):
                sql = sql_literal_after(src, m.end())
                if sql is None:
                    continue
                if 'tenant_id' in sql.lower():
                    continue
                line = src[:m.start()].count('\n') + 1
                first = ' '.join(sql.split())[:70]
                bad.append(f'{p}:{line}: sc.{m.group(1)} 缺租户条件 → {first}')
    if bad:
        print('❌ 下列 Scoped 调用的 SQL 没有租户条件（运行时会被 store 层拒绝）：')
        for b in sorted(bad):
            print('   ' + b)
        return 1
    print('✅ Scoped 调用的 SQL 均带租户条件')
    return 0

sys.exit(main())
