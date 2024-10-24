**Code:**
[![Go Report Card](https://goreportcard.com/badge/github.com/argoproj-labs/rollouts-plugin-trafficrouter-glooplatform)](https://goreportcard.com/report/github.com/argoproj-labs/rollouts-plugin-trafficrouter-glooplatform)
[![Gateway API plugin CI](https://github.com/argoproj-labs/rollouts-plugin-trafficrouter-glooplatform/actions/workflows/ci.yaml/badge.svg)](https://github.com/argoproj-labs/rollouts-plugin-trafficrouter-glooplatform/actions/workflows/ci.yaml)

**Social:**
[![Twitter Follow](https://img.shields.io/twitter/follow/argoproj?style=social)](https://twitter.com/argoproj)
[![Slack](https://img.shields.io/badge/slack-argoproj-brightgreen.svg?logo=slack)](https://argoproj.github.io/community/join-slack)

# Argo Rollout Gloo Platform API Plugin

An Argo Rollouts plugin for [Gloo Platform](https://www.solo.io/products/gloo-platform/).

### Quickstart

Install Argo Rollouts w/ downloaded Gloo Platform plugin (uses the vanilla `quay.io/argoproj/argo-rollouts:latest` image, which then downloads the Gloo Platform plugin on startup)

```bash
kubectl create ns argo-rollouts
kubectl apply -k ./deploy
```

Deploy the initial rollout state - 100% to `green`

```bash
kubectl apply -f ./examples/0-rollout-initial-state-green
kubectl argo rollouts dashboard &
open http://localhost:3100/rollouts
```

Add a rollout revision to perform a canary rollout to `blue`

```bash
kubectl apply -f ./examples/1-rollout-canary-blue
```

The rollout should progress to 10% `blue` and pause until manually promoted in the dashboard.

### Argo Rollouts Plugin Installation

Requirements:

1. Gloo Platform plugin the Argo Rollouts runtime container
1. Register the plugin in the Argo Rollouts argo-rollouts-config ConfigMap
1. Argo Rollouts RBAC to modify Gloo APIs

The plugin can be loaded into the controller runtime by building your own Argo Rollouts image, pulling it in an init container, or having the controller download it on startup. See [Traffic Router Plugins](https://argoproj.github.io/argo-rollouts/features/traffic-management/plugins/) for details.

See [Kustomize patches](./deploy/kustomization.yaml) in this repo for Argo Rollouts configuration examples.

### Usage

As of version 0.0.0-beta3 support for header-based routing has been added to the original weighted routing features.

#### Weighted Routing

Canary and stable services in the Rollout spec must refer to `forwardTo` destinations in [routes](https://docs.solo.io/gloo-mesh-enterprise/latest/troubleshooting/gloo/routes/) that exist in one or more Gloo Platform RouteTables.

RouteTable and route selection is specified in the plugin config. Either a RouteTable label selector or a named RouteTable must be specified. RouteSelector is entirely optional; this is useful to limit matches to specific routes in a RouteTable if it contains any references to canary or stable services that you do not want to modify.

```yaml
  strategy:
    canary:
      canaryService: canary
      stableService: stable
      trafficRouting:
        plugins:
          # the plugin name must match the name used in argo-rollouts-config ConfigMap
          solo-io/glooplatform:
            # select Gloo RouteTable(s); if both label and name selectors are used, the name selector
            # takes precedence
            routeTableSelector:
              # (optional) label selector
              labels:
                app: demo
              # filter by namespace
              namespace: gloo-mesh
              # (optional) select a specific RouteTable by name
              # name: rt-name
            # (optional) select specific route(s); useful to target specific routes in a RouteTable that has mutliple occurences of the canaryService or stableService 
            routeSelector:
              # (optional) label selector
              labels:
                route: demo-preview
              # (optional) select a specific route by name
              # name: route-name
```

#### Header-based Canary Routing

By defining a setHeaderRoute step in your canary rollout strategy you can instruct this plugin to crate a new routeTable route which will route to the specified canary destination when the header match is satisfied. This feature requires configuring managedRoutes, which grants the plugin ownership over all routes in the routeTable which have the same name. Caution should be used when adding a name to this list because the plugin may overwrite and/or delete any routes it is allowed to manage as needed to implement the behavior specifid in the setHeaderRoute step.

**Known Limitations** in 0.0.0-beta3 setHeaderRoute __does not__ support prefix header matching and will panic.

```yaml
  strategy:
    canary:
      canaryService: canary
      stableService: stable
      trafficRouting:
        # managedRoutes are required for using setHeaderRoute
        # routes specified here are owned by the plugin
        # the plugin will create/delete these routes as needed
        # do not specify routes which are already otherwise in use under this field
        managedRoutes: 
          - name: header-canary
        plugins:
          # the plugin name must match the name used in argo-rollouts-config ConfigMap
          solo-io/glooplatform:
            # canaryDestination describes where traffic matching the headers
            # in setHeaderRoute will be sent
            canaryDestination:
              port:
                number: 8080
              ref:
                name: canary
                namespace: gloo-rollouts-demo
            routeTableSelector:
              labels:
                app: demo
              namespace: gloo-mesh
      steps:
      - setWeight: 10
      - setHeaderRoute:
          match:
          - headerName: version
            headerValue:
              exact: canary
          name: header-canary
      - pause: {}
      - setWeight: 100
```

### Supported Gloo Platform Versions

* All Gloo Platform versions 2.0 and newer

### TODO

- implement [blue/green](./pkg/plugin/plugin_bluegreen.go)
- implement `SetHeaderRoute` and `SetMirrorRoute` in [plugin.go](./pkg/plugin/plugin.go)
- unit tests
  - update tests with mock gloo client using interfaces in [./pkg/gloo/client.go](./pkg/gloo/client.go)
  - add more tests
- replace demo api in examples folder w/ https://github.com/argoproj/rollouts-demo images (blue, green, red, etc.)
