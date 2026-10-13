package tui

import "slices"

func (m *model) navigateIssueList(key string) {
	layout := calculateLayout(m.width, m.height, m.activityVisible())
	listWidth, _ := inspectorWidths(layout.workspace.width)
	header, indices, _ := m.issueListContent(listWidth - 2)
	if len(indices) == 0 {
		return
	}
	position := max(0, slices.Index(indices, m.issueSelected))
	step := max(1, layout.workspace.height-len(header)-1)
	switch key {
	case "pgup":
		position = max(0, position-step)
	case "pgdown":
		position = min(len(indices)-1, position+step)
	case "home":
		position = 0
	case "end":
		position = len(indices) - 1
	}
	m.issueSelected = indices[position]
}

func (m *model) navigateEpicList(key string) {
	layout := calculateLayout(m.width, m.height, m.activityVisible())
	listWidth, _ := inspectorWidths(layout.workspace.width)
	header, groups, rows := m.epicListContent(listWidth - 2)
	if len(groups) == 0 {
		return
	}
	position := 0
	for index, group := range groups {
		if m.epicSelected && group.Epic == m.epicsSelected {
			position = index
			break
		}
	}
	switch key {
	case "home":
		position = 0
	case "end":
		position = len(groups) - 1
	case "pgup", "pgdown":
		direction := 1
		if key == "pgup" {
			direction = -1
		}
		budget := max(1, layout.workspace.height-len(header))
		// Keep the selected group as the overlap; include only whole groups.
		used, target := len(rows[position]), position
		for next := target + direction; next >= 0 && next < len(groups); next += direction {
			if used+len(rows[next]) > budget {
				break
			}
			used += len(rows[next])
			target = next
		}
		if target == position {
			target = min(max(0, position+direction), len(groups)-1)
		}
		position = target
	}
	m.epicsSelected, m.epicSelected = groups[position].Epic, true
}
