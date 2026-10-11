package tui

import "fmt"

// These commands are also the source of the idle action bar. The menu dispatches
// the same keys as direct keyboard input, including existing mutation guards.
type commandAction struct {
	key, chord, label string
}

func (m model) commandActions() []commandAction {
	actions := []commandAction{}
	add := func(key, chord, label string) { actions = append(actions, commandAction{key, chord, label}) }
	if m.focus == focusActivity {
		for _, action := range []commandAction{{"up", "↑", "Select previous activity"}, {"down", "↓", "Select next activity"}, {"pgup", "PgUp", "Previous page"}, {"pgdown", "PgDn", "Next page"}, {"home", "Home", "Newest activity"}, {"end", "End", "Oldest activity"}, {"r", "r", "Refresh activity"}, {"g", "g", "Close activity"}, {"q", "q", "Quit"}} {
			actions = append(actions, action)
		}
		for index, name := range []string{"Status", "Worklogs", "Plans", "Presets", "Trash"} {
			if index == 0 || m.workspace != nil {
				add(fmt.Sprint(index+1), fmt.Sprint(index+1), "Open "+name)
			}
		}
		return actions
	}
	if m.activeTab == worklogsTab && m.workspace != nil {
		add("a", "a", "Add worklog")
		add("A", "A", "Add from preset")
		if m.worklogView == dayWorklogView {
			add("/", "/", "Find local worklogs")
			if m.selectedWorklog() != nil {
				add("enter", "Enter", "Worklog details")
				add("e", "e", "Edit worklog")
				if !m.worklogLoading {
					add("M", "M", "Move worklog")
				}
				add("D", "D", "Move worklog to trash")
			}
		} else {
			add("enter", "Enter", "Open selected day")
			if m.worklogView == weekWorklogView && len(m.worklogs) > 0 {
				add("D", "D", "Move day to trash")
			}
		}
		add("d", "d", "Day view")
		add("w", "w", "Week view")
		add("m", "m", "Month view")
		if m.worklogView == monthWorklogView {
			add("pgup", "PgUp", "Previous month")
			add("pgdown", "PgDn", "Next month")
		}
	} else if m.activeTab == statusTab {
		add("enter", "Enter", "Expand selected details")
		add("s", "s", "Status diagnostics")
		add("c", "c", "Effective configuration")
	} else if m.activeTab == presetsTab && m.workspace != nil {
		add("a", "a", "Add preset")
		if m.selectedPreset() != nil {
			add("enter", "Enter", "Preset details")
			add("e", "e", "Edit preset")
			add("D", "D", "Delete preset")
		}
	} else if m.activeTab == plansTab && m.workspace != nil {
		if m.plan != nil {
			if m.selectedPlanItem() != nil {
				add("enter", "Enter", "Scope details")
			}
			add("a", "a", "Show ready or all scopes")
			if m.planExecutableCount("not_attempted") > 0 {
				add("A", "A", "Apply saved plan")
			}
			if m.planExecutableCount("failed") > 0 {
				add("f", "f", "Retry failed scopes")
			}
			if m.planExecutableCount("uncertain") > 0 {
				add("u", "u", "Reconcile uncertain scopes")
			}
			add("esc", "Esc", "Back to saved plans")
		} else {
			add("n", "n", "New reconcile plan")
			if m.selectedPlanEntry() != nil {
				add("enter", "Enter", "Review saved plan")
			}
			add("d", "d", "Day view")
			add("w", "w", "Week view")
		}
	} else if m.activeTab == trashTab && m.workspace != nil {
		if m.trashView == dayWorklogView && m.selectedTrash() != nil {
			add("enter", "Enter", "Trash details")
		}
		if m.trashView == weekWorklogView {
			add("enter", "Enter", "Open selected day")
		}
		add("d", "d", "Day view")
		add("w", "w", "Week view")
		add("s", "s", "Cycle local or remote scope")
		if (m.trashView == weekWorklogView && len(restorableLocalTrash(m.trashItems)) > 0) || (m.selectedTrash() != nil && m.selectedTrash().StorageScope == "local" && m.selectedTrash().SourceWorklogID != nil) {
			add("R", "R", "Restore local worklogs")
		}
	}
	add("up", "↑", "Select previous")
	add("down", "↓", "Select next")
	if m.activeTab == worklogsTab || m.activeTab == trashTab || (m.activeTab == plansTab && m.plan == nil) {
		add("h", "h / ←", "Previous date or period")
		add("l", "l / →", "Next date or period")
		add("t", "t", "Today")
	}
	add("r", "r", "Refresh current view")
	add("g", "g", "Toggle activity")
	for index, name := range []string{"Status", "Worklogs", "Plans", "Presets", "Trash"} {
		if index == 0 || m.workspace != nil {
			add(fmt.Sprint(index+1), fmt.Sprint(index+1), "Open "+name)
		}
	}
	add("q", "q", "Quit")
	return actions
}

type actionHint struct{ chord, label string }

func (m model) idleHints() (primary, secondary []actionHint) {
	primary = []actionHint{{"↑/↓", "Select"}}
	keys := []string{}
	switch m.activeTab {
	case statusTab:
		keys = []string{"s", "c", "enter", "r"}
	case presetsTab:
		keys = []string{"a", "e", "D", "enter", "r"}
	case trashTab:
		keys = []string{"R", "s", "enter", "r"}
	case plansTab:
		if m.plan == nil {
			keys = []string{"n", "enter", "r"}
		} else {
			keys = []string{"a", "A", "f", "u", "enter", "r", "esc"}
		}
	case worklogsTab:
		keys = []string{"a", "A", "e", "M", "D", "/", "enter"}
	}
	labels := map[string]string{"a": "Add", "e": "Edit", "M": "Move", "D": "Trash", "/": "Find", "enter": "Details", "s": "Scope", "c": "Config", "r": "Refresh", "R": "Restore", "n": "New plan", "A": "Apply", "f": "Retry failed", "u": "Retry uncertain", "esc": "Back"}
	if m.activeTab == statusTab {
		labels["s"] = "Status"
	}
	if m.activeTab == plansTab {
		labels["a"], labels["enter"] = "Ready/All", "Review"
		if m.plan != nil {
			labels["enter"] = "Details"
		}
	}
	if m.activeTab == worklogsTab && m.worklogView != dayWorklogView {
		labels["enter"] = "Open day"
	}
	if m.activeTab == worklogsTab {
		labels["A"] = "Preset"
	}
	for _, key := range keys {
		for _, action := range m.commandActions() {
			if action.key == key {
				primary = append(primary, actionHint{action.chord, labels[key]})
				break
			}
		}
	}
	primary = append(primary, actionHint{"?", "Actions"}, actionHint{"q", "Quit"})
	secondary = []actionHint{{"1", "Status"}, {"2", "Worklogs"}, {"3", "Plans"}, {"4", "Presets"}, {"5", "Trash"}}
	if m.activeTab == worklogsTab {
		secondary = append(secondary, actionHint{"d/w/m", "View"})
		if m.worklogView == monthWorklogView {
			primary = append([]actionHint{{"hjkl", "Day"}, {"PgUp/Dn", "Month"}}, primary[1:]...)
		}
	}
	if m.activeTab == trashTab || (m.activeTab == plansTab && m.plan == nil) {
		secondary = append(secondary, actionHint{"d/w", "View"})
	}
	activity := "Activity"
	if m.activityOpen {
		activity = "Close activity"
	}
	secondary = append(secondary, actionHint{"g", activity})
	return
}
