package app

import (
	"strings"
)

// dialogEdgeLeft / dialogEdgeRight 是输入框左右两侧那条竖线所占的那一格。
//
// 线本体由**格子的背景色**画：整格填满主题色，于是它必然逐行贴合、等高、纵向连续无缝，
// 且紧贴面板外缘 —— 灰面板严格夹在两条实心条之间（用户 2026-09-20 晚要的效果：
// 「把输入框放在两条蓝线的里面」，参考 opencode 截图）。
//
// 格内仍写一个字形，但 styles.boxEdge 让前景色 = 背景色，所以格内不可见。
// 留着它是**故意的**：frameCursor（钉 IME 硬件光标）与测试探针 isBoxRow 都按这个
// 字形认框行，换成空格会让那批探针全部静默失效（tui-render-verify 步骤 8）。
//
// ⚠️ 字形换过两次，别再随手换成别的：
//   - 最初是 │(U+2502) + 前景色画线：墨迹在格内**水平居中**、还填不满格高，而它那格
//     铺着面板灰底 —— 看起来是「一条蓝线画在输入框上面」，用户说不好看（遂改为整格
//     背景色画线）。
//   - 2026-09-20 晚用户又报「下面还有旧的竖线露出来」= 框底多出一截蓝色小尖。
//     实测根因：│ 是 **box-drawing 字符**，字体为了让相邻行的线衔接，把它墨迹做得比
//     格高还高，从框最后一行的底部漏到页面底色上。格内前景=背景所以不可见，漏出去
//     的背景变成页面色，它就显形了。上方的行漏出部分会被下一行的竖线格盖住，所以只
//     有最下面一行看得出来。
//   - 用无头 Chrome 逐像素实测候选字形（同一 Menlo、24px 字、28px 格）：
//     │ 溢出（上 2px / 下 1px）、⏐(U+23D0) 也溢出、∣(U+2223) 只有 0.55 列宽；
//     而 ǀ / ❘ / ¦ / | 的墨迹都完整落在格内。
//   - 终选 **ǀ (U+01C0 LATIN LETTER DENTAL CLICK)**：1 列宽、墨迹不越格，且它是 Latin
//     字母（EAW=Neutral、字体覆盖率最高；Dingbats 类的 ❘ 有可能缺字回退成别的宽度）。
//     刻意**不用** ASCII |，虽然它也不溢出 —— 用户会在输入框里打管道符，
//     frameCursor 的 LastIndex 会命中内容里的 | ，把光标位置算错。
const (
	dialogEdgeLeft  = "ǀ"
	dialogEdgeRight = "ǀ"
)

// renderDialog 把内容包成输入框：左右各一条主题色实心竖条，中间铺 colorInputSurface
// 灰面板，没有边框、没有圆角（2026-09-20 用户要求对齐 OpenCode 截图样式）。
// 竖条那一格的字形见上方 dialogEdgeLeft 的说明；逐行拼接而不是 JoinHorizontal，
// 避免它在行尾补空格。竖条格前景色 = 背景色 = 主题色（styles.go boxEdge），不留黑缝。
//
// 曾经的代价已消解：macOS 输入法组词时会把输入行从光标处向行尾整行擦除（BCE），
// 擦除填充色 = 面板色。页面底色还是纯黑时（Δ32），组词期间框右边距会临时突出灰块；
// 2026-09-20 页面底色提到 #0D0D0D 后与面板只差 Δ19（学 opencode 的 232/234 搭配，
// 当天抓其字节流破解），擦除了也隐形。黑白主题不铺底色（保持纯黑白）。
func (m Model) renderDialog(content string, boxWidth, padRows int) string {
	// 是否铺底色由样式决定（styles.inputSurface），不看 m.monochrome 标志——
	// 测试里的彩色模型只换 styles、不改标志，两种主题的分界以样式为准。
	box := m.styles.inputSurface.Width(boxWidth).Padding(padRows, 2)
	lines := strings.Split(box.Render(content), "\n")
	left := m.styles.boxEdge.Render(dialogEdgeLeft)
	right := m.styles.boxEdge.Render(dialogEdgeRight)

	// 彩色主题下面板有底色：lipgloss 只给「自己拥有」的内边距/补白格铺底，
	// 嵌套内容（textinput 输出、提示字符）内部的样式闭合（\x1b[m，也可能写成 \x1b[0m）
	// 会把底色一起清掉，文字背后留下黑色破洞。在每个 reset 之后重新打开面板底色，
	// 整行就没有破洞。行尾 reset 后多开一次无害——右缘竖线自带颜色并复位。
	// 黑白主题下面板无底色：探针渲染出来就是光秃秃的「 」，跳过重开逻辑。
	if probe := m.styles.inputSurface.Render(" "); probe != " " {
		bgReopen := probe[:strings.IndexByte(probe, ' ')]
		for index := range lines {
			line := strings.ReplaceAll(lines[index], "\x1b[m", "\x1b[m"+bgReopen)
			line = strings.ReplaceAll(line, "\x1b[0m", "\x1b[0m"+bgReopen)
			lines[index] = line
		}
	}
	for index := range lines {
		lines[index] = left + lines[index] + right
	}
	return strings.Join(lines, "\n")
}
