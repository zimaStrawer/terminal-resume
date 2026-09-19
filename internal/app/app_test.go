package app

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

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

// isBoxRow 判断一行是不是输入框的一行：框区自 2026-09-19 起**不再铺底色**
// （macOS 输入法擦除显形问题，见 box.go renderDialog），框行只能靠左右两条 │ 竖线辨认，
// 而全帧渲染里只有输入框会画这个字形（用户对话文本里打出的 │ 在框上方，不在框行）。
//
// ⚠️ 必须先 ansi.Strip 再数字形——竖线是「字形 + 前景色」，Strip 不影响字形本身。
// 彩色主题下线走终端主题色、黑白主题走近白，字形在两种主题下一致，探针不挑主题。
func isBoxRow(line string) bool {
	return strings.Count(ansi.Strip(line), "│") == 2
}

// dialogEdgeSeq 是**彩色主题**下竖线字符（▏/▕）的前景色序列 —— 细线由字形墨迹画，
// 颜色来自 boxEdge 的前景色（见 box.go 的 dialogEdgeLeft 与 styles.go 的 boxEdge）。
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
	page := m.renderPage()
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

func TestLongContentStillUsesScrollableViewport(t *testing.T) {
	m := testModel(t)
	m.width = 80
	m.height = 24
	m.resume.About.Summary = strings.Repeat("这是一段用于验证滚动区域的长内容。", 100)
	m.changeScreen(screenAbout)

	if got, want := m.viewport.Height(), m.height-5; got != want {
		t.Fatalf("viewport height = %d, want %d for long content", got, want)
	}
}

func TestCommandSuggestionsIncludeProjects(t *testing.T) {
	m := testModel(t)
	if !m.HasCommandSuggestion("/project 03") {
		t.Fatal("project 03 suggestion is missing")
	}
}

