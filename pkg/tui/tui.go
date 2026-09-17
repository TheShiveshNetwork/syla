package tui

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"math/rand"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/TheShiveshNetwork/syla/pkg/loop"
	"github.com/TheShiveshNetwork/syla/static"
)

// Theme: white/light-gray on a plain background, with a mint accent for
// the alligator title and stat icons.
var (
	colorWhite     = lipgloss.Color("#F5F5F5")
	colorLightGray = lipgloss.Color("#A3A3A3")
	colorDimGray   = lipgloss.Color("#6B7280")
	colorErrorRed  = lipgloss.Color("196")
	colorMint      = lipgloss.Color("#7CFFC4")
	colorLeaf      = lipgloss.Color("#4ADE80")

	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(colorWhite)
	helpStyle  = lipgloss.NewStyle().Foreground(colorDimGray)
	barStyle   = lipgloss.NewStyle().Foreground(colorLightGray)

	sparkleStyle    = lipgloss.NewStyle().Foreground(colorMint)
	dimSparkleStyle = lipgloss.NewStyle().Foreground(colorLightGray)
	leafStyle       = lipgloss.NewStyle().Foreground(colorLeaf)
)

const (
	contentMaxWidth   = 62
	minMarginForDecor = 4
)

func safeGotoBottom(vp *viewport.Model) {
	defer func() { _ = recover() }()
	if vp.Height < 1 {
		vp.Height = 1
	}
	vp.GotoBottom()
}

var (
	alliImg    image.Image
	alliOnce   sync.Once
	blockCache sync.Map
)

func getAlliImg() image.Image {
	alliOnce.Do(func() {
		img, err := png.Decode(bytes.NewReader(static.AlliPNG))
		if err == nil {
			alliImg = img
		}
	})
	return alliImg
}

func formatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	d = d.Truncate(time.Second)
	total := int(d.Seconds())
	h := total / 3600
	m := (total % 3600) / 60
	s := total % 60
	if h > 0 {
		return fmt.Sprintf("%dh %dm %ds", h, m, s)
	}
	if m > 0 {
		return fmt.Sprintf("%dm %ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

// formatClock renders elapsed time as a zero-padded HH:MM:SS clock, used
// for the big "running for" display.
func formatClock(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	d = d.Truncate(time.Second)
	total := int(d.Seconds())
	h := total / 3600
	m := (total % 3600) / 60
	s := total % 60
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}

func effectiveContentWidth(w int) int {
	if w <= 0 {
		return contentMaxWidth
	}
	if w < contentMaxWidth {
		return w
	}
	return contentMaxWidth
}

// renderScreen centers `main` (both axes) above `footer`, which is pinned
// to the bottom. When there's room on the sides, it flanks `main` with
// decorative leaf/sparkle glyphs so the layout doesn't need image assets.
func renderScreen(w, h int, main string, footer string) string {
	if w <= 0 {
		w = 80
	}
	if h <= 0 {
		h = 24
	}
	footerHeight := lipgloss.Height(footer)
	if footerHeight < 1 {
		footerHeight = 1
	}
	mainHeight := h - footerHeight
	if mainHeight < 1 {
		mainHeight = 1
	}

	cw := effectiveContentWidth(w)
	centeredMain := lipgloss.NewStyle().
		Width(cw).
		Height(mainHeight).
		Align(lipgloss.Center).
		AlignVertical(lipgloss.Center).
		Render(main)

	marginWidth := (w - cw) / 2
	if marginWidth >= minMarginForDecor {
		left := buildMargin(marginWidth, mainHeight, randomizedDecor(leftDecorSpecs, w, h, 11))
		right := buildMargin(marginWidth, mainHeight, randomizedDecor(rightDecorSpecs, w, h, 29))
		centeredMain = lipgloss.JoinHorizontal(lipgloss.Top, left, centeredMain, right)
	}
	centeredMain = lipgloss.PlaceHorizontal(w, lipgloss.Center, centeredMain)

	return lipgloss.JoinVertical(lipgloss.Left, centeredMain, footer)
}

// Decorative leaves/sparkles are plain unicode glyphs, not image assets:
// each one is placed in its own grid cell so styling it never shifts the
// column position of anything else on the same line.

type decoration struct {
	rowFrac, colFrac float64
	glyph            string
	style            lipgloss.Style
}

var leftDecorSpecs = []decoration{
	{0.08, 0.30, "🍃", leafStyle},
	{0.18, 0.65, "🍂", dimSparkleStyle},
	{0.28, 0.15, "🍃", sparkleStyle},
	{0.38, 0.70, "🍁", leafStyle},
	{0.52, 0.20, "🍃", dimSparkleStyle},
	{0.66, 0.55, "🍂", leafStyle},
	{0.78, 0.30, "🍃", sparkleStyle},
	{0.88, 0.60, "🍁", dimSparkleStyle},
}

var rightDecorSpecs = []decoration{
	{0.10, 0.45, "🍃", leafStyle},
	{0.20, 0.20, "🍂", dimSparkleStyle},
	{0.34, 0.75, "🍃", sparkleStyle},
	{0.46, 0.35, "🍁", leafStyle},
	{0.58, 0.10, "🍃", dimSparkleStyle},
	{0.70, 0.60, "🍂", leafStyle},
	{0.82, 0.25, "🍃", leafStyle},
	{0.90, 0.50, "🍁", dimSparkleStyle},
}

// centerGrass renders grasses below the mascot using 🌿 at random positions.
func centerGrass(w int) string {
	if w < 20 {
		return ""
	}
	r := rand.New(rand.NewSource(int64(w*7919 + 42)))
	line := make([]string, w)
	for i := range line {
		line[i] = " "
	}
	numGrass := 5 + r.Intn(3)
	for i := 0; i < numGrass; i++ {
		pos := r.Intn(w - 2)
		if line[pos] != " " || line[pos+1] != " " {
			continue
		}
		line[pos] = "🌿"
	}
	grassStr := strings.Join(line, "")
	grassStr = strings.ReplaceAll(grassStr, "  ", " ")
	style := lipgloss.NewStyle().Foreground(lipgloss.Color("#2d6a4f"))
	return lipgloss.PlaceHorizontal(w, lipgloss.Center, style.Render(strings.TrimSpace(grassStr)))
}

// randomizeDecor adds rotation/variation by randomly picking emoji and style offset.
// Called per render to give organic feel while keeping deterministic seed for stability.
func randomizedDecor(base []decoration, w, h int, seed int64) []decoration {
	r := rand.New(rand.NewSource(seed + int64(w*31+h*17)))
	out := make([]decoration, len(base))
	copy(out, base)
	// Add slight random jitter to row/col and random rotation via emoji choice
	choices := []string{"🍃", "🍁", "🍂"}
	for i, d := range out {
		jitterRow := (r.Float64() - 0.5) * 0.06
		jitterCol := (r.Float64() - 0.5) * 0.08
		nr := d.rowFrac + jitterRow
		nc := d.colFrac + jitterCol
		if nr < 0 {
			nr = 0
		}
		if nr > 0.95 {
			nr = 0.95
		}
		if nc < 0 {
			nc = 0
		}
		if nc > 0.95 {
			nc = 0.95
		}
		out[i].rowFrac = nr
		out[i].colFrac = nc
		// Randomly swap leaf type for variation towards bottom
		if nr > 0.6 && r.Float64() < 0.5 {
			out[i].glyph = choices[r.Intn(len(choices))]
		}
	}
	return out
}

func buildMargin(width, height int, specs []decoration) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	grid := make([][]string, height)
	for r := range grid {
		grid[r] = make([]string, width)
		for c := range grid[r] {
			grid[r][c] = " "
		}
	}
	for _, d := range specs {
		row := int(d.rowFrac * float64(height))
		col := int(d.colFrac * float64(width))
		if row >= 0 && row < height && col >= 0 && col < width {
			grid[row][col] = d.style.Render(d.glyph)
		}
	}
	lines := make([]string, height)
	for r, row := range grid {
		lines[r] = strings.Join(row, "")
	}
	return strings.Join(lines, "\n")
}

func renderTitle(w int, done bool) string {
	var t1, t2 string
	if done {
		t1 = "no more wait,"
		t2 = "I'm already great."
	} else {
		t1 = "See you later,"
		t2 = "alligator."
	}
	// Increased size via extra padding and bold, centered. Keep normal
	// lipgloss rendering to stay compact enough to fit with large image
	// and block numbers, while still feeling larger than before.
	line1 := lipgloss.NewStyle().
		Bold(true).
		Foreground(colorWhite).
		Padding(0, 1).
		Width(w).
		Align(lipgloss.Center).
		Render(t1)
	line2 := lipgloss.NewStyle().
		Bold(true).
		Foreground(colorMint).
		Padding(0, 1).
		Width(w).
		Align(lipgloss.Center).
		Render(t2)
	block := lipgloss.JoinVertical(lipgloss.Center, line1, line2)
	return lipgloss.NewStyle().
		Padding(1, 0).
		Margin(1, 0).
		Width(w).
		Align(lipgloss.Center).
		Render(block)
}

func averageRegion(img image.Image, x0, x1, y0, y1 int) (r, g, b, a uint8) {
	if x1 <= x0 {
		x1 = x0 + 1
	}
	if y1 <= y0 {
		y1 = y0 + 1
	}
	var rs, gs, bs, as, n uint64
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			cr, cg, cb, ca := img.At(x, y).RGBA()
			rs += uint64(cr >> 8)
			gs += uint64(cg >> 8)
			bs += uint64(cb >> 8)
			as += uint64(ca >> 8)
			n++
		}
	}
	if n == 0 {
		n = 1
	}
	return uint8(rs / n), uint8(gs / n), uint8(bs / n), uint8(as / n)
}

