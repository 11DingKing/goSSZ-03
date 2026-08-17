# Bug Reproduction

## 包的性质

当前 test_model_fix 保存的是被测模型修复后的结果源码，不是初始含 Bug 源码。要复现原始缺陷，必须检出下面固定的 parent SHA；不要在当前修复结果源码上期待重新出现修复前失败。生成系统使用的可信验证补丁和完整验证日志仅在本地留存，不提交到结果分支。

## 问题现象

派单上的设备清单会被后面生成的派单改掉，麻烦先帮我们查清原因，暂时不要动代码。

现象：
1. 一分队 team-001 提交计划 A（2026-08-16，要 1 台 GPS），审批后 POST /api/plans/{A}/dispatch，
   返回的派单里 equipment 是 [eq-gps-002]，物资员按这张单去仓库取 eq-gps-002
2. 二分队 team-002 提交计划 B（同一天，也要 1 台 GPS），审批后 POST /api/plans/{B}/dispatch，
   返回的派单里 equipment 是 [eq-gps-001]
3. 这时候回头 GET /api/dispatches/{A 的派单号}，equipment 变成了 [eq-gps-001] —— 也就是计划 B 那台设备

每生成一张新派单，之前所有派单的设备清单都会跟着变成最新那张的内容。
物资员按派单去仓库取设备会取错，完成巡护归还时账也对不上；只有一张派单的时候完全正常，所以一直没被发现。

请先不要修改任何代码，只做定位。我们需要：
- 出问题的具体 Go 文件和具体符号
- 该符号的什么错误行为造成的
- 它为什么会让一张已经生成并保存的派单的设备清单被后一次派单改写（完整因果机制）
- 你自己实际跑出来的证据（执行了什么命令、看到什么输出）
临时复现程序请放在仓库之外的临时目录，不要改动仓库里的文件。

## 含 Bug 版本

- 仓库：11DingKing/goSSZ-03
- 仓库地址：https://github.com/11DingKing/goSSZ-03.git
- parent SHA：347b65bac9d775515c495a4eec2ee0de1edfa5f0

## 复现步骤

```bash
git clone -- https://github.com/11DingKing/goSSZ-03.git bug-repro
cd bug-repro
git checkout --detach 347b65bac9d775515c495a4eec2ee0de1edfa5f0
go test ./internal/service/ -run "^TestEachDispatchOrderKeepsItsOwnEquipmentList$" -count=1 -v
```

## 双架构完整错误信息

### linux/amd64

- 容器内复现预期退出码：1
- 容器内复现实际退出码：1

stdout：

```text
$ go test ./internal/service/ -run "^TestEachDispatchOrderKeepsItsOwnEquipmentList$" -count=1 -v
=== RUN   TestEachDispatchOrderKeepsItsOwnEquipmentList
    dispatch_isolation_test.go:61: dispatch order for plan A now lists [{gps-2 gps_tracker regular}], want a single entry for gps-1
    dispatch_isolation_test.go:65: dispatch result for plan A now reports device gps-2, want gps-1
--- FAIL: TestEachDispatchOrderKeepsItsOwnEquipmentList (0.00s)
FAIL
FAIL	github.com/qilian/patrol-dispatch/internal/service	0.027s
FAIL

```

stderr：

```text
(empty)
```

### linux/arm64

- 容器内复现预期退出码：1
- 容器内复现实际退出码：1

stdout：

```text
$ go test ./internal/service/ -run "^TestEachDispatchOrderKeepsItsOwnEquipmentList$" -count=1 -v
=== RUN   TestEachDispatchOrderKeepsItsOwnEquipmentList
    dispatch_isolation_test.go:61: dispatch order for plan A now lists [{gps-2 gps_tracker regular}], want a single entry for gps-1
    dispatch_isolation_test.go:65: dispatch result for plan A now reports device gps-2, want gps-1
--- FAIL: TestEachDispatchOrderKeepsItsOwnEquipmentList (0.00s)
FAIL
FAIL	github.com/qilian/patrol-dispatch/internal/service	0.001s
FAIL

```

stderr：

```text
(empty)
```

## 通过条件

目标仓库工作区零改动：git status --porcelain 为空，执行前后 tree hash 一致，生产代码、测试与配置均未被修改。
指出具体 Go 文件与具体符号，并说明该符号的错误行为如何导致题面症状，因果机制完整（不能只描述代码现象）。
给出自己实际运行得到的证据（命令与输出），不能只做静态推断。
结论需与 gold_root_cause 的文件、符号和失效机制一致。
允许在仓库之外的临时目录写一次性复现程序；不产生代码修复提交。
