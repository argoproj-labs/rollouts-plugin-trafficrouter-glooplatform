package plugin

import (
	"context"
	"errors"
	"fmt"

	"github.com/argoproj-labs/rollouts-plugin-trafficrouter-glooplatform/pkg/gloo"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	pluginTypes "github.com/argoproj/argo-rollouts/utils/plugin/types"
	solov2 "github.com/solo-io/solo-apis/client-go/common.gloo.solo.io/v2"
	networkv2 "github.com/solo-io/solo-apis/client-go/networking.gloo.solo.io/v2"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (r *RpcPlugin) handleCanary(ctx context.Context, rollout *v1alpha1.Rollout, desiredWeight int32, additionalDestinations []v1alpha1.WeightDestination, glooPluginConfig *GlooPlatformAPITrafficRouting, glooMatchedRouteTables []*GlooMatchedRouteTable) pluginTypes.RpcError {
	remainingWeight := 100 - desiredWeight

	for _, rt := range glooMatchedRouteTables {
		// the original rt is preserved to use for patch generation
		ogRt := &networkv2.RouteTable{}
		rt.RouteTable.DeepCopyInto(ogRt)

		// set stable and canary (create canary destination if required)
		for _, matchedHttpRoute := range rt.HttpRoutes {
			if matchedHttpRoute.Destinations != nil {
				matchedHttpRoute.Destinations.StableOrActiveDestination.Weight = uint32(remainingWeight)

				if matchedHttpRoute.Destinations.CanaryOrPreviewDestination == nil {
					newDest, err := r.newCanaryDest(matchedHttpRoute.Destinations.StableOrActiveDestination, rollout)
					if err != nil {
						return pluginTypes.RpcError{
							ErrorString: err.Error(),
						}
					}
					matchedHttpRoute.Destinations.CanaryOrPreviewDestination = newDest
					matchedHttpRoute.HttpRoute.GetForwardTo().Destinations = append(matchedHttpRoute.HttpRoute.GetForwardTo().Destinations, matchedHttpRoute.Destinations.CanaryOrPreviewDestination)
				}

				matchedHttpRoute.Destinations.CanaryOrPreviewDestination.Weight = uint32(desiredWeight)
			}
		}

		// don't actually patch the RT
		if r.IsTest {
			continue
		}
		if err := r.Client.RouteTables().PatchRouteTable(ctx, rt.RouteTable, client.MergeFrom(ogRt)); err != nil {
			return pluginTypes.RpcError{
				ErrorString: fmt.Sprintf("failed to patch RouteTable: %s", err),
			}
		}
		r.LogCtx.Debugf("patched route table %s.%s", rt.RouteTable.Namespace, rt.RouteTable.Name)

	}

	return pluginTypes.RpcError{}
}

func (r *RpcPlugin) newCanaryDest(stableDest *solov2.DestinationReference, rollout *v1alpha1.Rollout) (*solov2.DestinationReference, error) {
	newDest := stableDest.Clone().(*solov2.DestinationReference)
	newDest.GetRef().Name = rollout.Spec.Strategy.Canary.CanaryService
	return newDest, nil
}

func typedCloneProto[T protoreflect.ProtoMessage](p T) T {
	return proto.Clone(p).(T)
}

func (r *RpcPlugin) getOrDeriveCanary(mrt *GlooMatchedHttpRoutes, canaryService string) *networkv2.HTTPRoute_ForwardTo {
	if mrt.Destinations.CanaryOrPreviewDestination != nil {
		canary := typedCloneProto(mrt.Destinations.CanaryOrPreviewDestination)
		canary.Weight = 0
		return &networkv2.HTTPRoute_ForwardTo{
			ForwardTo: &networkv2.ForwardToAction{
				Destinations: []*solov2.DestinationReference{canary},
			},
		}
	}
	if mrt.Destinations.StableOrActiveDestination != nil {
		newCanary := typedCloneProto(mrt.Destinations.StableOrActiveDestination)
		newCanary.GetRef().Name = canaryService
		newCanary.Weight = 0
		return &networkv2.HTTPRoute_ForwardTo{
			ForwardTo: &networkv2.ForwardToAction{
				Destinations: []*solov2.DestinationReference{newCanary},
			},
		}
	}
	return nil // we don't have a canary and can't derive one
}

func (r *RpcPlugin) handleHeaderRoute(ctx context.Context, routeTables []*GlooMatchedRouteTable, matcher *solov2.HTTPRequestMatcher, setHeaderRouteName string, canaryServiceName string) pluginTypes.RpcError {
	var combinedError error
	for _, rt := range routeTables {
		originalRouteTable := &networkv2.RouteTable{}
		rt.RouteTable.DeepCopyInto(originalRouteTable)

		newHeaderRoutes := make([]*networkv2.HTTPRoute, 0)

		for _, route := range rt.HttpRoutes {
			canaryDestination := r.getOrDeriveCanary(route, canaryServiceName)
			setHeaderRoute := typedCloneProto(route.HttpRoute)
			matcher := typedCloneProto(matcher)
			setHeaderRoute.ActionType = canaryDestination
			matchers := []*solov2.HTTPRequestMatcher{matcher}
			setHeaderRoute.Matchers = append(matchers, setHeaderRoute.Matchers...)
			setHeaderRoute.Name = setHeaderRouteName

			newHeaderRoutes = append(newHeaderRoutes, setHeaderRoute)

		}

		newHeaderRoutes = append(newHeaderRoutes, rt.RouteTable.Spec.Http...)
		rt.RouteTable.Spec.Http = newHeaderRoutes

		if r.IsTest {
			r.LogCtx.Debugf("test route table http routes: %v", rt.RouteTable.Spec.Http)
			continue
		}
		if e := patchRouteTable(ctx, r.Client, rt.RouteTable, originalRouteTable); e != nil {
			combinedError = errors.Join(combinedError, e)
			continue
		}
		r.LogCtx.Debugf("patched route table %s.%s", rt.RouteTable.Namespace, rt.RouteTable.Name)

	}

	if combinedError != nil {
		return pluginTypes.RpcError{
			ErrorString: combinedError.Error(),
		}
	}

	return pluginTypes.RpcError{}
}

func patchRouteTable(ctx context.Context, glooClient gloo.NetworkV2ClientSet, new, old *networkv2.RouteTable) error {
	if err := glooClient.RouteTables().PatchRouteTable(ctx, new, client.MergeFrom(old)); err != nil {
		return fmt.Errorf("failed to patch RouteTable: %s", err)
	}
	return nil
}