// Block font for large title and numbers - 5x7 pixel font using block chars.
var blockFont = map[rune][]string{
	'A': {"01110", "10001", "10001", "11111", "10001", "10001", "10001"},
	'B': {"11110", "10001", "10001", "11110", "10001", "10001", "11110"},
	'C': {"01110", "10001", "10000", "10000", "10000", "10001", "01110"},
	'D': {"11110", "10001", "10001", "10001", "10001", "10001", "11110"},
	'E': {"11111", "10000", "10000", "11110", "10000", "10000", "11111"},
	'F': {"11111", "10000", "10000", "11110", "10000", "10000", "10000"},
	'G': {"01110", "10001", "10000", "10111", "10001", "10001", "01110"},
	'H': {"10001", "10001", "10001", "11111", "10001", "10001", "10001"},
	'I': {"01110", "00100", "00100", "00100", "00100", "00100", "01110"},
	'J': {"00111", "00010", "00010", "00010", "10010", "10010", "01100"},
	'K': {"10001", "10010", "10100", "11000", "10100", "10010", "10001"},
	'L': {"10000", "10000", "10000", "10000", "10000", "10000", "11111"},
	'M': {"10001", "11011", "10101", "10101", "10001", "10001", "10001"},
	'N': {"10001", "11001", "10101", "10011", "10001", "10001", "10001"},
	'O': {"01110", "10001", "10001", "10001", "10001", "10001", "01110"},
	'P': {"11110", "10001", "10001", "11110", "10000", "10000", "10000"},
	'Q': {"01110", "10001", "10001", "10001", "10101", "10010", "01101"},
	'R': {"11110", "10001", "10001", "11110", "10100", "10010", "10001"},
	'S': {"01110", "10001", "10000", "01110", "00001", "10001", "01110"},
	'T': {"11111", "00100", "00100", "00100", "00100", "00100", "00100"},
	'U': {"10001", "10001", "10001", "10001", "10001", "10001", "01110"},
	'V': {"10001", "10001", "10001", "10001", "01010", "00100", "00100"},
	'W': {"10001", "10001", "10001", "10101", "10101", "11011", "10001"},
	'X': {"10001", "10001", "01010", "00100", "01010", "10001", "10001"},
	'Y': {"10001", "10001", "01010", "00100", "00100", "00100", "00100"},
	'Z': {"11111", "00001", "00010", "00100", "01000", "10000", "11111"},
	'0': {"01110", "10011", "10101", "11001", "10001", "10001", "01110"},
	'1': {"00100", "01100", "00100", "00100", "00100", "00100", "01110"},
	'2': {"01110", "10001", "00001", "00010", "00100", "01000", "11111"},
	'3': {"11110", "00001", "00001", "01110", "00001", "00001", "11110"},
	'4': {"00010", "00110", "01010", "10010", "11111", "00010", "00010"},
	'5': {"11111", "10000", "11110", "00001", "00001", "10001", "01110"},
	'6': {"00110", "01000", "10000", "11110", "10001", "10001", "01110"},
	'7': {"11111", "00001", "00010", "00100", "01000", "01000", "01000"},
	'8': {"01110", "10001", "10001", "01110", "10001", "10001", "01110"},
	'9': {"01110", "10001", "10001", "01111", "00001", "00010", "01100"},
	':': {"00000", "00100", "00000", "00000", "00000", "00100", "00000"},
	',': {"00000", "00000", "00000", "00000", "00000", "00100", "01000"},
	'.': {"00000", "00000", "00000", "00000", "00000", "00100", "00100"},
	'\'': {"00100", "00100", "00000", "00000", "00000", "00000", "00000"},
	' ': {"00000", "00000", "00000", "00000", "00000", "00000", "00000"},
	'-': {"00000", "00000", "00000", "11111", "00000", "00000", "00000"},
}

const blockFontRows = 7

func renderBlockText(s string, color lipgloss.Color) string {
	s = strings.ToUpper(s)
	raw := make([]string, blockFontRows)
	for _, ch := range s {
		glyph, ok := blockFont[ch]
		if !ok {
			glyph = blockFont[' ']
		}
		for r := 0; r < blockFontRows; r++ {
			for _, b := range glyph[r] {
				if b == '1' {
					raw[r] += "█"
				} else {
					raw[r] += " "
				}
			}
			raw[r] += " "
		}
	}
	style := lipgloss.NewStyle().Foreground(color).Bold(true)
	var out []string
	for _, line := range raw {
		out = append(out, style.Render(line))
	}
	return strings.Join(out, "\n")
}

// Small block font one level smaller - 3x5 for numbers.
var smallBlockFont = map[rune][]string{
	'0': {"111", "101", "101", "101", "111"},
	'1': {"010", "110", "010", "010", "111"},
	'2': {"111", "001", "111", "100", "111"},
	'3': {"111", "001", "111", "001", "111"},
	'4': {"101", "101", "111", "001", "001"},
	'5': {"111", "100", "111", "001", "111"},
	'6': {"111", "100", "111", "101", "111"},
	'7': {"111", "001", "010", "010", "010"},
	'8': {"111", "101", "111", "101", "111"},
	'9': {"111", "101", "111", "001", "111"},
	':': {"000", "010", "000", "010", "000"},
	' ': {"000", "000", "000", "000", "000"},
}

