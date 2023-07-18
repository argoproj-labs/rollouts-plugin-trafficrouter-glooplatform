package gloo

import (
	"context"

	networkv2 "github.com/solo-io/solo-apis/client-go/networking.gloo.solo.io/v2"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func (c *routeTableClient) GetRouteTable(ctx context.Context, name string, namespace string) (*networkv2.RouteTable, error) {
	rt := &networkv2.RouteTable{}
	if err := c.client.Get(ctx, k8sclient.ObjectKey{Name: name, Namespace: namespace}, rt); err != nil {
		return nil, err
	}
	return rt, nil
}

func (c *routeTableClient) ListRouteTable(ctx context.Context, opts ...k8sclient.ListOption) ([]*networkv2.RouteTable, error) {
	rtl := &networkv2.RouteTableList{}
	if err := c.client.List(ctx, rtl, opts...); err != nil {
		return nil, err
	}
	var result []*networkv2.RouteTable
	for i := 0; i < len(rtl.Items); i++ {
		result = append(result, &rtl.Items[i])
	}
	return result, nil
}

func (c *routeTableClient) PatchRouteTable(ctx context.Context, obj *networkv2.RouteTable, patch k8sclient.Patch, opts ...k8sclient.PatchOption) error {
	return c.client.Patch(ctx, obj, patch, opts...)
}
