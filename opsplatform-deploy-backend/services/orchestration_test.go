package services

import "testing"

func TestSetGetImageTag(t *testing.T) {
	v := []byte("image:\n  repository: harbor.x.com/g32/svc\n  tag: \"1.0\"\nreplicas: 1\n")
	out := SetImageTag(v, "2.5")
	repo, tag := GetImageRepoTag(out)
	if repo != "harbor.x.com/g32/svc" || tag != "2.5" {
		t.Fatalf("got repo=%q tag=%q", repo, tag)
	}
	// 清空 tag
	out2 := SetImageTag(v, "")
	_, tag2 := GetImageRepoTag(out2)
	if tag2 != "" {
		t.Errorf("clear tag failed: %q", tag2)
	}
	// 域名去 scheme
	if HarborDomain("https://harbor.x.com/") != "harbor.x.com" {
		t.Errorf("HarborDomain strip failed")
	}
}
