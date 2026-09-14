package notify

import "testing"

func TestNormalizeFencedBlocks(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "inline single-line fence",
			in:   "```2026-09-07 20:15:03 ERROR order timeout cost=8123ms```",
			want: "```\n2026-09-07 20:15:03 ERROR order timeout cost=8123ms\n```",
		},
		{
			name: "inline multi-line fence",
			in:   "```java.lang.RuntimeException: downstream timeout\n\tat com.acme.pay.GatewayClient.call(GatewayClient.java:142)\n\t... 27 more```",
			want: "```\njava.lang.RuntimeException: downstream timeout\n\tat com.acme.pay.GatewayClient.call(GatewayClient.java:142)\n\t... 27 more\n```",
		},
		{
			name: "fence with a language tag",
			in:   "```log some log content here```",
			want: "```log\nsome log content here\n```",
		},
		{
			name: "already correct fence is left untouched",
			in:   "```\nalready fine content\nspanning two lines\n```",
			want: "```\nalready fine content\nspanning two lines\n```",
		},
		{
			name: "already correct fence with a language tag is left untouched",
			in:   "```log\nalready fine content\n```",
			want: "```log\nalready fine content\n```",
		},
		{
			name: "text with no fence at all",
			in:   "plain **bold** text, no backticks here",
			want: "plain **bold** text, no backticks here",
		},
		{
			name: "unterminated fence is left untouched",
			in:   "before ```dangling open with no close",
			want: "before ```dangling open with no close",
		},
		{
			name: "two fences in one message",
			in:   "first: ```200 OK response body``` and second:\n```\nsecond fence\nalready fine\n```\ndone",
			want: "first: ```\n200 OK response body\n``` and second:\n```\nsecond fence\nalready fine\n```\ndone",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := NormalizeFencedBlocks(tc.in)
			if got != tc.want {
				t.Errorf("NormalizeFencedBlocks(%q)\n  got:  %q\n  want: %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestNormalizeFencedBlocksPreservesRealisticContent guards specifically
// against the ambiguity a naive "greedy leading alnum run = language tag"
// heuristic would create: real alert content commonly starts with a
// timestamp (digits/hyphens) or a fully-qualified class name (letters
// immediately followed by a dot), neither of which must be mistaken for a
// language tag and relocated off the content.
func TestNormalizeFencedBlocksPreservesRealisticContent(t *testing.T) {
	msg := "2026-09-07 20:15:03 ERROR [order-api] payment timeout for order 20260907-001 cost=8123ms"
	got := NormalizeFencedBlocks("```" + msg + "```")
	want := "```\n" + msg + "\n```"
	if got != want {
		t.Errorf("timestamp-led content mangled:\n  got:  %q\n  want: %q", got, want)
	}

	stack := "java.lang.RuntimeException: downstream timeout calling payment-gateway\n" +
		"\tat com.acme.pay.GatewayClient.call(GatewayClient.java:142)\n" +
		"\t... 27 more"
	got = NormalizeFencedBlocks("```" + stack + "```")
	want = "```\n" + stack + "\n```"
	if got != want {
		t.Errorf("class-name-led stack mangled:\n  got:  %q\n  want: %q", got, want)
	}
}
