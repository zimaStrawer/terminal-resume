package app

import "charm.land/lipgloss/v2"

const (
	// 页面底色（2026-09-20 从纯黑 #000000 提到 #0D0D0D，用户裁定）。
	// 演变：#0A0A0A →（2026-09-19 为让 IME 擦除隐形加深）→ #000000 →
	// （2026-09-20 框区铺回 #201E1E 灰面板后，纯黑边距与面板 Δ32 的高对比边界
	// 成了 IME 组词灰块显形的根因——pty 抓 opencode v2.0.9 字节流实证它靠
	// 「页面 232/面板 234 只差 Δ20」让擦除怎么擦都隐形）→ 提到 #0D0D0D。
	// 现在页面与面板只差 Δ19：稳定边界依然清晰可辨（opencode Δ20 用户就觉得
	// 「差异非常明显」，且框边界主要靠 #16B8F3 竖线识别），而组词期间的瞬态
	// 灰块低于察觉阈值、基本隐形。经 OSC 11 写入（app.go v.BackgroundColor），
	// 边距格天然就是这个色，无需逐格显式刷。
	colorBackground = "#0D0D0D"

	// 输入框面板底色（2026-09-20 用户要求对齐 OpenCode 截图的灰面板，从其截图采样 #201E1E）。
	// 曾经的「IME 组词灰块」代价（macOS 输入法 BCE 擦除把面板色带到框右侧边距）
	// 已通过把页面底色提到 #0D0D0D 消解：边距与面板只差 Δ19，擦除了也看不出来
	// （原理见 colorBackground 注释，2026-09-20 抓 opencode 字节流破解）。
	// 黑白主题不铺（保持纯黑白），见 box.go renderDialog。
	colorInputSurface = "#201E1E"

	colorSurface    = "#0F172A"
	colorForeground = "#F8FAFC"
	colorMuted      = "#94A3B8"
	colorDim        = "#6B7A90"
	// 强调蓝（RGB 0,187,249）：2026-09-20 用户从截图采样指定，由原来的青
	// #22D3EE 换过来。它是覆盖面最广的一个色 —— 章节标题、链接、文本光标、
	// 提示字符 ›、经历时间区间、进度条、/help 命令名、kicker 全走它。
	// ⚠️ 它与下方 colorSelection（#16B8F3 / RGB 22,184,243，hue 196° vs 195°）
	// 色相只差 1°、RGB 距离 23，视觉上几乎是同一个蓝 —— 要不要把两者并成
	// 同一个值（真正「只剩一种蓝」）等用户定，别自行合并。
	colorAccent  = "#00BBF9"
	colorSuccess = "#4ADE80"
	colorError   = "#FB7185"

	// 终端主题色（RGB 22,184,243）：用于下拉框选中项高亮。
	colorSelection = "#16B8F3"

	// 输入框左右两条细竖线也走终端主题色（用户 2026-09-19 指定）。
	// 它取代了原先后侧那条「三段式」渐变粗竖条（#00ECEA → #7777F5），
	// 想换颜色只改这一行。
	colorBoxEdge = colorSelection

	// 标题复用网站「一起做点有趣的事！」的金属色。SI 段银灰、ZHOU 段近白高光。
	// from 应用到 S/I，to 应用到 Z/H/O/U（见 paintLogo）。
	colorLogoFrom = "#A6AEBB"
	colorLogoTo   = "#FFFFFF"
)

type styles struct {
	accent     lipgloss.Style
	title      lipgloss.Style
	section    lipgloss.Style
	body       lipgloss.Style
	muted      lipgloss.Style
	dim        lipgloss.Style
	success    lipgloss.Style
	error      lipgloss.Style
	link       lipgloss.Style
	tag        lipgloss.Style
	selected   lipgloss.Style
	unselected lipgloss.Style
	menuItem   lipgloss.Style
	menuChosen lipgloss.Style
	themeText  lipgloss.Style
	// boxEdge 画输入框左右两条细竖线；animatedCursor 画那个会闪会跳的提示字符。
	// inputSurface 是输入框面板的底色（彩色主题 = colorInputSurface，黑白主题不铺）。
	boxEdge        lipgloss.Style
	inputSurface   lipgloss.Style
	animatedCursor lipgloss.Style
	prompt         lipgloss.Style
}

