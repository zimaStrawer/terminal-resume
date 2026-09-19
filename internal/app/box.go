package app

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// dialogEdgeLeft / dialogEdgeRight 是输入框左右两侧那条竖线所占的那一格。
//
// 线用**制表符 │**（U+2502）+ 前景色：终端里唯一能同时满足
// 「细（1–2px）、纵向连续不断开、与输入框等高」三个条件的画法
// （用户 2026-09-19 晚间拍板：不要断开的线）。
//
// 为什么不是别的：
//   - 1/8 方块字符 ▏/▕：够细，但墨迹高度由字体决定（用户终端实测 23.5px < 格 28px），
//     三行线被切成三段 —— 用户明确不要断开的。
//   - 整格背景色：连续且等高，但约 13.5px 粗 —— 用户嫌太粗。
//
// │ 的墨迹约 1.27em > 格高 1.22em，行与行之间能连上、上下与面板齐平；
// 代价是它在格子里**水平居中**，离面板边缘差约半格（用户终端约 6px）。
// 终端的字符网格画不出「细 + 连续 + 压边」三全的线，这是仅剩的取舍点。
// 若要改：换 box.go 两个常量即可，测试同步改
// TestDialogEdgeCellsAreThinSingleColumn / TestDialogBoxEdgesHugThePanel。
const (
	dialogEdgeLeft  = "│"
	dialogEdgeRight = "│"
)

// renderDialog 把内容包成输入框：左右各一条细竖线，中间是**不上色**的输入区域，
// 没有边框、没有圆角。竖线用制表符 │（见上方 dialogEdgeLeft 的说明），
// 逐行拼接而不是 JoinHorizontal，避免它在行尾补空格。
// 线的颜色来自 boxEdge 的前景色。
//
// 为什么中间不再铺 #1E1E1E 实心底色（2026-09-19 用户裁定）：
// macOS 输入法组词时会把输入行从光标处向行尾整行擦除（BCE），擦除填充色 =
// 终端遗留的画笔背景色 = 面板色；面板色落在框右侧页面底色的边距格上，
// 就是「打字时框外突出一条灰块」的来源。框区域不上色后，整页背景处处一致
// （styles.go colorBackground），擦除了也看不出来。框只靠两条 │ 线和 › 提示符辨认。
func (m Model) renderDialog(content string, boxWidth, padRows int) string {
	box := lipgloss.NewStyle().Width(boxWidth).Padding(padRows, 2)
	lines := strings.Split(box.Render(content), "\n")
	left := m.styles.boxEdge.Render(dialogEdgeLeft)
	right := m.styles.boxEdge.Render(dialogEdgeRight)
	for index := range lines {
		lines[index] = left + lines[index] + right
	}
	return strings.Join(lines, "\n")
}
