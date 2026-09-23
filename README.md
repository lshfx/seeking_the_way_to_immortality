# seeking_the_way_to_immortality

《问道长生》定位为本地终端摸鱼修仙游戏，适合在等待工作程序结果时玩几回合。TASK-01～TASK-08 已完成产品冻结、开发环境/存档基础、创角和修炼成长；当前还没有正式TUI，下一项为TASK-09，因此此仓库目前仍不能作为完整游戏启动。Windows Terminal 全流程实测尚待补齐。

## 开发验证

```powershell
.\scripts\test.ps1                          # Go 测试 + vet + 边界检查 + TASK-02 探针测试（56 项）
.\scripts\build.ps1 -Version 0.0.0-dev
.\scripts\verify-release.ps1                # 隔离目录 + PATH 仅 System32 运行校验
.\scripts\verify-release-account.ps1        # 独立账户验收证据采集
go run ./tools/task08-sim                    # 复现低/中/高资质的首轮修炼节奏模拟
```

构建结果在本地 `dist/wendao.exe`，该目录不提交。现有引擎已实现创角和修炼规则，但程序入口尚未接上可玩的 TUI 与会话恢复。发布给玩家的独立二进制不要求安装 Go；Go 1.26 仅用于开发、测试和构建。

环境要求：Go 1.26（本机 `go1.26.0`，与 `go.mod` 一致）。TASK-02 探针需要 Python 3 标准库。详见[开发环境配置记录](doc/DEVELOPMENT-ENVIRONMENT.md)。

## 项目文档

- [游戏与技术设计文档](doc/问道长生_游戏与技术设计文档.md)
- [分阶段开发方案](doc/问道长生_分阶段开发方案.md)
- [ADR-001 M1产品范围与规则冻结](doc/decisions/ADR-001-m1-product-freeze.md)
- [ADR-002 正式技术栈与Windows便携发行](doc/decisions/ADR-002-technology-and-packaging.md)
- [TASK-02 终端交互验证记录](doc/TASK-02-终端交互验证记录.md)
- [TASK-08 修炼成长与节奏模拟验证记录](doc/TASK-08-修炼成长与节奏模拟验证记录.md)
- [开发环境配置记录](doc/DEVELOPMENT-ENVIRONMENT.md)
