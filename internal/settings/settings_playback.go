package settings

// Quiet mode while a media server is playing. The switches and the server rows
// belong to internal/playback; this file hangs them on the document, refuses a
// bad list and cleans a hand-edited one. The tokens are sealed in
// accounts.Store, see that package's comment.

import "github.com/junkerderprovinz/knightloader/internal/playback"

func sanitizePlayback(n Settings) Settings {
	n.Playback = playback.Sanitize(n.Playback)
	n.PlaybackServers = playback.SanitizeServers(n.PlaybackServers)
	return n
}

// ValidatePlaybackServers refuses a list playback.SanitizeServers would cut,
// naming the row, so a save never drops a server without a word.
func (s Settings) ValidatePlaybackServers() error {
	if err := playback.ValidateServers(s.PlaybackServers); err != nil {
		return &FieldError{Field: "playbackServers", Err: err}
	}
	return nil
}