const smallBlockFontRows = 5

func renderSmallBlockText(s string, color lipgloss.Color) string {
	raw := make([]string, smallBlockFontRows)
	for _, ch := range s {
		glyph, ok := smallBlockFont[ch]
		if !ok {
			glyph = smallBlockFont[' ']
		}
		for r := 0; r < smallBlockFontRows; r++ {
			for _, b := range glyph[r] {
				if b == '1' {
					raw[r] += "█"
				} else {
					raw[r] += " "
				}
			}
			raw[r] += " "
		}
	}
	style := lipgloss.NewStyle().Foreground(color).Bold(true)
	var out []string
	for _, line := range raw {
		out = append(out, style.Render(line))
	}
	return strings.Join(out, "\n")
}

// renderAlliBlocks downsamples the alli sprite with higher quality half-blocks.
// Increased target resolution and tighter averaging for crisper edges.
func renderAlliBlocks(termWidth int) string {
	if termWidth <= 0 {
		termWidth = 80
	}
	if v, ok := blockCache.Load(termWidth); ok {
		return v.(string)
	}
	img := getAlliImg()
	if img == nil {
		return ""
	}
	bounds := img.Bounds()
	srcW := bounds.Dx()
	srcH := bounds.Dy()

	// Higher quality: use most of the content width for crisper detail.
	targetW := termWidth - 6
	if targetW > 78 {
		targetW = 78
	}
	if targetW < 32 {
		targetW = 32
	}
	if termWidth < 60 {
		targetW = termWidth - 4
		if targetW < 28 {
			targetW = 28
		}
		if targetW > 48 {
			targetW = 48
		}
	}

	targetH := targetW * srcH / srcW / 2
	if targetH < 14 {
		targetH = 14
	}
	if targetH > 30 {
		targetH = 30
		targetW = targetH * 2 * srcW / srcH
		if targetW > 78 {
			targetW = 78
		}
	}

	var lines []string
	for y := 0; y < targetH; y++ {
		var sb strings.Builder
		for x := 0; x < targetW; x++ {
			sx0 := bounds.Min.X + x*srcW/targetW
			sx1 := bounds.Min.X + (x+1)*srcW/targetW
			if sx1 > bounds.Max.X {
				sx1 = bounds.Max.X
			}

			syTop0 := bounds.Min.Y + (y*2)*srcH/(targetH*2)
			syTop1 := bounds.Min.Y + (y*2+1)*srcH/(targetH*2)
			syBot0 := syTop1
			syBot1 := bounds.Min.Y + (y*2+2)*srcH/(targetH*2)
			if syTop1 > bounds.Max.Y {
				syTop1 = bounds.Max.Y
			}
			if syBot1 > bounds.Max.Y {
				syBot1 = bounds.Max.Y
			}
			if syTop0 >= syTop1 {
				syTop0 = syTop1 - 1
			}
			if syBot0 >= syBot1 {
				syBot0 = syBot1 - 1
			}

			rT, gT, bT, aT := averageRegion(img, sx0, sx1, syTop0, syTop1)
			rB, gB, bB, aB := averageRegion(img, sx0, sx1, syBot0, syBot1)

			topTrans := aT < 96
			botTrans := aB < 96
			if topTrans && botTrans {
				sb.WriteString(" ")
				continue
			}
			hexTop := fmt.Sprintf("#%02X%02X%02X", rT, gT, bT)
			hexBot := fmt.Sprintf("#%02X%02X%02X", rB, gB, bB)
			if topTrans && !botTrans {
				st := lipgloss.NewStyle().Foreground(lipgloss.Color(hexBot))
				sb.WriteString(st.Render("▄"))
				continue
			}
			if !topTrans && botTrans {
				st := lipgloss.NewStyle().Foreground(lipgloss.Color(hexTop))
				sb.WriteString(st.Render("▀"))
				continue
			}
			st := lipgloss.NewStyle().Foreground(lipgloss.Color(hexTop)).Background(lipgloss.Color(hexBot))
			sb.WriteString(st.Render("▀"))
		}
		lines = append(lines, sb.String())
	}
	out := strings.Join(lines, "\n")
	blockCache.Store(termWidth, out)
	return out
}

func statSmallBlock(label, value string) string {
	labelStyled := lipgloss.NewStyle().
		Foreground(colorLightGray).
		Faint(true).
		Align(lipgloss.Center).
		Render(label)
	valueBlock := renderSmallBlockText(value, colorWhite)
	return lipgloss.JoinVertical(lipgloss.Center, labelStyled, valueBlock)
}

