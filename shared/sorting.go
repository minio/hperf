package shared

import (
	"cmp"
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

// The comparators return 0 for equal elements. Returning 1 instead, as these
// used to, breaks the strict weak ordering slices.SortFunc requires, which
// leaves the order of tied elements undefined: two analyses of the same file
// could disagree. Ties are common because a data point with no samples in its
// interval reports RMSH/TTFBH of 0.
func SortDataPointRMSH(dps []DP) {
	slices.SortFunc(dps, func(a DP, b DP) int {
		return cmp.Compare(a.RMSH, b.RMSH)
	})
}

func SortDataPointTTFBH(dps []DP) {
	slices.SortFunc(dps, func(a DP, b DP) int {
		return cmp.Compare(a.TTFBH, b.TTFBH)
	})
}
