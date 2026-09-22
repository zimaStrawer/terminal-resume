package app

import (
	"context"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/zhangsizhou/terminal-resume/internal/ai"
	"github.com/zhangsizhou/terminal-resume/profile"
)

func testModel(t *testing.T) Model {
	t.Helper()
	resume, err := profile.Load("")
	if err != nil {
		t.Fatal(err)
	}
	m := New(resume, true)
	// 单测绝不真的打网络：装一个空剧本的假后端。
	// New 默认指向 profile.website（即作品集站），真实请求只在运行时发生。
	m.aiClient = &fakeAI{}
	m.aiBase = "https://example.test/api/chat"
	return m
}

// testColorModel 返回**彩色主题**下的模型。
// testModel 用的是黑白主题（第二种样式），凡是断言主题色（竖线那一格的底色、
// 下拉框选中色等）的测试都要用它，否则拿彩色主题的色值去比对黑白主题的渲染必然对不上。
func testColorModel(t *testing.T) Model {
	t.Helper()
	m := testModel(t)
	m.styles = newStyles(false)
	return m
}

// answer 返回对话流里最后一条回答的纯文本（去掉 ANSI）。
func answer(t *testing.T, m Model) string {
	t.Helper()
	if len(m.chat) == 0 {
		t.Fatal("对话流为空，期望已经有回答")
	}
	return ansi.Strip(m.chat[len(m.chat)-1].text)
}

// rowOf 返回第一行包含 needle 的行号，找不到返回 -1。
// logoNeedle 是只可能出现在 logo 里的字样。
//
// ⚠️ 别退回裸 "██"：简历那条「等待简历下载中」的进度条也是用 █ 拼的，
// 拿 "██" 去找 logo 会先撞上进度条，把「logo 已经滚出屏幕」误判成「还在屏幕上」。
// logo 用的是 ANSI Shadow 字形，带 ╗、╔ 这类制表符，进度条则只有 █ 和空格。
const logoNeedle = "██╗"

func rowOf(lines []string, needle string) int {
	for index, line := range lines {
		if strings.Contains(ansi.Strip(line), needle) {
			return index
		}
	}
	return -1
}

// boxRows 返回输入框那几行的行号。单行输入区 + 上下各一行内边距 = 3 行；
// 行数不是 3 说明它被压扁或被撑开了。
func boxRows(lines []string) []int {
	rows := []int{}
	for index, line := range lines {
		if isBoxRow(line) {
			rows = append(rows, index)
		}
	}
	return rows
}

// isBoxRow 判断一行是不是输入框的一行：框行靠左右两个竖条格辨认
// （2026-09-20 起框内铺 colorInputSurface 底色，但底色不是可靠标识——
// 对话流里别的组件也可能带背景色，而全帧渲染里只有输入框会写 dialogEdgeLeft 字形）。
//
// ⚠️ 必须先 ansi.Strip 再数字形——竖条是「字形 + 前景色/背景色」，Strip 不影响字形本身。
// 彩色主题下线走终端主题色、黑白主题走近白，字形在两种主题下一致，探针不挑主题。
// ⚠️ 字形一律引用 dialogEdgeLeft 常量，别写字面量：换字形时才能一处改全（见 box.go）。
func isBoxRow(line string) bool {
	return strings.Count(ansi.Strip(line), dialogEdgeLeft) == 2
}

// dialogEdgeSeq 是**彩色主题**下竖线格子的样式序列（含前景色与面板底色）——
// 细线由字形墨迹画，颜色来自 boxEdge（见 box.go 的 dialogEdgeLeft 与 styles.go 的 boxEdge）。
// 只有断言竖线本体的测试会用到它，那几条都显式按彩色主题建模型
// （黑白主题下这条线走近白色，见 newStyles 的 monochrome 分支）。
func dialogEdgeSeq() string {
	probe := newStyles(false).boxEdge.Render(" ")
	if index := strings.IndexByte(probe, ' '); index > 0 {
		return probe[:index]
	}
	return probe
}

// boxTopRow 返回输入框第一行，用于「A 在输入框上方」这类比较。
func boxTopRow(lines []string) int {
	rows := boxRows(lines)
	if len(rows) == 0 {
		return -1
	}
	return rows[0]
}

// boxRow 返回输入框里那一行**输入区**（三行里的中间那行）。
func boxRow(lines []string) int {
	rows := boxRows(lines)
	if len(rows) != 3 {
		return -1
	}
	return rows[1]
}

// TestMenuMatchesAgreedCopy 固定 / 菜单的四条文案（用户 2026-09-19 指定，勿改）。
func TestMenuMatchesAgreedCopy(t *testing.T) {
	want := []menuItem{
		{command: "/skills", label: "explore my design & technical toolkit"},
		{command: "/resume", label: "download my latest resume"},
		{command: "/log", label: "browse recent builds & learning notes"},
		{command: "/personal", label: "discover the person behind the work"},
	}
	m := testModel(t)
	got := m.menuItems()
	if len(got) != len(want) {
		t.Fatalf("menu items = %d, want %d", len(got), len(want))
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("menu item %d = %+v, want %+v", index, got[index], want[index])
		}
	}
}

// TestPersonalCommandAnswersAbout 确保 /personal 这条新快捷命令有内容可答。
func TestPersonalCommandAnswersAbout(t *testing.T) {
	m := testModel(t)
	m.submit("/personal")
	if got := answer(t, m); !strings.Contains(got, "ABOUT") {
		t.Fatalf("answer = %q, want 关于我", got)
	}
}

// TestGreetingSitsBetweenLogoAndPromptBox 确保收尾文案落在 logo 与输入框之间：
// logo < 感谢看到最后！< Design Without Boundaries < 下拉框 < 输入框。
func TestGreetingSitsBetweenLogoAndPromptBox(t *testing.T) {
	m := testModel(t)
	m.width = 100
	m.height = 30
	m.recalculateLayout()
	m.menuOpen = true

	lines := strings.Split(m.renderHomeLayout(m.viewport.Width()), "\n")
	rows := map[string]int{
		"logo":     rowOf(lines, logoNeedle),
		"headline": rowOf(lines, greetingHeadline),
		"tagline":  rowOf(lines, greetingTagline),
		"menu":     rowOf(lines, "/skills"),
		"box":      boxTopRow(lines),
	}
	for name, row := range rows {
		if row == -1 {
			t.Fatalf("%s row not found", name)
		}
	}
	if !(rows["logo"] < rows["headline"] && rows["headline"] < rows["tagline"] &&
		rows["tagline"] < rows["menu"] && rows["menu"] < rows["box"]) {
		t.Fatalf("unexpected order: logo=%d headline=%d tagline=%d menu=%d box=%d",
			rows["logo"], rows["headline"], rows["tagline"], rows["menu"], rows["box"])
	}
}

