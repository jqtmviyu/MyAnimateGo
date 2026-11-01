//go:build (darwin && cgo && !ios) || windows

package desktop

func available() bool { return true }
