package certification

import transcodeboundary "github.com/fan-video/fan-video/internal/transcode/boundaryevidence"

func ticksToMicrosCertification(ticks int64, timeBase string) (int64, error) {
	return transcodeboundary.TicksToMicros(ticks, timeBase)
}