// TestGreetingScrollsUpWithLogo 确保收尾文案**不常驻**在输入框上方：
// 它紧跟在 logo 之后，会被对话一路往上顶，答案攒够了就和 logo 一起离屏。
// 输入框位置在任何时候都不动。
func TestGreetingScrollsUpWithLogo(t *testing.T) {
	m := testModel(t)
	m.width = 100
	m.height = 30
	m.recalculateLayout()
	placeholder := m.input.Placeholder

	empty := strings.Split(m.renderHomeLayout(m.viewport.Width()), "\n")
	emptyGreeting := rowOf(empty, greetingTagline)
	emptyBox := rowOf(empty, placeholder)
	if emptyGreeting == -1 || emptyBox == -1 {
		t.Fatalf("empty state: greeting=%d box=%d, both should exist", emptyGreeting, emptyBox)
	}
	if emptyGreeting >= emptyBox {
		t.Fatalf("empty state: greeting(%d) should sit above the prompt box(%d)", emptyGreeting, emptyBox)
	}

	// 一条回答之后：收尾文案被推到回答上方，而不是继续贴着输入框。
	m.submit("/skills")
	m.finishStreaming()
	one := strings.Split(m.renderHomeLayout(m.viewport.Width()), "\n")
	greeting := rowOf(one, greetingTagline)
	answer := rowOf(one, "ENGINEERING")
	if greeting == -1 || answer == -1 {
		t.Fatalf("one answer: greeting=%d answer=%d, both should exist", greeting, answer)
	}
	if greeting >= answer {
		t.Fatalf("one answer: greeting(%d) should be pushed above the answer(%d)", greeting, answer)
	}
	if got := rowOf(one, placeholder); got != emptyBox {
		t.Fatalf("prompt box moved after one answer: %d -> %d", emptyBox, got)
	}

	// 再攒几条：收尾文案与 logo 应该一起被顶出屏幕。
	for _, command := range []string{"/log", "/resume", "/status"} {
		m.submit(command)
		m.finishStreaming()
	}
	full := strings.Split(m.renderHomeLayout(m.viewport.Width()), "\n")
	if got := rowOf(full, greetingTagline); got != -1 {
		t.Fatalf("greeting tagline still on screen at row %d after several answers", got)
	}
	if got := rowOf(full, greetingHeadline); got != -1 {
		t.Fatalf("greeting headline still on screen at row %d after several answers", got)
	}
	if got := rowOf(full, logoNeedle); got != -1 {
		t.Fatalf("logo still on screen at row %d after several answers", got)
	}
	if got := rowOf(full, placeholder); got != emptyBox {
		t.Fatalf("prompt box moved after several answers: %d -> %d", emptyBox, got)
	}
}

func TestChineseAliasAnswersWithProjects(t *testing.T) {
	m := testModel(t)
	m.submit("/项目")
	if got := answer(t, m); !strings.Contains(got, "精选项目") {
		t.Fatalf("answer = %q, want 项目列表", got)
	}
}

func TestOpenProject(t *testing.T) {
	m := testModel(t)
	m.submit("project 02")
	if got := answer(t, m); !strings.Contains(got, "城市跑腿服务重设计") {
		t.Fatalf("answer = %q, want 项目详情", got)
	}
}

func TestUnknownCommandHasRecoverySuggestion(t *testing.T) {
	m := testModel(t)
	m.submit("porjects")
	if m.noticeKind != noticeError || !strings.Contains(m.notice, "projects") {
		t.Fatalf("notice = %q, kind = %v", m.notice, m.noticeKind)
	}
}

func TestResponsiveHomeHasCompactIdentity(t *testing.T) {
	m := testModel(t)
	m.width = 42
	m.height = 18
	m.recalculateLayout()
	m.refreshContent()
	page := m.renderViewportContent()
	if !strings.Contains(page, "SIZHOU") {
		t.Fatal("compact home identity is missing")
	}
}

func TestTallHomeDoesNotReserveBlankViewportRows(t *testing.T) {
	m := testModel(t)
	m.width = 100
	m.height = 60
	m.recalculateLayout()
	m.refreshContent()

	availableHeight := m.height - 5
	if got := m.viewport.Height(); got >= availableHeight {
		t.Fatalf("viewport height = %d, want less than available height %d for short content", got, availableHeight)
	}
}

// TestMenuRendersAbovePromptBox 确保输入 / 后命令菜单出现在输入框**上方**，
// 输入框因此可以严格钉在底部、位置不随菜单开合移动。
func TestMenuRendersAbovePromptBox(t *testing.T) {
	m := testModel(t)
	m.width = 100
	m.height = 30
	m.recalculateLayout()
	m.menuOpen = true
	m.refreshContent()

	lines := strings.Split(m.renderHomeLayout(m.viewport.Width()), "\n")
	logoRow := rowOf(lines, logoNeedle)
	menuRow := rowOf(lines, "/skills")
	box := boxTopRow(lines)
	if logoRow == -1 || box == -1 || menuRow == -1 {
		t.Fatalf("home layout rows not found: logo=%d box=%d menu=%d", logoRow, box, menuRow)
	}
	if !(logoRow < menuRow && menuRow < box) {
		t.Fatalf("expected logo(%d) < menu(%d) < prompt box(%d)", logoRow, menuRow, box)
	}
}

// TestMenuDoesNotMovePromptBox 确保菜单开合不会移动输入框（始终钉在底部）。
func TestMenuDoesNotMovePromptBox(t *testing.T) {
	rowFor := func(m Model) int {
		return boxRow(strings.Split(m.renderHomeLayout(m.viewport.Width()), "\n"))
	}

	m := testModel(t)
	m.width = 100
	m.height = 30
	m.recalculateLayout()
	m.refreshContent()
	closed := rowFor(m)
	if closed == -1 {
		t.Fatal("closed state: prompt box not found")
	}
	m.menuOpen = true
	if open := rowFor(m); open != closed {
		t.Fatalf("prompt box moved when menu opened: %d -> %d", closed, open)
	}
}

// TestLongContentUsesChatScrolling 长内容不靠独立页面 + viewport 滚动，
// 而是走对话流：内容超出可见区时，PgUp 才推得动 chatOffset。
// （2026-09-20 独立页面机器删除后，原来的 changeScreen(screenAbout) 写法已不可用。）
func TestLongContentUsesChatScrolling(t *testing.T) {
	m := testModel(t)
	m.width = 80
	m.height = 24
	m.recalculateLayout()
	m.refreshContent()

	m.submit("/experience")
	m.finishStreaming()

	metrics := m.homeMetrics()
	total := len(m.conversationLines(metrics.contentWidth))
	if total <= metrics.conversation {
		t.Fatalf("前置条件：内容应当超出可见区（共 %d 行，可见 %d 行）", total, metrics.conversation)
	}

	m, _ = sendMsg(m, tea.KeyPressMsg{Code: tea.KeyPgUp})
	if m.chatOffset == 0 {
		t.Fatal("内容超出可见区时 PgUp 应当能往回翻")
	}
}

func TestCommandSuggestionsIncludeProjects(t *testing.T) {
	m := testModel(t)
	if !m.HasCommandSuggestion("/project 03") {
		t.Fatal("project 03 suggestion is missing")
	}
}

// 原来的 TestHeaderDoesNotAssumeSSHConnection 随 renderHeader 一起删掉了：
// 顶部 header（「ZHANG / ABOUT」+「TUI SESSION」徽章）属于独立页面那套布局，
// 现在整个 TUI 只有一块屏、根本不渲染 header，那条断言没有对象了。

func TestLogoGlyphsKeepExpectedShape(t *testing.T) {
	if got := len(logoLetters); got != 6 {
		t.Fatalf("logo letters = %d, want 6", got)
	}
	for i, letter := range logoLetters {
		if got := len(letter); got != 6 {
			t.Fatalf("letter %d rows = %d, want 6", i, got)
		}
	}
}

