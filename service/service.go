package service

import (
	"context"
	"fmt"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/dell/csi-unity/core"
	"github.com/dell/csi-unity/service/utils"
	"github.com/dell/goiscsi"
	"github.com/dell/gounity"
	"github.com/rexray/gocsi"
	csictx "github.com/rexray/gocsi/context"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"net"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

const (
	// Name is the name of the Unity CSI.

	// VendorVersion is the version of this Unity CSI.
	VendorVersion = "0.0.0"

	//Tcp dial default timeout in Milliseconds
	TcpDialTimeout = 1000

	IScsiPort = "3260"
)

var Name string

//To maintain runid for Non debug mode. Note: CSI will not generate runid if CSI_DEBUG=false
var runid int64

// Manifest is the SP's manifest.
var Manifest = map[string]string{
	"url":    "http://github.com/dell/csi-unity",
	"semver": core.SemVer,
	"commit": core.CommitSha32,
	"formed": core.CommitTime.Format(time.RFC1123),
}

// Service is a CSI SP and idempotency.Provider.
type Service interface {
	csi.ControllerServer
	csi.IdentityServer
	csi.NodeServer
	BeforeServe(context.Context, *gocsi.StoragePlugin, net.Listener) error
}

// Opts defines service configuration options.
type Opts struct {
	Endpoint     string
	User         string
	Password     string
	NodeName     string
	LongNodeName string
	PvtMountDir  string
	Chroot       string
	Insecure     bool
	Thick        bool
	AutoProbe    bool
	UseCerts     bool // used for unit testing only
	Debug        bool
}

type service struct {
	opts        Opts
	mode        string
	unity       *gounity.Client
	iscsiClient goiscsi.ISCSIinterface
}

// New returns a new CSI Service.
func New() Service {
	return &service{}
}

// BeforeServe allows the SP to participate in the startup
// sequence. This function is invoked directly before the
// gRPC server is created, giving the callback the ability to
// modify the SP's interceptors, server options, or prevent the
// server from starting by returning a non-nil error.
func (s *service) BeforeServe(
	ctx context.Context, sp *gocsi.StoragePlugin, lis net.Listener) error {
	log := utils.GetLogger()
	defer func() {
		fields := map[string]interface{}{
			"endpoint":    s.opts.Endpoint,
			"user":        s.opts.User,
			"password":    "",
			"nodename":    s.opts.NodeName,
			"insecure":    s.opts.Insecure,
			"autoprobe":   s.opts.AutoProbe,
			"mode":        s.mode,
			"pvtMountDir": s.opts.PvtMountDir,
		}

		if s.opts.Password != "" {
			fields["password"] = "******"
		}

		log.WithFields(fields).Infof("configured %s", Name)
	}()

	// Get the SP's operating mode.
	s.mode = csictx.Getenv(ctx, gocsi.EnvVarMode)
	log.Info("X_CSI_MODE:", s.mode)

	opts := Opts{}

	if ep, ok := csictx.LookupEnv(ctx, EnvEndpoint); ok {
		opts.Endpoint = ep
	}
	if user, ok := csictx.LookupEnv(ctx, EnvUser); ok {
		opts.User = user
	}
	if opts.User == "" {
		opts.User = "admin"
	}
	if pw, ok := csictx.LookupEnv(ctx, EnvPassword); ok {
		opts.Password = pw
	}
	if name, ok := csictx.LookupEnv(ctx, gocsi.EnvVarDebug); ok {
		opts.Debug, _ = strconv.ParseBool(name)
	}
	if name, ok := csictx.LookupEnv(ctx, EnvNodeName); ok {
		log.Info("X_CSI_UNITY_NODENAME:", name)
		opts.LongNodeName = name
		shortHostName := strings.Split(name, ".")[0]
		opts.NodeName = shortHostName
	}

	if pvtmountDir, ok := csictx.LookupEnv(ctx, EnvPvtMountDir); ok {
		opts.PvtMountDir = pvtmountDir
	}

	// pb parses an environment variable into a boolean value. If an error
	// is encountered, default is set to false, and error is logged
	pb := func(n string) bool {
		if v, ok := csictx.LookupEnv(ctx, n); ok {
			b, err := strconv.ParseBool(v)
			if err != nil {
				log.WithField(n, v).Debug(
					"invalid boolean value. defaulting to false")
				return false
			}
			return b
		}
		return false
	}

	opts.Insecure = pb(EnvInsecure)
	opts.AutoProbe = pb(EnvAutoProbe)
	if opts.Insecure {
		opts.UseCerts = false
	} else {
		opts.UseCerts = true
	}

	// setup the iscsi client
	iscsiOpts := make(map[string]string, 0)
	if chroot, ok := csictx.LookupEnv(ctx, EnvISCSIChroot); ok {
		iscsiOpts[goiscsi.ChrootDirectory] = chroot
		opts.Chroot = chroot
	}
	s.iscsiClient = goiscsi.NewLinuxISCSI(iscsiOpts)

	s.opts = opts

	return nil
}

func GetRunidLog(ctx context.Context) (context.Context, *logrus.Entry, string) {
	var rid string
	headers, ok := metadata.FromIncomingContext(ctx)
	if ok {
		reqid, ok := headers["csi.requestid"]
		if ok && len(reqid) > 0 {
			rid = reqid[0]
		} else {
			atomic.AddInt64(&runid, 1)
			rid = fmt.Sprintf("%d", runid)
		}
	}
	l := utils.GetLogger()
	log := l.WithField(utils.RUNID, rid)
	ctx = context.WithValue(ctx, utils.RUNIDLOG, log)
	return ctx, log, rid
}

func (s *service) requireProbe(ctx context.Context) error {
	rid, log := utils.GetRunidAndLogger(ctx)
	if s.unity == nil {
		if !s.opts.AutoProbe {
			return status.Error(codes.FailedPrecondition, utils.GetMessageWithRunID(rid, "Controller Service has not been probed"))
		}
		log.Debug("probing controller service automatically")
		if err := s.controllerProbe(ctx); err != nil {
			return status.Error(codes.FailedPrecondition, utils.GetMessageWithRunID(rid, "failed to probe/init plugin: %s", err.Error()))
		}
	}
	return nil
}
