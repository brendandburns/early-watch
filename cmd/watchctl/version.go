package main

// Version is the current version of watchctl. It is set at build time via:
//
//	go build -ldflags "-X github.com/brendandburns/early-watch/cmd/watchctl.Version=<git-tag>" ./cmd/watchctl/...
//
// When built without the flag the value defaults to "latest".
var Version = "latest"