func TestGradientColorEndpoints(t *testing.T) {
	from := parseHexColor(colorAccent)
	to := parseHexColor(colorSuccess)
	if got := gradientColor(from, to, 0, 1); got != colorAccent {
		t.Fatalf("gradientColor(t=0) = %s, want %s (colorAccent)", got, colorAccent)
	}
	if got := gradientColor(from, to, 1, 1); got != "#4ADE80" {
		t.Fatalf("gradientColor(t=1) = %s, want #4ADE80", got)
	}
	// 半亮度期望值随 colorAccent 变（各通道折半：#00BBF9 → #005D7C）。
	if got := gradientColor(from, to, 0, 0.5); got != "#005D7C" {
		t.Fatalf("gradientColor(t=0,ratio=0.5) = %s, want #005D7C", got)
	}
}

func TestPaintedLogoTwoTone(t *testing.T) {
	painted := paintLogo(logoLetters, "#A6AEBB", "#FFFFFF", false)
	if got := strings.Count(painted, "\n") + 1; got != 6 {
		t.Fatalf("painted logo lines = %d, want 6", got)
	}
	// lipgloss 以 truecolor 形式（38;2;r;g;b）输出颜色，
	// SI 用银色(166;174;187)、ZHOU 用近白(255;255;255)，两种都应出现。
	if !strings.Contains(painted, "166;174;187") {
		t.Fatalf("painted logo missing silver (166;174;187) for SI")
	}
	if !strings.Contains(painted, "255;255;255") {
		t.Fatalf("painted logo missing white (255;255;255) for ZHOU")
	}
}

func TestStatusLogRandomAnswers(t *testing.T) {
	cases := map[string]string{
		"/status": "STATUS",
		"/log":    "LOG",
		"/random": "RANDOM",
		"/状态":     "STATUS",
		"/日志":     "LOG",
		"/彩蛋":     "RANDOM",
	}
	for command, want := range cases {
		m := testModel(t)
		m.submit(command)
		if got := answer(t, m); !strings.Contains(got, want) {
			t.Fatalf("submit(%q) answer = %q, want 含 %q", command, got, want)
		}
	}
}

func TestLogAnswerShowsLatestEntry(t *testing.T) {
	m := testModel(t)
	m.submit("/log")
	got := answer(t, m)
	for _, want := range []string{"Connected AI chat to terminal", "09.18"} {
		if !strings.Contains(got, want) {
			t.Fatalf("log answer is missing %q", want)
		}
	}
}

func TestStatusAnswerShowsHeadlineAndFocus(t *testing.T) {
	m := testModel(t)
	m.submit("/status")
	got := answer(t, m)
	for _, want := range []string{"正在寻找设计工程师与 UX 设计机会", "TARGET ROLES", "设计到代码的自动化工作流"} {
		if !strings.Contains(got, want) {
			t.Fatalf("status answer is missing %q", want)
		}
	}
}

// TestMenuCommandsProduceAnswers 确保每个快捷命令都能在对话流里产生一条回答。
func TestMenuCommandsProduceAnswers(t *testing.T) {
	m := testModel(t)
	for _, item := range m.menuItems() {
		next := testModel(t)
		next.submit(item.command)
		if len(next.chat) == 0 {
			t.Fatalf("menu command %q produced no answer", item.command)
		}
		if body := answer(t, next); strings.TrimSpace(body) == "" {
			t.Fatalf("menu command %q produced an empty answer", item.command)
		}
	}
}

func TestMenuRowsReserveViewportSpace(t *testing.T) {
	m := testModel(t)
	if m.menuRows() != 0 {
		t.Fatalf("menuRows() = %d when closed, want 0", m.menuRows())
	}
	m.menuOpen = true
	if want := len(m.menuItems()); m.menuRows() != want {
		t.Fatalf("menuRows() = %d, want %d", m.menuRows(), want)
	}
	m.closeMenu()
	if m.menuRows() != 0 {
		t.Fatalf("menuRows() = %d after closeMenu, want 0", m.menuRows())
	}
}

func TestEmptyCommandShowsMenuHint(t *testing.T) {
	m := testModel(t)
	m.submit("/")
	if m.noticeKind != noticeInfo || !strings.Contains(m.notice, "/") {
		t.Fatalf("notice = %q, kind = %v", m.notice, m.noticeKind)
	}
}

func TestRuneCountSupportsChinese(t *testing.T) {
	if got := RuneCount("设计工程师"); got != 5 {
		t.Fatalf("RuneCount() = %d, want 5", got)
	}
}

// TestDialogBoxHasSurfaceFill 锁住框区的灰面板设计（2026-09-20 用户要求对齐
// OpenCode 截图样式，推翻了 2026-09-19 的「不上色」裁定）：
// 彩色主题下输入框每一行都必须铺 colorInputSurface 背景；黑白主题保持不上色。
// 曾经的「IME 组词灰块」代价已通过页面底色 #0D0D0D（与面板只差 Δ19）消解，
// 见 styles.go colorBackground 与 box.go renderDialog。
// 框行 = 左右两个主题色竖条 + 铺底色的中间区域；框内只保留一行输入区（上下各一行内边距）。
func TestDialogBoxHasSurfaceFill(t *testing.T) {
	m := testColorModel(t)
	m.width = 100
	m.height = 30
	m.recalculateLayout()
	m.refreshContent()

	// 面板底色 #201E1E 的 SGR 背景序列。
	const surfaceSeq = "48;2;32;30;30"

	box := m.renderPromptBox(72)
	lines := strings.Split(box, "\n")
	if len(lines) != 3 {
		t.Fatalf("box lines = %d, want 3 (1 padding + 1 content + 1 padding)", len(lines))
	}
	for index, line := range lines {
		if !strings.Contains(line, surfaceSeq) {
			t.Fatalf("box line %d lost the surface fill (%s):\n%q", index, colorInputSurface, line[:120])
		}
	}

	// 文字格也不能露黑：嵌套内容（textinput、提示字符）内部的每个样式闭合
	// 都会清掉底色，renderDialog 必须在每个 reset 后重新打开面板底色
	// （见 box.go）。断言：框行内部（去掉左右竖线格）的每个 \x1b[m 后面
	// 都紧跟面板底色序列。
	colorStyles := newStyles(false)
	probe := colorStyles.inputSurface.Render(" ")
	bgReopen := probe[:strings.IndexByte(probe, ' ')]
	leftEdge := colorStyles.boxEdge.Render(dialogEdgeLeft)
	rightEdge := colorStyles.boxEdge.Render(dialogEdgeRight)
	for index, line := range lines {
		interior := strings.TrimSuffix(strings.TrimPrefix(line, leftEdge), rightEdge)
		if got, want := strings.Count(interior, "\x1b[m"), strings.Count(interior, "\x1b[m"+bgReopen); got != want {
			t.Fatalf("box line %d has %d bare resets (want each followed by the surface bg, %d):\n%q",
				index, got-want, want, line[:160])
		}
	}

	// 黑白主题不铺底色（保持纯黑白）。
	mono := testModel(t)
	mono.width = 100
	mono.height = 30
	mono.recalculateLayout()
	mono.refreshContent()
	for index, line := range strings.Split(mono.renderPromptBox(72), "\n") {
		if strings.Contains(line, "48;2;") || strings.Contains(line, "48;5;") {
			t.Fatalf("monochrome box line %d carries a background color:\n%q", index, line[:120])
		}
	}
}

