package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestNewFileRejectsPathLikeNames(t *testing.T) {
	for _, name := range []string{"", ".", "..", "../secret", "a/b.txt", "a\\b.txt", strings.Repeat("a", 256)} {
		t.Run(name, func(t *testing.T) {
			_, err := NewFile("id", "owner", name, "text/plain", 1, "hash")
			if !errors.Is(err, ErrInvalidFileName) {
				t.Fatalf("name %q error = %v", name, err)
			}
		})
	}
}

func TestNewFileRejectsInvalidContentType(t *testing.T) {
	for _, contentType := range []string{"not a media type", strings.Repeat("a", 129)} {
		t.Run(contentType, func(t *testing.T) {
			_, err := NewFile("id", "owner", "readme.txt", contentType, 1, "hash")
			if !errors.Is(err, ErrInvalidContentType) {
				t.Fatalf("content type %q error = %v", contentType, err)
			}
		})
	}
}

func TestNewFileNormalizesContentType(t *testing.T) {
	file, err := NewFile("id", "owner", "readme.txt", "", 0, "hash")
	if err != nil {
		t.Fatal(err)
	}
	if file.ContentType() != "application/octet-stream" {
		t.Fatalf("content type = %q", file.ContentType())
	}
}
