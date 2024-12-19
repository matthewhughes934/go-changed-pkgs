package main

import (
	_ "golang.org/x/mod/modfile"
	_ "golang.org/x/net/http2/hpack"

	_ "example.com/test-repo/internal/consumer"
)

func main() {}
