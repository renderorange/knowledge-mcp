package knowledge

import "testing"

func TestSplitSections(t *testing.T) {
	data := "# Title\n\nintro text\n\n## First Section\nbody one\n\n### Sub Heading\nsub body\n\n## Second Section\nbody two\n"
	sections := SplitSections(data)

	want := [][2]string{
		{"Overview", "# Title\n\nintro text"},
		{"First Section", "## First Section\nbody one\n\n### Sub Heading\nsub body"},
		{"Second Section", "## Second Section\nbody two"},
	}
	if len(sections) != len(want) {
		t.Fatalf("len(sections) = %d, want %d (%#v)", len(sections), len(want), sections)
	}
	for i, w := range want {
		if sections[i][0] != w[0] || sections[i][1] != w[1] {
			t.Errorf("section[%d] = %q/%q, want %q/%q", i, sections[i][0], sections[i][1], w[0], w[1])
		}
	}
}

func TestSplitSectionsNoPreamble(t *testing.T) {
	data := "## Only Heading\nbody\n"
	sections := SplitSections(data)
	if len(sections) != 1 {
		t.Fatalf("len(sections) = %d, want 1", len(sections))
	}
	if sections[0][0] != "Only Heading" || sections[0][1] != "## Only Heading\nbody" {
		t.Errorf("got %#v", sections)
	}
}
