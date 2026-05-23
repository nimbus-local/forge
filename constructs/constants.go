package constructs

// Lambda runtimes. Use these instead of raw strings so a future AWS runtime
// addition only requires updating this file.
const (
	RuntimeGo        = "provided.al2023" // Go compiled binary (recommended)
	RuntimeNodeJS24  = "nodejs24.x"
	RuntimeNodeJS22  = "nodejs22.x"
	RuntimeNodeJS20  = "nodejs20.x"
	RuntimePython313 = "python3.13"
	RuntimePython312 = "python3.12"
	RuntimeJava21    = "java21"
	RuntimeDotnet8   = "dotnet8"
)

// Lambda CPU architectures.
const (
	ArchARM64 = "arm64"
	ArchX8664 = "x86_64"
)
