package service

import "testing"

func TestPaginateUsesZeroBasedPagesAndWideOffset(t *testing.T) {
	offset, limit := paginate(1, 20)
	if offset != 20 || limit != 20 {
		t.Fatalf("paginate(1,20) = %d,%d", offset, limit)
	}
	offset, _ = paginate(2_000_000_000, 200)
	if offset != 400_000_000_000 {
		t.Fatalf("wide offset = %d", offset)
	}
}
