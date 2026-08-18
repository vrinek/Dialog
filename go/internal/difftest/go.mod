module github.com/vrinek/Dialog/go/internal/difftest

go 1.26.0

toolchain go1.26.6

require (
	github.com/fxamacker/cbor/v2 v2.9.3
	github.com/vrinek/Dialog/go v0.0.0
)

require github.com/x448/float16 v0.8.4 // indirect

replace github.com/vrinek/Dialog/go => ../..
