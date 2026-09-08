package ir

// LIMITS constants (help.txt:724-745). Only the target-independent rows are
// enforced by static validation; coordinate bounds, platform key support
// and exec output caps are preflight/runtime (out of scope here).
const (
	MaxLinesPerRun   = 1000
	MaxLineBytes     = 64 * 1024
	MaxKeysSeq       = 64 // k: sequential keys per line
	MaxChordKeys     = 8  // k: keys per chord including flags
	MinScrollTicks   = 1
	MaxScrollTicks   = 50
	MaxSleepMS       = 60 * 1000 // sleep <= 60s
	MaxDelayMS       = 10 * 1000 // d=, ms= <= 10s
	MaxWaitMS        = 60 * 1000 // wait= <= 60s
	MaxExecTimeoutMS = 60 * 1000 // exec timeout= <= 60s
	MinDragPoints    = 1
	MaxDragPoints    = 200
	MinDragSteps     = 1
	MaxDragSteps     = 200
	MaxCapN          = 120 // cap n= per line
	MaxCapturesTotal = 121 // captures per run
	MinScale         = 0.1
	MaxScale         = 4.0
	MaxLabelLen      = 48 // [A-Za-z0-9][A-Za-z0-9_-]{0,47}
)
