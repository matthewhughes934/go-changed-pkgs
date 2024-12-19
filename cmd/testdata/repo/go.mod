module example.com/test-repo

go 1.21.0

require (
	golang.org/x/mod v0.13.0
	golang.org/x/net v0.2.0
	golang.org/x/sys v0.14.0
	golang.org/x/time v0.4.0
)

replace golang.org/x/net => github.com/golang/net v0.1.0
