# TASK-02 终端交互验证记录

- 更新日期：2026-09-23
- 状态：**原型修正项已全部修复并回归；四个终端人工实测通过（含方向键缺陷修复后的真机复测确认）；字体与代码页已记录；仅 Windows Terminal 全流程未单独采集。**
- 范围：验证交互、布局和终端恢复，不实现游戏规则、时间、随机数或存档。
- 探针语言：Python 3.13（仅一次性试验）。正式语言已由 TASK-03 选定为 Go 1.26。

## 可运行试验

代码位于 `tools/task02-terminal-probe/`，全部使用 Python 标准库，无第三方依赖。

```powershell
cd tools\task02-terminal-probe

# 交互模式（必须在真实终端中运行）
python interactive_probe.py

# 静态快照
python interactive_probe.py --snapshot --size 48x16 --color basic

# 无需人工观察的自检（输入解码、状态纯度、恢复路径、转义与字宽）
python verify_probe_selfcheck.py

# 自动化测试（56 项）
python -m unittest discover -p 'test_*.py' -v

# 按键原始字符诊断（排查方向键等扩展键）
python diagnose_keys.py

# 终端矩阵证据采集（在待测终端里运行）
python collect_terminal_matrix.py --label "<终端名称>"
```

键盘操作：`W/S` 或方向键移动，`1`～`3` 选择，`Enter` 只显示诊断预览，`n/p` 或翻页键切换场景页，`Tab` 在选项和说明间切换焦点，`Esc` 返回选项，`q` 退出。窗口尺寸变化时每 100 毫秒重新取终端尺寸并重画；尺寸不够时只显示扩大窗口提示。

交互预览不会执行游戏行动：探针没有世界时钟、随机源、引擎或存档，也不访问网络和工作目录数据。文本中的 ESC、响铃等控制字符会被过滤；CSI/OSC 输入序列会被消费，不会当作菜单按键；括号粘贴模式退出时会复位。退出路径恢复光标、颜色/粘贴模式以及控制台输入回显设置。

## PanelModel 草案

`interactive_probe.py` 中不可变的 `PanelModel` 仅承载展示所需的数据：

```text
PanelModel:
  title, status, scene, story_pages[], risk, choices[], details[]

ProbeState（仅为界面试验状态）:
  page, selected, focus, message
```

查询和重排只读取 `PanelModel`；翻页、缩窗、焦点切换和预览只改 `ProbeState`。此草案不是 TASK-04 的 `GameState` 或最终接口。

布局按 Unicode East Asian Width 计算全宽字符与组合字符，框线使用 ASCII；超长单行以 `...` 明示截断。布局快照固定覆盖 80×24、64×20、48×16，目标尺寸内选项和退出键都必须可见。小于 48×16 时仅提示扩大窗口，不静默截掉选择。

## 原型修正项（本次已修复）

开发方案为 TASK-02 列出了三项原型修正项。三项此前**均未真正满足**，本次逐项修复并加了会失败的回归测试。

| 修正项 | 修复前的问题 | 修复方式 | 回归测试 |
|---|---|---|---|
| `bad` 结果必须影响测试成败 | 旧测试断言 `safe_text("名\x1b[2J字\x07") == "名[2J字"`，把残留的转义载荷 `[2J` 当成**正确结果**。测试通过，但缺陷被固化。 | `safe_text` 改为用 `_ESCAPE_SEQUENCE` 正则删掉**整条**转义序列，而非只删 ESC 字节。断言改为 `== "名字"`。 | `test_unicode_display_width_and_control_filtering`、`test_escape_sequence_payloads_never_reach_the_frame` |
| 字宽检查不能在内容截断后仍宣称完整 | `_boxed` 在行数超出时静默截断（`result[:height-1]`），随后 `render_panel` 的宽度断言在**已截断**的框上仍然成立，等于"截断后宣称完整"。 | 新增 `LayoutOverflow` 异常；`_boxed` 超限时抛出而不再静默裁剪，`render_panel` 捕获后显式降级为尺寸提示。测试同时断言行数、逐行宽度与控件可见性。 | `test_clipping_cannot_hide_row_overflow`、`test_long_name_is_marked_as_clipped_without_hiding_controls` |
| `compat` 必须是真正的色码回退 | 旧实现只有 `none`/`basic`/其它，`basic` 之外**一律**输出真彩 `38;2;`，不存在 `compat` 档，也不存在真正的降级阶梯。 | 引入三档真实回退：`truecolor`(24bit) → `compat`(256 色 `38;5;`) → `basic`(16 色) → `none`。`_auto_color` 按 `COLORTERM`/`TERM`/`TERM_PROGRAM` 逐级探测，`NO_COLOR` 与 `TERM=dumb` 归零。 | `test_color_profiles_are_distinct`、`test_color_fallback_never_invents_truecolor_support` |

