package schedule

import "sort"

func IsTechnologyFormat(format Format) bool {
	switch format {
	case Format3D, FormatIMAX, FormatDolby, FormatScreenX, FormatLaserUltra, Format4DX, FormatICE, FormatInfinityVision:
		return true
	default:
		return false
	}
}

func sortedFormats(formats []Format) []Format {
	sort.Slice(formats, func(i, j int) bool { return formats[i] < formats[j] })
	return formats
}
