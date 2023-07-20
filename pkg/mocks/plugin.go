package mocks

import (
	"context"
	"fmt"

	"github.com/argoproj-labs/rollouts-plugin-trafficrouter-glooplatform/pkg/gloo"
	gloov2 "github.com/solo-io/solo-apis/client-go/networking.gloo.solo.io/v2"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func NewGlooMockClient(routeTables []*gloov2.RouteTable) gloo.NetworkV2ClientSet {
	return &GlooMockClient{
		rtClient: &glooMockRouteTableClient{
			routeTables: routeTables,
		},
	}
}

type GlooMockClient struct {
	rtClient *glooMockRouteTableClient
}

func (c GlooMockClient) RouteTables() gloo.RouteTableClient {
	return c.rtClient
}

type glooMockRouteTableClient struct {
	routeTables []*gloov2.RouteTable
}

func (c glooMockRouteTableClient) GetRouteTable(ctx context.Context, name string, namespace string) (*gloov2.RouteTable, error) {
	if len(c.routeTables) > 0 {
		return c.routeTables[0], nil
	}
	return nil, fmt.Errorf("routeTable not found: %s:%s", namespace, name)
}

func (c glooMockRouteTableClient) PatchRouteTable(ctx context.Context, obj *gloov2.RouteTable, patch k8sclient.Patch, opts ...k8sclient.PatchOption) error {
	return nil
}

func (c glooMockRouteTableClient) ListRouteTable(ctx context.Context, opts ...k8sclient.ListOption) ([]*gloov2.RouteTable, error) {
	return c.routeTables, nil
}