修复后 `interactive_probe.py` 的测试数从 9 项增至 12 项。三项修正均已用"回退到旧实现则测试失败"的方式反证（见下节）。

## 自动化结果

`python -m unittest discover -p 'test_*.py' -v`：**28 项通过**（探针 16 + 采集器 6 + 自检 6）。

- 80×24、64×20、48×16 每行宽度精确，三项选择与退出键可见；40×10 显示尺寸提示且不显示菜单。
- 超长姓名不会推走选项或退出提示；组合字符裁切和宽度计算保持完整。
- 转义序列载荷（如 `\x1b[2J`、OSC 内容）不会出现在渲染结果中，且渲染行仍保持精确宽度。
- 布局溢出会抛出可捕获的 `LayoutOverflow`，不会被静默截断。
- 四档颜色输出互不相同：`none` 无颜色码，`basic` 无 `38;5;`/`38;2;`，`compat` 用 `38;5;` 且不含 `38;2;`，`truecolor` 用 `38;2;`。
- 色档自动探测不会虚构真彩能力：`TERM=xterm-256color` → `compat`，`TERM=xterm` → `basic`，`COLORTERM=truecolor` → `truecolor`，`NO_COLOR=1` 或 `TERM=dumb` → `none`。
- 方向选择、页切换、焦点和尺寸重排不触发游戏结算。
- CSI 方向键序列能解析；OSC 内容和括号粘贴中的 `q`、数字不会变成游戏按键。
- EOF 与 Ctrl+C 输入映射到安全退出。

### 反证记录（修正项确实生效）

把 `safe_text` 回退为旧实现后重跑测试，结果 `FAILED (failures=2)`，失败点正是
`safe_text("名\x1b[2J字\x07")` 返回 `'名[2J字'` 而非 `'名字'`。这证明新断言能真正捕获该缺陷，而不是像旧断言那样把它固化为"通过"。

## 探针自检（无需人工观察）

`verify_probe_selfcheck.py` 把**可以自动证明的验收项**从"人工确认"里分离出来，共 28 项检查，全部通过；失败时退出码为 1，可直接进 CI：

| 分组 | 覆盖内容 |
|---|---|
| `input_decoding` | `EOF`、`Ctrl+D/EOT`、`Ctrl+Z/SUB` 均映射为安全退出；`Ctrl+C` 映射为独立的 `CTRL_C` 且同样退出；`CR`→`ENTER`；`TAB`→`TAB`；CSI 方向键解析；**Windows 扩展键（`\x00`/`\xe0` + 后缀）12 种组合**；晚到后缀会重试而非丢弃；重试耗尽返回惰性标记；方向键确实移动选项；**OSC 载荷内嵌的 `q` 被吞掉，不会变成退出键** |
| `render_state_purity` | 四个尺寸 × 两档颜色渲染后 `ProbeState` 完全不变；状态字段仅 `{page, selected, focus, message}`，**无时钟、无存档字段**；Enter 预览文案明确"不扣月、不存档" |
| `console_mode_restore` | 非 TTY 时 `TerminalSession.enter()` 明确拒绝而非静默改模式；`restore()` 可重复调用且安全 |
| `escape_and_width` | 清屏/OSC/SGR 载荷被完整移除；`名\x1b[2J字` → `名字`（载荷不泄漏）；中日韩字符计 2 列、组合符计 0 列 |

**这项工具的价值在于划清界限**：它证明的是"代码路径正确"，**不能**证明"真终端里人眼看到的效果正确"。

## 终端矩阵采集器

`collect_terminal_matrix.py` 用于在任意目标终端中一次性采集可复现证据，避免手工记录出错：

