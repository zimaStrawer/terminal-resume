package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// resumeBarRow 找出进度条那一行，并单独切出条体。
// 行形如 `[██████████████████            ] 等待简历下载中`——
// 标签跟在方括号后面，算宽度、查字形时都必须把它排除掉。
func resumeBarRow(text string) (line string, track string) {
	for _, raw := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(raw)
		if !strings.HasPrefix(trimmed, "[") {
			continue
		}
		end := strings.Index(trimmed, "]")
		if end < 0 {
			continue
		}
		return trimmed, trimmed[:end+1]
	}
	return "", ""
}

// TestResumePendingStateWhenNoURL 覆盖简历 PDF 还没准备好时的占位。
//
// 背景：模板里原本写的是 https://zhang.design/resume.pdf，
// 域名根本不存在——访客点了只会 404，比不显示更糟。
//
// ⚠️ 「不能出现链接」这条只能对**简历这一段**断言，不能对整页断言：
// 联系页本来就带 WEBSITE（https://…）和 EMAIL（hello@zhang.design），
// 拿整页去查 "http" / "zhang.design" 一定会误伤。
func TestResumePendingStateWhenNoURL(t *testing.T) {
	m := testModel(t)
	// 显式清空，让这条测试只验逻辑，不受 resume.yaml 当前内容影响。
	m.resume.Profile.ResumeURL = ""

	block := ansi.Strip(m.resumePendingBlock())
	for _, stale := range []string{"http", "zhang.design", "resume.pdf"} {
		if strings.Contains(block, stale) {
			t.Fatalf("简历占位里仍残留链接 %q：\n%s", stale, block)
		}
	}
	if !strings.Contains(block, resumePendingHeadline) {
		t.Fatalf("占位里没有 %q：\n%s", resumePendingHeadline, block)
	}
	if !strings.Contains(block, resumePendingNote) {
		t.Fatalf("占位里没有 %q：\n%s", resumePendingNote, block)
	}

	line, track := resumeBarRow(block)
	if line == "" {
		t.Fatalf("没有找到进度条那一行：\n%s", block)
	}
	// 宽度必须恒定：进度条一长一短会把整段回答排版带歪。
	if width := ansi.StringWidth(track); width != resumeBarWidth+2 {
		t.Fatalf("进度条宽 %d 列（%q），want %d（含两侧方括号）", width, track, resumeBarWidth+2)
	}
	if filled := strings.Count(track, "█"); filled != resumeBarFilled {
		t.Fatalf("实心格 %d 个，want %d", filled, resumeBarFilled)
	}
	// 标签要挂在进度条同一行右侧。
	if !strings.Contains(line, resumePendingHeadline) {
		t.Fatalf("进度条那一行没有标签 %q：%q", resumePendingHeadline, line)
	}
	// 说明要在进度条上方，读起来才是「先解释、后进度」。
	if strings.Index(block, resumePendingNote) > strings.Index(block, resumePendingHeadline) {
		t.Fatal("说明文案排在进度条下方了")
	}

	// 再确认它确实接进了 /resume 这条命令的回复里。
	m.submit("/resume")
	m.finishStreaming()
	if got := answer(t, m); !strings.Contains(got, resumePendingHeadline) {
		t.Fatalf("/resume 的回复里没有占位：\n%s", got)
	}
}

// TestResumeLinkReturnsWhenURLLoaded 锁死「数据驱动」这一点：
// PDF 上线后只改 resume.yaml，一行代码都不用动，占位就该自动换回可点击的下载链接。
func TestResumeLinkReturnsWhenURLLoaded(t *testing.T) {
	m := testModel(t)
	m.resume.Profile.ResumeURL = "https://example.com/resume.pdf"

	m.submit("/resume")
	m.finishStreaming()

	got := answer(t, m)
	if !strings.Contains(got, "https://example.com/resume.pdf") {
		t.Fatalf("填了 resume_url 却没渲染下载链接：\n%s", got)
	}
	if strings.Contains(got, resumePendingHeadline) {
		t.Fatalf("填了 resume_url 还在显示占位：\n%s", got)
	}
	if _, track := resumeBarRow(got); track != "" {
		t.Fatalf("填了 resume_url 却还画着进度条：\n%s", got)
	}
}

// TestResumePendingBarUsesOnlySafeGlyphs 保护字形选择：
// 进度条只准用 █ 和空格。▏▎▍▌▋▊▉ 与 ░▒▓ 这一系列在 Unicode 里都是
// East Asian Width = Ambiguous，偏好 CJK 字体的终端会把它们排成两列，
// 整行就会歪掉——本项目已经为这类字形踩过一次坑。
func TestResumePendingBarUsesOnlySafeGlyphs(t *testing.T) {
	m := testModel(t)
	m.resume.Profile.ResumeURL = ""

	_, track := resumeBarRow(ansi.Strip(m.resumePendingBlock()))
	if track == "" {
		t.Fatal("没画进度条")
	}
	for _, r := range track {
		switch r {
		case '█', ' ', '[', ']':
		default:
			t.Fatalf("进度条里出现了未列入白名单的字符 %q（U+%04X）：%q", r, r, track)
		}
	}
}
