package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/argoproj-labs/rollouts-plugin-trafficrouter-glooplatform/pkg/gloo"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	pluginTypes "github.com/argoproj/argo-rollouts/utils/plugin/types"
	"github.com/sirupsen/logrus"
	solov2 "github.com/solo-io/solo-apis/client-go/common.gloo.solo.io/v2"
	networkv2 "github.com/solo-io/solo-apis/client-go/networking.gloo.solo.io/v2"
	"k8s.io/apimachinery/pkg/labels"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	Type                       = "GlooPlatformAPI"
	GlooPlatformAPIUpdateError = "GlooPlatformAPIUpdateError"
	PluginName                 = "solo-io/glooplatform"
)

type RpcPlugin struct {
	IsTest bool
	// temporary hack until mock clienset is fixed (missing some interface methods)
	// TestRouteTable *networkv2.RouteTable
	LogCtx *logrus.Entry
	Client gloo.NetworkV2ClientSet
}

type GlooPlatformAPITrafficRouting struct {
	RouteTableSelector *SimpleObjectSelector `json:"routeTableSelector" protobuf:"bytes,1,name=routeTableSelector"`
	RouteSelector      *SimpleRouteSelector  `json:"routeSelector" protobuf:"bytes,2,name=routeSelector"`
	// CanaryDestination  *SimpleDestinationReference `json:"canaryDestination" protobuf:"bytes,3,name=canaryDestination"`
}

// type SimpleObjectReference struct {
// 	Name      string `json:"name"`
// 	Namespace string `json:"namespace"`
// }

// type SimpleDestinationReference struct {
// 	Reference SimpleObjectReference `json:"ref"`
// 	Port      SimplePort            `json:"port"`
// }

// type SimplePort struct {
// 	Name   string `json:"name"`
// 	Number uint32 `json:"number"`
// }

type SimpleObjectSelector struct {
	Labels    map[string]string `json:"labels" protobuf:"bytes,1,name=labels"`
	Name      string            `json:"name" protobuf:"bytes,2,name=name"`
	Namespace string            `json:"namespace" protobuf:"bytes,3,name=namespace"`
}

type SimpleRouteSelector struct {
	Labels map[string]string `json:"labels" protobuf:"bytes,1,name=labels"`
	Name   string            `json:"name" protobuf:"bytes,2,name=name"`
}

type GlooDestinationMatcher struct {
	// Regexp *GlooDestinationMatcherRegexp `json:"regexp" protobuf:"bytes,1,name=regexp"`
	Ref *solov2.ObjectReference `json:"ref" protobuf:"bytes,2,name=ref"`
}

type GlooMatchedRouteTable struct {
	// matched gloo platform route table
	RouteTable *networkv2.RouteTable
	// matched http routes within the routetable
	HttpRoutes []*GlooMatchedHttpRoutes
	// matched tcp routes within the routetable
	TCPRoutes []*GlooMatchedTCPRoutes
	// matched tls routes within the routetable
	TLSRoutes []*GlooMatchedTLSRoutes
}

type GlooDestinations struct {
	StableOrActiveDestination  *solov2.DestinationReference
	CanaryOrPreviewDestination *solov2.DestinationReference
}

type GlooMatchedHttpRoutes struct {
	// matched HttpRoute
	HttpRoute *networkv2.HTTPRoute
	// matched destinations within the httpRoute
	Destinations *GlooDestinations
}

type GlooMatchedTLSRoutes struct {
	// matched HttpRoute
	TLSRoute *networkv2.TLSRoute
	// matched destinations within the httpRoute
	Destinations []*GlooDestinations
}

type GlooMatchedTCPRoutes struct {
	// matched HttpRoute
	TCPRoute *networkv2.TCPRoute
	// matched destinations within the httpRoute
	Destinations []*GlooDestinations
}

func (r *RpcPlugin) InitPlugin() pluginTypes.RpcError {
	if r.IsTest {
		return pluginTypes.RpcError{}
	}
	client, err := gloo.NewNetworkV2ClientSet()
	if err != nil {
		return pluginTypes.RpcError{
			ErrorString: err.Error(),
		}
	}
	r.Client = client
	return pluginTypes.RpcError{}
}

func (r *RpcPlugin) UpdateHash(rollout *v1alpha1.Rollout, canaryHash, stableHash string, additionalDestinations []v1alpha1.WeightDestination) pluginTypes.RpcError {
	return pluginTypes.RpcError{}
}

