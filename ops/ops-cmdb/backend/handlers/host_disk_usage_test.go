package handlers

import "testing"

func TestInstanceIP(t *testing.T) {
	cases := map[string]string{
		"10.128.0.41:9100":        "10.128.0.41",
		"10.128.0.41":             "10.128.0.41",
		"http://10.128.0.41:9100": "10.128.0.41",
		"":                        "",
	}
	for in, want := range cases {
		if got := instanceIP(in); got != want {
			t.Errorf("instanceIP(%q) = %q，期望 %q", in, got, want)
		}
	}
}

func TestMatchSingleDisk(t *testing.T) {
	// 文件系统容量总比云盘标称小一点（元数据 + 保留块），必须能容忍
	disks := []diskRecord{{ID: 1, SizeGB: 100, Name: "boot"}}
	fs := []fsUsage{{Mountpoint: "/", SizeGB: 96.8, UsedPct: 42}}

	got := matchDisks(disks, fs)
	if len(got) != 1 || got[1].Mountpoint != "/" {
		t.Fatalf("单盘应配对成功，得到 %#v", got)
	}
}

func TestMatchMultipleDistinctSizes(t *testing.T) {
	disks := []diskRecord{
		{ID: 1, SizeGB: 100, Name: "boot"},
		{ID: 2, SizeGB: 500, Name: "data"},
	}
	fs := []fsUsage{
		{Mountpoint: "/data", SizeGB: 492, UsedPct: 71},
		{Mountpoint: "/", SizeGB: 97, UsedPct: 38},
	}

	got := matchDisks(disks, fs)
	if got[1].Mountpoint != "/" {
		t.Errorf("100G 盘应配到 /，得到 %q", got[1].Mountpoint)
	}
	if got[2].Mountpoint != "/data" {
		t.Errorf("500G 盘应配到 /data，得到 %q", got[2].Mountpoint)
	}
}

func TestMatchIsDeterministicForEqualSizes(t *testing.T) {
	// Kafka 那种两块同容量数据盘本来就无法区分归属，
	// 但必须保证同样的输入每次得到同样的输出 ——
	// 否则用量会在两块盘之间来回跳，看起来像数据在抖。
	disks := []diskRecord{
		{ID: 1, SizeGB: 500, Name: "kafka-data-1"},
		{ID: 2, SizeGB: 500, Name: "kafka-data-2"},
	}
	fs := []fsUsage{
		{Mountpoint: "/data2", SizeGB: 492, UsedPct: 68},
		{Mountpoint: "/data1", SizeGB: 492, UsedPct: 71},
	}

	first := matchDisks(disks, fs)
	for i := 0; i < 20; i++ {
		again := matchDisks(disks, fs)
		for id, f := range first {
			if again[id].Mountpoint != f.Mountpoint {
				t.Fatalf("同容量盘的配对不稳定：盘 %d 先配到 %q，后配到 %q",
					id, f.Mountpoint, again[id].Mountpoint)
			}
		}
	}
	if len(first) != 2 {
		t.Fatalf("两块盘都应配上，得到 %d", len(first))
	}
}

func TestMatchRejectsOutOfTolerance(t *testing.T) {
	// 差得太远宁可不配 —— 配错的用量比没有用量更糟：
	// 没有用量前端显示「未接入」，配错了则显示一个看起来正常的错数字
	disks := []diskRecord{{ID: 1, SizeGB: 500, Name: "data"}}
	fs := []fsUsage{{Mountpoint: "/", SizeGB: 97, UsedPct: 42}}

	if got := matchDisks(disks, fs); len(got) != 0 {
		t.Fatalf("容差外不应配对，得到 %#v", got)
	}
}

func TestMatchPrefersClosest(t *testing.T) {
	// 两个候选都在容差内时，取最接近的那个
	disks := []diskRecord{{ID: 1, SizeGB: 100, Name: "boot"}}
	fs := []fsUsage{
		{Mountpoint: "/far", SizeGB: 90, UsedPct: 10},
		{Mountpoint: "/near", SizeGB: 99, UsedPct: 20},
	}
	if got := matchDisks(disks, fs); got[1].Mountpoint != "/near" {
		t.Fatalf("应取最接近的 /near，得到 %q", got[1].Mountpoint)
	}
}

func TestMatchOneFsNotReusedByTwoDisks(t *testing.T) {
	// 一个文件系统只能配给一块盘。否则两块盘会显示同一个用量，
	// 看起来像"两块盘都满了"，而实际只有一块。
	disks := []diskRecord{
		{ID: 1, SizeGB: 100, Name: "a"},
		{ID: 2, SizeGB: 100, Name: "b"},
	}
	fs := []fsUsage{{Mountpoint: "/", SizeGB: 98, UsedPct: 50}}

	got := matchDisks(disks, fs)
	if len(got) != 1 {
		t.Fatalf("只有一个文件系统，应只配上一块盘，得到 %d 块", len(got))
	}
}

func TestMatchEmptyInputs(t *testing.T) {
	if got := matchDisks(nil, nil); len(got) != 0 {
		t.Fatal("空输入应返回空结果")
	}
	if got := matchDisks([]diskRecord{{ID: 1, SizeGB: 100}}, nil); len(got) != 0 {
		t.Fatal("没有观测时不该配对")
	}
	// size 为 0 的盘（台账没采到容量）不参与配对，避免除零
	if got := matchDisks([]diskRecord{{ID: 1, SizeGB: 0}}, []fsUsage{{SizeGB: 100}}); len(got) != 0 {
		t.Fatal("容量为 0 的盘不应参与配对")
	}
}
