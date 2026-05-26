package services

import (
	"fmt"
	"regexp"
	"strconv"
)

type Version struct {
	Major int
	Minor int
	Patch int
	Build int64
	Raw   string
}

var versionRE = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)-gke\.(\d+)$`)

func ParseVersion(s string) (*Version, error) {
	m := versionRE.FindStringSubmatch(s)
	if m == nil {
		return nil, fmt.Errorf("invalid GKE version %q", s)
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	patch, _ := strconv.Atoi(m[3])
	build, _ := strconv.ParseInt(m[4], 10, 64)
	return &Version{Major: major, Minor: minor, Patch: patch, Build: build, Raw: s}, nil
}

func (v *Version) ArithmeticEncode() float64 {
	intPart := v.Major*10000 + v.Minor*100 + v.Patch
	s := fmt.Sprintf("%d.%d", intPart, v.Build)
	f, _ := strconv.ParseFloat(s, 64)
	return f
}

func (v *Version) MinorKey() string {
	return fmt.Sprintf("%d.%d", v.Major, v.Minor)
}
