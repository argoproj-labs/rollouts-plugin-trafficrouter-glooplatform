package gloo

import (
	"context"

	networkv2 "github.com/solo-io/solo-apis/client-go/networking.gloo.solo.io/v2"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func (c *routeTableClient) GetRouteTable(ctx context.Context, key k8sclient.ObjectKey) (*networkv2.RouteTable, error) {
	rt := &networkv2.RouteTable{}
	if err := c.client.Get(ctx, key, rt); err != nil {
		return nil, err
	}
	return rt, nil
}

func (c *routeTableClient) ListRouteTable(ctx context.Context, opts ...k8sclient.ListOption) ([]*networkv2.RouteTable, error) {
	panic("not impl")
}

func (c *routeTableClient) PatchRouteTable(ctx context.Context, obj *networkv2.RouteTable, patch k8sclient.Patch, opts ...k8sclient.PatchOption) error {
	panic("not impl")
}
