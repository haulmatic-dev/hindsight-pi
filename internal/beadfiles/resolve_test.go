package beadfiles

import (
	"reflect"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

func TestResolve_NilBead(t *testing.T) {
	if got := Resolve(nil); got != nil {
		t.Fatalf("nil bead: want nil, got %#v", got)
	}
}

func TestResolve_ZeroValue(t *testing.T) {
	if got := Resolve(&types.Bead{}); got != nil {
		t.Fatalf("zero bead: want nil, got %#v", got)
	}
}

func TestResolve_MetadataFiles(t *testing.T) {
	got := Resolve(&types.Bead{Metadata: map[string]string{"files": "a.go, b.go"}})
	want := []string{"a.go", "b.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("metadata files: want %#v, got %#v", want, got)
	}
}

func TestResolve_MetadataTargetFiles(t *testing.T) {
	got := Resolve(&types.Bead{Metadata: map[string]string{"target_files": "x/a.go, x/b.go"}})
	want := []string{"x/a.go", "x/b.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("metadata target_files: want %#v, got %#v", want, got)
	}
}

func TestResolve_MetadataFilesTakesPrecedence(t *testing.T) {
	got := Resolve(&types.Bead{Metadata: map[string]string{"files": "a.go", "target_files": "x/a.go"}})
	want := []string{"a.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("files first: want %#v, got %#v", want, got)
	}
}

func TestResolve_MetadataBlankSkipped(t *testing.T) {
	got := Resolve(&types.Bead{Metadata: map[string]string{"files": "   ", "target_files": "y/b.go"}})
	want := []string{"y/b.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("blank files skipped: want %#v, got %#v", want, got)
	}
}

func TestResolve_DescriptionFallback(t *testing.T) {
	got := Resolve(&types.Bead{Description: "Do X\nFiles: internal/x.go internal/y.go\nmore text"})
	want := []string{"internal/x.go", "internal/y.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("description fallback: want %#v, got %#v", want, got)
	}
}

func TestResolve_DescriptionFirstFilesLineWins(t *testing.T) {
	got := Resolve(&types.Bead{Description: "Files: first.go\nFiles: second.go"})
	want := []string{"first.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("first line only: want %#v, got %#v", want, got)
	}
}

func TestResolve_DescriptionCaseInsensitivePrefix(t *testing.T) {
	got := Resolve(&types.Bead{Description: "FILES: upper.go"})
	want := []string{"upper.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("case-insensitive prefix: want %#v, got %#v", want, got)
	}
}

func TestResolve_DescriptionTBDIgnored(t *testing.T) {
	got := Resolve(&types.Bead{Description: "Files: TBD"})
	if got != nil {
		t.Fatalf("TBD only: want nil, got %#v", got)
	}
}

func TestResolve_DescriptionNotInLine(t *testing.T) {
	got := Resolve(&types.Bead{Description: "some prose about files: that is not a list"})
	if got != nil {
		t.Fatalf("no files: prefix line: want nil, got %#v", got)
	}
}

func TestResolve_MetadataBeatsDescription(t *testing.T) {
	got := Resolve(&types.Bead{
		Description: "Files: from-desc.go",
		Metadata:    map[string]string{"files": "from-meta.go"},
	})
	want := []string{"from-meta.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("metadata wins: want %#v, got %#v", want, got)
	}
}

func TestSplitFileList(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", nil},
		{"spaces", "a.go b.go", []string{"a.go", "b.go"}},
		{"commas", "a.go,b.go", []string{"a.go", "b.go"}},
		{"semicolons", "a.go;b.go", []string{"a.go", "b.go"}},
		{"tabs", "a.go\tb.go", []string{"a.go", "b.go"}},
		{"mixed", "a.go, b.go;c.go d.go", []string{"a.go", "b.go", "c.go", "d.go"}},
		{"tbd-skipped", "a.go, TBD, b.go", []string{"a.go", "b.go"}},
		{"tbd-lower", "tbd", nil},
		{"tbd-upper", "TBD", nil},
		{"tbd-mixed-case", "Tbd", nil},
		{"whitespace-only", "   ", nil},
		{"duplicate", "a.go, a.go", []string{"a.go", "a.go"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SplitFileList(tc.in)
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("SplitFileList(%q): want %#v, got %#v", tc.in, tc.want, got)
			}
		})
	}
}