func newStyles(monochrome bool) styles {
	if monochrome {
		plain := lipgloss.NewStyle()
		return styles{
			accent:     plain.Bold(true),
			title:      plain.Bold(true),
			section:    plain.Bold(true),
			body:       plain,
			muted:      plain.Faint(true),
			dim:        plain.Faint(true),
			success:    plain.Bold(true),
			error:      plain.Bold(true),
			link:       plain.Underline(true),
			tag:        plain.Faint(true),
			selected:   plain.Bold(true),
			unselected: plain,
			menuItem:   plain,
			menuChosen: plain.Bold(true),
			themeText:  plain.Bold(true),
			// 黑白模式仍是「细线用近白前景色描」、不铺底色：mono 没有面板灰底，
			// 也就不存在「线看起来画在输入框上」的问题，保持纯黑白、不加粗。
			boxEdge:        plain.Foreground(lipgloss.Color(colorForeground)),
			inputSurface:   plain,
			animatedCursor: plain.Bold(true),
			prompt:         plain.Bold(true),
		}
	}

	return styles{
		accent:     lipgloss.NewStyle().Foreground(lipgloss.Color(colorAccent)),
		title:      lipgloss.NewStyle().Foreground(lipgloss.Color(colorForeground)).Bold(true),
		section:    lipgloss.NewStyle().Foreground(lipgloss.Color(colorAccent)).Bold(true),
		body:       lipgloss.NewStyle().Foreground(lipgloss.Color(colorForeground)),
		muted:      lipgloss.NewStyle().Foreground(lipgloss.Color(colorMuted)),
		dim:        lipgloss.NewStyle().Foreground(lipgloss.Color(colorDim)),
		success:    lipgloss.NewStyle().Foreground(lipgloss.Color(colorSuccess)).Bold(true),
		error:      lipgloss.NewStyle().Foreground(lipgloss.Color(colorError)).Bold(true),
		link:       lipgloss.NewStyle().Foreground(lipgloss.Color(colorAccent)).Underline(true),
		tag:        lipgloss.NewStyle().Foreground(lipgloss.Color("#CBD5E1")).Background(lipgloss.Color(colorSurface)).Padding(0, 1),
		selected:   lipgloss.NewStyle().Foreground(lipgloss.Color(colorAccent)).Bold(true),
		unselected: lipgloss.NewStyle().Foreground(lipgloss.Color(colorForeground)),
		// 下拉框（/ 命令列表）落在终端底色上，不需要背景色：
		// 未选中为灰色，选中项用终端主题色高亮。
		menuItem:   lipgloss.NewStyle().Foreground(lipgloss.Color(colorMuted)),
		menuChosen: lipgloss.NewStyle().Foreground(lipgloss.Color(colorSelection)),
		// 收尾文案里的英文标语走终端主题色，与下拉框选中项同一个色。
		themeText: lipgloss.NewStyle().Foreground(lipgloss.Color(colorSelection)),
		// 输入框左右两条竖线：由**整格背景色**画成实心条，不再靠字形墨迹。
		// 2026-09-20 晚改：字形法（│ + 前景色）的墨迹在格内水平居中、还填不满格高，
		// 而它那一格铺的是面板灰底 —— 于是蓝线看起来是「画在输入框上面」的一条细线
		// （用户实测不好看，给了 opencode 截图：一条紧贴面板外缘的实心蓝条）。
		// 改成整格背景色后：线必然逐行贴合、等高、连续无缝，且紧贴面板外缘，
		// 灰面板严格夹在两条蓝条之间 —— 就是用户要的「输入框放在两条蓝线的里面」。
		// 格内仍然写 dialogEdgeLeft，但前景色与背景色相同（因此不可见）：留下这个字形
		// 是为了让 frameCursor（钉 IME 硬件光标）与测试探针 isBoxRow 仍能按它认框行。
		// 换成空格会让那批探针全部静默失效（见 tui-render-verify 步骤 8）；
		// 字形怎么选、以及「墨迹不能溢出格子」的实测，见 box.go 里 dialogEdgeLeft 注释。
		boxEdge: lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorBoxEdge)).
			Background(lipgloss.Color(colorBoxEdge)),
		// 输入框面板底色（2026-09-20 起铺，对齐 OpenCode 截图；代价见上方 colorInputSurface）。
		inputSurface: lipgloss.NewStyle().Background(lipgloss.Color(colorInputSurface)),
		// 提示字符的常态色沿用 › 的强调蓝，乱序帧会临时换 muted 灰（照搬网页）。
		animatedCursor: lipgloss.NewStyle().Foreground(lipgloss.Color(colorAccent)).Bold(true),
		prompt:         lipgloss.NewStyle().Foreground(lipgloss.Color(colorAccent)).Bold(true),
	}
}
