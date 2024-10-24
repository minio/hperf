package shared

import (
	"slices"
	"strings"
)

type SortType string

const (
	SortDefault SortType = "RMSH"
	SortRMSH    SortType = "RMSH"
	SortTTFBH   SortType = "TTFBH"
)

func HostFilter(host string, dps []DP) (filtered []DP) {
	filtered = make([]DP, 0)
	for _, v := range dps {
		if strings.Contains(v.Local, host) {
			filtered = append(filtered, v)
		} else if strings.Contains(v.Remote, host) {
			filtered = append(filtered, v)
		}
	}

	return
}

func SortDataPoints(dps []DP, c Config) {
	switch c.Sort {
	case SortRMSH:
		SortDataPointRMSH(dps)
	case SortTTFBH:
		SortDataPointTTFBH(dps)
	default:
		c.Sort = SortDefault
		SortDataPointRMSH(dps)
	}
}

func SortDataPointRMSH(dps []DP) {
	slices.SortFunc(dps, func(a DP, b DP) int {
		if a.RMSH < b.RMSH {
			return -1
		} else {
			return 1
		}
	})
}

func SortDataPointTTFBH(dps []DP) {
	slices.SortFunc(dps, func(a DP, b DP) int {
		if a.TTFBH < b.TTFBH {
			return -1
		} else {
			return 1
		}
	})
}
