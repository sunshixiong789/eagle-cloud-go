package service

import "testing"

func TestPaginateUsesZeroBasedPages(t *testing.T) {
	offset, limit := paginate(0, 0)
	if offset != 0 || limit != defaultPageSize {
		t.Fatalf("first page = %d,%d", offset, limit)
	}
	offset, limit = paginate(1, 20)
	if offset != 20 || limit != 20 {
		t.Fatalf("second page = %d,%d", offset, limit)
	}
}