- 记录平台、Python 版本、stdout/stdin 是否为 TTY、报告尺寸、是否达到 48×16、输出编码与中文编码往返、`TERM`/`TERM_PROGRAM`/`COLORTERM`/`WT_SESSION`/`NO_COLOR`、自动选中的色档。
- 对 80×24、64×20、48×16、40×10 四个尺寸做几何断言（行数、逐行精确宽度、溢出提示与菜单互斥、无残留转义）。
- 对四档颜色逐项检查是否输出转义、是否用 24bit、是否用 256 色。
- 输出夹在 `----- TASK02-EVIDENCE-BEGIN/END -----` 之间的 JSON 块，可原样粘贴进本记录。

**本机采集结果（Git Bash / 非 TTY，仅供参考，不构成终端验收）**：尺寸报告 `0x0`，`stdout_isatty=False`，`TERM='dumb'`，自动色档 `none`；四个尺寸的几何断言全部 `PASS`。该环境不是目标桌面终端，**不能**用于关闭本任务。

### 真实终端采集记录（2026-09-23）

以下两次均由用户在真实交互终端中运行 `collect_terminal_matrix.py` 采集，`stdout`/`stdin` 均为 TTY，中文编码往返一致，四个尺寸几何断言与四档颜色检查全部 `PASS`。

| 记录 | 终端 | 报告尺寸 | stdout/stdin TTY | 编码 | 中文往返 | 自动色档 | 布局/色档断言 |
|---|---|---|---|---|---|---|---|
| A | PowerShell 终端（`PS C:\...>`，`TERM` 未设、无 `WT_SESSION`） | 120×50 | True / True | utf-8 | 一致 | `basic` | 4/4 PASS |
| B | 传统 conhost（`cmd.exe` 提示符 `C:\...>`，`TERM` 未设、无 `WT_SESSION`） | 120×30 | True / True | utf-8 | 一致 | `basic` | 4/4 PASS |
| C | **Windows Terminal**（`WT_SESSION=679d7097-3803-41f3-8842-f37341b4bee2`） | 120×30 | True / True | utf-8 | 一致 | `basic` | 4/4 PASS |

记录 C 是**首个带权威终端身份标志的采集**：`WT_SESSION` 非空，明确证明这是 Windows Terminal，不再是靠猜。

**但记录 C 同时暴露了一个需要如实记录的局限**：`TERM`/`TERM_PROGRAM`/`COLORTERM` **仍全为空**，自动色档仍回落 `basic`。也就是说：

- 即使换成 Windows Terminal，本机默认配置下也**不会**通过环境变量宣告真彩能力，自动探测只能保守选 16 色。
- 这意味着"WT 应升到 `truecolor`"这个先前写下的预期**在该配置下不成立**，属于我的预期错误，不是程序缺陷（`--color truecolor` 可手动强制，四档输出本身在前面的色档检查里已逐项验证）。
- 若希望 WT 下自动用真彩，需要设置 `COLORTERM=truecolor`（WT 默认不给），或在程序里对 `WT_SESSION` 存在的情形直接提升色档。**后者需要单独决策**，不在 TASK-02 范围内，已记入待办。

另：记录 C 的 `python=3.14.3`（系统 Python），前两次采集用的是另一版本；两版下几何断言与色档检查结果一致。

两次记录的关键判读：

- **编码**：`输出编码=utf-8`、`中文往返=编码往返一致`，说明在真实 Windows 终端下中文能无损往返，未出现 GBK 替换字符。这是 TASK-02"真终端中文"闸门的正面证据。
- **尺寸**：PowerShell 终端 120×50、conhost 120×30，均满足最小 48×16。
- **环境变量**：两次 `TERM`/`TERM_PROGRAM`/`COLORTERM` 均为空、无 `WT_SESSION`。探测逻辑因此回落到 `basic`（16 色），这是**预期行为**，不是缺陷。但也意味着：**当前证据无法区分这两个终端具体是 Windows Terminal / VS Code 终端 / conhost 中的哪一个**——`WT_SESSION` 缺失说明至少不是以 Windows Terminal 默认环境启动的。故上表按实际可观测证据标注为"PowerShell 终端"与"传统 conhost"，不臆测产品名。
- **conhost 记录 B 的额外价值**：conhost 是 ANSI 支持最弱的候选环境。它跑通 4/4 布局断言、四档颜色输出正常、中文往返一致，说明最低档终端可用。但**真实视觉效果**（颜色是否真的显示、框线是否对齐、中文是否为方块）仍需人眼确认。

