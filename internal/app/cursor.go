package app

import (
	"math/rand"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// 输入框提示字符的动画。节奏、字符集、颜色规则都照搬网站模块标题的光标
// （Zima_room2.0/src/components/AsciiTitle.astro）：
//
//	闪烁 10 步 × 300ms（步 0 起为暗）→ 停 560ms
//	→ 乱序 16 帧 × 64ms（8 个字符各洗一遍，走两轮）→ 停 720ms → 回到闪烁
//
// 首帧前再各延迟 cursorStartDelay，让刚进页面时先安静一下（与网页一致）。
//
// 性能：最密的阶段是乱序的 64ms 一帧（≈16fps），且每帧只改一个单元格的字符，
// 渲染器（bubbletea 标准渲染器做帧间差分）每个 tick 实际只往终端写十几到几十字节。
// 比已经在跑的流式回答（16ms 一帧 ≈62fps）还稀 4 倍，不会拖慢输入处理。
const (
	cursorBaseGlyph          = "›"
	cursorHiddenGlyph        = " "
	cursorBlinkSteps         = 10
	cursorBlinkInterval      = 300 * time.Millisecond
	cursorBlinkToScrambleGap = 560 * time.Millisecond
	cursorScrambleFrame      = 64 * time.Millisecond
	cursorScrambleRounds     = 2
	cursorScrambleToBlinkGap = 720 * time.Millisecond
	cursorStartDelay         = 680 * time.Millisecond
)

// cursorGlyphs 是乱序时轮换的字符集，与网页完全一致（网页那套里本来就有 '>'）。
// 全部是 1 列宽的 ASCII —— 换成宽字符会让输入框左右抖。
var cursorGlyphs = []string{"x", ">", "/", "&", "-", "~", "^", "="}

// cursorPhase 是动画的两个阶段。
type cursorPhase int

const (
	cursorBlink cursorPhase = iota
	cursorScramble
)

// cursorTickMsg 由 tea.Tick 定时投递，每次推进一帧。
type cursorTickMsg struct{}

func cursorTickCmd(delay time.Duration) tea.Cmd {
	return tea.Tick(delay, func(time.Time) tea.Msg { return cursorTickMsg{} })
}

// advanceCursor 推进一帧并返回下一帧该等的间隔。
// 状态机与网页的 runBlink / runScramble 一一对应：
// 闪烁阶段 step 从 0 数到 cursorBlinkSteps，然后停一拍转入乱序；
// 乱序阶段 step 依次吃掉 cursorSequence，吃完停一拍回到闪烁。
func (m *Model) advanceCursor() time.Duration {
	if m.cursorPhase == cursorScramble {
		if m.cursorStep >= len(m.cursorSequence) {
			m.cursorPhase, m.cursorStep, m.cursorSequence = cursorBlink, 0, nil
			return cursorScrambleToBlinkGap
		}
		m.cursorStep++
		return cursorScrambleFrame
	}

	if m.cursorStep >= cursorBlinkSteps {
		m.cursorPhase, m.cursorStep = cursorScramble, 0
		m.cursorSequence = newCursorSequence()
		return cursorBlinkToScrambleGap
	}
	m.cursorStep++
	return cursorBlinkInterval
}

// cursorGlyph 返回当前帧的字符与样式。
// 暗态返回一个空格而不是空串：空串会让输入框整行左移一格，布局会抖。
// 乱序帧临时换成灰色，和网页里 scrambling 态转灰的规则一致。
func (m Model) cursorGlyph() (string, lipgloss.Style) {
	switch {
	case m.cursorPhase == cursorScramble && m.cursorStep > 0 && m.cursorStep <= len(m.cursorSequence):
		return m.cursorSequence[m.cursorStep-1], m.styles.muted
	case m.cursorPhase == cursorBlink && m.cursorStep%2 == 1:
		return cursorHiddenGlyph, m.styles.animatedCursor
	default:
		return cursorBaseGlyph, m.styles.animatedCursor
	}
}

// newCursorSequence 把 8 个字符洗两轮拼成 16 帧，并避免两轮交界处撞成同一个字符
// （网页的 shuffledCharacters(avoidFirst) 就是这个意思）。
func newCursorSequence() []string {
	sequence := make([]string, 0, len(cursorGlyphs)*cursorScrambleRounds)
	previous := ""
	for round := 0; round < cursorScrambleRounds; round++ {
		shuffled := append([]string(nil), cursorGlyphs...)
		rand.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
		if previous != "" && shuffled[0] == previous {
			last := len(shuffled) - 1
			shuffled[0], shuffled[last] = shuffled[last], shuffled[0]
		}
		sequence = append(sequence, shuffled...)
		previous = shuffled[len(shuffled)-1]
	}
	return sequence
}
