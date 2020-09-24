module github.com/dell/csi-unity

go 1.13

// TODO: Remove these replace lines before releasing
replace github.com/dell/gounity => ../gounity

require (
	github.com/DATA-DOG/godog v0.7.13
	github.com/container-storage-interface/spec v1.1.0
	github.com/dell/gobrick v1.0.0
	github.com/dell/gofsutil v1.3.0
	github.com/dell/goiscsi v1.1.0
	github.com/dell/gounity v1.3.0
	github.com/fsnotify/fsnotify v1.4.9
	github.com/golang/protobuf v1.4.2
	github.com/rexray/gocsi v1.1.0
	github.com/sirupsen/logrus v1.6.0
	github.com/stretchr/testify v1.4.0
	golang.org/x/net v0.0.0-20200625001655-4c5254603344
	google.golang.org/grpc v1.26.0
)
