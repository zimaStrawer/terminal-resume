package app

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/zhangsizhou/terminal-resume/profile"
)

const Version = "0.1.0"

// 说明：这里曾经有一套「独立页面」的机器（screen 枚举 + changeScreen / goBack /
// handleHomeShortcut / renderHeader / renderStatusBar / renderFooter …），
// 2026-09-20 整段删掉了。原因：命令输出早就改成「进对话流」渲染
// （commandAnswer 返回正文当作回答），切页那条链再没有任何调用者，
// 于是首页那行「按 1—5 快速浏览」、页脚的「esc 返回」、/help 的「Esc 返回上一级」
// 全都在宣传不存在的能力。现在整个 TUI 只有一块屏：对话 + 钉在底部的输入框。
// 旧版实现见 .workbuddy/backup/app.go.before-deadcode-2026-09-20（也在 git 历史里）。

type menuItem struct {
	command string
	label   string
}

type noticeKind int

const (
	noticeInfo noticeKind = iota
	noticeSuccess
	noticeError
)

// 首页「连按两次 Esc 退出」（用户 2026-09-20 指定）。
//
// 为什么不做成按一下就退：Esc 在这个项目里还担着「中止 AI 生成 / 跳过流式打印 /
// 清空输入 / 回到最新」四件事，单按退出会把这四个全顶掉；而用户等回答时习惯连按 Esc，
// 单按退出几乎必然误触。两次之间给 escExitWindow 的窗口，只按一次会自己过期。
const escExitWindow = 800 * time.Millisecond

// escTimeoutMsg 由 tea.Tick 在武装窗口结束时投递，用来撤销武装状态——
// 否则「再按一次 esc 退出」那行提示会一直挂在屏幕上，像卡住了。
type escTimeoutMsg struct{}

func escTimeoutCmd() tea.Cmd {
	return tea.Tick(escExitWindow, func(time.Time) tea.Msg { return escTimeoutMsg{} })
}

// quit 是程序唯一的退出出口（Ctrl+C / /exit / 连按两次 Esc 都走它）。
//
// 为什么不能直接 return tea.Quit：退出前必须先把在途的 AI 流掐掉。
// 本地跑进程一退就没了无所谓，但 SSH 模式下服务端进程是常驻的，
// 访客在等回答时退出的话，那条 SSE 请求和它的 goroutine 会一直挂到上游自己结束为止。
// （Bubble Tea 收到 tea.Quit 就去还原终端状态了，不会替我们取消 context。）
func (m *Model) quit() tea.Cmd {
	m.cancelAI()
	return tea.Quit
}

type Model struct {
	resume          profile.Resume
	input           textinput.Model
	viewport        viewport.Model
	styles          styles
	monochrome      bool
	width           int
	height          int
	selectedProject int
	activeProjectID string
	history         []string
	historyIndex    int
	notice          string
	noticeKind      noticeKind
	menuOpen        bool
	menuIndex       int

	// escArmed 是「连按两次 Esc 退出」的中间态：第一次按下只武装，
	// escExitWindow 内再按一次才真退出（见 escTimeoutMsg）。任何其他按键都会撤销它。
	escArmed bool

	logoWide       string
	logoCompact    string
	chat           []chatMessage
	streaming      bool
	chatOffset     int
	cursorPhase    cursorPhase
	cursorStep     int
	cursorSequence []string

	// 自由问答（真实 AI 流式）。aiClient 为 nil 表示没配后端地址，
	// 此时只保留斜杠命令，输入自由文本会给出提示（见 ai.go）。
	// aiEvents 是当前这条流的 channel：listenAI 一次只取一个事件，
	// 取完必须重新挂上，否则流读到第一个分片就停了。
	aiClient aiStreamer
	aiEvents <-chan aiEvent
	aiBase   string
	aiSeq    int
	aiCancel func()
	awaiting bool
}

func New(resume profile.Resume, monochrome bool) Model {
	m := Model{
		resume:     resume,
		monochrome: monochrome,
		width:      92,
		height:     30,
	}
	m.styles = newStyles(monochrome)
	// 自由问答打的是作品集站的 /api/chat：站点地址就取简历里的 profile.website，
	// 这样「AI 助手和简历在同一个站点」是默认行为，不用额外配置。
	// 命令行 --api 可覆盖；传空串则关闭自由问答。
	apiBase := strings.TrimSpace(resume.Profile.Website)
	if apiBase == "" {
		apiBase = DefaultAPIBase
	}
	m.SetAPIBase(apiBase)
	m.viewport = viewport.New(viewport.WithWidth(88), viewport.WithHeight(22))
	m.viewport.SoftWrap = true
	m.viewport.FillHeight = false
	m.viewport.MouseWheelEnabled = false

	m.input = textinput.New()
	m.input.Prompt = ""
	// 占位文案不在这里定：applyPlaceholder 要按终端宽度在长句 / 短句之间选，
	// 由下面那次 recalculateLayout() 统一设上。
	m.input.CharLimit = 96
	m.input.ShowSuggestions = true
	m.input.SetSuggestions(m.commandSuggestions())
	// 用**真实终端光标**而不是 textinput 自带的虚拟光标（见 View 里的 frameCursor）：
	// 中文输入法的预编辑串（还没上屏的拼音）由终端按**硬件光标**的位置绘制，
	// 虚拟光标不联动硬件光标，拼音会被画到输入框外面（2026-09-19 用户实测）。
	m.input.SetVirtualCursor(false)
	m.applyInputStyles()
	m.input.Focus()
	m.refreshLogo()
	m.recalculateLayout()
	m.refreshContent()
	return m
}

func (m *Model) refreshLogo() {
	m.logoWide = paintLogo(logoLetters, colorLogoFrom, colorLogoTo, m.monochrome)
	m.logoCompact = m.styles.title.Render(logoCompactText)
}

