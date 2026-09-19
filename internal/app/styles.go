package app

import "charm.land/lipgloss/v2"

const (
	// 页面底色 = 纯黑（2026-09-19 从 #0A0A0A 加深，用户裁定）。
	// 为什么必须是纯黑：macOS 输入法组词时会把输入行从光标处向行尾整行擦除，
	// 擦除填充色 = 终端遗留的画笔背景色（应用侧无法控制）。输入框区域现在**不上色**
	// （见 box.go renderDialog），整页所有格子的背景都等于这个默认色，
	// 于是「擦除了也看不出来」——只要页面底色和任何填充色不一致，突色块就会显形。
	colorBackground = "#000000"
	colorSurface    = "#0F172A"
	colorForeground   = "#F8FAFC"
	colorMuted        = "#94A3B8"
	colorDim          = "#6B7A90"
	colorBorder       = "#334155"
	colorAccent       = "#22D3EE"
	colorSuccess      = "#4ADE80"
	colorError        = "#FB7185"

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
	boxEdge        lipgloss.Style
	animatedCursor lipgloss.Style
	prompt         lipgloss.Style
	headerBadge    lipgloss.Style
	borderColor    lipgloss.Style
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
			// 黑白模式不引色：细线用近白前景色描（同彩色主题的 ▏/▕ 字符）。
			boxEdge:        plain.Foreground(lipgloss.Color(colorForeground)),
			animatedCursor: plain.Bold(true),
			prompt:         plain.Bold(true),
			headerBadge:    plain.Bold(true),
			borderColor:    plain,
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
		// 输入框左右竖线：制表符 │ 以前景色描一条细线，纵向贯通不断开、上下齐平
		// （见 box.go 的说明；用户 2026-09-19 晚间拍板「不要断开的线」）。
		// 框区自 2026-09-19 起不再铺底色，这两条线就是框仅有的边界标识。
		boxEdge: lipgloss.NewStyle().Foreground(lipgloss.Color(colorBoxEdge)),
		// 提示字符的常态色沿用原 › 的青，乱序帧会临时换 muted 灰（照搬网页）。
		animatedCursor: lipgloss.NewStyle().Foreground(lipgloss.Color(colorAccent)).Bold(true),
		prompt:         lipgloss.NewStyle().Foreground(lipgloss.Color(colorAccent)).Bold(true),
		headerBadge: lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorMuted)).
			Bold(true),
		borderColor: lipgloss.NewStyle().Foreground(lipgloss.Color(colorBorder)),
	}
}
