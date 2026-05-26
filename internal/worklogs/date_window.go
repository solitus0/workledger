package worklogs

import (
	"errors"
	"time"

	"github.com/solitus0/workledger/internal/config"
)

const weekOffsetRequiresWeekdayMessage = "--week-offset requires exactly one weekday filter: --mon, --tue, --wed, --thu, --fri, --sat, or --sun"

func WeekOffsetRequiresWeekdayMessage() string {
	return weekOffsetRequiresWeekdayMessage
}

type DateWindowSelection struct {
	Today         bool
	Yesterday     bool
	Monday        bool
	Tuesday       bool
	Wednesday     bool
	Thursday      bool
	Friday        bool
	Saturday      bool
	Sunday        bool
	CurrentWeek   bool
	LastWeek      bool
	CurrentMonth  bool
	LastMonth     bool
	From          string
	To            string
	WeekOffset    int
	WeekOffsetSet bool
}

func HasDateWindowSelection(selection DateWindowSelection) bool {
	return selection.Today ||
		selection.Yesterday ||
		selection.Monday ||
		selection.Tuesday ||
		selection.Wednesday ||
		selection.Thursday ||
		selection.Friday ||
		selection.Saturday ||
		selection.Sunday ||
		selection.CurrentWeek ||
		selection.LastWeek ||
		selection.CurrentMonth ||
		selection.LastMonth ||
		selection.From != "" ||
		selection.To != ""
}

func ResolveDateWindowSelectionAt(cfg config.EffectiveConfig, selection DateWindowSelection, now func() time.Time) (*time.Time, *time.Time, error) {
	if err := validateDateWindowSelection(selection); err != nil {
		return nil, nil, err
	}

	switch {
	case selection.Today:
		current := now().In(cfg.Location)
		from, to := dayBounds(current, cfg.Location)
		return &from, &to, nil
	case selection.Yesterday:
		current := now().In(cfg.Location).AddDate(0, 0, -1)
		from, to := dayBounds(current, cfg.Location)
		return &from, &to, nil
	case selection.Monday:
		return resolveWeekdaySelection(now().In(cfg.Location), time.Monday, selection.WeekOffset, cfg.Location)
	case selection.Tuesday:
		return resolveWeekdaySelection(now().In(cfg.Location), time.Tuesday, selection.WeekOffset, cfg.Location)
	case selection.Wednesday:
		return resolveWeekdaySelection(now().In(cfg.Location), time.Wednesday, selection.WeekOffset, cfg.Location)
	case selection.Thursday:
		return resolveWeekdaySelection(now().In(cfg.Location), time.Thursday, selection.WeekOffset, cfg.Location)
	case selection.Friday:
		return resolveWeekdaySelection(now().In(cfg.Location), time.Friday, selection.WeekOffset, cfg.Location)
	case selection.Saturday:
		return resolveWeekdaySelection(now().In(cfg.Location), time.Saturday, selection.WeekOffset, cfg.Location)
	case selection.Sunday:
		return resolveWeekdaySelection(now().In(cfg.Location), time.Sunday, selection.WeekOffset, cfg.Location)
	case selection.CurrentWeek:
		from, to := weekBounds(now().In(cfg.Location), cfg.Location)
		return &from, &to, nil
	case selection.LastWeek:
		from, to := weekBounds(now().In(cfg.Location).AddDate(0, 0, -7), cfg.Location)
		return &from, &to, nil
	case selection.CurrentMonth:
		from, to := monthBounds(now().In(cfg.Location), cfg.Location)
		return &from, &to, nil
	case selection.LastMonth:
		from, to := monthBounds(now().In(cfg.Location).AddDate(0, -1, 0), cfg.Location)
		return &from, &to, nil
	case selection.From != "" || selection.To != "":
		var fromValue *time.Time
		var toValue *time.Time
		if selection.From != "" {
			parsed, err := parseDateSelectorAt(selection.From, cfg.Location, now)
			if err != nil {
				return nil, nil, err
			}
			start, _ := dayBounds(parsed, cfg.Location)
			fromValue = &start
		}
		if selection.To != "" {
			parsed, err := parseDateSelectorAt(selection.To, cfg.Location, now)
			if err != nil {
				return nil, nil, err
			}
			_, end := dayBounds(parsed, cfg.Location)
			toValue = &end
		}
		if fromValue != nil && toValue != nil && fromValue.After(*toValue) {
			return toValue, fromValue, nil
		}
		return fromValue, toValue, nil
	default:
		return nil, nil, nil
	}
}

func validateDateWindowSelection(selection DateWindowSelection) error {
	if selection.WeekOffsetSet {
		if hasNonWeekdayDateWindowSelection(selection) {
			return errors.New("--week-offset only modifies weekday filters and cannot be combined with --today, --yesterday, --current-week, --last-week, --current-month, --last-month, --from, or --to")
		}
		if countWeekdaySelections(selection) != 1 {
			return errors.New(weekOffsetRequiresWeekdayMessage)
		}
	}

	shortcuts := 0
	for _, selected := range []bool{
		selection.Today,
		selection.Yesterday,
		selection.Monday,
		selection.Tuesday,
		selection.Wednesday,
		selection.Thursday,
		selection.Friday,
		selection.Saturday,
		selection.Sunday,
		selection.CurrentWeek,
		selection.LastWeek,
		selection.CurrentMonth,
		selection.LastMonth,
	} {
		if selected {
			shortcuts++
		}
	}
	if shortcuts > 1 {
		return errors.New("today, yesterday, mon, tue, wed, thu, fri, sat, sun, current-week, last-week, current-month, and last-month are mutually exclusive")
	}
	if shortcuts > 0 && (selection.From != "" || selection.To != "") {
		return errors.New("today, yesterday, mon, tue, wed, thu, fri, sat, sun, current-week, last-week, current-month, and last-month cannot be combined with from or to")
	}
	return nil
}

func countWeekdaySelections(selection DateWindowSelection) int {
	count := 0
	for _, selected := range []bool{
		selection.Monday,
		selection.Tuesday,
		selection.Wednesday,
		selection.Thursday,
		selection.Friday,
		selection.Saturday,
		selection.Sunday,
	} {
		if selected {
			count++
		}
	}
	return count
}

func hasNonWeekdayDateWindowSelection(selection DateWindowSelection) bool {
	return selection.Today ||
		selection.Yesterday ||
		selection.CurrentWeek ||
		selection.LastWeek ||
		selection.CurrentMonth ||
		selection.LastMonth ||
		selection.From != "" ||
		selection.To != ""
}

func resolveWeekdaySelection(now time.Time, weekday time.Weekday, weekOffset int, location *time.Location) (*time.Time, *time.Time, error) {
	from, to := weekdayBounds(now.AddDate(0, 0, weekOffset*7), weekday, location)
	return &from, &to, nil
}
