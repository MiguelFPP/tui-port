package domain

// Hard limits applied by the adapter before returning data to the TUI.
const (
	MaxSockets            = 8192
	MaxProcesses          = 8192
	MaxFdLinksPerProcess  = 4096
	MaxCmdlineBytes       = 4096
	MaxProcessNameBytes   = 256
	MaxUserLookups        = 512
	MaxSocketsPerSnapshot = MaxSockets
)
