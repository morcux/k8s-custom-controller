package main

import (
	"context"
	"log"

	"gateway-proxy/internal/controller"
	"gateway-proxy/internal/proxy"
	"gateway-proxy/internal/state"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func main() {
	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))
	ctx := signals.SetupSignalHandler()

	routerState := state.NewRouter()

	proxyServer := proxy.NewServer(":8080", routerState)
	if err := runControlPlane(ctx, routerState, proxyServer); err != nil {
		log.Fatalf("failed to run control plane: %v", err)
	}

	log.Println("Shutdown complete.")
}

func runControlPlane(ctx context.Context, routerState *state.Router, proxyServer *proxy.Server) error {
	cfg, err := config.GetConfig()
	if err != nil {
		return err
	}

	mgr, err := manager.New(cfg, manager.Options{
		Metrics: server.Options{
			BindAddress: "0",
		},
	})
	if err != nil {
		return err
	}

	if err := gatewayv1.Install(mgr.GetScheme()); err != nil {
		return err
	}

	reconciler := &controller.HTTPRouteReconciler{
		Client: mgr.GetClient(),
		State:  routerState,
	}

	if err := reconciler.SetupWithManager(mgr); err != nil {
		return err
	}

	if err := mgr.Add(proxyServer); err != nil {
		log.Fatalf("unable to add proxy server to manager: %v", err)
		return err
	}

	log.Println("Starting control plane manager...")
	return mgr.Start(ctx)
}
