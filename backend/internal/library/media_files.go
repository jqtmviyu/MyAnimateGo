package library

import (
	"os"
	"path/filepath"
	"strings"
)

var supportedVideos = map[string]bool{".mkv": true, ".mp4": true, ".avi": true, ".wmv": true}
var supportedSubs = map[string]bool{".ass": true, ".ssa": true, ".sub": true, ".srt": true, ".lyc": true, ".sup": true, ".pgs": true, ".mks": true}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
func videoFile(name string) bool    { return supportedVideos[strings.ToLower(filepath.Ext(name))] }
func subtitleFile(name string) bool { return supportedSubs[strings.ToLower(filepath.Ext(name))] }
