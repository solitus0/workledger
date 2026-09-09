package worklogs

import (
	"context"
	"fmt"
	"testing"
)

func TestLookupIssueMetadataBatchesLargeKeySets(t *testing.T) {
	store, service := newTestService(t)
	defer store.Close()

	const keyCount = 33_000
	keys := make([]string, 0, keyCount)
	for index := 1; index <= keyCount; index++ {
		keys = append(keys, fmt.Sprintf("APP-%d", index))
	}

	items, err := service.LookupIssueMetadata(context.Background(), keys)
	if err != nil {
		t.Fatalf("lookup large metadata key set: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected no metadata rows, got %d", len(items))
	}
}
