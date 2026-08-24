package schedule

type Provider string
type Scope string
type Language string
type Format string

const (
	ProviderUGC       Provider = "ugc"
	ProviderKinepolis Provider = "kinepolis"
	ProviderPathe     Provider = "pathe"
	ProviderCombined  Provider = "combined"

	ScopeAll    Scope = "all_cinemas"
	ScopeSingle Scope = "single_cinema"

	LanguageAll    Language = "ALL"
	LanguageVOSTFR Language = "VOSTFR"
	LanguageVF     Language = "VF"
	LanguageVO     Language = "VO"
	LanguageVFSME  Language = "VF_SME"

	FormatAll        Format = "ALL"
	Format2D         Format = "2D"
	Format3D         Format = "3D"
	FormatIMAX       Format = "IMAX"
	FormatDolby      Format = "DOLBY"
	FormatScreenX    Format = "SCREENX"
	FormatLaserUltra Format = "LASER_ULTRA"
	Format4DX        Format = "4DX"
	FormatICE        Format = "ICE"
)
