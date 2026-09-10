package tui

type rectangle struct {
	x      int
	y      int
	width  int
	height int
}

func (r rectangle) contains(x, y int) bool {
	return x >= r.x && x < r.x+r.width && y >= r.y && y < r.y+r.height
}

type appLayout struct {
	body      rectangle
	rail      rectangle
	workspace rectangle
	activity  rectangle
	footer    rectangle
}

func calculateLayout(width, height int, showActivity bool) appLayout {
	const (
		railWidth    = 30
		panelGap     = 1
		footerHeight = 3
	)
	bodyHeight := height - footerHeight
	workspaceHeight := bodyHeight
	if showActivity {
		workspaceHeight -= activityDrawerHeight
	}
	layout := appLayout{
		body:      rectangle{width: width, height: bodyHeight},
		rail:      rectangle{width: railWidth, height: bodyHeight},
		workspace: rectangle{x: railWidth + panelGap, width: width - railWidth - panelGap, height: workspaceHeight},
		footer:    rectangle{y: bodyHeight, width: width, height: footerHeight},
	}
	if showActivity {
		layout.activity = rectangle{x: layout.workspace.x, y: workspaceHeight, width: layout.workspace.width, height: activityDrawerHeight}
	}

	return layout
}
