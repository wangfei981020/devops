package services

// IndexDiff: low 落后 high 多少个发布。valid 列表是 GCP 返回的 validVersions（新→旧顺序）。
// 返回 indexOf(low) - indexOf(high)。任一不存在时用插入位置近似。
func IndexDiff(low, high string, valid []string) int {
	li := indexOf(low, valid)
	hi := indexOf(high, valid)
	return li - hi
}

func indexOf(v string, valid []string) int {
	for i, x := range valid {
		if x == v {
			return i
		}
	}
	target, err := ParseVersion(v)
	if err != nil {
		return len(valid)
	}
	for i, x := range valid {
		xv, err := ParseVersion(x)
		if err != nil {
			continue
		}
		if compareVer(xv, target) <= 0 {
			return i
		}
	}
	return len(valid)
}

func compareVer(a, b *Version) int {
	if a.Major != b.Major {
		return a.Major - b.Major
	}
	if a.Minor != b.Minor {
		return a.Minor - b.Minor
	}
	if a.Patch != b.Patch {
		return a.Patch - b.Patch
	}
	if a.Build > b.Build {
		return 1
	}
	if a.Build < b.Build {
		return -1
	}
	return 0
}

// ArithmeticDiff: encode(high) - encode(low)
func ArithmeticDiff(low, high string) (float64, error) {
	lv, err := ParseVersion(low)
	if err != nil {
		return 0, err
	}
	hv, err := ParseVersion(high)
	if err != nil {
		return 0, err
	}
	return hv.ArithmeticEncode() - lv.ArithmeticEncode(), nil
}