func TestHeaderDoesNotAssumeSSHConnection(t *testing.T) {
	m := testModel(t)
	header := m.renderHeader(80)
	if !strings.Contains(header, "TUI SESSION") || strings.Contains(header, "SSH RESUME") {
		t.Fatalf("unexpected header: %q", header)
	}
}

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
	if got := gradientColor(from, to, 0, 1); got != "#22D3EE" {
		t.Fatalf("gradientColor(t=0) = %s, want #22D3EE", got)
	}
	if got := gradientColor(from, to, 1, 1); got != "#4ADE80" {
		t.Fatalf("gradientColor(t=1) = %s, want #4ADE80", got)
	}
	if got := gradientColor(from, to, 0, 0.5); got != "#116977" {
		t.Fatalf("gradientColor(t=0,ratio=0.5) = %s, want #116977", got)
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

// TestDialogBoxHasNoSurfaceFill 锁住框区的「不上色」设计（2026-09-19 用户裁定）：
// 输入框中间**不得出现任何背景色序列**。原因见 box.go renderDialog——
// macOS 输入法组词会从光标处向行尾整行擦除（BCE），擦除填充色 = 终端遗留画笔背景色；
// 只要框行带任何与页面底色不同的填充，填充色落在框外边距上就是「打字时框外突出色块」。
// 框行 = 左右两条 │ 竖线 + 不上色的中间区域；框内只保留一行输入区（上下各一行内边距）。
func TestDialogBoxHasNoSurfaceFill(t *testing.T) {
	m := testModel(t)
	m.width = 100
	m.height = 30
	m.recalculateLayout()
	m.refreshContent()

	box := m.renderPromptBox(72)
	lines := strings.Split(box, "\n")
	if len(lines) != 3 {
		t.Fatalf("box lines = %d, want 3 (1 padding + 1 content + 1 padding)", len(lines))
	}
	for index, line := range lines {
		// "48;2;"/"48;5;" 是背景色的SGR参数，组合序列（如 \x1b[1;48;2;…m）里同样会出现；
		// 前景色参数是 38;…，不会被误伤。
		if strings.Contains(line, "48;2;") || strings.Contains(line, "48;5;") {
			t.Fatalf("box line %d carries a background color (must stay unfilled):\n%q", index, line[:80])
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

// TestDialogBoxEdgesHugThePanel 锁住竖线怎么画（用户 2026-09-19 晚间改定）：
// 线是**制表符 │ + 前景色**，纵向贯通不断开、上下齐平；中间区域**不上色**（同日裁定，
// 根治输入法擦除显形，见 box.go renderDialog 与 TestDialogBoxHasNoSurfaceFill）。
//
// 取舍记一笔：方块字符 ▏/▕ 够细但墨迹高度填不满一格（切成三段，用户不要断开的）；
// 整格背景色连续但太粗；│ 连续、细、等高，唯独水平居中、离框边缘差约半格
// —— 终端字符网格下仅剩的取舍点（详见 box.go 注释）。
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

	lines := strings.Split(m.renderPromptBox(72), "\n")
	for index, line := range lines {
		// 每行都是「细竖线 + 不上色的输入区 + 细竖线」：左右各一段，不多不少。
		if count := strings.Count(line, edge); count != 2 {
			t.Fatalf("box line %d has %d edge cells, want 2 (左右各一条竖线):\n%q",
				index, count, ansi.Strip(line))
		}
		// 线字符本身必须紧跟在线色之后（前景色确实落在 │ 上，不是空序列）。
		plain := ansi.Strip(line)
		if !strings.HasPrefix(plain, "│") || !strings.HasSuffix(plain, "│") {
			t.Fatalf("box line %d does not start/end with the thin edge glyphs: %q", index, plain)
		}
		// 框行不得带任何背景色（不上色设计，见 TestDialogBoxHasNoSurfaceFill）。
		if strings.Contains(line, "48;2;") || strings.Contains(line, "48;5;") {
			t.Fatalf("box line %d carries a background color:\n%q", index, ansi.Strip(line))
		}
	}
}

// TestDialogEdgeCellsAreThinSingleColumn 锁住竖线那一格的内容与宽度（用户 2026-09-19 晚间改定）。
//
// 两侧必须是**制表符 │**（U+2502）：终端里唯一细（1–2px）、纵向连续不断开、
// 与面板等高的画法（方块字符墨迹高度由字体决定、会被切成几段；整格背景色又太粗，见 box.go）。
// 必须恰好 1 列宽，否则整行宽度随主题漂移、把输入框挤歪。
// 代价：│ 在格子里水平居中，离面板边缘差约半格 —— 终端字符网格下仅剩的取舍点。
// （2026-09-19 白天是「空格 + 整格背景色」，因太粗被否；晚间短暂试过 ▏/▕，因断成几段被否。）
func TestDialogEdgeCellsAreThinSingleColumn(t *testing.T) {
	for _, edge := range []string{dialogEdgeLeft, dialogEdgeRight} {
		if edge != "│" {
			t.Fatalf("edge cell content = %q, want %q (制表符细线，见 box.go)", edge, "│")
		}
		if width := ansi.StringWidth(edge); width != 1 {
			t.Fatalf("edge cell is %d columns wide, want 1", width)
		}
	}
	// 彩色主题下线的颜色必须是终端主题色（RGB 22,184,243）走前景色。
	styled := newStyles(false).boxEdge.Render(dialogEdgeLeft)
	if !strings.Contains(styled, "38;2;22;184;243") {
		t.Fatalf("edge style renders %q, want the theme color as glyph foreground", styled)
	}
	// 黑白主题下不引色，走的是近白色。
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

	// 独立复算期望位置：输入行 = 带左右 │ 的 3 行框里中间那行（框区无底色，按字形找）；
	// 列 = 行内左缘 │ + 5（1 边框 + 2 内边距 + 2 提示字符）+ 光标前文本的显示格数。
	content := m.renderHomeLayout(m.viewport.Width())
	lines := strings.Split(content, "\n")
	rows := boxRows(lines)
	if len(rows) != 3 {
		t.Fatalf("box rows = %v, want exactly 3 prompt-box rows", rows)
	}
	plain := ansi.Strip(lines[rows[1]])
	left := strings.Index(plain, "│")
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
	left := strings.Index(plain, "│")
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
