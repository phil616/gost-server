package localfile

// Windows does not support fsync on an ordinary directory handle. The file
// contents are flushed before replacement; Unix additionally flushes its parent.
func syncDir(string) error { return nil }
