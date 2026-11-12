package adapters

// PTZ adapter keys are stable references used by the capability release. The
// gateway binds each key to its active PTZ dispatcher at process startup; the
// catalog can select a key but cannot supply executable behavior.
const (
	PTZMoveAdapterKey              = "delegated.ptz.move.v1"
	PTZPreciseAdapterKey           = "delegated.ptz.precise.v1"
	PTZExtendedAdapterKey          = "delegated.ptz.extended.v1"
	PTZWiperAdapterKey             = "delegated.ptz.wiper.v1"
	PTZHomePositionSetAdapterKey   = "delegated.ptz.home-position-set.v1"
	PTZCruiseControlAdapterKey     = "delegated.ptz.cruise-control.v1"
	PTZCruiseTrackCreateAdapterKey = "delegated.ptz.cruise-track-create.v1"
	PTZCruiseTrackReadAdapterKey   = "delegated.ptz.cruise-track-read.v1"
	PTZTargetTrackAdapterKey       = "delegated.ptz.target-track.v1"
)