func renderStatPills(elapsed time.Duration, iteration int, done bool) string {
	timeVal := formatClock(elapsed)
	iterVal := fmt.Sprintf("%d", iteration)
	label := "RUNNING FOR"
	if done {
		label = "RAN FOR"
	}
	timeBlock := statSmallBlock(label, timeVal)
	// Iterations as simple normal text line "ITERATIONS: number" on next line below timer
	iterLine := lipgloss.NewStyle().Foreground(colorLightGray).Faint(true).Render("ITERATIONS: ") +
		lipgloss.NewStyle().Bold(true).Foreground(colorWhite).Render(iterVal)
	iterLine = lipgloss.PlaceHorizontal(lipgloss.Width(timeBlock), lipgloss.Center, iterLine)
	return lipgloss.JoinVertical(lipgloss.Center, timeBlock, "", iterLine)
}

// renderFooter builds the pinned help line: "—   lead • keys   —".
func renderFooter(w int, lead, keys string) string {
	text := fmt.Sprintf("—   %s • %s   —", lead, keys)
	return helpStyle.Width(w).Align(lipgloss.Center).Render(text)
}

// Model is the Bubble Tea model for a running loop.
type Model struct {
	engine          *loop.Engine
	viewport        viewport.Model
	status          string
	iteration       int
	elapsed         string
	elapsedDur      time.Duration
	startedAt       time.Time
	budget          string
	logs            []string
	quitting        bool
	detached        bool
	done            bool
	confirmingAbort bool
	abortError      string
	width           int
	height          int
}

type eventMsg loop.Event
type tickMsg struct{}

func New(engine *loop.Engine) Model {
	vp := viewport.New(80, 20)
	vp.SetContent("Starting syla...\n")
	started := time.Now()
	if engine != nil {
		started = engine.StartedAt()
	}
	return Model{
		engine:    engine,
		viewport:  vp,
		status:    "running",
		startedAt: started,
		width:     80,
		height:    24,
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(waitForEvent(m.engine), tickCmd())
}

func waitForEvent(e *loop.Engine) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-e.Events()
		if !ok {
			return tea.Quit()
		}
		return eventMsg(ev)
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg{} })
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.confirmingAbort {
			switch msg.String() {
			case "y", "Y":
				if err := m.engine.Abort(); err != nil {
					m.abortError = err.Error()
				} else {
					m.status = "aborted"
					m.done = true
					m.logs = append(m.logs, "■ aborted by user")
					m.viewport.SetContent(strings.Join(m.logs, "\n"))
					safeGotoBottom(&m.viewport)
				}
				m.confirmingAbort = false
				return m, tea.Batch(waitForEvent(m.engine), tickCmd())
			case "n", "N", "esc":
				m.confirmingAbort = false
				m.abortError = ""
				return m, nil
			}
			return m, nil
		}
		switch msg.String() {
		case "q", "ctrl+c":
			m.detached = true
			m.quitting = true
			return m, tea.Quit
		case "p":
			m.status = "paused"
			return m, nil
		case "a", "A":
			m.confirmingAbort = true
			m.abortError = ""
			return m, nil
		}
	case tickMsg:
		m.elapsedDur = time.Since(m.startedAt)
		m.elapsed = formatDuration(m.elapsedDur)
		if !m.quitting {
			return m, tickCmd()
		}
		return m, nil
	case eventMsg:
		ev := loop.Event(msg)
		switch ev.Type {
		case "IterationStarted":
			m.iteration = ev.Iteration
			m.status = fmt.Sprintf("iteration %d", ev.Iteration)
			m.logs = append(m.logs, fmt.Sprintf("→ %s", ev.Message))
		case "IterationCompleted":
			if ev.Result != nil {
				m.logs = append(m.logs, fmt.Sprintf("✓ iter %d: %s", ev.Iteration, ev.Result.Output.Summary))
			} else if ev.Message != "" {
				m.logs = append(m.logs, ev.Message)
			}
		case "Failure":
			m.logs = append(m.logs, fmt.Sprintf("✗ %v", ev.Err))
		case "StopConditionMet":
			m.status = "completed"
			m.done = true
			m.logs = append(m.logs, "■ "+ev.Message)
		case "RateLimitWait":
			m.status = "rate-limit wait"
			m.logs = append(m.logs, "⏳ "+ev.Message)
		case "BudgetWarning":
			m.budget = ev.Message
		}
		if len(m.logs) > 200 {
			m.logs = m.logs[len(m.logs)-200:]
		}
		m.viewport.SetContent(strings.Join(m.logs, "\n"))
		safeGotoBottom(&m.viewport)
		if !m.quitting {
			return m, waitForEvent(m.engine)
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if msg.Width < 40 {
			msg.Width = 40
		}
		if msg.Height < 10 {
			msg.Height = 10
		}
		m.viewport.Width = msg.Width
		m.viewport.Height = msg.Height - 6
		if m.viewport.Height < 1 {
			m.viewport.Height = 1
		}
		if m.viewport.Width < 1 {
			m.viewport.Width = 1
		}
	}
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m Model) View() string {
	w := m.width
	if w == 0 {
		w = 80
	}
	h := m.height
	if h == 0 {
		h = 24
	}

	if m.confirmingAbort {
		modal := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorErrorRed).Padding(1, 2).Render(
			"Abort session " + m.engine.RunID() + "?\n\nThis will stop the agent loop and mark the run as aborted.\n\nPress y to confirm, n/esc to cancel",
		)
		if m.abortError != "" {
			modal += "\n" + lipgloss.NewStyle().Foreground(colorErrorRed).Render("Error: "+m.abortError)
		}
		footer := helpStyle.Width(w).Align(lipgloss.Center).Render("y: confirm abort • n/esc: cancel")
		return renderScreen(w, h, modal, footer)
	}
	if m.quitting && m.detached {
		return helpStyle.Render("Detached. Run `syla attach " + m.engine.RunID() + "` to reattach. `syla stop " + m.engine.RunID() + "` to stop.\n")
	}
	if m.quitting {
		return "Quitting...\n"
	}

	cw := effectiveContentWidth(w)
	title := renderTitle(cw, m.done)
	mascot := lipgloss.PlaceHorizontal(cw, lipgloss.Center, renderAlliBlocks(cw))
	pills := lipgloss.PlaceHorizontal(cw, lipgloss.Center, renderStatPills(m.elapsedDur, m.iteration, m.done))
	main := lipgloss.JoinVertical(lipgloss.Center, title, "", mascot, "", pills)
	if m.budget != "" {
		budgetLine := barStyle.Width(cw).Align(lipgloss.Center).Render(m.budget)
		main = lipgloss.JoinVertical(lipgloss.Center, main, "", budgetLine)
	}

	lead, keys := "Alligator wants to see you", "q: detach • p: pause • a: abort • ctrl+c: detach"
	if m.done {
		lead, keys = "Process completed", "q: detach • syla status to see summary"
	}
	footer := renderFooter(w, lead, keys)
	return renderScreen(w, h, main, footer)
}

