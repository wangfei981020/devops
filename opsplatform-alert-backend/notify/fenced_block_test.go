package notify

import (
	"strings"
	"testing"
)

func TestFencedBlockPutsDelimitersOnTheirOwnLines(t *testing.T) {
	got := FencedBlock("2026-09-09 06:37:36.638 ERROR boom")

	want := "```\n2026-09-09 06:37:36.638 ERROR boom\n```"
	if got != want {
		t.Errorf("FencedBlock() = %q, want %q", got, want)
	}
}

// A fence whose delimiters are already on their own lines is what
// NormalizeFencedBlocks emits, so passing FencedBlock's output through the
// notifier decoration must be a no-op — otherwise the two disagree about the
// canonical shape and one of them is rewriting the other's output.
func TestFencedBlockOutputSurvivesNormalizeUnchanged(t *testing.T) {
	block := FencedBlock("line one\nline two")

	if got := NormalizeFencedBlocks(block); got != block {
		t.Errorf("NormalizeFencedBlocks(FencedBlock(x)) = %q, want it unchanged at %q", got, block)
	}
}

func TestFencedBlockTrimsBoundaryNewlines(t *testing.T) {
	got := FencedBlock("\n\nlog line\n\n")

	if strings.Contains(got, "```\n\n") || strings.Contains(got, "\n\n```") {
		t.Errorf("FencedBlock() left a blank line against a delimiter: %q", got)
	}
}

// A log line containing ``` would otherwise close the fence early, leaving the
// rest of the alert to render as markdown.
func TestFencedBlockNeutralizesAnInnerFence(t *testing.T) {
	got := FencedBlock("panic: unexpected ``` in payload")

	if strings.Count(got, "```") != 2 {
		t.Errorf("FencedBlock() should leave exactly the two delimiters, got %q", got)
	}
	if strings.Count(got, "`") != strings.Count("panic: unexpected ``` in payload", "`")+6 {
		t.Errorf("FencedBlock() dropped or added backticks: %q", got)
	}
	// The whole block must still be one fence to the platform renderers.
	if got != NormalizeFencedBlocks(got) {
		t.Errorf("FencedBlock() output is not already canonical: %q", got)
	}
}

func TestFencedBlockNeutralizesALongBacktickRun(t *testing.T) {
	got := FencedBlock("`````")

	if strings.Count(got, "```") != 2 {
		t.Errorf("FencedBlock() should leave exactly the two delimiters, got %q", got)
	}
}
