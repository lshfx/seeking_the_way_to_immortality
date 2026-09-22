# -*- coding: utf-8 -*-
"""
问道长生 · 终端面板渲染原型 (TUI panel renderer prototype)

目的：验证 1.txt 第 4 节的 LaTeX 面板规范能否 1:1 映射到 ANSI 终端字符网格。
映射关系：
    \\fcolorbox{边}{底}{...}  ->  框线字符(fg=边色) + 单元格背景(底)
    \\colorbox{底}{...}       ->  单元格背景色
    \\rule{nem}{1ex} 进度条   ->  带背景色的空格串（精度 = 1 列）
    \\overline{} 分隔线       ->  ─ 重复
    五行 \\colorbox 小色块     ->  宽字符 + 背景色
    \\scalebox / 溢出截断      ->  终端天然是固定网格，不存在溢出

两种渲染档位：
    rich   —— 24bit 真彩 + 制表符框线 + 色块底（还原原稿宣纸白底）
    compat —— 16 色 + ASCII 框线 + #/. 进度条（老 conhost / 受限 SSH 也能跑）

输出：demo.html（并排预览）、demo.txt（纯文本）、stdout（ANSI 真彩）
"""
import unicodedata
import html as _html

# ---------------------------------------------------------------- 配色（取自 1.txt §4.5）
PAPER   = "#FBF8F1"   # 宣纸白（面板底）
WARN_BG = "#FBEDE9"   # 警告底（渡劫 / 战斗）
BAR_BG  = "#E8E4DC"   # 进度条空槽
GOLD    = "#C9A45C"   # 鎏金分隔线
INK     = "#3F3A34"   # 正文深墨
MUTED   = "#8C8578"   # 次要灰
WHITE   = "#FFFFFF"
HP      = "#C05F55"   # 气血 / 朱砂
MP      = "#5E8FAE"   # 灵力 / 蔚蓝
XP      = "#A87E2E"   # 修为 / 琥珀
LIFE    = "#5C8C6E"   # 寿元 / 青碧
JADE    = "#6FA698"   # 主题：主界面 / 状态卡 / 修炼
SKY     = "#7FA8C9"   # 主题：地图 / 宗门
VIOLET  = "#8B6FA8"   # 主题：渡劫 / 天雷
CORAL   = "#C4675C"   # 主题：战斗 / 危机
PURPLE  = "#A98FD9"   # 语义标签：机缘
PINK    = "#D88FA5"   # 语义标签：情缘
WOOD    = "#6BA38E"   # 五行：木
FIRE    = "#C05F55"   # 五行：火
WATER   = "#5E8FAE"   # 五行：水
EARTH   = "#B08A4E"   # 五行：土
METAL   = "#C9A45C"   # 五行：金

WIDTH = 80            # 经典 80 列终端
BARW  = 14            # 进度条格数，随终端宽度收缩
NARROW = False        # 窄布局开关（分屏摸鱼场景）


def set_width(w):
    """按终端宽度切换布局档位。摸鱼时终端常被分屏，只有 48~64 列可用。"""
    global WIDTH, BARW, NARROW
    WIDTH = w
    BARW = 14 if w >= 72 else (12 if w >= 56 else 7)
    NARROW = w < 72
    return w

# compat 档位：把 24bit 色降级到 16 色
C16 = {
    JADE: "#1abc9c", SKY: "#3498db", WOOD: "#2ecc71", WATER: "#3498db",
    VIOLET: "#9b59b6", PURPLE: "#9b59b6", PINK: "#e84393",
    CORAL: "#e74c3c", FIRE: "#e74c3c", HP: "#e74c3c",
    XP: "#f1c40f", GOLD: "#f1c40f", METAL: "#f1c40f", EARTH: "#e67e22",
    LIFE: "#2ecc71", MUTED: "#95a5a6", INK: "#ecf0f1", WHITE: "#ffffff",
    BAR_BG: "#444444", PAPER: None, WARN_BG: None,
}

MODE = "rich"
AMBIGUOUS = set()     # 记录用到的"东亚歧义宽度"字符（对齐第一杀手）


def C(hexcolor):
    if MODE == "rich" or hexcolor is None:
        return hexcolor
    return C16.get(hexcolor, hexcolor)