func (m Model) Init() tea.Cmd {
	// 输入框提示字符的闪烁 / 乱序动画：由这条 tick 链一路自续。
	return cursorTickCmd(cursorStartDelay)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = max(msg.Width, 24)
		m.height = max(msg.Height, 12)
		m.recalculateLayout()
		m.refreshContent()
		return m, nil

	case streamTickMsg:
		if m.advanceStream() {
			return m, streamCmd()
		}
		return m, nil

	case aiDeltaMsg:
		// listenAI 一次只取一个事件，处理完必须重新挂上，
		// 否则流读到第一个分片就停了（真实的坑：只显示第一段就卡住）。
		cmd := m.handleAIDelta(msg)
		return m, tea.Batch(listenAI(m.aiEvents), cmd)

	case aiDoneMsg:
		cmd := m.handleAIDone(msg)
		return m, cmd

	case cursorTickMsg:
		return m, cursorTickCmd(m.advanceCursor())

	case escTimeoutMsg:
		// 武装窗口过了还没按第二次：撤销，底部那行提示换回常态文案。
		m.escArmed = false
		return m, nil

	case tea.KeyPressMsg:
		key := msg.String()
		// 除了 Esc 自己，任何一次按键都撤销「待退出」。
		// 否则「Esc（想回最新）→ 打字 → Esc（想清空）」会被误判成连按两次退出。
		if key != "esc" {
			m.escArmed = false
		}
		if m.menuOpen {
			switch key {
			case "ctrl+c":
				cmd := m.quit()
				return m, cmd
			case "up":
				m.moveMenu(-1)
				return m, nil
			case "down":
				m.moveMenu(1)
				return m, nil
			case "esc":
				m.input.Reset()
				m.closeMenu()
				// 关菜单的这一次 Esc 不算「第一下」，否则菜单里连按两下会直接退程序。
				m.escArmed = false
				return m, nil
			case "enter":
				items := m.menuItems()
				command := ""
				if len(items) > 0 {
					command = items[m.menuIndex%len(items)].command
				}
				m.closeMenu()
				m.input.Reset()
				if command == "" {
					return m, nil
				}
				return m, m.submit(command)
			}
			m.closeMenu()
		}
		switch key {
		case "ctrl+c":
			cmd := m.quit()
			return m, cmd
		case "ctrl+p":
			return m, m.submit("/help")
		case "esc":
			// AI 正在回答：Esc 中止生成。已经收到的部分照样留着，只是不再往下打。
			if m.awaiting {
				m.cancelAI()
				m.finishStreaming()
				m.dropEmptyAnswer()
				m.notice = "已停止生成。"
				m.noticeKind = noticeInfo
				return m, nil
			}
			// 打印中按 Esc 直接跳过流式，把整段答案一次性显示出来。
			if m.streaming {
				m.finishStreaming()
				return m, nil
			}
			if m.input.Value() != "" {
				m.input.Reset()
				return m, nil
			}
			// 走到这里：输入框是空的，也没有在生成 / 打印 —— Esc 还剩下两层语义：
			//   1. 正在回看历史 → 回到最新（要再往回翻就用 PgUp/PgDn 或 ↑↓）
			//   2. 已经在最新   → 连按两次退出（第一次只武装，见 escExitWindow）
			//
			// 这里原先调的是 scrollChat(+conversation)，那是「往上翻一页」，
			// 跟 /help 写的「返回上一级」和代码注释里的「回到最新」都对不上号，
			// 而且按一下 Esc（想退出没退成）会把画面莫名顶走一屏，所以改成回到最新。
			// 「返回上一级」那一层随独立页面机器一起删掉了（见文件开头的说明）。
			if m.chatOffset > 0 {
				m.chatOffset = 0
				return m, nil
			}
			if m.escArmed {
				m.escArmed = false
				// 显式接一下：`return m, m.quit()` 会先复制 m 再执行 quit，
				// 而 quit 内部要改 aiCancel/awaiting，写成两步更不容易被误读。
				cmd := m.quit()
				return m, cmd
			}
			m.escArmed = true
			return m, escTimeoutCmd()
		case "pgup":
			m.scrollChat(m.homeMetrics().conversation)
			return m, nil
		case "pgdown":
			m.scrollChat(-m.homeMetrics().conversation)
			return m, nil
		case "enter":
			text := strings.TrimSpace(m.input.Value())
			if text == "" {
				return m, nil
			}
			m.input.Reset()
			return m, m.submit(text)
		}

		if m.input.Value() == "" && key == "up" {
			m.scrollChat(1)
			return m, nil
		}
		if m.input.Value() == "" && key == "down" {
			m.scrollChat(-1)
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if strings.TrimSpace(m.input.Value()) == "/" && !m.menuOpen {
		m.openMenu()
	}
	return m, cmd
}

func (m Model) View() tea.View {
	contentWidth := m.viewport.Width()
	content := m.renderHomeLayout(contentWidth)
	// 帧始终保持满屏尺寸：内容变短时旧画面不会被留在屏幕上。
	content = lipgloss.NewStyle().Width(contentWidth).Height(m.height).Render(content)

	marginLeft := max((m.width-contentWidth)/2, 0)
	if marginLeft > 0 {
		content = lipgloss.NewStyle().MarginLeft(marginLeft).Render(content)
	}

	v := tea.NewView(content)
	v.AltScreen = true
	v.WindowTitle = "ZHANG — Terminal Resume"
	v.Cursor = m.frameCursor(content)
	if !m.monochrome {
		v.BackgroundColor = lipgloss.Color(colorBackground)
		v.ForegroundColor = lipgloss.Color(colorForeground)
	}
	return v
}

// frameCursor 把真实终端光标钉到输入框光标所在的格子上（帧内坐标，含左边距）。
//
// 为什么必须这么做：macOS 上中文输入法的**预编辑串**（还没上屏的拼音，如 `d da s d`）
// 由终端按**硬件光标**的当前位置绘制；候选词条浮窗也跟着硬件光标走。
// 渲染完一帧后硬件光标会停在帧尾的任意角落，于是拼音和候选词全画到输入框外
// （2026-09-19 用户实测截图）。每帧显式定位即可修复，本地 TUI 与 SSH 访客同样生效。
//
// 定位算法（不信任 textinput.Cursor().X——它是 rune 数，中文 1 rune 占 2 格会偏左）：
//  1. 从帧底部向上找带竖线字形的行（dialogEdgeLeft）：输入框永远在页面最底部
//     （下方只有提示行），最先命中的就是框；框是 3 行（上下各一行内边距），
//     输入行取中间那条。（2026-09-19 起框区不再铺底色，行探测由底色序列改为字形。）
//  2. 行内找左缘字形（首现）与右缘字形（末现）——文本起点 = 左缘 +1(边) +2(内边距) +2(提示字符)；
//  3. 光标列 = 文本起点 + 光标前文本的**显示格数**，超宽文本被截断时贴住右缘。
func (m Model) frameCursor(content string) *tea.Cursor {
	c := m.input.Cursor()
	if c == nil {
		return nil // 输入框未聚焦：光标保持隐藏
	}
	lines := strings.Split(content, "\n")
	edgeRows := []int{}
	for index := len(lines) - 1; index >= 0 && len(edgeRows) < 3; index-- {
		if strings.Contains(ansi.Strip(lines[index]), dialogEdgeLeft) {
			edgeRows = append(edgeRows, index)
		}
	}
	if len(edgeRows) == 0 {
		return nil
	}
	// 框是 3 行（上下各一行内边距），输入行取中间那条。
	row := edgeRows[0]
	if len(edgeRows) >= 3 {
		row = edgeRows[1]
	}
	plain := ansi.Strip(lines[row])
	left := strings.Index(plain, dialogEdgeLeft)
	right := strings.LastIndex(plain, dialogEdgeLeft)
	if left < 0 || right <= left {
		return nil
	}

	// 菜单打开时框内渲染的是 "/菜单：光标落在 / 后的反白块上（1 格）。
	cells := 0
	if m.menuOpen {
		cells = 1
	} else {
		runes := []rune(m.input.Value())
		pos := m.input.Position()
		if pos > len(runes) {
			pos = len(runes)
		}
		cells = lipgloss.Width(string(runes[:pos]))
	}
	// 可用文本列数 = 右缘 - 左缘 - 1(两边框) - 4(左右内边距) - 2(提示字符 › 加空格)。
	if avail := right - left - 7; avail > 0 && cells > avail {
		cells = avail
	}
	c.X = left + 5 + cells
	c.Y = row
	return c
}

func (m *Model) recalculateLayout() {
	contentWidth := min(max(m.width-2, 22), 104)
	viewportHeight := max(m.height-5, 6)
	m.viewport.SetWidth(contentWidth)
	m.viewport.SetHeight(viewportHeight)
	promptWidth := max(contentWidth-4, 8)
	m.input.SetWidth(promptWidth)
	m.applyPlaceholder()
}

func (m *Model) refreshContent() {
	y := m.viewport.YOffset()
	content := m.renderViewportContent()
	m.viewport.SetContent(content)
	m.resizeViewport(content)
	m.viewport.SetYOffset(y)
	if m.viewport.PastBottom() {
		m.viewport.GotoBottom()
	}
}

func (m *Model) resizeViewport(content string) {
	availableHeight := max(m.height-5-m.menuRows(), 6)
	wrapped := ansi.Wrap(content, max(m.viewport.Width(), 1), "")
	contentHeight := max(lipgloss.Height(wrapped), 1)
	m.viewport.SetHeight(min(contentHeight, availableHeight))
}

func (m *Model) rememberCommand(command string) {
	if len(m.history) == 0 || m.history[len(m.history)-1] != command {
		m.history = append(m.history, command)
	}
	m.historyIndex = len(m.history)
}

// menuItems 是输入 / 时弹出的预设快捷命令。
// 文案固定为用户指定的四条英文短句（2026-09-19），内容都来自 profile/resume.yaml，
// 不经过任何 AI 生成。
func (m Model) menuItems() []menuItem {
	return []menuItem{
		{command: "/skills", label: "explore my design & technical toolkit"},
		{command: "/resume", label: "download my latest resume"},
		{command: "/log", label: "browse recent builds & learning notes"},
		{command: "/personal", label: "discover the person behind the work"},
	}
}

func (m Model) menuRows() int {
	if !m.menuOpen {
		return 0
	}
	return len(m.menuItems())
}

func (m *Model) closeMenu() {
	if !m.menuOpen {
		return
	}
	m.menuOpen = false
	m.menuIndex = 0
	m.refreshContent()
}

// openMenu 打开快捷命令菜单。
func (m *Model) openMenu() {
	if m.menuOpen {
		return
	}
	m.menuOpen = true
	m.menuIndex = 0
	m.refreshContent()
}

func (m *Model) moveMenu(delta int) {
	items := m.menuItems()
	if len(items) == 0 {
		return
	}
	m.menuIndex = (m.menuIndex + delta + len(items)) % len(items)
}

// submit 处理一次提交：内容型命令生成回复，流式打印到对话流里；
// 行为型命令（主题 / 清空 / 退出）仍按原来的方式执行。
func (m *Model) submit(raw string) tea.Cmd {
	text := strings.TrimSpace(raw)
	if text == "" {
		return nil
	}
	m.rememberCommand(text)
	m.notice = ""

	command, args := normalizeCommand(text)
	switch command {
	case "theme":
		m.switchTheme(args)
		return nil
	case "exit":
		return m.quit()
	case "clear":
		// 清空对话时把在途的 AI 请求也一起掐掉，否则旧流还会往空列表里插字。
		m.cancelAI()
		m.finishStreaming()
		m.chat = nil
		m.chatOffset = 0
		return nil
	}

	answer, ok := m.commandAnswer(command, args)
	if ok {
		m.finishStreaming()
		m.chatOffset = 0
		m.chat = append(m.chat, newChatMessage(chatUser, text))
		m.chat = append(m.chat, newStreamingMessage(answer))
		return m.startStreaming()
	}

	if command == "" {
		m.notice = "输入 / 打开快捷菜单，或输入命令。"
		m.noticeKind = noticeInfo
		return nil
	}

	// 走到这里说明不是命令，分两种情况：
	//   1. 以 / 开头、或跟某条命令只差一两个字母 —— 当打错字处理，提示正确写法；
	//   2. 其余自由文本 —— 交给作品集的 /api/chat 真实回答（见 ai.go）。
	if !strings.HasPrefix(text, "/") && !looksLikeCommandTypo(text) {
		return m.askAI(text)
	}

	suggestion := nearestCommand(command)
	m.notice = fmt.Sprintf("没有找到命令 %q。你是否想输入 %q？", text, "/"+suggestion)
	m.noticeKind = noticeError
	return nil
}

// commandAnswer 返回命令对应的回复正文；ok 为 false 表示没有这条命令。
func (m *Model) commandAnswer(command string, args []string) (string, bool) {
	switch command {
	case "whoami", "personal":
		return m.renderAbout(), true
	case "experience":
		return m.renderExperience(), true
	case "projects":
		m.selectedProject = -1
		return m.renderProjects(), true
	case "project":
		if len(args) == 0 {
			m.selectedProject = -1
			return m.renderProjects(), true
		}
		if _, ok := m.resume.ProjectByID(args[0]); !ok {
			return "", false
		}
		m.activeProjectID = args[0]
		return m.renderProject(), true
	case "skills":
		return m.renderSkills(), true
	case "status":
		return m.renderStatusPage(), true
	case "log":
		return m.renderLogPage(), true
	case "random":
		return m.renderRandomPage(), true
	case "contact", "resume":
		return m.renderContact(), true
	case "help":
		return m.renderHelp(), true
	case "home":
		// /home、/首页、/返回 这几个别名一直存在，但独立页面删掉之后
		// 已经没有「别的页面」可回了；不接这一条的话会掉进「没有找到命令」，
		// 提示用户去输 /home —— 而 /home 正是刚打过的那个。
		return "已经在首页了。这里只有一块屏：上方是对话，底部是输入框。\n输入 /help 查看全部命令。", true
	}
	if _, ok := m.resume.ProjectByID(command); ok {
		m.activeProjectID = command
		return m.renderProject(), true
	}
	return "", false
}

func normalizeCommand(raw string) (string, []string) {
	fields := strings.Fields(strings.ToLower(strings.TrimSpace(raw)))
	if len(fields) == 0 {
		return "", nil
	}
	fields[0] = strings.TrimPrefix(fields[0], "/")
	aliases := map[string]string{
		"首页": "home", "返回": "home",
		"关于": "whoami", "关于我": "whoami", "about": "whoami",
		"个人": "personal", "人物": "personal",
		"经历": "experience", "工作经历": "experience", "work": "experience",
		"项目": "projects", "作品": "projects", "portfolio": "projects",
		"打开": "project", "open": "project",
		"能力": "skills", "技能": "skills",
		"状态": "status", "当前状态": "status", "求职": "status",
		"日志": "log", "记录": "log", "动态": "log",
		"随机": "random", "彩蛋": "random",
		"联系": "contact", "联系方式": "contact",
		"简历": "resume", "下载": "resume",
		"帮助": "help", "?": "help",
		"清空": "clear", "cls": "clear",
		"主题": "theme",
		"退出": "exit", "quit": "exit", "q": "exit",
	}
	if canonical, ok := aliases[fields[0]]; ok {
		fields[0] = canonical
	}
	return fields[0], fields[1:]
}

func (m *Model) switchTheme(args []string) {
	if len(args) == 0 {
		m.monochrome = !m.monochrome
	} else {
		switch args[0] {
		case "mono", "monochrome", "黑白":
			m.monochrome = true
		case "cyan", "color", "彩色":
			m.monochrome = false
		default:
			m.notice = "可用主题：/theme cyan 或 /theme mono。"
			m.noticeKind = noticeError
			return
		}
	}
	m.styles = newStyles(m.monochrome)
	m.applyInputStyles()
	m.refreshLogo()
	m.notice = "主题已切换。"
	m.noticeKind = noticeSuccess
	m.refreshContent()
}

func (m *Model) applyInputStyles() {
	s := textinput.DefaultDarkStyles()
	if m.monochrome {
		s.Focused.Text = lipgloss.NewStyle()
		s.Focused.Placeholder = lipgloss.NewStyle().Faint(true)
		s.Focused.Suggestion = lipgloss.NewStyle().Faint(true)
		s.Cursor.Color = nil
	} else {
		s.Focused.Text = lipgloss.NewStyle().Foreground(lipgloss.Color(colorForeground))
		s.Focused.Placeholder = lipgloss.NewStyle().Foreground(lipgloss.Color(colorDim))
		s.Focused.Suggestion = lipgloss.NewStyle().Foreground(lipgloss.Color(colorMuted))
		s.Cursor.Color = lipgloss.Color(colorAccent)
	}
	s.Cursor.Shape = tea.CursorBar
	s.Cursor.Blink = false
	m.input.SetStyles(s)
}

func (m Model) commandSuggestions() []string {
	suggestions := []string{
		"/home", "/about", "/personal", "/experience", "/projects", "/skills", "/contact",
		"/status", "/log", "/random", "/resume", "/help",
		"/theme cyan", "/theme mono", "/clear", "/exit",
	}
	for _, project := range m.resume.Projects {
		suggestions = append(suggestions, "/project "+project.ID)
	}
	return suggestions
}

func nearestCommand(command string) string {
	commands := []string{"home", "whoami", "personal", "experience", "projects", "project", "skills", "status", "log", "random", "contact", "resume", "help", "theme", "clear", "exit"}
	best := "help"
	bestDistance := 1 << 30
	for _, candidate := range commands {
		distance := levenshtein(command, candidate)
		if distance < bestDistance {
			best = candidate
			bestDistance = distance
		}
	}
	return best
}

func levenshtein(a, b string) int {
	ar, br := []rune(a), []rune(b)
	previous := make([]int, len(br)+1)
	for j := range previous {
		previous[j] = j
	}
	for i, ra := range ar {
		current := make([]int, len(br)+1)
		current[0] = i + 1
		for j, rb := range br {
			cost := 0
			if ra != rb {
				cost = 1
			}
			current[j+1] = min(current[j]+1, previous[j+1]+1, previous[j]+cost)
		}
		previous = current
	}
	return previous[len(br)]
}

// renderMenu 渲染 / 快捷命令下拉框。按用户要求：不使用背景色（直接落在终端底色上），
// 未选中项为灰色，选中项用终端主题色 (#16B8F3) 高亮——参考 Gemini CLI 的补全列表。
func (m Model) renderMenu(width int) string {
	items := m.menuItems()
	if len(items) == 0 {
		return ""
	}
	nameWidth := 0
	for _, item := range items {
		if w := RuneCount(item.command); w > nameWidth {
			nameWidth = w
		}
	}
	lines := make([]string, 0, len(items))
	for index, item := range items {
		style := m.styles.menuItem
		marker := "  "
		if index == m.menuIndex {
			style = m.styles.menuChosen
			marker = "> "
		}
		line := marker + fmt.Sprintf("%-*s  %s", nameWidth, item.command, item.label)
		lines = append(lines, style.Render(ansi.Truncate(line, max(width, 1), "")))
	}
	return strings.Join(lines, "\n")
}

// renderViewportContent 往 viewport 里放的兜底内容。
//
// 页面本身由 View → renderHomeLayout 直接渲染（对话 + 底部输入框），
// viewport 这里只留一份居中 logo：它仍被用来量内容宽度与高度。
func (m Model) renderViewportContent() string {
	logo := m.logoCompact
	if m.viewport.Width() >= logoWideMinWidth {
		logo = m.logoWide
	}
	return lipgloss.NewStyle().Width(m.viewport.Width()).Align(lipgloss.Center).Render(logo)
}

// homeMetrics 汇总首页（对话界面）的尺寸，渲染与滚动共用同一套计算，避免两处算法漂移。
type homeMetrics struct {
	contentWidth int
	boxWidth     int
	boxVisual    int
	menuHeight   int
	blockHeight  int
	conversation int
}

// homeMetrics 计算对话界面各块尺寸：
// 自下而上是「提示行 / 输入框 / （菜单打开时的下拉框）」，其余空间留给对话区。
// 其中「提示行」是输入框下面的三行：**空行间距 + 常驻操作提示 + notice**（见 renderHomeLayout）。
// 空行是用户 2026-09-20 要求的：提示紧贴框下沿太挤，参考 OpenCode 隔开一行。
func (m Model) homeMetrics() homeMetrics {
	contentWidth := max(m.viewport.Width(), 1)
	boxWidth := min(contentWidth-8, 72)
	if boxWidth < 24 {
		boxWidth = max(contentWidth-2, 16)
	}
	box := m.renderPromptBox(boxWidth)
	menuHeight := m.menuRows()
	// 输入框 + 间距空行 + 常驻操作提示行 + notice 行
	blockHeight := menuHeight + lipgloss.Height(box) + 3
	return homeMetrics{
		contentWidth: contentWidth,
		boxWidth:     boxWidth,
		boxVisual:    lipgloss.Width(box),
		menuHeight:   menuHeight,
		blockHeight:  blockHeight,
		conversation: max(m.height-blockHeight, 1),
	}
}

// scrollChat 调整对话区向上滚动的行数（delta 为正表示回看更早的内容）。
func (m *Model) scrollChat(delta int) {
	metrics := m.homeMetrics()
	total := len(m.conversationLines(metrics.contentWidth))
	maxOffset := max(total-metrics.conversation, 0)
	m.chatOffset = min(max(m.chatOffset+delta, 0), maxOffset)
}

// renderHomeLayout 渲染首页对话界面：
// 自下而上是「提示行 / 输入框 / 打开时的下拉框」，上方是对话区（logo + 历史回答）。
// 对话区底部对齐：新回答紧贴输入框，旧内容连同 logo 一起被顶上去。
func (m Model) renderHomeLayout(contentWidth int) string {
	metrics := m.homeMetrics()
	center := lipgloss.NewStyle().Width(contentWidth).Align(lipgloss.Center)

	lines := m.conversationLines(contentWidth)
	end := min(max(len(lines)-m.chatOffset, 0), len(lines))
	start := max(end-metrics.conversation, 0)
	window := lines[start:end]

	parts := make([]string, 0, metrics.blockHeight+metrics.conversation)
	// 内容不足一屏时在顶部补空行，让对话贴着底部（输入框上方）。
	for index := 0; index < metrics.conversation-len(window); index++ {
		parts = append(parts, "")
	}
	parts = append(parts, window...)

	if m.menuOpen {
		// 下拉框不使用背景色，各选项长度不一。先补齐到与输入框等宽再整体居中，
		// 否则 lipgloss 会逐行居中、左边缘参差不齐；左侧缩进让选项与输入框文字对齐。
		menu := lipgloss.NewStyle().
			Width(metrics.boxVisual).
			PaddingLeft(3).
			Render(m.renderMenu(metrics.boxVisual))
		parts = append(parts, strings.Split(center.Render(menu), "\n")...)
	}

	parts = append(parts, strings.Split(center.Render(m.renderPromptBox(metrics.boxWidth)), "\n")...)
	// 输入框下面三行：间距空行 + 常驻操作提示（贴左缘）+ notice。
	// 空行让提示与框拉开距离（用户 2026-09-20：紧贴太挤）。
	parts = append(parts, "")
	parts = append(parts, m.renderHomeHint(metrics))
	parts = append(parts, center.Render(m.renderHomeTip(contentWidth)))
	return strings.Join(parts, "\n")
}

// 输入框空态占位文案（用户 2026-09-19 指定）。
// 快捷键提示从框内那一行挪到了这里，所以框里只剩一行输入区。
// 终端窄到放不下整句时换短句，避免占位文案被拦腰截断。
const (
	homePlaceholder         = "Ask me anything, or press / for shortcuts."
	homePlaceholderShort    = "Ask me anything, or press /"
	homePlaceholderMinWidth = 62
)

// applyPlaceholder 按当前宽度选占位文案。窗口尺寸一变就要重算，
// 否则拉宽 / 收窄终端后占位文案会停在旧的那句上。
func (m *Model) applyPlaceholder() {
	if m.viewport.Width() >= homePlaceholderMinWidth {
		m.input.Placeholder = homePlaceholder
		return
	}
	m.input.Placeholder = homePlaceholderShort
}

// renderPromptBox 渲染首页输入框：左右各一条主题色细竖线，中间一整块实心底色。
// 框内只有一行输入区，上下各留一行内边距让它垂直居中；
// 快捷键提示不占框内第二行，那部分由占位符承担。
func (m Model) renderPromptBox(boxWidth int) string {
	// 提示字符会闪、会乱序跳（见 cursor.go）。它任何一帧都只占 1 列，
	// 所以这一行的宽度恒定，输入框不会跟着抖。
	glyph, glyphStyle := m.cursorGlyph()
	prefix := glyphStyle.Render(glyph) + " "
	// 菜单打开时输入框内容恒为 "/"（再输任何字符都会立即关菜单），
	// 自己渲染 "/" 加光标，绕开 textinput 的行内补全残留。
	var line string
	if m.menuOpen {
		line = prefix + m.styles.prompt.Render("/") + lipgloss.NewStyle().Reverse(true).Render(" ")
	} else {
		line = prefix + m.input.View()
	}
	return m.renderDialog(line, boxWidth, 1)
}

// 首页常驻收尾文案（用户 2026-09-19 指定，位置在 logo 与输入框之间）。
// 行尾的 "_" 是静态光标——项目坚持「无持续动画」，所以它不闪烁。
const (
	greetingHeadline = "感谢看到最后！"
	greetingInvite   = "随便问问、随便逛逛，也期待有机会进一步交流"
	greetingCursor   = "_"
	greetingTagline  = "Design Without Boundaries"
)

// greetingLines 渲染 logo 与输入框之间那段收尾文案，返回已按内容宽度居中的若干行。
// 它紧跟在 logo 下方（见 conversationLines）：空对话时正好落在 logo 与输入框之间，
// 有回答后会与 logo 一起被顶上去、最终离屏。**不要改成常驻输入框上方。**
func (m Model) greetingLines(width int) []string {
	available := max(width, 1)
	center := lipgloss.NewStyle().Width(available).Align(lipgloss.Center)
	invite := m.styles.muted.Render(greetingInvite) + m.styles.themeText.Render(greetingCursor)
	block := strings.Join([]string{
		m.styles.title.Render(greetingHeadline),
		invite,
		m.styles.themeText.Render(greetingTagline),
	}, "\n")
	return strings.Split(center.Render(ansi.Wrap(block, available, "")), "\n")
}

// renderHomeTip 是输入框下方的那一行：平时留空，只在有 notice 时显示反馈。
// 快捷键提示已经交给占位符（homePlaceholder），这里不再常驻任何文案；
// 这一行仍然要保留，否则 notice 出现/消失会让输入框位置上下跳。
func (m Model) renderHomeTip(width int) string {
	if m.notice == "" {
		return ""
	}
	prefix := "[i] "
	style := m.styles.muted
	switch m.noticeKind {
	case noticeSuccess:
		prefix, style = "[ok] ", m.styles.success
	case noticeError:
		prefix, style = "[error] ", m.styles.error
	}
	return ansi.Wrap(style.Render(prefix+m.notice), max(width, 1), "")
}

// 首页输入框下方的常驻操作提示（用户 2026-09-20 指定）：样式照 OpenCode 的提示行——
// 灰字、贴着输入框左缘左对齐，且与输入框之间**隔一个空行**（贴太紧太挤）。
// 2026-09-20 二次收窄：用户要求只留双击 Esc 这一条，原先那串
// `| ctrl+c 退出 | / 命令 | enter 发送` 全部去掉——提示越干净越有人看。
// 文案短到任何能用的宽度都放得下，不再需要「窄屏换短句」那一套。
const (
	homeHint      = "连按两次 esc 退出"
	homeHintArmed = "再按一次 esc 退出"
)

// renderHomeHint 渲染输入框下方那行常驻操作提示。
// 左缘与输入框对齐：输入框是居中渲染的，它在内容宽度里的左侧留白就是提示行的缩进
// （用和 lipgloss Align(Center) 相同的整除算法，见 align.go 的 left = shortAmount/2）。
// 武装中时换一句更亮的文案，告诉用户还差一下。
func (m Model) renderHomeHint(metrics homeMetrics) string {
	text, style := homeHint, m.styles.dim
	if m.escArmed {
		text, style = homeHintArmed, m.styles.muted
	}
	indent := max((metrics.contentWidth-metrics.boxVisual)/2, 0)
	if indent >= metrics.contentWidth {
		indent = 0
	}
	return strings.Repeat(" ", indent) +
		style.Render(ansi.Truncate(text, max(metrics.contentWidth-indent, 1), ""))
}

func (m Model) renderAbout() string {
	var b strings.Builder
	b.WriteString(m.pageTitle("ABOUT", "关于我"))
	b.WriteString("\n\n")
	b.WriteString(m.styles.title.Render(m.resume.Profile.ChineseName + "  /  " + strings.Join(m.resume.Profile.Roles, " · ")))
	b.WriteString("\n")
	b.WriteString(m.styles.muted.Render(m.resume.Profile.Location + "  ·  " + m.resume.Status.Headline))
	b.WriteString("\n\n")
	b.WriteString(m.wrap(m.resume.About.Summary, m.viewport.Width()))
	b.WriteString("\n\n")
	b.WriteString(m.styles.section.Render("WORKING PRINCIPLES / 工作方式"))
	for _, principle := range m.resume.About.Principles {
		b.WriteString("\n")
		b.WriteString(m.bullet(principle))
	}
	return b.String()
}

func (m Model) renderExperience() string {
	var b strings.Builder
	b.WriteString(m.pageTitle("EXPERIENCE", "工作经历"))
	for _, item := range m.resume.Experience {
		b.WriteString("\n\n")
		b.WriteString(m.styles.accent.Render(item.Period))
		b.WriteString("  ")
		b.WriteString(m.styles.title.Render(item.Role))
		b.WriteString("\n")
		b.WriteString(m.styles.muted.Render(item.Company))
		b.WriteString("\n")
		b.WriteString(m.wrap(item.Description, m.viewport.Width()))
		for _, highlight := range item.Highlights {
			b.WriteString("\n")
			b.WriteString(m.bullet(highlight))
		}
	}
	return b.String()
}

func (m Model) renderProjects() string {
	var b strings.Builder
	b.WriteString(m.pageTitle("PROJECTS", "精选项目"))
	if len(m.resume.Projects) == 0 {
		b.WriteString("\n\n")
		b.WriteString(m.styles.muted.Render("暂时没有项目。请编辑 profile/resume.yaml。"))
		return b.String()
	}
	for index, project := range m.resume.Projects {
		b.WriteString("\n\n")
		prefix := "  "
		style := m.styles.unselected
		if index == m.selectedProject {
			prefix = "> "
			style = m.styles.selected
		}
		b.WriteString(style.Render(prefix + project.ID + "  " + project.Title))
		b.WriteString(m.styles.dim.Render("  " + project.Year))
		b.WriteString("\n")
		b.WriteString(m.indented(project.Subtitle, 4))
		b.WriteString("\n")
		b.WriteString(strings.Repeat(" ", 4))
		b.WriteString(m.renderTags(project.Tags))
	}
	return b.String()
}

func (m Model) renderProject() string {
	project, ok := m.resume.ProjectByID(m.activeProjectID)
	if !ok {
		return m.styles.error.Render("项目内容不可用。按 Esc 返回项目列表。")
	}
	var b strings.Builder
	b.WriteString(m.pageTitle("PROJECT "+project.ID, project.Title))
	b.WriteString("\n")
	b.WriteString(m.styles.muted.Render(project.Year + "  ·  " + project.Role))
	b.WriteString("\n")
	b.WriteString(m.renderTags(project.Tags))

	sections := []struct {
		label string
		text  string
	}{
		{"问题", project.Problem},
		{"关键决策", project.Decision},
		{"结果", project.Result},
	}
	for _, section := range sections {
		b.WriteString("\n\n")
		b.WriteString(m.styles.section.Render(section.label))
		b.WriteString("\n")
		b.WriteString(m.wrap(section.text, m.viewport.Width()))
	}

	b.WriteString("\n\n")
	b.WriteString(m.styles.section.Render("交付"))
	for _, deliverable := range project.Deliverables {
		b.WriteString("\n")
		b.WriteString(m.bullet(deliverable))
	}
	if project.Link != "" {
		b.WriteString("\n\n")
		b.WriteString(m.styles.section.Render("查看项目"))
		b.WriteString("\n")
		b.WriteString(m.hyperlink(project.Link, project.Link))
	}
	return b.String()
}

func (m Model) renderSkills() string {
	var b strings.Builder
	b.WriteString(m.pageTitle("SKILLS", "能力图谱"))
	for _, group := range m.resume.Skills {
		b.WriteString("\n\n")
		b.WriteString(m.styles.section.Render(group.Name))
		b.WriteString("\n")
		b.WriteString(m.wrap(strings.Join(group.Items, "  /  "), m.viewport.Width()))
	}
	b.WriteString("\n\n")
	b.WriteString(m.styles.muted.Render("能力不是工具清单：项目详情中展示了它们如何共同解决问题。"))
	return b.String()
}

func (m Model) renderContact() string {
	var b strings.Builder
	b.WriteString(m.pageTitle("CONTACT", "联系方式"))
	b.WriteString("\n\n")
	b.WriteString(m.styles.title.Render(m.resume.Status.Headline))
	b.WriteString("\n\n")
	contacts := []struct {
		label string
		value string
		url   string
	}{
		{"EMAIL", m.resume.Profile.Email, "mailto:" + m.resume.Profile.Email},
		{"WEBSITE", m.resume.Profile.Website, m.resume.Profile.Website},
		{"GITHUB", m.resume.Profile.GitHub, m.resume.Profile.GitHub},
	}
	for _, contact := range contacts {
		b.WriteString(m.styles.section.Render(contact.label))
		b.WriteString("\n")
		b.WriteString(m.hyperlink(contact.url, contact.value))
		b.WriteString("\n\n")
	}
	// RESUME 单独走一支：有地址就是可点击的下载链接，
	// 没地址就渲染「等待简历下载中」的进度占位（见 resumePendingBlock）。
	b.WriteString(m.styles.section.Render("RESUME"))
	b.WriteString("\n")
	if url := strings.TrimSpace(m.resume.Profile.ResumeURL); url != "" {
		b.WriteString(m.hyperlink(url, url))
	} else {
		b.WriteString(m.resumePendingBlock())
	}
	b.WriteString("\n\n")
	b.WriteString(m.styles.muted.Render("感谢你花时间在终端中认识我。"))
	return b.String()
}

// 简历 PDF 还没准备好时的占位（用户 2026-09-19 指定）。
//
// 原来是模板里那条假链接（https://zhang.design/resume.pdf——域名根本不存在），
// 访客点开只会 404。现在换成一条「还在准备」的进度占位；
// 等真实 PDF 上线，把 profile.resume_url 填上就自动变回下载链接，不用改代码。
//
// 进度条只用 `█` 和空格：`█` 是本项目 logo 已经在用的字符，
// 刻意不再引入 ░ / ▒ 这类 East Asian Width = Ambiguous 的新字形——
// 它们在偏好 CJK 字体的终端里可能被排成两列，把整行挤歪。
const (
	resumePendingHeadline = "等待简历下载中"
	resumePendingNote     = "简历 PDF 正在收尾，准备好后会挂在这里。"
	resumeBarWidth        = 30
	resumeBarFilled       = 18
)

// resumePendingBlock 渲染简历未就绪时的占位：一行说明 + 一条进度条。
func (m Model) resumePendingBlock() string {
	filled := strings.Repeat("█", resumeBarFilled)
	empty := strings.Repeat(" ", max(resumeBarWidth-resumeBarFilled, 0))
	return m.styles.muted.Render(resumePendingNote) + "\n" +
		"[" + m.styles.accent.Render(filled) + empty + "] " +
		m.styles.muted.Render(resumePendingHeadline)
}

func (m Model) renderStatusPage() string {
	var b strings.Builder
	b.WriteString(m.pageTitle("STATUS", "当前状态"))
	status := m.resume.Status
	b.WriteString("\n\n")
	b.WriteString(m.styles.title.Render(status.Headline))
	if len(status.Roles) > 0 {
		b.WriteString("\n\n")
		b.WriteString(m.styles.section.Render("TARGET ROLES / 目标岗位"))
		for _, role := range status.Roles {
			b.WriteString("\n")
			b.WriteString(m.bullet(role))
		}
	}
	if len(status.Focus) > 0 {
		b.WriteString("\n\n")
		b.WriteString(m.styles.section.Render("FOCUS / 当前关注"))
		for _, item := range status.Focus {
			b.WriteString("\n")
			b.WriteString(m.bullet(item))
		}
	}
	return b.String()
}

func (m Model) renderLogPage() string {
	var b strings.Builder
	b.WriteString(m.pageTitle("LOG", "学习与构建记录"))
	if len(m.resume.Log) == 0 {
		b.WriteString("\n\n")
		b.WriteString(m.styles.muted.Render("暂时没有记录。请编辑 profile/resume.yaml。"))
		return b.String()
	}
	for _, entry := range m.resume.Log {
		b.WriteString("\n\n")
		b.WriteString(m.styles.accent.Render(entry.Date))
		b.WriteString("  ")
		if strings.TrimSpace(entry.Kind) != "" {
			b.WriteString(m.styles.tag.Render(entry.Kind))
			b.WriteString("  ")
		}
		b.WriteString(m.styles.body.Render(entry.Text))
	}
	b.WriteString("\n\n")
	b.WriteString(m.styles.muted.Render("这里记录最近在学、在建、在试的东西。"))
	return b.String()
}

func (m Model) renderRandomPage() string {
	var b strings.Builder
	b.WriteString(m.pageTitle("RANDOM", "别处没有的东西"))
	if len(m.resume.Random) == 0 {
		b.WriteString("\n\n")
		b.WriteString(m.styles.muted.Render("暂时没有内容。请编辑 profile/resume.yaml。"))
		return b.String()
	}
	for _, note := range m.resume.Random {
		b.WriteString("\n\n")
		b.WriteString(m.styles.section.Render(note.Label))
		b.WriteString("\n")
		b.WriteString(m.wrap(note.Text, m.viewport.Width()))
	}
	return b.String()
}

func (m Model) renderHelp() string {
	var b strings.Builder
	b.WriteString(m.pageTitle("HELP", "命令与快捷键"))
	commands := [][2]string{
		{"/about /personal", "关于我"},
		{"/experience", "工作经历"},
		{"/projects", "项目列表"},
		{"/project 01", "打开项目详情"},
		{"/skills", "能力与工具"},
		{"/status", "当前状态与求职方向"},
		{"/log", "学习与构建记录"},
		{"/random", "别处没有的东西"},
		{"/contact", "联系方式"},
		{"/resume", "PDF 简历地址"},
		{"/theme cyan|mono", "彩色 / 黑白主题"},
		{"/clear", "清空当前对话"},
		{"/exit", "退出程序"},
	}
	for _, command := range commands {
		b.WriteString("\n")
		b.WriteString(m.styles.accent.Render(fmt.Sprintf("%-19s", command[0])))
		b.WriteString(m.styles.body.Render(command[1]))
	}
	b.WriteString("\n\n")
	b.WriteString(m.styles.section.Render("KEYBOARD / 键盘"))
	keys := []string{
		"/               打开快捷命令菜单",
		"Tab             接受自动补全建议",
		"Ctrl+P          打开这个帮助页",
		"↑ / ↓           滚动对话（输入框为空时）",
		"PgUp / PgDn     上下翻页",
		"Esc             清空输入 · 回看历史时回到最新",
		"Esc Esc         连按两次退出程序",
		"Ctrl+C          随时退出",
	}
	for _, key := range keys {
		b.WriteString("\n")
		b.WriteString(m.styles.body.Render(key))
	}
	return b.String()
}

func (m Model) pageTitle(kicker, title string) string {
	return m.styles.accent.Render(kicker) + m.styles.dim.Render(" / ") + m.styles.title.Render(title)
}

func (m Model) wrap(text string, width int) string {
	return ansi.Wrap(m.styles.body.Render(text), max(width, 12), "")
}

func (m Model) indented(text string, indent int) string {
	available := max(m.viewport.Width()-indent, 12)
	wrapped := strings.Split(ansi.Wrap(m.styles.muted.Render(text), available, ""), "\n")
	prefix := strings.Repeat(" ", indent)
	for i := range wrapped {
		wrapped[i] = prefix + wrapped[i]
	}
	return strings.Join(wrapped, "\n")
}

func (m Model) bullet(text string) string {
	prefix := m.styles.accent.Render("+ ")
	available := max(m.viewport.Width()-2, 12)
	lines := strings.Split(ansi.Wrap(m.styles.body.Render(text), available, ""), "\n")
	for index := range lines {
		if index == 0 {
			lines[index] = prefix + lines[index]
		} else {
			lines[index] = "  " + lines[index]
		}
	}
	return strings.Join(lines, "\n")
}

func (m Model) renderTags(tags []string) string {
	if len(tags) == 0 {
		return ""
	}
	if m.viewport.Width() < 48 {
		return m.styles.muted.Render(strings.Join(tags, " / "))
	}
	rendered := make([]string, 0, len(tags))
	for _, tag := range tags {
		rendered = append(rendered, m.styles.tag.Render(tag))
	}
	return strings.Join(rendered, " ")
}

func (m Model) hyperlink(url, label string) string {
	if strings.TrimSpace(url) == "" {
		return m.styles.muted.Render("暂未填写")
	}
	styled := m.styles.link.Render(label)
	return "\x1b]8;;" + url + "\x1b\\" + styled + "\x1b]8;;\x1b\\"
}

func (m Model) HasCommandSuggestion(value string) bool {
	return slices.Contains(m.commandSuggestions(), value)
}

func RuneCount(value string) int {
	return utf8.RuneCountInString(value)
}
