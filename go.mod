module github.com/unxed/keytrans

go 1.25.5

require (
	github.com/ebitengine/purego v0.8.0
	github.com/jezek/xgb v1.3.1
	github.com/unxed/winkeys v0.1.0
	github.com/unxed/xkb-go v0.1.8
)

require github.com/go-webgpu/goffi v0.5.2 // indirect

replace github.com/ebitengine/purego => github.com/unxed/pureffi v0.1.1