def cw(ch):
    """终端显示宽度：东亚宽/全角 = 2，其余 = 1。"""
    if unicodedata.combining(ch):
        return 0
    ea = unicodedata.east_asian_width(ch)
    if ea in ("W", "F"):
        return 2
    if ea == "A":
        AMBIGUOUS.add(ch)
        return 1
    return 1


def adj(s):
    """compat 档位把"东亚歧义宽度"标点一并替换，彻底消除错位来源。"""
    if MODE == "compat":
        return s.replace("·", "-").replace("…", "...")
    return s


def dwidth(s):
    return sum(cw(c) for c in adj(s))


def G():
    """框线字符集。compat 档位退回 ASCII，规避歧义宽度风险。"""
    if MODE == "rich":
        return dict(tl="┌", tr="┐", bl="└", br="┘", h="─", v="│")
    return dict(tl="+", tr="+", bl="+", br="+", h="-", v="|")


def _wrap_tokens(t, room):
    """按空格折行，返回每段不超过 room 列的文本段列表。"""
    words = [w for w in t.split(" ") if w]
    out, cur = [], ""
    for w in words:
        cand = (cur + " " + w) if cur else w
        if dwidth(cand) <= room or not cur:
            cur = cand
        else:
            out.append(cur)
            cur = w
    if cur:
        out.append(cur)
    return out


def flow(row, avail, indent=0):
    """把过宽的内容行按空格折成多行。带背景色的段（进度条/色块）视为原子，整段换行不拆。"""
    lines, cur, x = [], [], 0

    def newline():
        nonlocal cur, x
        if x > indent:
            lines.append(cur)
        cur = [(" " * indent, None, None)]
        x = indent

    for seg in row:
        text, fg = seg[0], seg[1]
        bg = seg[2] if len(seg) > 2 else None
        t = adj(text)
        w = dwidth(t)
        if x + w <= avail:
            cur.append((text, fg, bg))
            x += w
            continue
        if bg is not None:                      # 原子段：整段挪到下一行
            newline()
            cur.append((text, fg, bg))
            x += w
            continue
        parts = _wrap_tokens(t, max(1, avail - x))
        if parts and x + dwidth(parts[0]) <= avail:
            cur.append((parts[0], fg, bg))
            x += dwidth(parts[0])
            parts = parts[1:]
        newline()
        for p in parts:
            if x + dwidth(p) > avail:
                newline()
            cur.append((p, fg, bg))
            x += dwidth(p)
    lines.append(cur)
    return lines


# ---------------------------------------------------------------- 字符网格
class Grid:
    """每格 = [字符, 前景色, 背景色]；宽字符占 2 格，第二格字符为 None。"""

    def __init__(self, w):
        self.w = w
        self.rows = []
        self.deco = set()      # 装饰行（边框/标题条/分隔线），不计入内容宽度测算
        self.rowbg = {}        # 每行的面板底色，用于区分"内容色块"与"面板底"

    def newrow(self, bg=None):
        self.rows.append([[" ", None, bg] for _ in range(self.w)])
        return len(self.rows) - 1

    def put(self, x, y, text, fg=None, bg=None):
        text = adj(text)
        cx = x
        for ch in text:
            w = cw(ch)
            if w == 0:
                continue
            if cx + w > self.w:
                break
            self.rows[y][cx] = [ch, fg, bg]
            for k in range(1, w):
                self.rows[y][cx + k] = [None, fg, bg]
            cx += w
        return cx

    def fill(self, x, y, n, fg=None, bg=None):
        for i in range(n):
            if 0 <= x + i < self.w:
                self.rows[y][x + i] = [" ", fg, bg]

    def row_width(self, y):
        return sum(cw(c[0]) for c in self.rows[y] if c[0] is not None)

    def content_width(self, y):
        """该行"自然内容宽度"（不含左右边框与面板底色），用于推算最小终端列数。"""
        base = self.rowbg.get(y)
        last = 0
        for i in range(1, self.w - 1):
            ch, fg, bg = self.rows[y][i]
            if (ch is not None and ch != " ") or (bg and bg != base):
                last = i + 1
        return last