// TestPromptBoxSingleCenteredRow 锁住输入框的新形态：框内只有一行输入区，
// 该行落在面板正中间（上下内边距相等），且框内不再出现旧的两行快捷键提示。
func TestPromptBoxSingleCenteredRow(t *testing.T) {
	m := testColorModel(t)
	m.width = 100
	m.height = 30
	m.recalculateLayout()
	m.refreshContent()

	lines := strings.Split(m.renderPromptBox(72), "\n")
	contentRows := []int{}
	for index, line := range lines {
		if isBoxRow(line) {
			contentRows = append(contentRows, index)
		}
	}
	if len(contentRows) != 3 {
		t.Fatalf("box rows = %v, want exactly 3 (each with a left and a right edge)", contentRows)
	}
	// 每行都必须被左右两条细竖线夹住：恰好两段竖线前景色序列，中间不上色。
	for _, index := range contentRows {
		line := lines[index]
		if !isBoxRow(line) {
			t.Fatalf("box row %d is not an edge-framed row: %q", index, ansi.Strip(line))
		}
		if count := strings.Count(line, dialogEdgeSeq()); count != 2 {
			t.Fatalf("box row %d has %d edge cells, want 2 (左右各一条竖线): %q",
				index, count, ansi.Strip(line))
		}
	}
	row := contentRows[1]
	if above, below := row, len(lines)-1-row; above != below {
		t.Fatalf("input row %d not vertically centered in %d-line box (above=%d below=%d)",
			row, len(lines), above, below)
	}

	// 空态时应显示新的占位文案。
	if !strings.Contains(ansi.Strip(lines[row]), homePlaceholder) {
		t.Fatalf("placeholder %q missing from input row %q", homePlaceholder, ansi.Strip(lines[row]))
	}

	// 旧的框内快捷键提示应该整体消失。
	plain := ansi.Strip(strings.Join(lines, "\n"))
	for _, gone := range []string{"shortcuts   ", "ctrl+p", "ctrl+c"} {
		if strings.Contains(plain, gone) {
			t.Fatalf("stale in-box shortcut hint %q still rendered:\n%q", gone, plain)
		}
	}
}

// TestNoPersistentTipRow 确保输入框下方不再常驻提示文案（改由占位符承担），
// 但这一行本身要保留：否则 notice 出现/消失会让输入框上下跳。
func TestNoPersistentTipRow(t *testing.T) {
	m := testModel(t)
	m.width = 100
	m.height = 30
	m.recalculateLayout()
	m.menuOpen = true
	m.refreshContent()

	lines := strings.Split(m.renderHomeLayout(m.viewport.Width()), "\n")
	if got := rowOf(lines, "● Tip"); got != -1 {
		t.Fatalf("persistent tip row still rendered at row %d", got)
	}
	if got := rowOf(lines, "Type a question"); got != -1 {
		t.Fatalf("stale tip copy still rendered at row %d", got)
	}

	// 通知仍然要能显示，并且不能让输入框移位。
	rowFor := func(model Model) int {
		return boxRow(strings.Split(model.renderHomeLayout(model.viewport.Width()), "\n"))
	}
	before := rowFor(m)
	m.notice = "已经在首页。输入 /help 查看全部命令。"
	m.noticeKind = noticeInfo
	after := rowFor(m)
	if before == -1 || after != before {
		t.Fatalf("notice shifted the prompt box: %d -> %d", before, after)
	}
	rows := strings.Split(m.renderHomeLayout(m.viewport.Width()), "\n")
	if got := rowOf(rows, "已经在首页"); got == -1 || got <= after {
		t.Fatalf("notice row = %d, want below the prompt box %d", got, after)
	}
}

// TestDialogBoxEdgesHugThePanel 锁住竖条怎么画（2026-09-20 晚改定，参考 opencode 截图）：
// 线由**整格背景色**画成实心条（主题色铺满那一格），紧贴面板外缘；
// 灰面板严格夹在两条实心条之间 —— 就是用户要的「输入框放在两条蓝线的里面」。
//
// 为什么不是字形墨迹（详见 box.go 注释）：字形墨迹由字体决定，画不出「贴边 + 连续 +
// 满格」；而且 box-drawing 类的 │ 墨迹比格还高，会从框底漏出去显形（2026-09-20 用户
// 报的「下面还有旧的竖线露出来」）。整格背景色与字体无关，必然贴边、等高、连续无缝。
//
// 格内仍写 dialogEdgeLeft，但前景色 = 背景色（因此不可见）—— 留着这个字形是因为
// frameCursor（钉 IME 硬件光标）与 isBoxRow 都按它认框行，换成空格会让那批探针静默失效。
func TestDialogBoxEdgesHugThePanel(t *testing.T) {
	m := testColorModel(t)
	m.width = 100
	m.height = 30
	m.recalculateLayout()
	m.refreshContent()

	edge := dialogEdgeSeq()
	if edge == " " {
		t.Skip("no color profile in test environment")
	}
	if !strings.Contains(edge, "38;2;") {
		t.Fatalf("edge style = %q, want a truecolor foreground", edge)
	}
	// 新契约：线本体是**格子背景色**铺满整格，所以背景必须也是终端主题色。
	// 少了这条，线就退回成「字形墨迹浮在面板上」的老样子。
	if !strings.Contains(edge, "48;2;22;184;243") {
		t.Fatalf("edge style = %q, want the theme color as cell background (实心条)", edge)
	}

	lines := strings.Split(m.renderPromptBox(72), "\n")
	for index, line := range lines {
		// 每行都是「实心条 + 铺底色的输入区 + 实心条」：左右各一段，不多不少。
		if count := strings.Count(line, edge); count != 2 {
			t.Fatalf("box line %d has %d edge cells, want 2 (左右各一条竖线):\n%q",
				index, count, ansi.Strip(line))
		}
		// 线字符仍必须紧贴线色：保留这个字形是故意的（探针认框行用），
		// 只是前景色与背景色相同，所以看不见。
		plain := ansi.Strip(line)
		if !strings.HasPrefix(plain, dialogEdgeLeft) || !strings.HasSuffix(plain, dialogEdgeLeft) {
			t.Fatalf("box line %d does not start/end with the edge glyphs: %q", index, plain)
		}
		// 实心条与面板之间不能留缝：面板底色必须紧跟在竖线之后。
		if !strings.Contains(line, "48;2;32;30;30") {
			t.Fatalf("box line %d lost the surface fill next to the edges:\n%q", index, ansi.Strip(line))
		}
	}
}

// TestDialogEdgeCellsAreThinSingleColumn 锁住竖条那一格的内容与宽度（2026-09-20 晚改定）。
//
// 那一格必须恰好 1 列宽，否则整行宽度随主题漂移、把输入框挤歪。
// 格内写 dialogEdgeLeft：线本体由**背景色**画（整格实心），这个字形只是留给探针认框行
// 的锚点，前景色与背景色相同所以看不见 —— 详见 box.go 与 TestDialogBoxEdgesHugThePanel。
//
// 历史（别再反复）：2026-09-19 白天「空格 + 整格背景色」因太粗被否；晚间 │ 字形法
// （细、连续、等高）胜出，但它水平居中、浮在面板灰底上；2026-09-20 晚用户拿 opencode
// 截图判定「蓝线压在输入框上」不好看 —— 定为整格背景色实心条；当晚又被用户发现 │ 的
// 墨迹从框底漏出 3px，遂把占位字形从 │ 换成墨迹不出格的 ǀ（见 box.go 的候选实测表）。
func TestDialogEdgeCellsAreThinSingleColumn(t *testing.T) {
	for _, edge := range []string{dialogEdgeLeft, dialogEdgeRight} {
		if edge != dialogEdgeLeft {
			t.Fatalf("edge cell content = %q, want %q (占位字形，见 box.go)", edge, dialogEdgeLeft)
		}
		if width := ansi.StringWidth(edge); width != 1 {
			t.Fatalf("edge cell is %d columns wide, want 1", width)
		}
	}
	// 彩色主题下这一格的前景与背景**同为**终端主题色（RGB 22,184,243）：
	// 前景色 = 背景色 ⇒ 字形不可见，用户看到的就是整格实心条。
	styled := newStyles(false).boxEdge.Render(dialogEdgeLeft)
	if !strings.Contains(styled, "38;2;22;184;243") {
		t.Fatalf("edge style renders %q, want the theme color as glyph foreground", styled)
	}
	if !strings.Contains(styled, "48;2;22;184;243") {
		t.Fatalf("edge style renders %q, want the theme color as cell background (实心条)", styled)
	}
	// 黑白主题下不引色，走的是近白色（且不铺底色，仍是细线，保持纯黑白）。
	if mono := newStyles(true).boxEdge.Render(dialogEdgeLeft); strings.Contains(mono, "22;184;243") {
		t.Fatalf("monochrome edge leaked the theme color: %q", mono)
	}
}

