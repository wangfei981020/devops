#!/usr/bin/env python3
"""检查后台循环是否都过了 leader 闸。

# 为什么需要这个

多副本下，每个 `go StartXxxScheduler(...)` 都会在**每个 Pod 上各跑一份**。
不加约束的话：

  - 只读同步：N 倍的云 API 调用，撞限流
  - 非幂等写（域名续费）：**扣 N 次钱**

而症状极其滞后 —— 单副本时一切正常，扩容后才出问题，
且账单要到月底才看得出来。等发现时钱已经花了。

新写一个后台循环时忘了加闸，是一个非常自然的疏忽：
本地跑单副本，怎么测都对。所以让 CI 来记这件事。

# 规则

main.go 里所有 `go xxxScheduler(...)` / `go Start...(...)` 形式的调用，
参数里必须出现 leaderGate 或 leader —— 或者在调用上一行写
`//nolint:leadergate 理由` 显式豁免。

豁免必须写理由：一个不解释为什么的豁免，下次读的人只能猜。
"""
import re
import sys

TARGET = 'main.go'
CALL = re.compile(r'^\s*go\s+([\w.]*(?:Scheduler|Start\w*))\s*\(', re.M)
EXEMPT = re.compile(r'//\s*nolint:leadergate\s+(\S.*)')

def check_cancellable_run(src):
    """长驻循环不能用 context.Background() —— 那样退出分支永远走不到。

    踩过：leader 的 Run 收到 ctx 取消才会主动释放租约，
    但接线时传了 context.Background()（永不取消），
    于是"优雅让位"这段代码写了、测了、从不执行。
    实测表现是滚动更新时新副本要多等一个 TTL 才当选。

    这类 bug 单测抓不到（测的是 Run 本身，传的是可取消 ctx），
    只有看真实滚动更新的时间线才会露出来 —— 所以放到静态检查里。
    """
    bad = []
    for m in re.finditer(r'go\s+(?:func\s*\(\s*\)\s*\{\s*)?(\w+)\.Run\(context\.Background\(\)\)', src):
        line = src[:m.start()].count('\n') + 1
        bad.append(f'{TARGET}:{line}: {m.group(1)}.Run 用了 context.Background() —— '
                   f'该 context 永不取消，退出/释放分支不会执行')
    return bad


def main():
    src = open(TARGET, encoding='utf-8').read()
    lines = src.split('\n')
    bad = check_cancellable_run(src)
    for m in CALL.finditer(src):
        lineno = src[:m.start()].count('\n')
        # 收集这次调用的完整实参（可能跨行）
        depth, i, args = 0, m.end() - 1, []
        while i < len(src):
            if src[i] == '(':
                depth += 1
            elif src[i] == ')':
                depth -= 1
                if depth == 0:
                    break
            args.append(src[i])
            i += 1
        argstr = ''.join(args)
        if 'leaderGate' in argstr or 'leader' in argstr:
            continue
        prev = lines[lineno - 1] if lineno > 0 else ''
        ex = EXEMPT.search(prev)
        if ex:
            print(f'   ⓘ {TARGET}:{lineno+1} {m.group(1)} 已豁免：{ex.group(1).strip()}')
            continue
        bad.append(f'{TARGET}:{lineno+1}: {m.group(1)} 没有过 leader 闸 —— '
                   f'多副本下会在每个 Pod 各跑一份')
    if bad:
        print('❌ 下列后台循环缺少 leader 约束：')
        for b in bad:
            print('   ' + b)
        print('   修法：传入 leaderGate(leader)，或在上一行写 //nolint:leadergate <理由>')
        return 1
    print('✅ 后台循环均已过 leader 闸')
    return 0

sys.exit(main())
