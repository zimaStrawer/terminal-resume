package app

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
)

// logoLetters 是 SIZHOU 六个字母各自的 ANSI Shadow 字形（每个字母 6 行）。
// 拆成单字母存储，便于分别上色：S、I 用银灰(#A6AEBB)，Z、H、O、U 用近白(#FFFFFF)，
// 形成用户指定的两段式配色，而不是沿整行做平滑渐变。
var logoLetters = [][]string{
	// S
	{
		"███████╗",
		"██╔════╝",
		"███████╗",
		"╚════██║",
		"███████║",
		"╚══════╝",
	},
	// I
	{
		"██╗",
		"██║",
		"██║",
		"██║",
		"██║",
		"╚═╝",
	},
	// Z
	{
		"███████╗",
		"╚══███╔╝",
		"  ███╔╝ ",
		" ███╔╝  ",
		"███████╗",
		"╚══════╝",
	},
	// H
	{
		"██╗  ██╗",
		"██║  ██║",
		"███████║",
		"██╔══██║",
		"██║  ██║",
		"╚═╝  ╚═╝",
	},
	// O
	{
		" ██████╗ ",
		"██╔═══██╗",
		"██║   ██║",
		"██║   ██║",
		"╚██████╔╝",
		" ╚═════╝ ",
	},
	// U
	{
		"██╗   ██╗",
		"██║   ██║",
		"██║   ██║",
		"██║   ██║",
		"╚██████╔╝",
		" ╚═════╝ ",
	},
}

const (
	logoCompactText  = "SIZHOU"
	logoWideMinWidth = 52
)

type rgb struct{ r, g, b int }

func parseHexColor(value string) rgb {
	digits := strings.TrimPrefix(value, "#")
	if len(digits) != 6 {
		return rgb{255, 255, 255}
	}
	out := rgb{}
	for index, channel := range []*int{&out.r, &out.g, &out.b} {
		parsed, ok := parseHexByte(digits[index*2 : index*2+2])
		if !ok {
			return rgb{255, 255, 255}
		}
		*channel = parsed
	}
	return out
}

func parseHexByte(value string) (int, bool) {
	var out int
	for _, c := range value {
		d := hexDigit(c)
		if d < 0 {
			return 0, false
		}
		out = out*16 + d
	}
	return out, true
}

func hexDigit(c rune) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return -1
}

func clampByte(value int) int {
	if value < 0 {
		return 0
	}
	if value > 255 {
		return 255
	}
	return value
}

// gradientColor 在 from→to 之间按 position(0..1) 插值，再整体乘以 ratio 调暗。
// 主要用于输入框左侧竖条的青→蓝渐变。
func gradientColor(from, to rgb, position, ratio float64) string {
	mix := func(a, b int) int {
		return int(float64(a) + (float64(b)-float64(a))*position)
	}
	scale := func(channel int) int {
		return clampByte(int(float64(channel) * ratio))
	}
	return fmt.Sprintf("#%02X%02X%02X",
		scale(mix(from.r, to.r)),
		scale(mix(from.g, to.g)),
		scale(mix(from.b, to.b)),
	)
}

// paintLogo 给 SIZHOU 上色：前两个字母（S、I）用 from 色，后四个（Z、H、O、U）用 to 色。
// 字母之间留 1 列空格；黑白模式下所有字母退化为 faint。
func paintLogo(letters [][]string, from, to string, monochrome bool) string {
	n := len(letters)
	if n == 0 {
		return ""
	}
	rows := len(letters[0])

	// 每个字母的统一定宽（取该字母各行最大宽度），不足右侧补空格。
	widths := make([]int, n)
	for li := range letters {
		for _, row := range letters[li] {
			if w := utf8.RuneCountInString(row); w > widths[li] {
				widths[li] = w
			}
		}
	}

	out := make([]string, rows)
	for ri := 0; ri < rows; ri++ {
		var line strings.Builder
		for li := 0; li < n; li++ {
			if li > 0 {
				line.WriteString(" ")
			}
			color := from
			if li >= 2 {
				color = to
			}
			row := letters[li][ri]
			var cell strings.Builder
			for _, r := range row {
				if r == ' ' {
					cell.WriteRune(' ')
					continue
				}
				style := lipgloss.NewStyle()
				if monochrome {
					style = style.Faint(true)
				} else {
					style = style.Foreground(lipgloss.Color(color))
				}
				cell.WriteString(style.Render(string(r)))
			}
			pad := widths[li] - utf8.RuneCountInString(row)
			if pad > 0 {
				cell.WriteString(strings.Repeat(" ", pad))
			}
			line.WriteString(cell.String())
		}
		out[ri] = line.String()
	}
	return strings.Join(out, "\n")
}
