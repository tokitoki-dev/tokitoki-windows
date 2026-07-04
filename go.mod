module github.com/labx/tracklm-windows

go 1.25.0

require (
	github.com/fsnotify/fsnotify v1.10.1
	github.com/labx/tokitoki-agent v0.0.0
	github.com/lxn/walk v0.0.0-20210112085537-c389da54e794
	github.com/lxn/win v0.0.0-20210218163916-a377121e959e
	golang.org/x/sys v0.42.0
)

require (
	go.etcd.io/bbolt v1.4.3 // indirect
	gopkg.in/Knetic/govaluate.v3 v3.0.0 // indirect
)

replace github.com/labx/tokitoki-agent => ../tracklm-goagent