func (r *RpcPlugin) SetWeight(rollout *v1alpha1.Rollout, desiredWeight int32, additionalDestinations []v1alpha1.WeightDestination) pluginTypes.RpcError {
	ctx := context.TODO()
	glooPluginConfig, err := getPluginConfig(rollout)
	if err != nil {
		return pluginTypes.RpcError{
			ErrorString: err.Error(),
		}
	}

	// get the matched routetables
	matchedRts, err := r.getRouteTables(ctx, rollout, glooPluginConfig)
	if err != nil {
		return pluginTypes.RpcError{
			ErrorString: err.Error(),
		}
	}

	if rollout.Spec.Strategy.Canary != nil {
		return r.handleCanary(ctx, rollout, desiredWeight, additionalDestinations, glooPluginConfig, matchedRts)
	} else if rollout.Spec.Strategy.BlueGreen != nil {
		return r.handleBlueGreen(rollout)
	}

	return pluginTypes.RpcError{}
}

func (r *RpcPlugin) SetHeaderRoute(rollout *v1alpha1.Rollout, headerRouting *v1alpha1.SetHeaderRoute) pluginTypes.RpcError {
	r.LogCtx.Debugln("SetHeaderRoute")
	ctx := context.TODO()

	glooPluginConfig, err := getPluginConfig(rollout)
	if err != nil {
		return pluginTypes.RpcError{
			ErrorString: err.Error(),
		}
	}
	// get the matched routetables
	matchedRts, err := r.getRouteTables(ctx, rollout, glooPluginConfig)
	if err != nil {
		return pluginTypes.RpcError{
			ErrorString: err.Error(),
		}
	}
	if len(matchedRts) == 0 {
		// nothing to update, don't bother computing things
		return pluginTypes.RpcError{
			ErrorString: "unable to find qualifying RouteTables", // TODO: include the selection criteria which failed (may require update to getRouteTables to do nicely)
		}
	}

	return r.handleHeaderRoute(ctx, matchedRts, buildGlooMatches(headerRouting), headerRouting.Name, rollout.Spec.Strategy.Canary.CanaryService)
}

func (r *RpcPlugin) SetMirrorRoute(rollout *v1alpha1.Rollout, setMirrorRoute *v1alpha1.SetMirrorRoute) pluginTypes.RpcError {
	return pluginTypes.RpcError{}
}

func (r *RpcPlugin) VerifyWeight(rollout *v1alpha1.Rollout, desiredWeight int32, additionalDestinations []v1alpha1.WeightDestination) (pluginTypes.RpcVerified, pluginTypes.RpcError) {
	return pluginTypes.Verified, pluginTypes.RpcError{}
}