// TestFrameCursorPinsHardwareCursorToCaret 锁死硬件光标定位（用户 2026-09-19 晚间报的 IME bug）。
//
// macOS 中文输入法的预编辑串（未上屏的拼音）由**终端**按硬件光标位置绘制；
// 不主动定位时拼音会画到输入框外面。修复方式：View() 每帧把硬件光标钉到输入光标处。
// 光标列必须按**显示格数**算（中文 1 rune 占 2 格），不能直接用 rune 数。
func TestFrameCursorPinsHardwareCursorToCaret(t *testing.T) {
	m := testColorModel(t)
	m.width = 100
	m.height = 30
	m.recalculateLayout()
	m.refreshContent()

	m.input.SetValue("他做过哪些项目？")
	m.input.CursorEnd()

	v := m.View()
	if v.Cursor == nil {
		t.Fatal("View().Cursor is nil, want the hardware cursor pinned inside the input box")
	}

	// 独立复算期望位置：输入行 = 带左右竖条字形的 3 行框里中间那行（按字形找）；
	// 列 = 行内左缘字形 + 5（1 边框 + 2 内边距 + 2 提示字符）+ 光标前文本的显示格数。
	content := m.renderHomeLayout(m.viewport.Width())
	lines := strings.Split(content, "\n")
	rows := boxRows(lines)
	if len(rows) != 3 {
		t.Fatalf("box rows = %v, want exactly 3 prompt-box rows", rows)
	}
	plain := ansi.Strip(lines[rows[1]])
	left := strings.Index(plain, dialogEdgeLeft)
	if left < 0 {
		t.Fatalf("input row %d has no left edge glyph: %q", rows[1], plain)
	}
	// View() 里整帧还会加左右留白：marginLeft = (终端宽 - 内容宽) / 2。
	marginLeft := max((m.width-m.viewport.Width())/2, 0)
	// 「他做过哪些项目？」= 8 个 CJK rune × 2 格 = 16 格。
	wantX := marginLeft + left + 5 + 16
	if v.Cursor.Y != rows[1] || v.Cursor.X != wantX {
		t.Fatalf("cursor at (%d,%d), want (%d,%d)", v.Cursor.X, v.Cursor.Y, wantX, rows[1])
	}
}

// TestFrameCursorCaretUsesDisplayCellsNotRunes 专门锁 CJK 的格数换算：
// 若有人把光标列改回 rune 数，「大帅」会偏左 2 格，IME 预编辑串又会画歪。
func TestFrameCursorCaretUsesDisplayCellsNotRunes(t *testing.T) {
	m := testColorModel(t)
	m.width = 100
	m.height = 30
	m.recalculateLayout()
	m.refreshContent()

	m.input.SetValue("大帅")
	m.input.CursorEnd()

	v := m.View()
	if v.Cursor == nil {
		t.Fatal("View().Cursor is nil, want the hardware cursor pinned inside the input box")
	}
	content := m.renderHomeLayout(m.viewport.Width())
	lines := strings.Split(content, "\n")
	rows := boxRows(lines)
	if len(rows) != 3 {
		t.Fatalf("box rows = %v, want exactly 3 prompt-box rows", rows)
	}
	plain := ansi.Strip(lines[rows[1]])
	left := strings.Index(plain, dialogEdgeLeft)
	marginLeft := max((m.width-m.viewport.Width())/2, 0)
	// 「大帅」= 2 rune 但 4 格；若按 rune 数会差 2 格。
	if want := marginLeft + left + 5 + 4; v.Cursor.X != want {
		t.Fatalf("cursor X = %d, want %d (光标列必须按显示格数，中文 1 rune 占 2 格)", v.Cursor.X, want)
	}
}

// TestCursorAnimationMatchesSiteSchedule 逐帧走完一整轮，锁死节奏与字符集，
// 与网页 AsciiTitle.astro 的 runBlink / runScramble 一一对应。
func TestCursorAnimationMatchesSiteSchedule(t *testing.T) {
	m := testModel(t)

	// 起始状态：显示基字符，不隐藏。
	if glyph, _ := m.cursorGlyph(); glyph != cursorBaseGlyph {
		t.Fatalf("initial glyph = %q, want %q", glyph, cursorBaseGlyph)
	}

	// 1) 闪烁 10 步 × 300ms：step 0 起为暗，之后一亮一暗。
	hidden := 0
	for step := 0; step < cursorBlinkSteps; step++ {
		if delay := m.advanceCursor(); delay != cursorBlinkInterval {
			t.Fatalf("blink step %d delay = %v, want %v", step, delay, cursorBlinkInterval)
		}
		glyph, _ := m.cursorGlyph()
		want := cursorBaseGlyph
		if step%2 == 0 {
			want = cursorHiddenGlyph
			hidden++
		}
		if glyph != want {
			t.Fatalf("blink step %d glyph = %q, want %q", step, glyph, want)
		}
	}
	if hidden != cursorBlinkSteps/2 {
		t.Fatalf("hidden frames = %d, want %d", hidden, cursorBlinkSteps/2)
	}

	// 2) 停一拍，转入乱序。
	if delay := m.advanceCursor(); delay != cursorBlinkToScrambleGap {
		t.Fatalf("blink→scramble gap = %v, want %v", delay, cursorBlinkToScrambleGap)
	}
	if m.cursorPhase != cursorScramble {
		t.Fatalf("phase = %v, want scramble", m.cursorPhase)
	}

	// 3) 乱序 16 帧 × 64ms，每帧 1 列宽、都是字符集里的字符，两轮各把 8 个字符走一遍。
	frames := len(cursorGlyphs) * cursorScrambleRounds
	seen := map[string]int{}
	for frame := 0; frame < frames; frame++ {
		if delay := m.advanceCursor(); delay != cursorScrambleFrame {
			t.Fatalf("scramble frame %d delay = %v, want %v", frame, delay, cursorScrambleFrame)
		}
		glyph, _ := m.cursorGlyph()
		if !slices.Contains(cursorGlyphs, glyph) {
			t.Fatalf("scramble frame %d glyph = %q, not in %v", frame, glyph, cursorGlyphs)
		}
		if width := ansi.StringWidth(glyph); width != 1 {
			t.Fatalf("scramble frame %d glyph %q width = %d, want 1 (会撑歪输入框)", frame, glyph, width)
		}
		seen[glyph]++
	}
	for _, glyph := range cursorGlyphs {
		if seen[glyph] != cursorScrambleRounds {
			t.Fatalf("glyph %q appeared %d times, want %d", glyph, seen[glyph], cursorScrambleRounds)
		}
	}

	// 4) 停一拍，回到闪烁的初始态。
	if delay := m.advanceCursor(); delay != cursorScrambleToBlinkGap {
		t.Fatalf("scramble→blink gap = %v, want %v", delay, cursorScrambleToBlinkGap)
	}
	if m.cursorPhase != cursorBlink || m.cursorStep != 0 {
		t.Fatalf("after a full loop phase=%v step=%d, want blink/0", m.cursorPhase, m.cursorStep)
	}
	if glyph, _ := m.cursorGlyph(); glyph != cursorBaseGlyph {
		t.Fatalf("glyph after a full loop = %q, want %q", glyph, cursorBaseGlyph)
	}
}

