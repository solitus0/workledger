package worklogs

import (
	"errors"
	"time"
)

// NormalizeTiming is shared by imports and restoration, which retain their own
// minimum-duration policies instead of adopting the manual creation minimum.
func NormalizeTiming(start time.Time, seconds int) (time.Time, int, error) {
	if seconds <= 0 || int64(seconds) > int64((time.Duration(1<<63-1))/time.Second) {
		return time.Time{}, 0, errors.New("duration must be positive and representable")
	}
	rounded, err := normalizeDuration(time.Duration(seconds) * time.Second)
	if err != nil {
		return time.Time{}, 0, err
	}
	return start.UTC().Round(time.Minute), rounded, nil
}

func HasMinutePrecision(start time.Time, seconds int) bool {
	return !start.IsZero() && start.Equal(start.Truncate(time.Minute)) && seconds > 0 && seconds%60 == 0
}

func normalizeDuration(duration time.Duration) (int, error) {
	if duration <= 0 {
		return 0, errors.New("duration must be positive")
	}
	rounded := duration.Round(time.Minute)
	if rounded == 0 {
		return 0, errors.New("duration rounds to zero minutes")
	}
	if rounded%time.Minute != 0 {
		return 0, errors.New("duration is too large to round to whole minutes")
	}
	return int(rounded / time.Second), nil
}

func minuteMinimum(seconds int) int {
	return seconds + (60-seconds%60)%60
}

// A usable slot is rounded inward so placement never enters occupied time.
func minuteSlot(start, end time.Time) (ContextFreeSlot, bool) {
	roundedStart := start.Truncate(time.Minute)
	if roundedStart.Before(start) {
		roundedStart = roundedStart.Add(time.Minute)
	}
	roundedEnd := end.Truncate(time.Minute)
	if !roundedStart.Before(roundedEnd) {
		return ContextFreeSlot{}, false
	}
	return ContextFreeSlot{Start: roundedStart, End: roundedEnd, DurationSeconds: int(roundedEnd.Sub(roundedStart) / time.Second)}, true
}
