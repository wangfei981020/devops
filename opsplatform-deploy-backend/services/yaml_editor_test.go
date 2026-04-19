package services

import (
	"strings"
	"testing"
)

func TestUpdateImageTag_BasicReplace(t *testing.T) {
	input := `# some header comment
image:
  repository: harbor.xx/foo/bar
  pullPolicy: Always
  # Overrides the image tag whose default is the chart appVersion.
  tag: "20260415000000-1"
replicaCount: 1
`
	out, changed, err := UpdateImageTag([]byte(input), "20260416111111-9")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true")
	}
	s := string(out)
	if !strings.Contains(s, `tag: "20260416111111-9"`) {
		t.Fatalf("tag not updated: %s", s)
	}
	if !strings.Contains(s, "# Overrides the image tag") {
		t.Fatal("comment not preserved")
	}
	if !strings.Contains(s, "replicaCount: 1") {
		t.Fatal("other keys lost")
	}
}

func TestUpdateImageTag_SameTagReturnsNoChange(t *testing.T) {
	input := `image:
  tag: "v1"
`
	_, changed, err := UpdateImageTag([]byte(input), "v1")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if changed {
		t.Fatal("expected changed=false when tag unchanged")
	}
}

func TestReadImage(t *testing.T) {
	input := `image:
  repository: harbor.xx/proj/mod
  tag: "v1"
`
	repo, tag, err := ReadImage([]byte(input))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if repo != "harbor.xx/proj/mod" || tag != "v1" {
		t.Fatalf("got repo=%s tag=%s", repo, tag)
	}
}
