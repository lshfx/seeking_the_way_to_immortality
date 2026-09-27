# seeking_the_way_to_immortality

《问道长生》是本地终端摸鱼修仙游戏，适合在等待工作程序结果时玩几回合。现在可创角、修炼、在四个地点移动、接取并执行委托、领取散修收入、完成入宗试行、执行宗门巡逻、前往坊市买卖、打开背包使用丹药，并随时自动保存、退出和恢复。月末事件调度、场景门控、具名 NPC、伤势与因果等引擎能力已接通。战斗与突破等完整 M1 玩法仍由后续任务开发。Windows Terminal 等目标终端的正式程序人工兼容矩阵尚待补测。

Windows 上直接运行已构建的本地程序，不要求安装 Go：

```powershell
.\dist\wendao.exe
```

首次启动按 `1`、`2` 或 `3` 选开局预设，再按 `c` 开始。每个预设都有现成姓名；确认页按 `n` 可选其他姓名，按 `g` 可选男、女或不指定。想自定义姓名时，只需一次输入完整姓名；道号、外观、年龄等放在可选的 `e` 详细设定中。每个按键都要按回车，创角随时可退出并恢复草稿。

从源码开发则需要 Go 1.26：

```powershell
go run ./cmd/wendao
```

## 开发验证

```powershell
.\scripts\test.ps1                          # Go 测试 + vet + 边界检查 + TASK-02 探针测试（56 项）
.\scripts\build.ps1 -Version 0.0.0-dev
.\scripts\verify-release.ps1                # 隔离目录 + PATH 仅 System32 运行校验
.\scripts\verify-release-account.ps1        # 独立账户验收证据采集
go run ./tools/task08-sim                    # 复现低/中/高资质的首轮修炼节奏模拟
```

构建结果在本地 `dist/wendao.exe`，该目录不提交。首次启动需在交互式终端完成创角；后续启动自动恢复本地进度。当前每次普通修炼结算一个游戏月并立即保存。发布给玩家的独立二进制不要求安装 Go；Go 1.26 仅用于开发、测试和构建。

游玩时从洞府按 `2` 前往坊市，在主界面按 `4` 查看委托、在坊市按 `3` 买卖；委托详情中用 `a` 接取、`x` 执行并确认、`c` 领取奖励。`i` 打开背包，再按 `u` 使用丹药。移动、接取、领取、买卖和使用丹药不推进游戏月份；执行委托推进一个游戏月。

环境要求：Go 1.26（本机 `go1.26.0`，与 `go.mod` 一致）。TASK-02 探针需要 Python 3 标准库。详见[开发环境配置记录](doc/DEVELOPMENT-ENVIRONMENT.md)。

## 项目文档

- [游戏与技术设计文档](doc/问道长生_游戏与技术设计文档.md)
- [分阶段开发方案](doc/问道长生_分阶段开发方案.md)
- [ADR-001 M1产品范围与规则冻结](doc/decisions/ADR-001-m1-product-freeze.md)
- [ADR-002 正式技术栈与Windows便携发行](doc/decisions/ADR-002-technology-and-packaging.md)
- [TASK-02 终端交互验证记录](doc/TASK-02-终端交互验证记录.md)
- [TASK-08 修炼成长与节奏模拟验证记录](doc/TASK-08-修炼成长与节奏模拟验证记录.md)
- [TASK-09 紧凑TUI与短循环验证记录](doc/TASK-09-紧凑TUI与短循环验证记录.md)
- [TASK-10 事件调度与白名单条件验证记录](doc/TASK-10-事件调度与白名单条件验证记录.md)
- [TASK-11 M1场景开局与主线切片验证记录](doc/TASK-11-M1场景开局与主线切片验证记录.md)
- [TASK-12 伤势寿元疗伤与因果验证记录](doc/TASK-12-伤势寿元疗伤与因果验证记录.md)
- [TASK-13 背包与基础坊市验证记录](doc/TASK-13-背包与基础坊市验证记录.md)
- [TASK-14 宗门任务与散修收入验证记录](doc/TASK-14-宗门任务与散修收入验证记录.md)
- [开发环境配置记录](doc/DEVELOPMENT-ENVIRONMENT.md)
