module github.com/dell/csi-unity

go 1.13

// TODO: Remove these replace lines before releasing
replace github.com/dell/gounity => ../gounity

require (
	github.com/DATA-DOG/godog v0.7.13
	github.com/container-storage-interface/spec v1.1.0
	github.com/dell/gobrick v1.0.0
	github.com/dell/gofsutil v1.2.0
	github.com/dell/goiscsi v1.1.0
	github.com/dell/gounity v1.2.1
	github.com/fsnotify/fsnotify v1.4.9
	github.com/golang/protobuf v1.3.2
	github.com/rexray/gocsi v1.1.0
	github.com/sirupsen/logrus v1.4.2
	github.com/stretchr/testify v1.4.0
	golang.org/x/crypto v0.0.0-20190605123033-f99c8df09eb5 // indirect
	golang.org/x/net v0.0.0-20190620200207-3b0461eec859
	golang.org/x/text v0.3.2 // indirect
	google.golang.org/genproto v0.0.0-20190819201941-24fa4b261c55 // indirect
	google.golang.org/grpc v1.21.1
)