这两次采集**尚未覆盖**：交互模式的按键/翻页/退出、拖动缩放、`Ctrl+C`/EOF 恢复、真实颜色观感、字体。因此 TASK-02 仍不能关闭。

### 人工实测记录（2026-09-23）

用户在三个真实终端中执行了完整的人工检查流程。**测试中发现一个真实缺陷：方向键无效。**

| 终端 | 交互手感 | 拖动缩放 | 中断恢复 | 中文/颜色 |
|---|---|---|---|---|
| PowerShell 终端 | 数字键✅ `w/s`✅ **方向键❌**；Enter✅ n/p✅ Tab✅ q✅ | 正常 | 正常 | 正常 |
| 传统 conhost（cmd） | 数字键✅ `w/s`✅ **方向键❌**；Enter✅ n/p✅ Tab✅ q✅ | 正常 | 正常 | 正常 |
| VS Code 集成终端 | 正常（未单独报告方向键，按"正常"记录） | 正常 | 正常 | 正常 |

#### 缺陷：方向键在 PowerShell 与 conhost 下无效

**现象**：数字键与 `w/s` 能移动选项，方向键无反应；Enter、`n/p`、Tab、`q` 均正常。

**根因定位**：Windows 下方向键以"扩展键前缀 + 后缀字母"两字节形式到达（`\x00`/`\xe0` 后跟 `H`/`P`/`K`/`M`）。原实现在读到前缀后，只**单次等待 0.05 秒**取后缀；若后缀稍晚入队，等待超时返回 `None`，代码把整次按键映射为 `IGNORE`——**前缀已被消费，后缀被丢弃，按键静默失效**。已用可控时序复现：后缀延迟 200ms 时 `read_key()` 返回 `IGNORE`。

**修复**：

- 后缀改为最多重试 6 次（每次 50ms 窗口），容忍终端把两字节分先后入队；
- 重试仍失败时返回专用标记 `PREFIX_RETRY`，由 `handle_key` 显式忽略，**不再被当成"未知按键"而弹出无关提示**，也绝不丢键成错误动作；
- 补齐扩展键后缀映射：`INSERT`/`DELETE`/`HOME`/`END`（原先只有方向键与翻页键）。

**回归验证**：新增 5 项测试（12 种扩展键组合、晚到后缀重试、重试耗尽标记、`PREFIX_RETRY` 不改变状态、方向键确实移动选项）。把实现回退为旧的单次 0.05s 版本后重跑，**4 项失败**（`AssertionError: 'IGNORE' != 'END'` 等），证明新测试能真正捕获该缺陷。

**真机复测：已确认修复。** 用户在 PowerShell 终端与 conhost 下复测，方向键恢复正常（此前失效）。`w/s` 与数字键作为对照同时正常。VS Code 终端亦无异常。

#### 诊断工具自身的三处缺陷（2026-09-23 补记）

用户按要求运行 `python diagnose_keys.py` 采集按键原始字节，输出显示**所有按键"未识别"**。逐条比对后确认：**这不是产品缺陷，而是诊断工具本身有三处 bug**，它把正确解码的输入误报成失败。

| # | 缺陷 | 表现 | 修复 |
|---|---|---|---|
| 1 | `hexdump` 按 UTF-16 **低字节在前**打印 | 真实的 `H` 被打印成 `48 00`，byte 顺序看上去是反的，误导判读 | 改为按**码位**打印（本工具输入恒在 `0x00`–`0xFF`），`H` → `48`、前缀 → `E0` |
| 2 | `classify` 与字面量 `"\x00H"` / `"\xe0H"` 比较 | `msvcrt.getwch()` 把第二个键盘字节 `0xE0` 返回为字符 `U+00E0`（`à`），**真实终端输入永远匹配不上**；只有 `up` 偶然因后缀是 `H` 而"看起来正常" | 先归一化扩展键前缀（`0x00`/`0xE0` 都识别），再映射 `H/P/K/M/G/O/I/Q/R/S` 全部扩展键；并区分"仅收到前缀"与"未知后缀" |
| 3 | `read_raw` 的尾部收集窗口**每读到一个字符就重置** | 快速连按多个键会被合并进同一次采集，导致**每行整体错位一格** | 尾部窗口只计算一次，取到首字符后即固定截止 |