// Attach runs the TUI attached to the given engine. It returns when detached.
func Attach(engine *loop.Engine) error {
	m := New(engine)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

type remoteStatusMsg struct {
	iteration  int
	status     string
	elapsed    string
	elapsedDur time.Duration
	startedAt  time.Time
	err        error
}

type RemoteModel struct {
	runID           string
	runDir          string
	viewport        viewport.Model
	status          string
	iteration       int
	elapsed         string
	elapsedDur      time.Duration
	startedAt       time.Time
	logs            []string
	quitting        bool
	detached        bool
	confirmingAbort bool
	abortError      string
	width           int
	height          int
}

func NewRemote(runID, runDir string) RemoteModel {
	vp := viewport.New(80, 20)
	vp.SetContent("Connecting to daemon...\n")
	var started time.Time
	if cp, err := loop.LoadCheckpoint(runDir); err == nil {
		started = cp.StartedAt
	}
	if started.IsZero() {
		started = time.Now()
	}
	return RemoteModel{
		runID:     runID,
		runDir:    runDir,
		viewport:  vp,
		status:    "connecting",
		startedAt: started,
		width:     80,
		height:    24,
	}
}

func (m RemoteModel) Init() tea.Cmd {
	return pollRemote(m.runID, m.runDir)
}

func pollRemote(runID, runDir string) tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
		status, _ := fetchRemoteStatus(runDir)
		if status != nil {
			return remoteStatusMsg{iteration: status.Iteration, status: status.Status, elapsed: status.Elapsed, elapsedDur: status.ElapsedDur, startedAt: status.StartedAt}
		}
		return remoteStatusMsg{status: "unknown", err: fmt.Errorf("no status")}
	})
}

// fetchRemoteStatus asks the run's control socket for live status, falling
// back to reading its checkpoint file if the daemon isn't reachable.
func fetchRemoteStatus(runDir string) (*remoteStatus, error) {
	sock := runDir + "/ctl.sock"
	if conn, err := net.DialTimeout("unix", sock, 200*time.Millisecond); err == nil {
		req := map[string]any{"id": 1, "method": "Status"}
		b, _ := json.Marshal(req)
		b = append(b, '\n')
		_, _ = conn.Write(b)
		_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		scanner := bufio.NewScanner(conn)
		buf := make([]byte, 0, 64*1024)
		scanner.Buffer(buf, 1024*1024)
		if scanner.Scan() {
			var res struct {
				Result *remoteStatus `json:"result"`
				Error  string        `json:"error"`
			}
			if err := json.Unmarshal(scanner.Bytes(), &res); err == nil && res.Result != nil && res.Error == "" {
				_ = conn.Close()
				if res.Result.ElapsedDur == 0 && res.Result.Elapsed != "" {
					if d, err := time.ParseDuration(res.Result.Elapsed); err == nil {
						res.Result.ElapsedDur = d
					}
				}
				return res.Result, nil
			}
		}
		_ = conn.Close()
	}
	cpPath := runDir + "/checkpoint.json"
	data, err := os.ReadFile(cpPath)
	if err != nil {
		return nil, err
	}
	var cp struct {
		Iteration  int       `json:"iteration"`
		Status     string    `json:"status"`
		StopReason string    `json:"stop_reason"`
		StartedAt  time.Time `json:"started_at"`
		UpdatedAt  time.Time `json:"updated_at"`
	}
	if err := json.Unmarshal(data, &cp); err != nil {
		return nil, err
	}
	var elapsedDur time.Duration
	if cp.Status == "completed" || cp.Status == "failed" || cp.StopReason != "" || isCompletedStatus(cp.Status) {
		if !cp.UpdatedAt.IsZero() && !cp.StartedAt.IsZero() {
			elapsedDur = cp.UpdatedAt.Sub(cp.StartedAt).Truncate(time.Second)
		} else if !cp.StartedAt.IsZero() {
			elapsedDur = time.Since(cp.StartedAt).Truncate(time.Second)
		}
	} else if !cp.StartedAt.IsZero() {
		elapsedDur = time.Since(cp.StartedAt).Truncate(time.Second)
	}
	elapsed := formatDuration(elapsedDur)
	if cp.Status == "" {
		cp.Status = "running"
	}
	if cp.StopReason != "" {
		cp.Status = cp.StopReason
	}
	return &remoteStatus{Iteration: cp.Iteration, Status: cp.Status, Elapsed: elapsed, ElapsedDur: elapsedDur, StartedAt: cp.StartedAt}, nil
}