func (r *RpcPlugin) RemoveManagedRoutes(rollout *v1alpha1.Rollout) pluginTypes.RpcError {
	if !slices.ContainsFunc(rollout.Spec.Strategy.Canary.Steps, func(s v1alpha1.CanaryStep) bool {
		return s.SetHeaderRoute != nil
	}) {
		// none of the steps have a SetHeaderRoute so nothing to clean up
		return pluginTypes.RpcError{}
	}

	ctx := context.TODO()
	glooPluginConfig, err := getPluginConfig(rollout)
	if err != nil {
		return pluginTypes.RpcError{
			ErrorString: err.Error(),
		}
	}

	// get the matched routetables
	matchedRts, err := r.getRouteTables(ctx, rollout, glooPluginConfig)
	if err != nil {
		return pluginTypes.RpcError{
			ErrorString: err.Error(),
		}
	}
	if len(matchedRts) == 0 {
		// nothing to update, don't bother computing things
		return pluginTypes.RpcError{}
	}

	var combinedError error
	for _, rt := range matchedRts {
		originalRouteTable := &networkv2.RouteTable{}
		rt.RouteTable.DeepCopyInto(originalRouteTable)
		newRoutes := slices.DeleteFunc(rt.RouteTable.Spec.Http, func(r *networkv2.HTTPRoute) bool {
			for _, managed := range rollout.Spec.Strategy.Canary.TrafficRouting.ManagedRoutes {
				if strings.EqualFold(r.GetName(), managed.Name) {
					return true
				}
			}
			return false
		})

		rt.RouteTable.Spec.Http = newRoutes
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

func (r *RpcPlugin) Type() string {
	return Type
}

func (r *RpcPlugin) getRouteTables(ctx context.Context, rollout *v1alpha1.Rollout, glooPluginConfig *GlooPlatformAPITrafficRouting) ([]*GlooMatchedRouteTable, error) {
	if glooPluginConfig.RouteTableSelector == nil {
		return nil, fmt.Errorf("routeTable selector is required")
	}

	if strings.EqualFold(glooPluginConfig.RouteTableSelector.Namespace, "") {
		r.LogCtx.Debugf("defaulting routeTableSelector namespace to Rollout namespace %s for rollout %s", rollout.Namespace, rollout.Name)
		glooPluginConfig.RouteTableSelector.Namespace = rollout.Namespace
	}

	var rts []*networkv2.RouteTable

	if !strings.EqualFold(glooPluginConfig.RouteTableSelector.Name, "") {
		r.LogCtx.Debugf("getRouteTables using ns:name ref %s:%s to get single table", glooPluginConfig.RouteTableSelector.Name, glooPluginConfig.RouteTableSelector.Namespace)
		result, err := r.Client.RouteTables().GetRouteTable(ctx, glooPluginConfig.RouteTableSelector.Name, glooPluginConfig.RouteTableSelector.Namespace)
		if err != nil {
			return nil, err
		}

		r.LogCtx.Debugf("getRouteTables using ns:name ref %s:%s found 1 table", glooPluginConfig.RouteTableSelector.Name, glooPluginConfig.RouteTableSelector.Namespace)
		rts = append(rts, result)
	} else {
		opts := &k8sclient.ListOptions{}

		if glooPluginConfig.RouteTableSelector.Labels != nil {
			opts.LabelSelector = labels.SelectorFromSet(glooPluginConfig.RouteTableSelector.Labels)
		}
		if !strings.EqualFold(glooPluginConfig.RouteTableSelector.Namespace, "") {
			opts.Namespace = glooPluginConfig.RouteTableSelector.Namespace
		}

		r.LogCtx.Debugf("getRouteTables listing tables with opts %+v", opts)
		var err error

		rts, err = r.Client.RouteTables().ListRouteTable(ctx, opts)
		if err != nil {
			return nil, err
		}
		r.LogCtx.Debugf("getRouteTables listing tables with opts %+v; found %d routeTables", opts, len(rts))
	}

	matched := []*GlooMatchedRouteTable{}

	for _, rt := range rts {
		matchedRt := &GlooMatchedRouteTable{
			RouteTable: rt,
		}
		// destination matching
		if err := matchedRt.matchRoutes(r.LogCtx, rollout, glooPluginConfig); err != nil {
			return nil, err // TODO: don't short circuit, potentially other RTs will match if we continue instead of immediately returning an error
		}

		matched = append(matched, matchedRt)
	}

	return matched, nil
}

func (g *GlooMatchedRouteTable) matchRoutes(logCtx *logrus.Entry, rollout *v1alpha1.Rollout, trafficConfig *GlooPlatformAPITrafficRouting) error {
	if g.RouteTable == nil {
		return fmt.Errorf("matchRoutes called for nil RouteTable")
	}

	// HTTP Routes
	for _, httpRoute := range g.RouteTable.Spec.Http {
		// find the destination that matches the stable svc
		fw := httpRoute.GetForwardTo()
		if fw == nil {
			logCtx.Debugf("skipping route %s.%s because forwardTo is nil", g.RouteTable.Name, httpRoute.Name)
			continue
		}

		// skip non-matching routes if RouteSelector provided
		if trafficConfig.RouteSelector != nil {
			// if name was provided, skip if route name doesn't match
			if !strings.EqualFold(trafficConfig.RouteSelector.Name, "") && !strings.EqualFold(trafficConfig.RouteSelector.Name, httpRoute.Name) {
				logCtx.Debugf("skipping route %s.%s because it doesn't match route name selector %s", g.RouteTable.Name, httpRoute.Name, trafficConfig.RouteSelector.Name)
				continue
			}
			// if labels provided, skip if route labels do not contain all specified labels
			if trafficConfig.RouteSelector.Labels != nil {
				matchedLabels := func() bool {
					for k, v := range trafficConfig.RouteSelector.Labels {
						if vv, ok := httpRoute.Labels[k]; ok {
							if !strings.EqualFold(v, vv) {
								logCtx.Debugf("skipping route %s.%s because route labels do not contain %s=%s", g.RouteTable.Name, httpRoute.Name, k, v)
								return false
							}
						}
					}
					return true
				}()
				if !matchedLabels {
					continue
				}
			}
			logCtx.Debugf("route %s.%s passed RouteSelector", g.RouteTable.Name, httpRoute.Name)
		}

		// find destinations
		// var matchedDestinations []*GlooDestinations
		var canary, stable *solov2.DestinationReference
		for _, dest := range fw.Destinations {
			ref := dest.GetRef()
			if ref == nil {
				logCtx.Debugf("skipping destination %s.%s because destination ref was nil; %+v", g.RouteTable.Name, httpRoute.Name, dest)
				continue
			}
			if strings.EqualFold(ref.Name, rollout.Spec.Strategy.Canary.StableService) {
				logCtx.Debugf("matched stable ref %s.%s.%s", g.RouteTable.Name, httpRoute.Name, ref.Name)
				stable = dest
				continue
			}
			if strings.EqualFold(ref.Name, rollout.Spec.Strategy.Canary.CanaryService) {
				logCtx.Debugf("matched canary ref %s.%s.%s", g.RouteTable.Name, httpRoute.Name, ref.Name)
				canary = dest
				// bail if we found both stable and canary
				if stable != nil {
					break
				}
				continue
			}
		}

		if stable != nil {
			dest := &GlooMatchedHttpRoutes{
				HttpRoute: httpRoute,
				Destinations: &GlooDestinations{
					StableOrActiveDestination:  stable,
					CanaryOrPreviewDestination: canary,
				},
			}
			g.HttpRoutes = append(g.HttpRoutes, dest)
		}
	} // end range httpRoutes

	return nil
}

// func buildGlooHTTPRoute(name string, matcher *solov2.HTTPRequestMatcher, canaryDestination *SimpleDestinationReference) *GlooMatchedHttpRoutes {
// 	return &GlooMatchedHttpRoutes{
// 		HttpRoute: &networkv2.HTTPRoute{
// 			Name: name,
// 			Matchers: []*solov2.HTTPRequestMatcher{
// 				matcher,
// 			},
// 			ActionType: &networkv2.HTTPRoute_ForwardTo{
// 				ForwardTo: &networkv2.ForwardToAction{
// 					Destinations: []*solov2.DestinationReference{
// 						{
// 							RefKind: &solov2.DestinationReference_Ref{
// 								Ref: &solov2.ObjectReference{
// 									Name:      canaryDestination.Reference.Name,
// 									Namespace: canaryDestination.Reference.Namespace,
// 								},
// 							},
// 							Port: &solov2.PortSelector{
// 								Specifier: &solov2.PortSelector_Number{
// 									Number: canaryDestination.Port.Number,
// 								},
// 							},
// 						},
// 					},
// 				},
// 			},
// 		},
// 	}
// }

func buildGlooMatches(headerRouting *v1alpha1.SetHeaderRoute) *solov2.HTTPRequestMatcher {
	matcher := &solov2.HTTPRequestMatcher{
		Name:    headerRouting.Name + "-matcher",
		Headers: []*solov2.HeaderMatcher{},
	}

	for _, m := range headerRouting.Match {
		var isRegex bool
		var matchValue string
		if m.HeaderValue.Exact != "" {
			matchValue = m.HeaderValue.Exact
		} else if m.HeaderValue.Regex != "" {
			matchValue = m.HeaderValue.Regex
			isRegex = true
		} else if m.HeaderValue.Prefix != "" {
			matchValue = "^" + regexp.QuoteMeta(m.HeaderValue.Prefix)
			isRegex = true
		}
		headerMatcher := &solov2.HeaderMatcher{
			Name:  m.HeaderName,
			Value: matchValue,
			Regex: isRegex,
		}
		matcher.Headers = append(matcher.Headers, headerMatcher)
	}
	return matcher
}

func getPluginConfig(rollout *v1alpha1.Rollout) (*GlooPlatformAPITrafficRouting, error) {
	glooplatformConfig := GlooPlatformAPITrafficRouting{}

	err := json.Unmarshal(rollout.Spec.Strategy.Canary.TrafficRouting.Plugins[PluginName], &glooplatformConfig)
	if err != nil {
		return nil, err
	}

	return &glooplatformConfig, nil
}