// TestCursorAnimationKeepsPromptBoxStill 动画跑一整轮，输入框的行号与列宽都不许动。
func TestCursorAnimationKeepsPromptBoxStill(t *testing.T) {
	m := testModel(t)
	m.width = 100
	m.height = 30
	m.recalculateLayout()

	lines := func() []string {
		return strings.Split(m.renderHomeLayout(m.viewport.Width()), "\n")
	}
	before := boxRow(lines())
	beforeWidth := ansi.StringWidth(m.renderPromptBox(72))

	for frame := 0; frame < 40; frame++ {
		m.advanceCursor()
		if got := boxRow(lines()); got != before {
			t.Fatalf("frame %d moved the prompt box: %d -> %d", frame, before, got)
		}
		if got := ansi.StringWidth(m.renderPromptBox(72)); got != beforeWidth {
			t.Fatalf("frame %d changed the box width: %d -> %d", frame, beforeWidth, got)
		}
	}
}

// TestPromptPlaceholderSwitchesWhenNarrow 窄屏换短句，宽屏回到整句（用户 2026-09-19 指定）。
func TestPromptPlaceholderSwitchesWhenNarrow(t *testing.T) {
	placeholderAt := func(cols int) string {
		m := testModel(t)
		m.width, m.height = cols, 24
		m.recalculateLayout()
		return m.input.Placeholder
	}

	if got := placeholderAt(120); got != homePlaceholder {
		t.Fatalf("wide terminal placeholder = %q, want the long one", got)
	}
	if got := placeholderAt(homePlaceholderMinWidth + 2); got != homePlaceholder {
		t.Fatalf("placeholder at %d cols = %q, want the long one", homePlaceholderMinWidth+2, got)
	}
	if got := placeholderAt(homePlaceholderMinWidth); got != homePlaceholderShort {
		t.Fatalf("placeholder at %d cols = %q, want the short one", homePlaceholderMinWidth, got)
	}
	if got := placeholderAt(50); got != homePlaceholderShort {
		t.Fatalf("narrow terminal placeholder = %q, want the short one", got)
	}

	// 拉宽终端后要自动换回整句（不能停在旧的那句上）。
	m := testModel(t)
	m.width, m.height = 50, 24
	m.recalculateLayout()
	if got := m.input.Placeholder; got != homePlaceholderShort {
		t.Fatalf("narrow placeholder = %q", got)
	}
	m.width = 120
	m.recalculateLayout()
	if got := m.input.Placeholder; got != homePlaceholder {
		t.Fatalf("placeholder after widening = %q, want the long one", got)
	}
}

// ---------- 首页「连按两次 Esc 退出」与输入框下方的常驻操作提示 ----------

// homeHintModel 返回一个宽屏首页模型：提示行有位置放整句文案。
func homeHintModel(t *testing.T) Model {
	t.Helper()
	m := testModel(t)
	m.width, m.height = 100, 30
	m.recalculateLayout()
	m.refreshContent()
	return m
}

// homeRows 渲染一次首页并切成行。
func homeRows(m Model) []string {
	return strings.Split(m.renderHomeLayout(m.viewport.Width()), "\n")
}

// sendMsg 投递一条消息，返回更新后的模型与命令。
func sendMsg(m Model, msg tea.Msg) (Model, tea.Cmd) {
	updated, cmd := m.Update(msg)
	return updated.(Model), cmd
}

// escKey 是「按一下 Esc」。
func escKey() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyEscape} }

// TestDoubleEscQuitsFromHome 覆盖用户 2026-09-20 指定的退出方式：
// 第一次 Esc 只**武装**并明确提示还差一下，窗口内再按一次才真退出。
func TestDoubleEscQuitsFromHome(t *testing.T) {
	m := homeHintModel(t)

	m, cmd := sendMsg(m, escKey())
	if !m.escArmed {
		t.Fatal("第一次 Esc 之后应当处于「待退出」状态")
	}
	if cmd == nil {
		t.Fatal("第一次 Esc 之后应当挂上超时命令，否则提示会一直挂在屏幕上")
	}
	if msg := cmd(); msg != (escTimeoutMsg{}) {
		t.Fatalf("第一次 Esc 的命令 = %T, want escTimeoutMsg", msg)
	}
	if rowOf(homeRows(m), homeHintArmed) == -1 {
		t.Fatalf("武装后提示行应当换成 %q", homeHintArmed)
	}

	m, cmd = sendMsg(m, escKey())
	if m.escArmed {
		t.Fatal("退出时应当把武装状态清掉")
	}
	if cmd == nil {
		t.Fatal("第二次 Esc 应当返回退出命令")
	}
	if msg := cmd(); msg != (tea.QuitMsg{}) {
		t.Fatalf("第二次 Esc 的命令 = %T, want tea.QuitMsg", msg)
	}
}

// TestSingleEscExpiresWithoutQuitting 只按一次不会退出：窗口过期后自动撤销武装，
// 提示行换回常态（否则那句「再按一次」会像卡住了一样一直挂着）。
func TestSingleEscExpiresWithoutQuitting(t *testing.T) {
	m := homeHintModel(t)
	m, _ = sendMsg(m, escKey())
	if !m.escArmed {
		t.Fatal("第一次 Esc 之后应当武装")
	}

	m, cmd := sendMsg(m, escTimeoutMsg{})
	if m.escArmed {
		t.Fatal("窗口过期后应当撤销武装")
	}
	if cmd != nil {
		t.Fatal("超时不该产生额外命令")
	}
	if rowOf(homeRows(m), homeHint) == -1 {
		t.Fatalf("过期后提示行应当换回 %q", homeHint)
	}
	if rowOf(homeRows(m), homeHintArmed) != -1 {
		t.Fatal("过期后不该还显示武装中的提示")
	}
}

// TestEscArmCancelledByAnyOtherKey 「Esc → 打字 → Esc」不能被算成连按两次。
func TestEscArmCancelledByAnyOtherKey(t *testing.T) {
	m := homeHintModel(t)
	m, _ = sendMsg(m, escKey())
	if !m.escArmed {
		t.Fatal("第一次 Esc 之后应当武装")
	}

	m, _ = sendMsg(m, tea.KeyPressMsg{Code: 'a', Text: "a"})
	if m.escArmed {
		t.Fatal("别的按键应当撤销武装")
	}
	if m.input.Value() == "" {
		t.Fatal("前置条件：字母应当写进输入框")
	}

	m.input.Reset()
	m, cmd := sendMsg(m, escKey())
	if !m.escArmed {
		t.Fatal("输入框清空后 Esc 应当重新武装")
	}
	// 没走退出分支的凭证：退出那条路会把 escArmed 清掉再返回 tea.Quit。
	// （不去执行 cmd()——那是 800ms 的 tea.Tick，会在测试里白等一拍。）
	if cmd == nil {
		t.Fatal("重新武装时应当挂上超时命令")
	}
}

