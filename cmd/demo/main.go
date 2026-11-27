// Command demo makes a made-up music folder and pretends to be a DLNA speaker and a
// Chromecast on this computer, to try TrackFerry without real devices.
//
//	go run ./cmd/demo music <folder>   write songs, covers and lyrics
//	go run ./cmd/demo cast             run the two pretend devices; a panel on :18590
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "用法：demo music <文件夹> | demo cast")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "music":
		if len(os.Args) < 3 {
			err = fmt.Errorf("缺文件夹")
		} else {
			err = writeMusic(os.Args[2])
		}
	case "cast":
		err = runCast()
	default:
		err = fmt.Errorf("不认识 %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "demo:", err)
		os.Exit(1)
	}
}
