package controller

import (
	"context"
	"gateway-proxy/internal/state"

	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

const httpRouteFinalizer = "gateway-proxy.example.com/finalizer"

type HTTPRouteReconciler struct {
	client.Client
	State *state.Router
}

func (r *HTTPRouteReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	logger := log.FromContext(ctx)

	var route gatewayv1.HTTPRoute
	if err := r.Get(ctx, req.NamespacedName, &route); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("HTTPRoute not found, assuming it was deleted.")
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	if !route.ObjectMeta.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(&route, httpRouteFinalizer) {
			logger.Info("Cleaning up routes for deleted HTTPRoute")
			r.cleanupStateForRoute(&route)

			controllerutil.RemoveFinalizer(&route, httpRouteFinalizer)
			if err := r.Update(ctx, &route); err != nil {
				return reconcile.Result{}, err
			}
		}
		return reconcile.Result{}, nil
	}

	if !controllerutil.ContainsFinalizer(&route, httpRouteFinalizer) {
		controllerutil.AddFinalizer(&route, httpRouteFinalizer)
		if err := r.Update(ctx, &route); err != nil {
			return reconcile.Result{}, err
		}
		return reconcile.Result{Requeue: true}, nil
	}

	var collectedRoutes []state.RouteInfo

	for _, rule := range route.Spec.Rules {
		for _, backendRef := range rule.BackendRefs {
			if backendRef.Kind != nil && *backendRef.Kind != "Service" {
				continue
			}
			serviceName := string(backendRef.Name)

			var endpointSliceList discoveryv1.EndpointSliceList
			listOptions := []client.ListOption{
				client.InNamespace(route.Namespace),
				client.MatchingLabels{discoveryv1.LabelServiceName: serviceName},
			}
			if err := r.List(ctx, &endpointSliceList, listOptions...); err != nil {
				logger.Error(err, "Failed to list EndpointSlices for service", "service", serviceName)
				continue
			}

			var allIPs []string
			for _, slice := range endpointSliceList.Items {
				for _, endpoint := range slice.Endpoints {
					if endpoint.Conditions.Ready != nil && *endpoint.Conditions.Ready {
						allIPs = append(allIPs, endpoint.Addresses...)
					}
				}
			}

			if len(allIPs) == 0 {
				continue
			}

			backendInfo := state.BackendInfo{
				Service:   serviceName,
				Port:      int(*backendRef.Port),
				Addresses: allIPs,
			}

			for _, match := range rule.Matches {
				if match.Path == nil {
					continue
				}
				routeInfo := state.RouteInfo{
					PathMatchType: *match.Path.Type,
					PathValue:     *match.Path.Value,
					Backend:       backendInfo,
				}
				collectedRoutes = append(collectedRoutes, routeInfo)
			}
		}
	}
	for _, host := range route.Spec.Hostnames {
		r.State.UpdateRoutes(string(host), collectedRoutes)
		logger.Info("Updated routes for host", "host", host, "route_count", len(collectedRoutes))
	}

	return reconcile.Result{}, nil
}

func (r *HTTPRouteReconciler) SetupWithManager(mgr manager.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gatewayv1.HTTPRoute{}).
		Watches(&discoveryv1.EndpointSlice{}, handler.EnqueueRequestsFromMapFunc(
			func(ctx context.Context, o client.Object) []reconcile.Request {
				endpoints, ok := o.(*discoveryv1.EndpointSlice)
				if !ok {
					return nil
				}
				svcName := endpoints.Labels[discoveryv1.LabelServiceName]
				if svcName == "" {
					return nil
				}

				routeList := &gatewayv1.HTTPRouteList{}
				if err := r.Client.List(ctx, routeList, client.InNamespace(endpoints.Namespace)); err != nil {
					return nil
				}

				var requests []reconcile.Request
			RouteLoop:
				for _, route := range routeList.Items {
					for _, rule := range route.Spec.Rules {
						for _, backendRef := range rule.BackendRefs {
							if backendRef.BackendObjectReference.Kind != nil && *backendRef.BackendObjectReference.Kind == "Service" &&
								string(backendRef.BackendObjectReference.Name) == svcName {

								requests = append(requests, reconcile.Request{
									NamespacedName: types.NamespacedName{
										Name:      route.Name,
										Namespace: route.Namespace,
									},
								})

								continue RouteLoop
							}
						}
					}
				}

				return requests
			},
		)).
		Complete(r)
}
func (r *HTTPRouteReconciler) cleanupStateForRoute(route *gatewayv1.HTTPRoute) {
	for _, host := range route.Spec.Hostnames {
		r.State.UpdateRoutes(string(host), nil) 
	}
}
