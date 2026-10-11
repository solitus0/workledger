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
	header    rectangle
	body      rectangle
	rail      rectangle
	workspace rectangle
	activity  rectangle
	footer    rectangle
}

const (
	navigationWidth = 18
	appHeaderHeight = 2
	appFooterHeight = 3
)

func calculateLayout(width, height int, showActivity bool) appLayout {
	bodyHeight := max(1, height-appHeaderHeight-appFooterHeight)
	workspaceHeight := bodyHeight
	if showActivity {
		workspaceHeight -= activityDrawerHeight
	}
	layout := appLayout{
		header:    rectangle{width: width, height: appHeaderHeight},
		body:      rectangle{y: appHeaderHeight, width: width, height: bodyHeight},
		rail:      rectangle{y: appHeaderHeight, width: navigationWidth, height: bodyHeight},
		workspace: rectangle{x: navigationWidth + 1, y: appHeaderHeight, width: width - navigationWidth - 1, height: workspaceHeight},
		footer:    rectangle{y: height - appFooterHeight, width: width, height: appFooterHeight},
	}
	if showActivity {
		layout.activity = rectangle{x: layout.workspace.x, y: appHeaderHeight + workspaceHeight, width: layout.workspace.width, height: activityDrawerHeight}
	}

	return layout
}

func inspectorWidths(width int) (list, detail int) {
	detail = max(28, width/3)
	return width - detail - 1, detail
}
