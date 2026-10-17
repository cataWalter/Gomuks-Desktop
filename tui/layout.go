package tui

// rect is a screen region in cells; its edges include any border.
type rect struct{ x, y, w, h int }

func (r rect) contains(x, y int) bool {
	return x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h
}

// inner returns the content area of a bordered box with one cell of horizontal padding.
func (r rect) inner() rect {
	return rect{x: r.x + 2, y: r.y + 1, w: max(r.w-4, 1), h: max(r.h-2, 1)}
}

// screenLayout holds the panel regions for the current size and focus.
type screenLayout struct {
	compact  bool
	header   rect
	rooms    rect
	timeline rect
	composer rect
	footer   rect
}

func (m Model) size() (int, int) {
	width, height := m.width, m.height
	if width < 1 {
		width = 100
	}
	if height < 1 {
		height = 30
	}
	return width, height
}

func (m Model) layout() screenLayout {
	width, height := m.size()
	if width < 84 || height < 20 {
		return m.compactLayout(width, height)
	}
	sidebarWidth := clampInt(width/4-4, 20, 30)
	rightX := sidebarWidth + 2
	boxWidth := width - rightX - 4
	return screenLayout{
		rooms:    rect{x: 0, y: 0, w: rightX, h: height},
		header:   rect{x: rightX, y: 0, w: boxWidth - 2, h: 1},
		timeline: rect{x: rightX, y: 1, w: boxWidth, h: height - 6},
		composer: rect{x: rightX, y: height - 5, w: boxWidth, h: 4},
		footer:   rect{x: rightX, y: height - 1, w: boxWidth - 2, h: 1},
	}
}

func (m Model) compactLayout(width, height int) screenLayout {
	boxWidth := clampInt(width-2, 1, width)
	timelineHeight := clampInt(height-12, 1, height) + 2
	roomsHeight := clampInt(height-5, 1, height) + 2
	lay := screenLayout{
		compact:  true,
		header:   rect{x: 0, y: 0, w: boxWidth, h: 1},
		rooms:    rect{x: 0, y: 1, w: boxWidth, h: roomsHeight},
		timeline: rect{x: 0, y: 1, w: boxWidth, h: timelineHeight},
		composer: rect{x: 0, y: 1 + timelineHeight, w: boxWidth, h: 4},
	}
	bodyHeight := timelineHeight + lay.composer.h
	if m.focus == FocusRooms {
		bodyHeight = roomsHeight
	}
	lay.footer = rect{x: 0, y: 1 + bodyHeight, w: boxWidth, h: 1}
	return lay
}

// panelAt returns the focusable panel visible under a screen cell.
func (m Model) panelAt(x, y int) (Focus, bool) {
	lay := m.layout()
	roomsVisible := !lay.compact || m.focus == FocusRooms
	bodyVisible := !lay.compact || m.focus != FocusRooms
	switch {
	case roomsVisible && lay.rooms.contains(x, y):
		return FocusRooms, true
	case bodyVisible && lay.timeline.contains(x, y):
		return FocusTimeline, true
	case bodyVisible && lay.composer.contains(x, y):
		return FocusComposer, true
	}
	return FocusRooms, false
}