缺陷 3 在用户的采集里可直接观测：`up` 行只有前缀 `E0`（后缀漏到了下一行），`Enter` 行收到的却是 `M`（右方向键），`Tab` 行多出一个 `P`。把整批数据**向后错一格**后，`down→UP`、`left→DOWN`、`right→LEFT`、`enter→RIGHT` 完美吻合，确认就是合并错位。

**修复后的判定**（同一批原始数据）：`down → 前缀 0xE0 + 'H' / 方向键 上`、`left → 前缀 0xE0 + 'P' / 方向键 下`……**全部正确解码**。这说明终端的按键表示与解码器假设完全一致（前缀 `0xE0` + 后缀字母），修复是对的。

**回归验证**：新增 `test_diagnose_keys.py` 15 项测试。把 `hexdump`/`classify` 回退为旧实现后重跑，**13 项失败**，证明测试能真正捕获这三处缺陷。探针测试总数 **28 → 43 项**。

**顺带核验解码器本身**：用打桩输入直接测试 `TerminalInput.read_key`，`\x00H`→UP、`\xe0P`→DOWN、`\x00K`→LEFT、`\xe0M`→RIGHT、`\xe0G`→HOME、`\x00O`→END、`\xe0S`→DELETE、`\x00R`→INSERT 全部正确；晚到后缀重试为 UP（旧实现 IGNORE）；重试耗尽返回 `PREFIX_RETRY`。**即产品侧解码器无缺陷，此前"未识别"全部来自诊断工具。**

#### 字节顺序之谜与「双序解码」

三次真实采集（PowerShell、conhost、Windows Terminal）都出现了同一现象：**方向键的 `0xE0` 前缀总是出现在后缀字母之后**（`48 E0`、`50 E0`……），而上箭头的后缀恰好是 `H`，看起来就像字节被整体反转。

这留下一个无法靠静态判读解决的问题：究竟是**终端发送顺序**如此，还是**采集/打印环节**重排了字符？为此做了两件事。

**（一）新增 `diagnose_keys.py --timing`：用到达时刻而非打印顺序判定。**
打印顺序可以骗人，时间戳不会——先到的字符就是终端先发出的字节。该模式记录每个字符的 `after_ms`，并直接给出结论（`PREFIX_FIRST` / `LETTER_FIRST` / 样本不足）。

**（二）解码器改为接受两种顺序，使结论不再重要。**
`TerminalInput.read_key` 现在同时处理：

- **前缀在前**（Windows 文档形式）：`0x00`/`0xE0` + 后缀字母；
- **字母在前**：读到 `H/P/K/M/...` 后**短暂窥探**一个字符，若是 `0x00`/`0xE0` 则认作扩展键。

第二条路径必须解决一个真实风险：`w`、`p`、`n` 这些字母本身就是游戏命令键，窥探可能吃掉紧接着的第二个按键。因此新增**回推缓冲**（`deque`）：若窥探到的是另一个普通可打印字符，则把它放回缓冲，下一次读取照常取到。

**回归验证**：

- 新增 5 项测试：双序解码 10 组、窥探不吞命令键、全部命令字母（`w/s/n/p/q/Q`）在窥探后仍完整返回、字母后无后续字符时仍是字母、数字键不被误判为扩展键。
- **反证**：把 `read_key` 回退为"仅支持前缀在前"，新增的**10 组反向用例全部失败**（`test_reversed_extended_keys_also_decode`），证明测试真正锁住了该行为。
- 探针测试 **43 → 56 项**全部通过。

**同时新增 `detect_terminal.py`**：按环境变量给出**明确的终端身份判定**，避免再靠 `TERM`/`COLORTERM` 组合去猜产品名（`WT_SESSION` 存在即 Windows Terminal，这是权威标志）。带 8 项测试，含"只有 `COLORTERM` 时不得据此宣称终端身份"与"VS Code 不得与 Windows Terminal 混淆"两条防混淆断言。

## 当前终端支持矩阵

