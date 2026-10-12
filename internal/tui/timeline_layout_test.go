package tui

import (
	"fmt"
	"testing"
	"time"
)

func TestTimelineCellsFillWidthWithBalancedRounding(t *testing.T) {
	for _, width := range []int{26, 30, 75, 95, 140, 570} {
		for _, offset := range []time.Duration{0, 8 * time.Minute, 52 * time.Minute} {
			t.Run(fmt.Sprintf("%d/%s", width, offset), func(t *testing.T) {
				start := time.Date(2026, 10, 9, 7, 0, 0, 0, time.FixedZone("local", 310*60)).Add(offset)
				end := start.Add(10*time.Hour + 17*time.Minute)
				scale := newTimelineScale(start, end, width)
				if scale.width != width || !scale.start.Equal(start) || !scale.end.Equal(end) {
					t.Fatalf("grid exceeded viewport or changed bounds: %+v", scale)
				}
				rules := timelineCellColumns(scale, timelineQuarter)
				complete := 0
				minWidth, maxWidth := width, 0
				for column, cellWidth := range rules {
					if column+cellWidth < scale.width {
						complete++
						minWidth = min(minWidth, cellWidth)
						maxWidth = max(maxWidth, cellWidth)
					}
				}
				if maxWidth-minWidth > 1 {
					t.Fatalf("complete cells differ by more than one column: %d–%d", minWidth, maxWidth)
				}
				if complete == 0 {
					t.Fatal("no complete cells tested")
				}
				if !scale.midpoint(scale.width - 1).Before(end) {
					t.Fatal("last column samples outside actual bounds")
				}
			})
		}
	}
}
