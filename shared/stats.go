package shared

func UpdatePSStats(b []int64, dp DP, c Config) {
	switch c.Sort {
	case SortRMSH:
		UpdatePSStatsRMHS(b, dp)
	case SortTTFBH:
		UpdatePSStatsTTFBH(b, dp)
	default:
		c.Sort = SortRMSH
		UpdatePSStatsRMHS(b, dp)
	}
}

func UpdatePSStatsRMHS(b []int64, dp DP) {
	b[0]++
	b[1] += dp.RMSH
	if dp.RMSH < b[2] {
		b[2] = dp.RMSH
	}
	b[3] = b[1] / b[0]
	if dp.RMSH > b[4] {
		b[4] = dp.RMSH
	}
}

func UpdatePSStatsTTFBH(b []int64, dp DP) {
	b[0]++
	b[1] += dp.TTFBH
	if dp.TTFBH < b[2] {
		b[2] = dp.TTFBH
	}
	b[3] = b[1] / b[0]
	if dp.TTFBH > b[4] {
		b[4] = dp.TTFBH
	}
}