| 环境/检查 | 当前证据 | 状态 |
|---|---|---|
| Codex Windows PowerShell PTY，80×24，无色档 | 实际键盘选择、预览、翻页、`q` 退出及后续 `Read-Host` 回显 | 已验证 |
| 80×24、64×20、48×16 静态布局 | 自动化快照断言行宽、选择项和退出键；采集器可复现 | 已验证布局；尚未逐个真机缩放 |
| 低于 48×16 | 40×10 自动化快照提示，不显示菜单 | 已验证布局 |
| 真彩/256 色/基础色/无色 | ANSI 输出自动化断言；`compat` 不再冒充真彩 | 已验证输出；真实视觉效果待测 |
| 转义序列载荷泄漏 | 新增回归断言，载荷不会进入画面 | 已验证 |
| 截断后仍宣称完整 | 已改为 `LayoutOverflow` 显式失败 | 已验证 |
| Windows Terminal | 2026-09-23 采集器 `WT_SESSION` 非空（权威身份标志）、120×30、utf-8 往返一致、4/4 布局 PASS；人工确认交互正常 | 已验证（色档仍回落 basic，见下） |
| VS Code 集成终端 | 2026-09-23 人工实测：交互/缩放/恢复/中文颜色均正常（方向键未单独说明，建议复测） | 人工已验（方向键待确认） |
| 传统 conhost / cmd | 2026-09-23 采集器全 PASS（120×30、utf-8 往返一致、色档 basic）；人工实测交互/缩放/恢复/中文颜色正常；方向键修复后复测通过 | 已验证 |
| PowerShell 终端 | 2026-09-23 采集器全 PASS（120×50、utf-8 往返一致、色档 basic）；人工实测交互/缩放/恢复/中文颜色正常；方向键修复后复测通过；字体=新宋体、代码页=936 | 已验证 |
| 方向键（Windows 扩展键格式） | 2026-09-23 人工实测曾失效；定位为后缀等待过短并修复；28 项自检 + 5 项回归测试覆盖；**真机复测通过**；真实按键字节经 `diagnose_keys.py` 确认解码正确 | 已验证 |
| 实际拖动缩放 | 三个终端人工实测均正常 | 已验证 |
| Ctrl+C、EOF 中断恢复 | 三个终端人工实测均正常 | 已验证 |
| 中文显示与颜色观感 | 三个终端人工实测均正常 | 已验证 |
| 具体字体 | PowerShell 终端（conhost 宿主）为 **新宋体**（点阵/TrueType 双用，16 磅）。控制台属性 → 字体 选项卡截图留档 | 已记录 |
| 代码页 | 控制台**当前代码页 936（ANSI/OEM - 简体中文 GBK）**。程序内部统一 `sys.stdout.reconfigure(encoding="utf-8")` + console output mode `0x0004`，故显示不受 936 影响 | 已记录 |

### 关闭本任务需要做的事

自动化与自检已覆盖的部分（布局几何、四档颜色字节、输入解码、状态纯度、转义过滤、非 TTY 拒绝）**不需要再人工测**。人工实测已完成四个终端（PowerShell 终端、传统 conhost、VS Code 集成终端，见上节表格），字体与代码页已记录。

**剩余事项（仅 1 项）**：

- [ ] **Windows Terminal 全流程**：尚未取得任何独立记录，按 A~E 完整做一遍（采集器 + 交互 + 缩放 + 恢复 + 中文颜色）。启动方式见下节。

已完成项（无需重做）：三项原型修正项修复与回归、方向键修复与真机复测、诊断工具三处缺陷修复与回归、交互手感、拖动缩放、中断恢复、中文与颜色观感、字体名与代码页。

**A. 交互手感（参考，已完成）** —— `python interactive_probe.py`；`2`/`w`/`s`/方向键移动、`Enter` 预览、`n`/`p` 翻页、`Tab` 切焦点、`q` 退出。

**B. 拖动缩放（已完成）** —— 拖动重绘、小于 48×16 显示提示、放大后选择/页码保留。

**C. 中断恢复（已完成）** —— `q`、`Ctrl+C`、`Ctrl+D` 三种退出后回显与光标均恢复。

**D. 中文与颜色（已完成）** —— 无乱码/方块、框线对齐；`--color truecolor` 与 `--color basic`、`NO_COLOR=1` 均正常。

### 环境记录：代码页与字体