type remoteStatus struct {
	Iteration  int
	Status     string
	Elapsed    string
	ElapsedDur time.Duration
	StartedAt  time.Time
}

func (m RemoteModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.confirmingAbort {
			switch msg.String() {
			case "y", "Y":
				if err := abortRemote(m.runDir); err != nil {
					m.abortError = err.Error()
				} else {
					m.status = "aborted"
					m.logs = append(m.logs, "■ aborted by user")
					m.viewport.SetContent(strings.Join(m.logs, "\n"))
					safeGotoBottom(&m.viewport)
				}
				m.confirmingAbort = false
				return m, pollRemote(m.runID, m.runDir)
			case "n", "N", "esc":
				m.confirmingAbort = false
				m.abortError = ""
				return m, nil
			}
			return m, nil
		}
		switch msg.String() {
		case "q", "ctrl+c":
			m.detached = true
			m.quitting = true
			return m, tea.Quit
		case "a", "A":
			if isCompletedStatus(m.status) {
				m.logs = append(m.logs, "Already "+m.status+", cannot abort")
				m.viewport.SetContent(strings.Join(m.logs, "\n"))
				safeGotoBottom(&m.viewport)
				return m, nil
			}
			m.confirmingAbort = true
			m.abortError = ""
			return m, nil
		}
	case remoteStatusMsg:
		if msg.err == nil {
			changed := msg.iteration != m.iteration || msg.status != m.status
			m.iteration = msg.iteration
			m.status = msg.status
			m.elapsed = msg.elapsed
			m.elapsedDur = msg.elapsedDur
			if !msg.startedAt.IsZero() {
				m.startedAt = msg.startedAt
			}
			if changed {
				if msg.status == "running" {
					m.logs = append(m.logs, fmt.Sprintf("→ iter %d • %s", msg.iteration, msg.elapsed))
				} else {
					m.logs = append(m.logs, fmt.Sprintf("■ %s • iter %d • %s", msg.status, msg.iteration, msg.elapsed))
				}
				if len(m.logs) > 200 {
					m.logs = m.logs[len(m.logs)-200:]
				}
				m.viewport.SetContent(strings.Join(m.logs, "\n"))
				safeGotoBottom(&m.viewport)
			} else {
				m.viewport.SetContent(strings.Join(m.logs, "\n"))
			}
			if isCompletedStatus(msg.status) {
				m.status = msg.status
				if len(m.logs) == 0 || !strings.Contains(m.logs[len(m.logs)-1], "completed") {
					m.logs = append(m.logs, fmt.Sprintf("✔ Run %s completed: %s after %d iterations", m.runID, msg.status, msg.iteration))
					m.viewport.SetContent(strings.Join(m.logs, "\n"))
					safeGotoBottom(&m.viewport)
				}
				return m, pollRemote(m.runID, m.runDir)
			}
		}
		return m, pollRemote(m.runID, m.runDir)
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if msg.Width < 40 {
			msg.Width = 40
		}
		if msg.Height < 10 {
			msg.Height = 10
		}
		m.viewport.Width = msg.Width
		m.viewport.Height = msg.Height - 6
		if m.viewport.Height < 1 {
			m.viewport.Height = 1
		}
		if m.viewport.Width < 1 {
			m.viewport.Width = 1
		}
	}
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

// isCompletedStatus treats anything other than the known in-progress states
// as finished, since remote status strings come from arbitrary stop reasons
// (max_iterations, budget, no_diff, ...).
func isCompletedStatus(s string) bool {
	if s == "completed" || s == "failed" || s == "aborted" {
		return true
	}
	if strings.Contains(s, "max_duration") || strings.Contains(s, "max_iterations") || strings.Contains(s, "budget") || strings.Contains(s, "no_diff") || strings.Contains(s, "aborted") {
		return true
	}
	if s != "running" && s != "unknown" && s != "connecting" && s != "" {
		return true
	}
	return false
}

