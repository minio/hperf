package shared

import (
	"slices"
)

type SortType string

const (
	SortDefault SortType = "RMSH"
	SortRMSH    SortType = "RMSH"
	SortTTFBH   SortType = "TTFBH"
)

// HostFilter keeps the data points where host is either end of the measurement.
// The comparison is per address and not a substring match, so filtering on
// 10.0.0.1 does not also return 10.0.0.10.
func HostFilter(host string, dps []DP) (filtered []DP) {
	filtered = make([]DP, 0)
	for _, v := range dps {
		if SameHost(HostOnly(v.Local), host) {
			filtered = append(filtered, v)
		} else if SameHost(HostOnly(v.Remote), host) {
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
