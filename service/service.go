package service

import (
	"context"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/dell/csi-unity/core"
	"github.com/dell/csi-unity/service/utils"
	"github.com/dell/gounity"
	"github.com/rexray/gocsi"
	csictx "github.com/rexray/gocsi/context"
	"github.com/sirupsen/logrus"
	"net"
	"strconv"
	"strings"
	"time"
)

const (
	// Name is the name of the Unity CSI.
	Name = "csi-unity.dellemc.com"

	// VendorVersion is the version of this Unity CSI.
	VendorVersion = "0.0.0"
)

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
	Endpoint    string
	User        string
	Password    string
	SystemName  string
	NodeName    string
	PvtMountDir string
	Insecure    bool
	Thick       bool
	AutoProbe   bool
	UseCerts    bool // used for unit testing only
	Debug       bool
}

type service struct {
	opts  Opts
	mode  string
	unity *gounity.Client
}

// New returns a new CSI Service.
func New() Service {
	return &service{}
}

var log *logrus.Logger

func SetLogger(l *logrus.Logger) {
	log = l
}

// BeforeServe allows the SP to participate in the startup
// sequence. This function is invoked directly before the
// gRPC server is created, giving the callback the ability to
// modify the SP's interceptors, server options, or prevent the
// server from starting by returning a non-nil error.
func (s *service) BeforeServe(
	ctx context.Context, sp *gocsi.StoragePlugin, lis net.Listener) error {
	SetLogger(utils.GetLogger())
	defer func() {
		fields := map[string]interface{}{
			"endpoint":    s.opts.Endpoint,
			"user":        s.opts.User,
			"password":    "",
			"systemname":  s.opts.SystemName,
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
	if name, ok := csictx.LookupEnv(ctx, EnvSystemName); ok {
		opts.SystemName = name
	}
	if name, ok := csictx.LookupEnv(ctx, gocsi.EnvVarDebug); ok {
		opts.Debug, _ = strconv.ParseBool(name)
	}
	if name, ok := csictx.LookupEnv(ctx, EnvNodeName); ok {
		log.Info("X_CSI_UNITY_NODENAME:", name)
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
	s.opts = opts

	return nil
}
