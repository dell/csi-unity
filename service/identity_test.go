package service

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/dell/csi-unity/core"
	"github.com/stretchr/testify/assert"
)

type probeService interface {
	controllerProbe(ctx context.Context, arrayID string) error
	nodeProbe(ctx context.Context, arrayID string) error
}

type mockProbeService struct {
	mockControllerProbe func(ctx context.Context, arrayID string) error
	mockNodeProbe       func(ctx context.Context, arrayID string) error
}

func (m *mockProbeService) controllerProbe(ctx context.Context, arrayID string) error {
	if m.mockControllerProbe != nil {
		return m.mockControllerProbe(ctx, arrayID)
	}
	return nil
}

func (m *mockProbeService) nodeProbe(ctx context.Context, arrayID string) error {
	if m.mockNodeProbe != nil {
		return m.mockNodeProbe(ctx, arrayID)
	}
	return nil
}

func TestService_Probe(t *testing.T) {
	tests := []struct {
		name      string
		mode      string
		probeMock *mockProbeService
		expectErr bool
	}{
		{
			name: "Controller Mode - Success",
			mode: "controller",
			probeMock: &mockProbeService{
				mockControllerProbe: func(_ context.Context, _ string) error { return nil },
			},
			expectErr: false,
		},
		{
			name: "Controller Mode - Failure",
			mode: "controller",
			probeMock: &mockProbeService{
				mockControllerProbe: func(_ context.Context, _ string) error { return errors.New("controller probe failed") },
			},
			expectErr: true,
		},
		{
			name: "Node Mode - Success",
			mode: "node",
			probeMock: &mockProbeService{
				mockNodeProbe: func(_ context.Context, _ string) error { return nil },
			},
			expectErr: false,
		},
		{
			name: "Node Mode - Failure",
			mode: "node",
			probeMock: &mockProbeService{
				mockNodeProbe: func(_ context.Context, _ string) error { return errors.New("node probe failed") },
			},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			if tt.mode == "controller" {
				err = tt.probeMock.controllerProbe(context.TODO(), "")
			} else {
				err = tt.probeMock.nodeProbe(context.TODO(), "")
			}
			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestService_GetPluginInfo(t *testing.T) {
	s := &service{}

	resp, err := s.GetPluginInfo(context.TODO(), &csi.GetPluginInfoRequest{})
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, Name, resp.Name)
	assert.Equal(t, core.SemVer, resp.VendorVersion)
	assert.Equal(t, Manifest, resp.Manifest)
}

func TestService_GetPluginCapabilities(t *testing.T) {
	s := &service{}
	resp, err := s.GetPluginCapabilities(context.TODO(), &csi.GetPluginCapabilitiesRequest{})

	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Len(t, resp.Capabilities, 4)

	expectedCapabilities := []*csi.PluginCapability{
		{
			Type: &csi.PluginCapability_Service_{
				Service: &csi.PluginCapability_Service{
					Type: csi.PluginCapability_Service_CONTROLLER_SERVICE,
				},
			},
		},
		{
			Type: &csi.PluginCapability_VolumeExpansion_{
				VolumeExpansion: &csi.PluginCapability_VolumeExpansion{
					Type: csi.PluginCapability_VolumeExpansion_ONLINE,
				},
			},
		},
		{
			Type: &csi.PluginCapability_VolumeExpansion_{
				VolumeExpansion: &csi.PluginCapability_VolumeExpansion{
					Type: csi.PluginCapability_VolumeExpansion_OFFLINE,
				},
			},
		},
		{
			Type: &csi.PluginCapability_Service_{
				Service: &csi.PluginCapability_Service{
					Type: csi.PluginCapability_Service_VOLUME_ACCESSIBILITY_CONSTRAINTS,
				},
			},
		},
	}

	for i, expected := range expectedCapabilities {
		assert.Equal(t, expected.Type, resp.Capabilities[i].Type)
	}
}

func TestRunProbe(t *testing.T) {
	svc := &service{
		arrays: &sync.Map{},
		mode:   "controller",
	}
	ctx := context.Background()
	req := &csi.ProbeRequest{}
	_, err := svc.Probe(ctx, req)
	assert.Error(t, err)

	svc.mode = "node"
	_, err = svc.Probe(ctx, req)
	assert.Error(t, err)

	svc.mode = ""
	_, err = svc.Probe(ctx, req)
	assert.NoError(t, err)
}
