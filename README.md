# seeking_the_way_to_immortality

《问道长生》是一款面向碎片时间的本地终端修仙游戏。TASK-01已完成M1范围与规则冻结；TASK-02三项原型修正项已修复并回归、四个终端人工实测通过（含「方向键失效」缺陷修复与真机复测、诊断工具三处缺陷修复），字体=新宋体、代码页=936 已记录，仅 Windows Terminal 全流程待补；TASK-03已建立Go正式工程和Windows x64独立产物，独立干净Windows用户账户验收已完成。游戏功能尚未实现。

## 开发验证

```powershell
.\scripts\test.ps1                          # Go 测试 + vet + 边界检查 + TASK-02 探针测试（56 项）
.\scripts\build.ps1 -Version 0.0.0-dev
.\scripts\verify-release.ps1                # 隔离目录 + PATH 仅 System32 运行校验
.\scripts\verify-release-account.ps1        # 独立账户验收证据采集
```

构建结果在本地 `dist/wendao.exe`，该目录不提交。当前程序只提供工程诊断入口，不是可玩的M1版本。

环境要求：Go 1.26（本机 `go1.26.0`，与 `go.mod` 一致）。TASK-02 探针需要 Python 3 标准库。详见[开发环境配置记录](doc/DEVELOPMENT-ENVIRONMENT.md)。

## 项目文档

- [游戏与技术设计文档](doc/问道长生_游戏与技术设计文档.md)
- [分阶段开发方案](doc/问道长生_分阶段开发方案.md)
- [ADR-001 M1产品范围与规则冻结](doc/decisions/ADR-001-m1-product-freeze.md)
- [ADR-002 正式技术栈与Windows便携发行](doc/decisions/ADR-002-technology-and-packaging.md)
- [TASK-02 终端交互验证记录](doc/TASK-02-终端交互验证记录.md)
- [开发环境配置记录](doc/DEVELOPMENT-ENVIRONMENT.md)
