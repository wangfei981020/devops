package notify

import (
	"strings"
	"testing"
)

const (
	zwsp = "\u200b" // zero width space
	bom  = "\ufeff" // zero width no-break space / BOM
)

// A template copied out of a chat client can carry a zero-width space in front
// of each fence. It is invisible everywhere an operator would look, and it
// breaks Lark alone: Telegram's fence regex is not line-anchored, so the same
// content renders correctly there and wrongly here.
func TestNormalizeStripsAZeroWidthSpaceBeforeAFence(t *testing.T) {
	in := "Exception:\n" + zwsp + "```\nboom\n" + zwsp + "```"

	got := NormalizeFencedBlocks(in)

	if strings.Contains(got, zwsp) {
		t.Errorf("NormalizeFencedBlocks() left a zero-width space: %q", got)
	}
	if got != "Exception:\n```\nboom\n```" {
		t.Errorf("NormalizeFencedBlocks() = %q", got)
	}
}

// The failure this was found by: with the delimiters unrecognised, Lark pairs
// each block's CLOSING fence with the next block's, so the fields between two
// log blocks end up rendered as code.
func TestNormalizeKeepsMultipleBlocksPairedAfterStripping(t *testing.T) {
	in := "1/2\nException:\n" + zwsp + "```\nlog-1\n" + zwsp + "```\n" +
		"\n2/2\nException:\n" + zwsp + "```\nlog-2\n" + zwsp + "```"

	got := NormalizeFencedBlocks(in)

	want := "1/2\nException:\n```\nlog-1\n```\n\n2/2\nException:\n```\nlog-2\n```"
	if got != want {
		t.Errorf("NormalizeFencedBlocks() = %q,\nwant %q", got, want)
	}
	// Every delimiter must start its own line, or Lark pairs them shifted.
	for _, line := range strings.Split(got, "\n") {
		if strings.Contains(line, "```") && line != "```" {
			t.Errorf("a fence delimiter does not own its line: %q", line)
		}
	}
}

func TestNormalizeStripsIndentationBeforeAFence(t *testing.T) {
	// Four spaces before a closing delimiter stop it closing anything.
	in := "Exception:\n    ```\nboom\n    ```"

	got := NormalizeFencedBlocks(in)

	if got != "Exception:\n```\nboom\n```" {
		t.Errorf("NormalizeFencedBlocks() = %q", got)
	}
}

func TestNormalizeStripsAByteOrderMarkBeforeAFence(t *testing.T) {
	got := NormalizeFencedBlocks("Exception:\n" + bom + "```\nboom\n" + bom + "```")

	if strings.Contains(got, bom) {
		t.Errorf("NormalizeFencedBlocks() left a BOM: %q", got)
	}
}

// Only the run in front of a delimiter is cleared. Indentation is meaningful
// inside a stack trace and must survive verbatim.
func TestNormalizeLeavesIndentationInsideAFenceAlone(t *testing.T) {
	in := "```\njava.lang.IllegalStateException: boom\n\tat com.sl.Service.settle(Service.java:88)\n```"

	if got := NormalizeFencedBlocks(in); got != in {
		t.Errorf("NormalizeFencedBlocks() rewrote fenced content: %q", got)
	}
}

// A zero-width space in ordinary prose is left alone; only fence lines are
// touched.
func TestNormalizeLeavesAZeroWidthSpaceInProseAlone(t *testing.T) {
	in := "Msg: lock" + zwsp + " error\n```\nboom\n```"

	if got := NormalizeFencedBlocks(in); got != in {
		t.Errorf("NormalizeFencedBlocks() = %q, want it unchanged", got)
	}
}