// TestEscWhileBusyNeverArmsExit 正在打印 / 正在等后端时，Esc 仍然是「跳过 / 中止」，
// 绝不能顺手武装退出——用户等回答时习惯连按 Esc，误触代价太大。
func TestEscWhileBusyNeverArmsExit(t *testing.T) {
	m := homeHintModel(t)
	m.submit("/skills")
	if !m.streaming {
		t.Fatal("前置条件：/skills 应当进入流式打印")
	}
	m, _ = sendMsg(m, escKey())
	if m.streaming {
		t.Fatal("打印中按 Esc 应当跳过流式，把整段答案一次显示出来")
	}
	if m.escArmed {
		t.Fatal("打印中按 Esc 是「跳过」，不该武装退出")
	}

	m.aiClient = &fakeAI{}
	_ = m.submit("他在哪？")
	if !m.awaiting {
		t.Fatal("前置条件：提交自由提问后应当处于等待后端的状态")
	}
	m, _ = sendMsg(m, escKey())
	if m.awaiting || m.escArmed {
		t.Fatalf("等后端时按 Esc 只该中止生成：awaiting=%v armed=%v", m.awaiting, m.escArmed)
	}
}

// 原来的 TestEscExitIsHomeOnly 随独立页面机器一起删掉了：
// 它断言的是「非首页按 Esc 不参与连按退出」，而现在根本没有非首页——
// 整个 TUI 只有一块屏，Esc 的层级由 TestEscReturnsToLatestWhenReviewingHistory
// 和 TestEscLadderAbortsStreamThenQuits 覆盖。

// TestHomeHintSitsUnderPromptBox 锁住用户 2026-09-20 指定的位置与式样：
// 常驻操作提示在输入框下方、与框**隔一个空行**（贴太紧太挤，用户第二次调整），
// 左缘与输入框左竖线同列（「对话框的左下角」），样式参考 OpenCode 的提示行。
func TestHomeHintSitsUnderPromptBox(t *testing.T) {
	m := homeHintModel(t)
	lines := homeRows(m)

	box := boxRow(lines)
	hint := rowOf(lines, homeHint)
	if box == -1 || hint == -1 {
		t.Fatalf("box=%d hint=%d，两者都该存在：\n%s", box, hint, strings.Join(lines, "\n"))
	}
	// 框占 box-1 / box / box+1 三行，往下空一行才是提示行。
	if hint != box+3 {
		t.Fatalf("提示行 = %d, want %d（与输入框之间隔一个空行）", hint, box+3)
	}
	if gap := ansi.Strip(lines[box+2]); strings.TrimSpace(gap) != "" {
		t.Fatalf("输入框与提示行之间应当是空行，实际为 %q", gap)
	}

	// 输入框是居中渲染的：它左竖线所在的列就是提示行该缩进的列数。
	edge := strings.Index(ansi.Strip(lines[box]), dialogEdgeLeft)
	plain := ansi.Strip(lines[hint])
	indent := len(plain) - len(strings.TrimLeft(plain, " "))
	if indent != edge {
		t.Fatalf("提示行缩进 = %d, want %d（与输入框左缘对齐）", indent, edge)
	}
	// 提示行不能出现竖条字形，否则会被 isBoxRow 当成输入框的一行。
	if strings.Contains(plain, dialogEdgeLeft) {
		t.Fatalf("提示行不该出现竖线：%q", plain)
	}
}

// TestHomeHintTextIsSingleAndStable 2026-09-20 二次收窄：提示行**只留双击 Esc 这一条**，
// 原先的 `| ctrl+c 退出 | / 命令 | enter 发送` 全删。文案短到任何能用的宽度都放得下，
// 因此宽度变化不再切换文案（旧的「窄屏换短句」那套已随短句一起删掉）。
func TestHomeHintTextIsSingleAndStable(t *testing.T) {
	hintAt := func(cols int) string {
		m := testModel(t)
		m.width, m.height = cols, 24
		m.recalculateLayout()
		m.refreshContent()
		for _, line := range homeRows(m) {
			if plain := ansi.Strip(line); strings.Contains(plain, "退出") {
				return strings.TrimSpace(plain)
			}
		}
		return ""
	}
	for _, cols := range []int{120, 62, 50, 30} {
		if got := hintAt(cols); got != homeHint {
			t.Fatalf("%d 列时提示 = %q, want %q", cols, got, homeHint)
		}
	}
	for _, gone := range []string{"ctrl+c 退出", "/ 命令", "enter 发送"} {
		if got := hintAt(120); strings.Contains(got, gone) {
			t.Fatalf("提示行不该再出现 %q，实际 %q", gone, got)
		}
	}
}

// TestHomeHintRowDoesNotMovePromptBox 输入框下方的三行（间距空行 / 提示行 / notice 行）
// 是固定占位：chrome 总高度写死在 blockHeight 里，所以改文案（武装态、notice）
// 不能让输入框上下跳。注意 +3 是**有意**的（2026-09-20 加了间距空行，输入框整体上移一行）。
func TestHomeHintRowDoesNotMovePromptBox(t *testing.T) {
	m := homeHintModel(t)
	metrics := m.homeMetrics()
	// 输入框自身的高度：3 行（单行输入区 + 上下各一行内边距）。
	boxHeight := len(strings.Split(m.renderPromptBox(metrics.boxWidth), "\n"))
	if want := metrics.menuHeight + boxHeight + 3; metrics.blockHeight != want {
		t.Fatalf("blockHeight = %d, want %d（输入框 + 间距空行 + 提示行 + notice 行）", metrics.blockHeight, want)
	}

	before := boxRow(homeRows(m))
	m.escArmed = true // 武装态只是换文案，同样不许移位
	if after := boxRow(homeRows(m)); after != before {
		t.Fatalf("武装提示让输入框移动了：%d -> %d", before, after)
	}
	m.escArmed = false
	m.notice = "已选择项目 01，按 Enter 查看详情。"
	if after := boxRow(homeRows(m)); after != before {
		t.Fatalf("notice 让输入框移动了：%d -> %d", before, after)
	}
}

// TestEscReturnsToLatestWhenReviewingHistory 2026-09-20：输入框为空时，Esc 的第一层
// 语义是「回到最新」，下一层才是连按两次退出。
// 改前的写法是 scrollChat(+conversation)，等于按一下 Esc 就往回翻一屏 ——
// 想退出却只按了一下的人，会看到画面莫名其妙被顶走，而且 /help 写的是「返回上一级」，
// 跟实现完全对不上。
func TestEscReturnsToLatestWhenReviewingHistory(t *testing.T) {
	m := homeHintModel(t)
	m.submit("/skills") // 攒够内容，PgUp 才有得翻
	m.finishStreaming()
	m, _ = sendMsg(m, tea.KeyPressMsg{Code: tea.KeyPgUp})
	if m.chatOffset == 0 {
		t.Fatal("前置条件：PgUp 之后应当处于回看历史的状态")
	}

	m, cmd := sendMsg(m, escKey())
	if m.chatOffset != 0 {
		t.Fatalf("Esc 应当回到最新：chatOffset = %d, want 0", m.chatOffset)
	}
	if m.escArmed {
		t.Fatal("回到最新这一下不该顺手武装退出")
	}
	if cmd != nil {
		t.Fatalf("回到最新不该产生命令，got %T", cmd)
	}

	m, cmd = sendMsg(m, escKey())
	if !m.escArmed {
		t.Fatal("已经在最新时按 Esc 才轮到武装退出")
	}
	if cmd == nil {
		t.Fatal("武装时应当挂上超时命令")
	}
}