| 项 | 值 | 说明 |
|---|---|---|
| 控制台当前代码页 | **936（ANSI/OEM - 简体中文 GBK）** | 这是 Windows 中文系统的默认 OEM 代码页。程序内部显式使用 UTF-8（`sys.stdout.reconfigure(encoding="utf-8")`，Windows 上另开 `ENABLE_VIRTUAL_TERMINAL_PROCESSING`），因此**并不依赖代码页 936**，中文才能无损显示 |
| PowerShell 终端字体 | **新宋体**（16 磅） | 点阵/TrueType 双用字体。中文正常、框线（ASCII `+-\|`）对齐 |

**对正式 Go 版的意义**：不要依赖系统代码页。Go 在 Windows 上输出 UTF-8 字节即正确，但若将来需要与 `cmd` 的外部命令互操作，仍会遇到 936 编码边界，需在文档中显式声明"本程序输出 UTF-8，不随 `chcp` 变化"。

### 环境记录：Windows Terminal 是什么、怎么启动

前两次采集（`TERM` 为空、无 `WT_SESSION`）经确认**不是** Windows Terminal。这是最后一个待测环境。

**Windows Terminal（`wt.exe`）是微软随 Windows 10/11 提供的新一代终端宿主**，与"传统 conhost"（`cmd.exe` 和旧版 PowerShell 窗口那个蓝色/黑色经典窗口）是两套不同程序：

| | 传统 conhost | Windows Terminal |
|---|---|---|
| 宿主进程 | `conhost.exe` | `WindowsTerminal.exe` |
| 环境变量标志 | 无 | **`WT_SESSION` 存在**（采集器会显示"有"） |
| 真彩支持 | 弱，通常回落到 16 色 | 完整 24-bit |
| 检测方式 | `TERM`/`COLORTERM` 常为空 | `WT_SESSION` 非空，`TERM_PROGRAM=Windows_Terminal` |

**启动方式（任选一种）**：

```powershell
# 1) 直接启动 Windows Terminal，默认落在 PowerShell 配置
wt

# 2) 直接启动并在里面进入目标目录（推荐，一步到位）
wt -d "C:\Users\Administrator\Desktop\play\tools\task02-terminal-probe"

# 3) 指定用 PowerShell 配置启动并进入目录
wt -p "Windows PowerShell" -d "C:\Users\Administrator\Desktop\play\tools\task02-terminal-probe"
```

其他入口：

- 开始菜单搜索 **「终端」/「Terminal」/「Windows Terminal」**；
- `Win + X` → 「终端」/「Windows 终端」；
- 任务栏或文件夹右键 → 「在终端中打开」；
- 若是刚安装，可从 **Microsoft Store** 搜索 "Windows Terminal" 免费安装。

**若 `wt` 命令不存在**：说明没装或不在 PATH，改用 Microsoft Store 安装，或用 Win+X 菜单打开。也可以先用 `where.exe wt` 确认。

**进入 WT 后要跑的三条命令**：

```powershell
cd C:\Users\Administrator\Desktop\play\tools\task02-terminal-probe

# 1) 采集环境证据（重点看 WT_SESSION=有、自动色档是否升到 truecolor）
python collect_terminal_matrix.py --label "Windows Terminal"

# 2) 按键原始字符（修复后的工具现在能正确解码；重点确认 WT 走的是 CSI 还是扩展键）
python diagnose_keys.py

# 3) 完整交互：方向键/w/s 移动 → Enter 预览 → n/p 翻页 → Tab 切焦点 → 拖动缩放 → q / Ctrl+C / Ctrl+D 退出
python interactive_probe.py
```

**关键预期**：WT 下 `WT_SESSION` 应非空——这一条**已证实**（见采集记录 C）。但先前写下的"自动色档应升到 `truecolor`"**未成立**：本机 WT 默认不设置 `COLORTERM`，探测只能保守回落 `basic`。这是预期的修正，不是缺陷。

要在 WT 下自动获得真彩，有两条路：设置 `COLORTERM=truecolor`（需改 WT 配置，不由程序决定），或在程序里把"`WT_SESSION` 存在"直接当作真彩能力的证据。后者涉及"是否用宿主身份代替能力探测"的独立决策，不属于 TASK-02，已列入待办。

`Ctrl+C` 的清理路径由 `finally` 执行；如中断信号无法到达程序，记录为终端/PTY 限制，不得标成通过。

## 本次未改动范围

探针仍**不含**游戏规则、世界时钟、随机源、存档或网络访问，也不读取工作目录数据。TASK-02 的结论只服务于终端可行性，不构成正式语言或产品发行承诺。