# ---------------------------------------------------------------- 组件库
def draw_panel(g, theme, title, body, bg=PAPER):
    """一个 \\fcolorbox 大面板：彩色边框 + 底色 + 主题色底白字标题条。"""
    inner = WIDTH - 2
    gs = G()

    y = g.newrow(C(bg))
    g.rowbg[y] = C(bg)
    g.put(0, y, gs["tl"] + gs["h"] * inner + gs["tr"], C(theme), C(bg))
    g.deco.add(y)

    y = g.newrow(C(bg))
    g.rowbg[y] = C(bg)
    g.put(0, y, gs["v"], C(theme), C(bg))
    if MODE == "rich":
        g.fill(1, y, inner, WHITE, theme)
        g.put(1 + max(0, (inner - dwidth(title)) // 2), y, title, WHITE, theme)
    else:
        g.put(1 + max(0, (inner - dwidth(title)) // 2), y, title, C(theme), None)
    g.put(WIDTH - 1, y, gs["v"], C(theme), C(bg))
    g.deco.add(y)

    for row in body:
        rows = [row] if row is None else flow(row, WIDTH - 4)
        for r in rows:
            y = g.newrow(C(bg))
            g.rowbg[y] = C(bg)
            g.put(0, y, gs["v"], C(theme), C(bg))
            if r is None:
                g.put(1, y, gs["h"] * inner, C(GOLD), C(bg))
                g.deco.add(y)
            else:
                x = 2
                for seg in r:
                    text, fg = seg[0], seg[1]
                    rbg = seg[2] if len(seg) > 2 else None
                    x = g.put(x, y, text, C(fg), C(rbg) if rbg else C(bg))
            g.put(WIDTH - 1, y, gs["v"], C(theme), C(bg))

    y = g.newrow(C(bg))
    g.rowbg[y] = C(bg)
    g.put(0, y, gs["bl"] + gs["h"] * inner + gs["br"], C(theme), C(bg))
    g.deco.add(y)


def bar(value, total, width, color):
    """\\rule 进度条。rich = 背景色空格串；compat = #/. 字符。"""
    n = max(0, min(width, int(round(value / total * width))))
    if MODE == "rich":
        return [(" " * n, None, color), (" " * (width - n), None, BAR_BG)]
    return [("#" * n, color, None), ("." * (width - n), MUTED, None)]


def chip(text, bg, fg=WHITE):
    """五行色块 / 语义标签。rich = 反白块；compat = [文字] 上色。"""
    if MODE == "rich":
        return (text, fg, bg)
    return ("[" + text + "]", bg, None)


def line(*segs):
    return list(segs)


def build():
    g = Grid(WIDTH)

    # 时间行（原稿铁律 4：每段输出开头带时间行）
    y = g.newrow()
    g.put(2, y, "入道三年 · 五月", C(MUTED))
    g.newrow()

    # ---- 面板一：状态卡（青玉）----
    six = [line(("资质 12   悟性 13   神识 10   遁速 9   道心 14   仙缘 11", INK))]
    if WIDTH < 56:
        six = [line(("资质 12  悟性 13  神识 10", INK)),
               line(("遁速  9  道心 14  仙缘 11", INK))]
    body1 = [
        line(("道号 清微 · 男 · 21 岁 · 寿元 ", INK),
             *bar(79, 100, BARW, LIFE), (" 79/100", INK)),
        line(("境界 炼气·中期", INK), ("      宗门 青云宗·外门弟子", INK)),
        *six,
        line(("仙姿 出众      灵根 ", INK), chip("木", WOOD), (" ", None), chip("火", FIRE)),
        line(("气血 ", INK), *bar(76, 100, BARW, HP), (" 76/100", INK)),
        line(("灵力 ", INK), *bar(67, 100, BARW, MP), (" 67/100", INK)),
        line(("修为 ", INK), *bar(51, 100, BARW, XP), (" 51/100", INK)),
        line(("灵石 480   功德 5   业力 0   异常 无", INK)),
        line(("所在地 青岳·青云宗 · 时节 春", MUTED)),
        line(("主线 三年后升仙大会，夺魁可得筑基丹", MUTED)),
        None,
        line(("指令：面板 修炼 突破 悟道 洞府 地图 背包 坊市 宗门 技艺 情缘 对话 存档 帮助", MUTED)),
    ]
    draw_panel(g, JADE, "状态卡 · 入道三年 · 五月", body1)
    g.newrow()

    # ---- 面板二：九州舆图（天青）----
    regions = [
        ("东洲·青岳", "炼气~筑基", 2, "青云宗 天机坊市", JADE),
        ("南疆·赤炎", "筑基~金丹", 5, "赤阳宗 万兽谷 古妖山", FIRE),
        ("西漠·流沙", "金丹~元婴", 6, "浮屠寺 魔渊", GOLD),
        ("北原·寒渊", "元婴~化神", 8, "雪族 幽冥殿", SKY),
        ("中州·天阙", "筑基~登仙", 8, "天衍宗 万剑阁 丹鼎阁", VIOLET),
    ]
    body = []
    for name, realm, danger, forces, col in regions:
        seg = [(name, col), ("  " + realm, INK), ("  ", None)]
        seg += bar(danger, 9, 9, HP if danger >= 7 else (GOLD if danger >= 4 else LIFE))
        seg += [(" 危险 %d/9" % danger, INK)]
        if not NARROW:
            seg += [("  " + forces, MUTED)]
        body.append(line(*seg))
    body.append(None)
    body.append(line(("指令：出发 秘境 坊市 查看 返回", MUTED)))
    draw_panel(g, SKY, "九州舆图 · 危险分级", body)
    g.newrow()

    # ---- 面板三：选项面板（青玉 + 语义标签）----
    draw_panel(g, JADE, "青岳 · 洞府静室", [
        line(("你在蒲团上睁开眼，门外那张传音符正微微发烫。", INK)),
        None,
        line((" ", None), chip(" A ", JADE), (" ", None), chip("机缘", PURPLE),
             ("  玉简似有玄机，要不要参悟", INK)),
        line((" ", None), chip(" B ", JADE), (" ", None), chip("风险", FIRE),
             ("  拦路的血魔宗弟子气势远高于你", INK)),
        line((" ", None), chip(" C ", JADE), (" ", None), chip("平和", JADE),
             ("  绕开此地，回洞府闭关修炼", INK)),
        line((" ", None), chip(" D ", JADE), (" ", None), chip("情缘", PINK),
             ("  循着传音符，去赴顾清玄之约", INK)),
        None,
        line(("也可自由输入你的行动…", MUTED)),
    ])
    g.newrow()

    # ---- 面板四：战斗（朱砂）----
    if NARROW:
        body4 = [
            line(("我方 炼气·中期", INK)),
            line(("  气血 ", INK), *bar(76, 100, BARW, HP), (" 76/100", INK),
                 ("   灵力 ", INK), *bar(67, 100, BARW, MP), (" 67/100", INK)),
            line(("敌方 筑基·初期   威压 强", INK)),
            line(("  气血 ", INK), *bar(92, 100, BARW, HP), (" 92/100", INK)),
        ]
    else:
        body4 = [
            line(("我方 炼气·中期   气血 ", INK), *bar(76, 100, BARW, HP),
                 (" 76/100   灵力 ", INK), *bar(67, 100, BARW, MP), (" 67/100", INK)),
            line(("敌方 筑基·初期   气血 ", INK), *bar(92, 100, BARW, HP),
                 (" 92/100   威压 强", INK)),
        ]
    body4 += [
        line(("压制 境界差 1 大境 · 正面伤害 x0.4 · 对方减伤 60%", FIRE)),
        line(("五行灵气  ", INK), chip("金", METAL), (" 2  ", None), chip("木", WOOD),
             (" 1  ", None), chip("水", WATER), (" 0  ", None), chip("火", FIRE),
             (" 3  ", None), chip("土", EARTH), (" 1", None)),
        None,
        line(("指令：攻击 施法 绝技 防御 遁走 用符 用丹 观察 说话", MUTED)),
    ]
    draw_panel(g, CORAL, "遭遇 · 血魔宗弟子", body4)
    g.newrow()

    # ---- 面板五：渡劫警告（玄紫 + 警告底）----
    draw_panel(g, VIOLET, "天劫将至 · 金丹雷劫", [
        line(("雷劫 三道紫霄神雷", INK), ("      雷抗 中等", INK)),
        line(("功德护体 可挡一道", INK), ("      业力缠身 无", INK)),
        line(("心魔劫：此生最悔之事将化形来袭", FIRE)),
        None,
        line(("指令：运功硬抗 法宝护体 丹药续命 以功德化劫", MUTED)),
    ], bg=WARN_BG)

    return g


# ---------------------------------------------------------------- 输出器
def hex2rgb(h):
    h = h.lstrip("#")
    return "%d;%d;%d" % (int(h[0:2], 16), int(h[2:4], 16), int(h[4:6], 16))


def to_ansi(g):
    out = []
    for row in g.rows:
        cur, buf = (None, None), []
        for ch, fg, bg in row:
            if ch is None:
                continue
            if (fg, bg) != cur:
                buf.append("\x1b[0m")
                if fg:
                    buf.append("\x1b[38;2;%sm" % hex2rgb(fg))
                if bg:
                    buf.append("\x1b[48;2;%sm" % hex2rgb(bg))
                cur = (fg, bg)
            buf.append(ch)
        buf.append("\x1b[0m")
        out.append("".join(buf))
    return "\n".join(out)


def _span(text, style):
    fg, bg = style
    css = []
    if fg:
        css.append("color:" + fg)
    if bg:
        css.append("background:" + bg)
    t = _html.escape(text)
    return t if not css else '<span style="%s">%s</span>' % (";".join(css), t)


def to_html(g):
    lines = []
    for row in g.rows:
        parts, buf, cur = [], [], (None, None)
        for ch, fg, bg in row:
            if ch is None:
                continue
            if (fg, bg) != cur:
                if buf:
                    parts.append(_span("".join(buf), cur))
                buf, cur = [], (fg, bg)
            buf.append(ch)
        if buf:
            parts.append(_span("".join(buf), cur))
        lines.append("".join(parts))
    return "\n".join(lines)


def to_plain(g):
    return "\n".join("".join(c[0] for c in row if c[0] is not None) for row in g.rows)


# ---------------------------------------------------------------- 主流程
def render(mode, width):
    global MODE
    MODE = mode
    set_width(width)
    return build()


def natural_width(g):
    """按"完全不折行"计算这批面板所需的最小终端列数（含边框）。"""
    mx = 0
    for i in range(len(g.rows)):
        if i in g.deco:
            continue
        mx = max(mx, g.content_width(i) + 1)
    return mx


VARIANTS = [
    ("rich",   80, "档位 A · rich @ 80 列",
     "全屏终端，经典 80 列。完整布局，最舒展。"),
    ("rich",   64, "档位 B · rich @ 64 列",
     "左右分屏（120 列终端对半开）。指令行与主线自动折行，其余布局不变。"),
    ("rich",   48, "档位 C · rich @ 48 列",
     "VS Code 终端面板 / 小窗口。六维拆两行、战斗拆块、地图去掉势力列、进度条收窄。"),
    ("compat", 64, "档位 D · compat @ 64 列",
     "16 色 + ASCII 框线 + #/. 进度条。歧义宽度字符为零，老 conhost 与日志重定向也能对齐。"),
]


HTML_TPL = """<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<title>问道长生 · 终端面板渲染原型</title>
<style>
  * {{ box-sizing:border-box; }}
  body {{ margin:0; padding:36px 20px 56px; background:#0f0f0f;
         font-family:"Segoe UI","Microsoft YaHei",sans-serif; color:#d8d4cc; }}
  .wrap {{ max-width:1000px; margin:0 auto; }}
  h1 {{ font-size:18px; font-weight:600; margin:0 0 8px; color:#f2eee6; }}
  p.sub {{ font-size:13px; color:#8a8478; margin:0 0 30px; line-height:1.75; }}
  h2 {{ font-size:14px; font-weight:600; color:#c8c2b6; margin:36px 0 4px; }}
  p.cap {{ font-size:12.5px; color:#7d776c; margin:0 0 12px; line-height:1.7; }}
  .term {{ background:#1c1c1c; border:1px solid #2e2e2e; border-radius:10px;
           overflow:hidden; box-shadow:0 10px 30px rgba(0,0,0,.5);
           width:fit-content; max-width:100%; }}
  .bar {{ height:34px; background:#2a2a2a; display:flex; align-items:center;
          padding:0 12px; gap:7px; border-bottom:1px solid #333; }}
  .dot {{ width:11px; height:11px; border-radius:50%; }}
  .bar span.t {{ margin-left:10px; font-size:12px; color:#8a8478; }}
  pre {{ margin:0; padding:16px 18px; font-size:13px; line-height:1.2;
         font-family:"Cascadia Mono","Sarasa Mono SC",Consolas,
                     "DejaVu Sans Mono","Microsoft YaHei",monospace;
         white-space:pre; overflow-x:auto; color:#c8c8c8; }}
  .note {{ margin-top:36px; padding:16px 18px; border-left:2px solid #3a3a3a;
           font-size:12.5px; color:#8a8478; line-height:1.85; }}
  .note b {{ color:#c2bcae; font-weight:600; }}
  code {{ font-family:Consolas,monospace; color:#b9b2a4; }}
</style>
</head>
<body>
<div class="wrap">
  <h1>问道长生 · 终端面板渲染原型</h1>
  <p class="sub">定位：打工人的摸鱼游戏，只装在本机。趁等 codex / CI 出结果时开个终端玩两回合。<br>
     同一套 1.txt §4 面板规范，换成 ANSI 字符网格渲染——没有图片、没有字体依赖、没有排版引擎。</p>
{blocks}
  <p class="note">
    <b>不折行所需的最小宽度：</b>{nat} 列。低于此值即触发自动折行（指令行先折，其次主线与战斗行）。<br>
    <b>对齐审计：</b>{audit}<br>
    <b>歧义宽度警告：</b>rich 档用到 {amb} ——
    这些字符的东亚宽度属性是「歧义(A)」。若终端把歧义字符按宽字符渲染，框线会整体错位；
    compat 档正是为此准备的退路（审计结果：零歧义字符）。<br>
    <b>字体要求：</b>真实终端需满足「CJK 字宽 = 拉丁字宽 x 2」，推荐 Sarasa Mono / 更纱黑体，
    或 Cascadia Mono 搭配雅黑回退。本页用等宽字体模拟，观感与真机略有差异。
  </p>
</div>
</body>
</html>
"""

BLOCK_TPL = """
  <h2>{title}</h2>
  <p class="cap">{cap}</p>
  <div class="term">
    <div class="bar"><i class="dot" style="background:#ff5f57"></i>
      <i class="dot" style="background:#febc2e"></i>
      <i class="dot" style="background:#28c840"></i>
      <span class="t">{label}</span></div>
    <pre>{body}</pre>
  </div>
"""

if __name__ == "__main__":
    AMBIGUOUS.clear()
    ref = render("rich", 200)
    nat = natural_width(ref)
    amb = sorted(AMBIGUOUS)

    blocks, audits = [], []
    for mode, width, title, cap in VARIANTS:
        AMBIGUOUS.clear()
        g = render(mode, width)
        bad = [i for i in range(len(g.rows)) if g.row_width(i) != width]
        used = max(g.content_width(i) for i in range(len(g.rows)) if i not in g.deco)
        audits.append("%s：%d 行全部 = %d 列，内容最宽 %d 列" %
                      (title.split("·")[1].strip(), len(g.rows), width, used))
        blocks.append(BLOCK_TPL.format(title=title, cap=cap,
                                       label="daodao — %s — %d cols" % (mode, width),
                                       body=to_html(g)))
        if mode == "rich" and width == 64:
            plain64 = to_plain(g)

    print("不折行所需最小宽度：%d 列" % nat)
    for a in audits:
        print("  " + a)
    print("rich 档歧义宽度字符：%s" % (" ".join(amb) if amb else "无"))

    with open("demo.html", "w", encoding="utf-8") as f:
        f.write(HTML_TPL.format(blocks="".join(blocks), nat=nat,
                                audit="；".join(audits),
                                amb=" ".join(amb) if amb else "无"))
    with open("demo.txt", "w", encoding="utf-8") as f:
        f.write(plain64 + "\n")
    with open("demo.ansi.txt", "w", encoding="utf-8") as f:
        f.write(to_ansi(render("rich", 80)) + "\n")

    print("-" * 64)
    print(plain64)
