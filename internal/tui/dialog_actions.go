package tui

func (m model) actionContextLabel() string {
	if label, _ := m.dialogActions(); label != "" {
		return label
	}
	if m.focus == focusActivity {
		return "Activity"
	}
	return m.activeTabLabel()
}

// Help reads the live underlying window. It owns only its search and selection,
// so asynchronous results can update a draft or finish an operation behind it.
func (m model) dialogActions() (string, []commandAction) {
	var actions []commandAction
	add := func(key, chord, label string) {
		actions = append(actions, commandAction{key, chord, label})
	}
	rows := func() {
		add("up", "↑", "Previous item")
		add("down", "↓", "Next item")
	}
	pages := func() {
		add("pgup", "PgUp", "Previous page")
		add("pgdown", "PgDn", "Next page")
	}
	edges := func() {
		add("home", "Home", "First content")
		add("end", "End", "Last content")
	}
	fields := func() {
		add("tab", "Tab", "Next field or accept suggestion")
		add("shift+tab", "Shift+Tab", "Previous field")
		add("up", "↑", "Previous field")
		add("down", "↓", "Next field")
		add("enter", "Enter", "Next field or save final field")
		add("ctrl+s", "Ctrl+S", "Save")
		add("esc", "Esc", "Cancel")
	}
	switch {
	case m.overlay == detailsOverlay:
		add("up", "↑", "Scroll up")
		add("down", "↓", "Scroll down")
		pages()
		edges()
		if m.issueHistory != nil {
			add("tab", "Tab", "Switch metadata or worklogs pane")
			add("r", "r", "Refresh or retry issue history")
		}
		add("enter", "Enter", "Close details")
		add("esc", "Esc", "Close details")
		if m.issueHistory != nil {
			return "Issue details", actions
		}
		return m.detailTitle, actions
	case m.overlay == issueSortOverlay:
		rows()
		add("enter", "Enter", "Apply sort")
		add("esc", "Esc", "Cancel")
		return "Sort", actions
	case m.overlay == epicAssignmentOverlay:
		if m.epicAssignment != nil && !m.epicAssignment.saving {
			rows()
			if !m.epicAssignment.stale {
				add("enter", "Enter", "Assign selected issues to epic")
			}
			add("esc", "Esc", "Cancel")
		}
		return "Assign epic", actions
	case m.overlay != noOverlay:
		copy, _ := m.overlayContent()
		add("enter", "Enter", m.confirmationAction())
		add("y", "y", m.confirmationAction())
		add("esc", "Esc", "Cancel")
		add("n", "n", "Cancel")
		return copy.title, actions
	case m.planOperation != nil:
		add("esc", "Esc", "Cancel operation")
		return "Plan operation", actions
	case m.issuePicker != nil:
		if m.issuePicker.detailFocus {
			add("up", "↑", "Scroll worklog history up")
			add("down", "↓", "Scroll worklog history down")
		} else {
			rows()
		}
		pages()
		if !m.issuePicker.searching || m.issuePicker.detailFocus {
			add("/", "/", "Find issues")
		}
		add("ctrl+t", "Ctrl+T", "Cycle key, description, or title search")
		add("tab", "Tab", "Switch issues or worklogs pane")
		if !m.issuePicker.loading && len(m.issuePicker.items) > 0 {
			add("enter", "Enter", "Choose selected issue")
		}
		label := "Close picker"
		if m.issuePicker.searching {
			label = "Clear search and browse"
		}
		add("esc", "Esc", label)
		return "Find issue", actions
	case m.presetPicker != nil:
		rows()
		if !m.presetPicker.searching {
			add("/", "/", "Find presets")
		}
		if len(m.presetPicker.items) > 0 {
			add("enter", "Enter", "Choose selected preset")
		}
		label := "Close picker"
		if m.presetPicker.searching {
			add("tab", "Tab", "Accept suggestion")
			label = "Clear search and browse"
		}
		add("esc", "Esc", label)
		return "Find preset", actions
	case m.issueForm != nil:
		if !m.issueForm.saving {
			fields()
		}
		return "Issue metadata", actions
	case m.presetForm != nil:
		fields()
		return "Preset editor", actions
	case m.planForm != nil:
		add("enter", "Enter", "Inspect and save a reconcile plan")
		add("ctrl+s", "Ctrl+S", "Inspect and save a reconcile plan")
		add("esc", "Esc", "Cancel")
		add("p", "p", "Push direction")
		add("u", "u", "Pull direction")
		add("d", "d", "Day window")
		add("w", "w", "Week window")
		add("c", "c", "Custom date window")
		add("x", "x", "Cycle target")
		add("v", "v", "Cycle route profile")
		if m.planForm.window == planCustomWindow {
			add("tab", "Tab", "Switch date field")
			add("shift+tab", "Shift+Tab", "Switch date field")
		}
		return "New reconcile plan", actions
	case m.move != nil:
		if !m.move.saving {
			add("up", "↑", "Earlier position")
			add("down", "↓", "Later position")
			add("ctrl+s", "Ctrl+S", "Save selected position")
			add("r", "r", "Refresh positions")
			add("esc", "Esc", "Cancel")
		}
		return "Move worklog", actions
	case m.form != nil:
		fields()
		if !m.form.editing {
			add("ctrl+p", "Ctrl+P", "Cycle placement")
			if m.form.focus == issueField {
				add("ctrl+k", "Ctrl+K", "Open issue history picker")
			}
			if m.form.placement != manualPlacement {
				add("ctrl+o", "Ctrl+O", "Toggle late-start eligibility")
				add("ctrl+l", "Ctrl+L", "Toggle lunch availability")
			}
		}
		return "Worklog editor", actions
	case m.worklogFilter != nil && m.worklogFilter.editing && m.activeTab == worklogsTab && m.worklogView == dayWorklogView:
		rows()
		add("enter", "Enter", "Browse filtered worklogs")
		add("esc", "Esc", "Clear filter")
		return "Find worklogs", actions
	case m.issueSearch != nil && m.issueSearch.editing && m.activeTab == issuesTab && m.issueView == issueListView:
		rows()
		pages()
		add("enter", "Enter", "Browse filtered issues")
		add("esc", "Esc", "Clear filter")
		return "Find issues", actions
	case m.epicSearch != nil && m.epicSearch.editing && m.activeTab == issuesTab && m.issueView == epicListView:
		rows()
		pages()
		add("enter", "Enter", "Browse filtered epics")
		add("esc", "Esc", "Clear filter")
		return "Find epics", actions
	}
	return "", nil
}