// ctxProbeAI 只关心一件事：交给它的 context 有没有被取消。
// 退出时若不取消在途请求，本地看不出来（进程直接没了），
// 但 SSH 模式下服务端进程常驻，那条流会一直挂到上游自己结束。
type ctxProbeAI struct{ ready chan context.Context }

func (p *ctxProbeAI) Stream(ctx context.Context, _ []ai.Message, _ func(string)) error {
	p.ready <- ctx
	<-ctx.Done()
	return ctx.Err()
}

// TestEscLadderAbortsStreamThenQuits 把首页这一格的层级走一遍：
// 等回答时 Esc 是「中止生成」→ 再按才武装 → 第三下才退出。
func TestEscLadderAbortsStreamThenQuits(t *testing.T) {
	m := homeHintModel(t)
	probe := &ctxProbeAI{ready: make(chan context.Context, 1)}
	m.aiClient = probe
	if cmd := m.submit("他在哪？"); cmd == nil {
		t.Fatal("前置条件：自由提问应当返回命令")
	}
	ctx := <-probe.ready

	m, _ = sendMsg(m, escKey())
	if m.awaiting {
		t.Fatal("第一下 Esc 应当是中止生成，不是退出")
	}
	if m.escArmed {
		t.Fatal("中止生成这一下不该武装退出")
	}
	if ctx.Err() == nil {
		t.Fatal("中止生成必须真的取消在途请求")
	}

	m, cmd := sendMsg(m, escKey())
	if !m.escArmed || cmd == nil {
		t.Fatalf("第二下应当是武装：armed=%v cmd=%T", m.escArmed, cmd)
	}

	m, cmd = sendMsg(m, escKey())
	if m.escArmed {
		t.Fatal("退出时应当把武装状态清掉")
	}
	if cmd == nil || cmd() != (tea.QuitMsg{}) {
		t.Fatalf("第三下的命令 = %T, want tea.QuitMsg", cmd)
	}
}

// TestQuitPathsCancelInflightAI 退出前必须掐断在途的 AI 流。
// 覆盖真正够得着的两条路：Ctrl+C、/exit。
// （「连按两次 Esc」到不了「有请求在飞」的状态——第一下 Esc 已经把它中止了，
// 见 TestEscLadderAbortsStreamThenQuits；实测跑一遍 pty 也确认了。）
func TestQuitPathsCancelInflightAI(t *testing.T) {
	inflight := func(t *testing.T) (Model, context.Context) {
		t.Helper()
		m := homeHintModel(t)
		probe := &ctxProbeAI{ready: make(chan context.Context, 1)}
		m.aiClient = probe
		if cmd := m.submit("他在哪？"); cmd == nil {
			t.Fatal("前置条件：自由提问应当返回命令")
		}
		ctx := <-probe.ready
		if ctx.Err() != nil {
			t.Fatal("前置条件：刚提交时请求不该已经被取消")
		}
		return m, ctx
	}

	check := func(t *testing.T, m Model, ctx context.Context, cmd tea.Cmd) {
		t.Helper()
		if cmd == nil {
			t.Fatal("退出路径应当返回命令")
		}
		if msg := cmd(); msg != (tea.QuitMsg{}) {
			t.Fatalf("命令 = %T, want tea.QuitMsg", msg)
		}
		if m.awaiting {
			t.Fatal("退出时应当把等待态清掉")
		}
		if ctx.Err() == nil {
			t.Fatal("退出时必须取消在途请求，否则 SSH 常驻进程里这条流会一直挂着")
		}
	}

	t.Run("ctrl+c", func(t *testing.T) {
		m, ctx := inflight(t)
		m, cmd := sendMsg(m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
		check(t, m, ctx, cmd)
	})

	t.Run("/exit", func(t *testing.T) {
		m, ctx := inflight(t)
		check(t, m, ctx, m.submit("/exit"))
	})
}

// TestPromptBoxRowsAndNoNULAcrossWidths 锁住 bubbles v2.2.1 的一个坑，以及框的 3 行形态。
//
// textinput 的 placeholderView() 先 `p := make([]rune, Width+1)`（未填充处是 rune
// 零值），再按 `p[1:minWidth]` 切片，而 minWidth 取的是 `lipgloss.Width(Placeholder)`
// —— **显示宽度被当成了 rune 下标**。占位文案含 CJK 时两者不相等（现文案 31 列 / 18
// rune），多切出来的 13 个位置全是 `\x00`：这些 NUL 混进渲染后，终端与 lipgloss 对
// 「这行到底多宽」的认知不一致 —— 输入行左竖条丢失、面板被挤到下一行，整个框看着错位
// （2026-09-22 用户 80x24 截图实测；英文占位文案时代 Width 恰等于 rune 数，所以没暴露）。
//
// renderPromptBox 现在空输入时自己渲染占位文案（不走 textinput 的 Placeholder），
// 这里把结果锁住：各宽度下框都是 3 行、每行左右各一条竖线、整帧渲染里不含 NUL。
func TestPromptBoxRowsAndNoNULAcrossWidths(t *testing.T) {
	for _, width := range []int{56, 64, 72, 80, 88, 100, 140} {
		m := testColorModel(t)
		m.width = width
		m.height = 24
		m.recalculateLayout()
		m.refreshContent()

		box := m.renderPromptBox(m.homeMetrics().boxWidth)
		if strings.ContainsRune(box, 0) {
			t.Fatalf("width=%d: 输入框渲染里混进 NUL（bubbles placeholder 坑复发）:\n%q", width, box)
		}
		if lines := strings.Split(box, "\n"); len(lines) != 3 {
			t.Fatalf("width=%d: box rows = %d, want 3", width, len(lines))
		}

		// 整帧渲染：框区域必须恰好 3 行 —— 上面那个错位的症状就是框多出一行、且竖条丢失。
		layout := strings.Split(m.renderHomeLayout(m.viewport.Width()), "\n")
		rows := 0
		for _, line := range layout {
			if isBoxRow(line) {
				rows++
			}
		}
		if rows != 3 {
			t.Fatalf("width=%d: framed rows in full layout = %d, want 3", width, rows)
		}
	}
}

// TestGreetingHeadlineMatchesInvite 锁住用户 2026-09-22 的要求：
// 第一行 headline 用与第二行 invite 完全相同的 muted 样式，不再用 title 亮白加粗。
func TestGreetingHeadlineMatchesInvite(t *testing.T) {
	m := testColorModel(t)
	rendered := strings.Join(m.greetingLines(64), "\n")
	if !strings.Contains(rendered, m.styles.muted.Render(greetingHeadline)) {
		t.Fatalf("headline 未使用 muted 样式（应与 invite 一致）:\n%q", rendered)
	}
	if strings.Contains(rendered, m.styles.title.Render(greetingHeadline)) {
		t.Fatal("headline 仍在使用 title（亮白加粗）样式")
	}
	if !strings.Contains(rendered, m.styles.muted.Render(greetingInvite)) {
		t.Fatalf("invite 不再是 muted 样式:\n%q", rendered)
	}
}
