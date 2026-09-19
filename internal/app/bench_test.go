package app

import (
	"strings"
	"testing"

	"github.com/zhangsizhou/terminal-resume/profile"
)

func benchModel(b *testing.B) Model {
	b.Helper()
	resume, err := profile.Load("")
	if err != nil {
		b.Fatal(err)
	}
	return New(resume, false)
}

// benchmarkHomeLayout 复用一组固定尺寸的模型，避免把 New 的开销算进渲染里。
func benchmarkHomeLayout(b *testing.B, answers int) {
	b.Helper()
	m := benchModel(b)
	m.width, m.height = 100, 30
	m.recalculateLayout()
	for index := 0; index < answers; index++ {
		m.submit("/skills")
		m.finishStreaming()
	}
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		_ = m.renderHomeLayout(m.viewport.Width())
	}
}

// BenchmarkHomeFrame 是动画每个 tick 都要付的代价：渲染一帧首页。
func BenchmarkHomeFrame(b *testing.B) {
	benchmarkHomeLayout(b, 0)
}

// BenchmarkHomeFrameWithLongChat 对话攒了 40 条回答之后的一帧
// （动画帧率与流式帧率都要付这个代价，是最坏情况）。
func BenchmarkHomeFrameWithLongChat(b *testing.B) {
	benchmarkHomeLayout(b, 40)
}

// BenchmarkCursorTick 只看动画自己的状态机开销（不含渲染）。
func BenchmarkCursorTick(b *testing.B) {
	m := benchModel(b)
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		m.advanceCursor()
	}
}

// BenchmarkStreamFrame 对照项：流式回答每 16ms 走一次同样的渲染，
// 也就是动画在乱序阶段（64ms 一帧）的 4 倍频。
func BenchmarkStreamFrame(b *testing.B) {
	m := benchModel(b)
	m.width, m.height = 100, 30
	m.recalculateLayout()
	m.submit("/skills")
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		m.advanceStream()
		_ = m.renderHomeLayout(m.viewport.Width())
	}
}

// 保证基准里那份占位文案与我们锁定的常量一致，避免基准悄悄跑在别的状态上。
var _ = strings.Contains(homePlaceholder, "press / for shortcuts")
