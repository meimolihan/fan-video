package service

// The static browser baseline must contain only codecs that are safe to assume
// without a client probe. FLAC and Opus support varies by browser/container and
// is now reported explicitly by the Web capability probe. Removing them from
// the unconditional baseline lets a positive client probe opt in, while a
// negative or missing probe stays conservative and can use Smart Remux.
//
// MP3-in-MP4 is removed for the same reason: several browsers (notably Chromium)
// fail to decode a copied MP3 audio track inside MPEG-4, producing silent video.
// Keeping it out of the baseline forces audio → AAC transcoding so sound always
// works, while a browser that genuinely supports it can opt in via probe.
func init() {
	delete(browserCompatibleAudioCodecs, "flac")
	delete(browserCompatibleAudioCodecs, "opus")
	delete(browserCompatibleAudioCodecs, "mp3")
}