func (m RemoteModel) View() string {
	w := m.width
	if w == 0 {
		w = 80
	}
	h := m.height
	if h == 0 {
		h = 24
	}

	if m.confirmingAbort {
		modal := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorErrorRed).Padding(1, 2).Render(
			"Abort session " + m.runID + "?\n\nThis will stop the agent loop and mark the run as aborted.\n\nPress y to confirm, n/esc to cancel",
		)
		if m.abortError != "" {
			modal += "\n" + lipgloss.NewStyle().Foreground(colorErrorRed).Render("Error: "+m.abortError)
		}
		footer := helpStyle.Width(w).Align(lipgloss.Center).Render("y: confirm abort • n/esc: cancel")
		return renderScreen(w, h, modal, footer)
	}
	if m.quitting {
		if m.detached {
			return helpStyle.Render(fmt.Sprintf("Detached. Run `syla attach %s` to reattach. `syla stop %s` to stop.\n", m.runID, m.runID))
		}
		return "Quitting...\n"
	}

	cw := effectiveContentWidth(w)
	done := isCompletedStatus(m.status)
	title := renderTitle(cw, done)
	mascot := lipgloss.PlaceHorizontal(cw, lipgloss.Center, renderAlliBlocks(cw))
	pills := lipgloss.PlaceHorizontal(cw, lipgloss.Center, renderStatPills(m.elapsedDur, m.iteration, done))
	main := lipgloss.JoinVertical(lipgloss.Center, title, "", mascot, "", pills)

	lead, keys := "Alligator wants to see you", "q: detach • a: abort • ctrl+c: detach"
	if done {
		lead, keys = "Process completed", "q: detach • syla status to see summary"
	}
	footer := renderFooter(w, lead, keys)
	return renderScreen(w, h, main, footer)
}

// AttachRemote attaches to a remote daemon via polling (fallback when not in-process).
func AttachRemote(runID, runDir string) error {
	if !isTTY() {
		return attachRemoteNonTTY(runID, runDir)
	}
	m := NewRemote(runID, runDir)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	if err != nil {
		if strings.Contains(err.Error(), "/dev/tty") || strings.Contains(err.Error(), "tty") {
			return attachRemoteNonTTY(runID, runDir)
		}
		return err
	}
	if data, err := os.ReadFile(runDir + "/exit-summary.txt"); err == nil {
		fmt.Println("\n" + string(data))
	} else if data, err := os.ReadFile(runDir + "/checkpoint.json"); err == nil {
		var cp struct {
			Iteration  int    `json:"iteration"`
			Status     string `json:"status"`
			StopReason string `json:"stop_reason"`
		}
		if err := json.Unmarshal(data, &cp); err == nil && isCompletedStatus(cp.Status) {
			fmt.Printf("\n✔ Run %s completed: %s after %d iterations\n", runID, cp.Status, cp.Iteration)
			if cp.StopReason != "" {
				fmt.Printf("  stop reason: %s\n", cp.StopReason)
			}
		}
	}
	return nil
}

func isTTY() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

func abortRemote(runDir string) error {
	sock := runDir + "/ctl.sock"
	conn, err := net.DialTimeout("unix", sock, 500*time.Millisecond)
	if err != nil {
		return err
	}
	defer conn.Close()
	req := map[string]any{"id": 2, "method": "Close"}
	b, _ := json.Marshal(req)
	b = append(b, '\n')
	if _, err := conn.Write(b); err != nil {
		return err
	}
	_ = conn.SetReadDeadline(time.Now().Add(1 * time.Second))
	scanner := bufio.NewScanner(conn)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	if scanner.Scan() {
		var res struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(scanner.Bytes(), &res)
		if res.Error != "" {
			return fmt.Errorf("%s", res.Error)
		}
	}
	return nil
}

func attachRemoteNonTTY(runID, runDir string) error {
	fmt.Printf("syla • %s • polling (no TTY, showing plain logs)\n", runID)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	var lastIter int
	var lastStatus string
	for {
		select {
		case <-ticker.C:
			st, err := fetchRemoteStatus(runDir)
			if err != nil {
				continue
			}
			if st.Iteration != lastIter || st.Status != lastStatus {
				fmt.Printf("iter %d • %s • %s\n", st.Iteration, st.Status, formatDuration(st.ElapsedDur))
				lastIter = st.Iteration
				lastStatus = st.Status
			}
			if isCompletedStatus(st.Status) {
				fmt.Printf("\n✔ Run %s completed: %s after %d iterations\n", runID, st.Status, st.Iteration)
				if data, err := os.ReadFile(runDir + "/exit-summary.txt"); err == nil {
					fmt.Println(string(data))
				}
				fmt.Println("Process completed.")
				return nil
			}
		}
	}
}
