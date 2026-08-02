package controllers

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	v1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gwapiv1a2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	"github.com/l7mp/stunner-gateway-operator/internal/config"
	"github.com/l7mp/stunner-gateway-operator/internal/event"
	"github.com/l7mp/stunner-gateway-operator/internal/store"
	opdefault "github.com/l7mp/stunner-gateway-operator/pkg/config"

	stnrgwv1 "github.com/l7mp/stunner-gateway-operator/api/v1"
)

const (
	serviceUDPRouteIndex           = "serviceUDPRouteIndex"
	serviceUDPRouteIndexV1A2       = "serviceUDPRouteIndexV1A2"
	staticServiceUDPRouteIndex     = "staticServiceUDPRouteIndex"
	staticServiceUDPRouteIndexV1A2 = "staticServiceUDPRouteIndexV1A2"
	serviceTCPRouteIndex           = "serviceTCPRouteIndex"
	serviceTCPRouteIndexV1         = "serviceTCPRouteIndexV1"
	staticServiceTCPRouteIndex     = "staticServiceTCPRouteIndex"
	staticServiceTCPRouteIndexV1   = "staticServiceTCPRouteIndexV1"
)

type routeReconciler struct {
	client.Client
	eventCh       event.EventChannel
	terminating   bool
	skipGwapiv1a2 bool
	skipGwapiV1   bool
	log           logr.Logger
}

// routeBackends accumulates the backend objects referenced by the reconciled routes.
type routeBackends struct {
	svcList, ssvcList, endpointList, namespaceList []client.Object
}

func NewRouteController(mgr manager.Manager, ch event.EventChannel, log logr.Logger) (Controller, error) {
	ctx := context.Background()
	r := &routeReconciler{
		Client:  mgr.GetClient(),
		eventCh: ch,
		log:     log.WithName("route-controller"),
	}

	c, err := controller.New("route", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return nil, err
	}

	// increase the ref count on the channel
	r.eventCh.Get()

	r.log.Info("Created route controller")

	// watch UDPRoute objects
	if err := c.Watch(
		source.Kind(mgr.GetCache(), &stnrgwv1.UDPRoute{},
			&handler.TypedEnqueueRequestForObject[*stnrgwv1.UDPRoute]{},
			predicate.TypedGenerationChangedPredicate[*stnrgwv1.UDPRoute]{}),
	); err != nil {
		return nil, err
	}
	r.log.Info("Watching UDPRoute objects")

	// index UDPRoute objects as per the referenced Services and StaticServices
	if err := mgr.GetFieldIndexer().IndexField(ctx, &stnrgwv1.UDPRoute{},
		serviceUDPRouteIndex, serviceRouteIndexFunc); err != nil {
		return nil, err
	}

	if err := mgr.GetFieldIndexer().IndexField(ctx, &stnrgwv1.UDPRoute{},
		staticServiceUDPRouteIndex, staticServiceRouteIndexFunc); err != nil {
		return nil, err
	}

	// watch TCPRoute objects
	if err := c.Watch(
		source.Kind(mgr.GetCache(), &stnrgwv1.TCPRoute{},
			&handler.TypedEnqueueRequestForObject[*stnrgwv1.TCPRoute]{},
			predicate.TypedGenerationChangedPredicate[*stnrgwv1.TCPRoute]{}),
	); err != nil {
		return nil, err
	}
	r.log.Info("Watching TCPRoute objects")

	// index TCPRoute objects as per the referenced Services and StaticServices
	if err := mgr.GetFieldIndexer().IndexField(ctx, &stnrgwv1.TCPRoute{},
		serviceTCPRouteIndex, serviceRouteIndexFunc); err != nil {
		return nil, err
	}

	if err := mgr.GetFieldIndexer().IndexField(ctx, &stnrgwv1.TCPRoute{},
		staticServiceTCPRouteIndex, staticServiceRouteIndexFunc); err != nil {
		return nil, err
	}

	// watch UDPRouteV1A2 objects only when the CRD is loaded
	udpRouteV1A2Loaded, err := r.isRouteResourceServed(mgr, &gwapiv1a2.UDPRoute{}, "udproutes")
	if err != nil {
		return nil, err
	}

	if udpRouteV1A2Loaded {
		// watch UDPRouteV1A2 objects
		if err := c.Watch(
			source.Kind(mgr.GetCache(), &gwapiv1a2.UDPRoute{},
				&handler.TypedEnqueueRequestForObject[*gwapiv1a2.UDPRoute]{},
				predicate.TypedGenerationChangedPredicate[*gwapiv1a2.UDPRoute]{}),
		); err != nil {
			return nil, err
		}

		// index UDPRouteV1A2 objects as per the referenced Services and StaticServices
		if err := mgr.GetFieldIndexer().IndexField(ctx, &gwapiv1a2.UDPRoute{},
			serviceUDPRouteIndexV1A2, serviceRouteIndexFunc); err != nil {
			return nil, err
		}

		if err := mgr.GetFieldIndexer().IndexField(ctx, &gwapiv1a2.UDPRoute{},
			staticServiceUDPRouteIndexV1A2, staticServiceRouteIndexFunc); err != nil {
			return nil, err
		}
		r.log.Info("Watching UDPRouteV1A2 objects")
	} else {
		r.skipGwapiv1a2 = true
		r.log.V(1).Info("Gateway API v1alpha2 UDPRoute CRD not available, skipping")
	}

	// watch Gateway API TCPRoute objects only when the CRD is loaded at version v1
	tcpRouteV1Loaded, err := r.isRouteResourceServed(mgr, &gwapiv1.TCPRoute{}, "tcproutes")
	if err != nil {
		return nil, err
	}

	if tcpRouteV1Loaded {
		// watch TCPRouteV1 objects
		if err := c.Watch(
			source.Kind(mgr.GetCache(), &gwapiv1.TCPRoute{},
				&handler.TypedEnqueueRequestForObject[*gwapiv1.TCPRoute]{},
				predicate.TypedGenerationChangedPredicate[*gwapiv1.TCPRoute]{}),
		); err != nil {
			return nil, err
		}

		// index TCPRouteV1 objects as per the referenced Services and StaticServices
		if err := mgr.GetFieldIndexer().IndexField(ctx, &gwapiv1.TCPRoute{},
			serviceTCPRouteIndexV1, serviceRouteIndexFunc); err != nil {
			return nil, err
		}

		if err := mgr.GetFieldIndexer().IndexField(ctx, &gwapiv1.TCPRoute{},
			staticServiceTCPRouteIndexV1, staticServiceRouteIndexFunc); err != nil {
			return nil, err
		}
		r.log.Info("Watching TCPRouteV1 objects")
	} else {
		r.skipGwapiV1 = true
		r.log.V(1).Info("Gateway API v1 TCPRoute CRD not available, skipping")
	}

	// a label-selector predicate to select the loadbalancer services we are interested in
	loadBalancerPredicate, err := ServiceLabelSelectorPredicate(
		metav1.LabelSelector{
			MatchLabels: map[string]string{
				// LB services have both "app:stunner" and
				// "stunner.l7mp.io/owned-by:stunner" labels set, we use the app
				// label here
				opdefault.OwnedByLabelKey: opdefault.OwnedByLabelValue,
			},
		})
	if err != nil {
		return nil, err
	}

	// watch Service objects referenced by one of our routes
	if err := c.Watch(
		source.Kind(mgr.GetCache(), &v1.Service{},
			&handler.TypedEnqueueRequestForObject[*v1.Service]{},
			// trigger when either a gateway-loadbalancer service (svc annotated as a
			// related-service for a gateway) or a backend-service changes
			predicate.Or(
				predicate.NewTypedPredicateFuncs[*v1.Service](r.validateBackendServiceForReconcile),
				loadBalancerPredicate)),
	); err != nil {
		return nil, err
	}
	r.log.Info("Watching Service objects")

	// watch EndPoints object references by one of the ref'd Services
	if config.EnableEndpointDiscovery {
		// try to set up a watch for EndpointSlices if the endpointslice controller was not
		// explicitly disabled on the command line
		if config.EndpointSliceAvailable {
			if err := c.Watch(
				source.Kind(mgr.GetCache(), &discoveryv1.EndpointSlice{},
					&handler.TypedEnqueueRequestForObject[*discoveryv1.EndpointSlice]{},
					predicate.NewTypedPredicateFuncs[*discoveryv1.EndpointSlice](r.validateEndpointSliceForReconcile)),
			); err == nil {
				r.log.Info("Watching EndpointSlice objects")
				config.EndpointSliceAvailable = true
			} else {
				r.log.Info("Warning: EndpointSlice support diabled, falling back to " +
					"the Endpoints controller and disabling graceful backend " +
					"shutdown, see https://github.com/l7mp/stunner/issues/138")
				config.EndpointSliceAvailable = false
			}
		}

		// if EndpointSlices are still not available, fall back to wathing Endpoints
		if !config.EndpointSliceAvailable {
			if err := c.Watch(
				//nolint:staticcheck
				source.Kind(mgr.GetCache(), &v1.Endpoints{},
					&handler.TypedEnqueueRequestForObject[*v1.Endpoints]{},
					predicate.NewTypedPredicateFuncs[*v1.Endpoints](r.validateBackendEndpointsForReconcile)),
			); err != nil {
				return nil, err
			}

			config.EndpointSliceAvailable = false
			r.log.Info("Watching Endpoints objects")
		}
	}

	// watch StaticService objects referenced by one of our routes
	if err := c.Watch(
		source.Kind(mgr.GetCache(), &stnrgwv1.StaticService{},
			&handler.TypedEnqueueRequestForObject[*stnrgwv1.StaticService]{},
			predicate.NewTypedPredicateFuncs[*stnrgwv1.StaticService](r.validateStaticServiceForReconcile)),
	); err != nil {
		return nil, err
	}
	r.log.Info("Watching StaticService objects")

	return r, nil
}

// Reconcile handles an update to a route or a Service/Endpoints referenced by a route.
func (r *routeReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	log := r.log.WithValues("resource", req.String())

	if r.terminating {
		r.log.V(2).Info("Controller terminating, suppressing reconciliation")
		return reconcile.Result{}, nil
	}

	log.Info("Reconciling")
	udpRouteList := []client.Object{}
	udpRouteListV1A2 := []client.Object{}
	tcpRouteList := []client.Object{}
	tcpRouteListV1 := []client.Object{}
	backends := routeBackends{}

	// find all related-services that we use as LoadBalancers for Gateways (i.e., have label
	// "app:stunner")
	svcs := &v1.ServiceList{}
	err := r.List(ctx, svcs, client.MatchingLabels{opdefault.OwnedByLabelKey: opdefault.OwnedByLabelValue})
	if err == nil {
		for _, svc := range svcs.Items {
			svc := svc
			backends.svcList = append(backends.svcList, &svc)
		}
	}

	// find all UDPRoutes
	udpRoutes := &stnrgwv1.UDPRouteList{}
	if err := r.List(ctx, udpRoutes); err != nil {
		r.log.Info("No UDPRoutes found")
	} else {
		for i := range udpRoutes.Items {
			ro := &udpRoutes.Items[i]
			r.log.V(1).Info("Processing UDPRoute", "name", store.GetObjectKey(ro))

			udpRouteList = append(udpRouteList, ro)
			r.collectBackends(ctx, ro, ro.Spec.Rules, &backends)
		}
	}

	// find all gwapi.v1alpha2 UDPRoutes and convert to our own UDPRoute format
	if !r.skipGwapiv1a2 {
		routesV1A2 := &gwapiv1a2.UDPRouteList{}
		if err := r.List(ctx, routesV1A2); err != nil {
			r.log.V(2).Info("No UDPRouteV1A2 resources found")
			return reconcile.Result{}, err
		}

		for i := range routesV1A2.Items {
			ro := stnrgwv1.ConvertV1A2UDPRouteToV1(&routesV1A2.Items[i])
			r.log.V(1).Info("Processing UDPRouteV1A2", "name", store.GetObjectKey(ro))

			udpRouteListV1A2 = append(udpRouteListV1A2, ro)
			r.collectBackends(ctx, ro, ro.Spec.Rules, &backends)
		}
	}

	// find all TCPRoutes
	tcpRoutes := &stnrgwv1.TCPRouteList{}
	if err := r.List(ctx, tcpRoutes); err != nil {
		r.log.Info("No TCPRoutes found")
	} else {
		for i := range tcpRoutes.Items {
			ro := &tcpRoutes.Items[i]
			r.log.V(1).Info("Processing TCPRoute", "name", store.GetObjectKey(ro))

			tcpRouteList = append(tcpRouteList, ro)
			r.collectBackends(ctx, ro, ro.Spec.Rules, &backends)
		}
	}

	// find all gwapi.v1 TCPRoutes and convert to our own TCPRoute format
	if !r.skipGwapiV1 {
		routesV1 := &gwapiv1.TCPRouteList{}
		if err := r.List(ctx, routesV1); err != nil {
			r.log.V(2).Info("No TCPRouteV1 resources found")
			return reconcile.Result{}, err
		}

		for i := range routesV1.Items {
			ro := stnrgwv1.ConvertV1TCPRouteToStnrV1(&routesV1.Items[i])
			r.log.V(1).Info("Processing TCPRouteV1", "name", store.GetObjectKey(ro))

			tcpRouteListV1 = append(tcpRouteListV1, ro)
			r.collectBackends(ctx, ro, ro.Spec.Rules, &backends)
		}
	}

	store.UDPRoutes.Reset(udpRouteList)
	r.log.V(2).Info("Reset UDPRoute store", "udproutes", store.UDPRoutes.String())

	store.UDPRoutesV1A2.Reset(udpRouteListV1A2)
	r.log.V(2).Info("Reset UDPRoute V1A2 store", "udproutes", store.UDPRoutesV1A2.String())

	store.TCPRoutes.Reset(tcpRouteList)
	r.log.V(2).Info("Reset TCPRoute store", "tcproutes", store.TCPRoutes.String())

	store.TCPRoutesV1.Reset(tcpRouteListV1)
	r.log.V(2).Info("Reset TCPRoute V1 store", "tcproutes", store.TCPRoutesV1.String())

	store.Namespaces.Reset(backends.namespaceList)
	r.log.V(2).Info("Reset Namespace store", "namespaces", store.Namespaces.String())

	store.Services.Reset(backends.svcList)
	r.log.V(2).Info("Reset Service store", "services", store.Services.String())

	if config.EndpointSliceAvailable {
		store.EndpointSlices.Reset(backends.endpointList)
		r.log.V(2).Info("Reset EndpointSlice store", "endpointslices", store.EndpointSlices.String())
	} else {
		store.Endpoints.Reset(backends.endpointList)
		r.log.V(2).Info("Reset Endpoints store", "endpoints", store.Endpoints.String())
	}

	store.StaticServices.Reset(backends.ssvcList)
	r.log.V(2).Info("Reset StaticService store", "static-services", store.StaticServices.String())

	r.eventCh.Channel() <- event.NewEventReconcile()

	return reconcile.Result{}, nil
}

// collectBackends gathers the backend Services, StaticServices, Endpoints/EndpointSlices and the
// namespace referenced by a route into the accumulator.
func (r *routeReconciler) collectBackends(ctx context.Context, ro client.Object, rules []stnrgwv1.RouteRule, acc *routeBackends) {
	for _, rule := range rules {
		for _, ref := range rule.BackendRefs {
			ref := ref

			// is this a static service?
			if store.IsReferenceStaticService(&ref) {
				if svc := r.getStaticServiceForBackend(ctx, ro, &ref); svc != nil {
					acc.ssvcList = append(acc.ssvcList, svc)
				}
				continue
			}

			if store.IsReferenceService(&ref) {
				if svc := r.getServiceForBackend(ctx, ro, &ref); svc != nil {
					r.log.V(2).Info("Found service for route backend ref",
						"route", store.GetObjectKey(ro),
						"ref", store.DumpBackendRef(&ref),
						"svc", store.GetObjectKey(svc))
					acc.svcList = append(acc.svcList, svc)
				}

				if config.EnableEndpointDiscovery {
					if config.EndpointSliceAvailable {
						es := r.getEndpointSlicesForBackend(ctx, ro, &ref)
						acc.endpointList = append(acc.endpointList, es...)
					} else {
						if e := r.getEndpointsForBackend(ctx, ro, &ref); e != nil {
							acc.endpointList = append(acc.endpointList, e)
						}
					}
				}

				continue
			}
		}
	}

	nsName := ro.GetNamespace()
	r.log.V(2).Info("Looking for the namespace of route", "name", nsName)
	namespace := v1.Namespace{}
	if err := r.Get(ctx, types.NamespacedName{Name: nsName}, &namespace); err != nil {
		r.log.Error(err, "Error getting namespace for route", "route",
			store.GetObjectKey(ro), "namespace-name", nsName)
		return
	}

	acc.namespaceList = append(acc.namespaceList, &namespace)
}

func (r *routeReconciler) validateBackendServiceForReconcile(svc *v1.Service) bool {
	return r.validateBackendForReconcile(store.GetObjectKey(svc), serviceUDPRouteIndex,
		serviceUDPRouteIndexV1A2, serviceTCPRouteIndex, serviceTCPRouteIndexV1)
}

func (r *routeReconciler) validateStaticServiceForReconcile(staticSvc *stnrgwv1.StaticService) bool {
	return r.validateBackendForReconcile(store.GetObjectKey(staticSvc), staticServiceUDPRouteIndex,
		staticServiceUDPRouteIndexV1A2, staticServiceTCPRouteIndex, staticServiceTCPRouteIndexV1)
}

//nolint:staticcheck
func (r *routeReconciler) validateBackendEndpointsForReconcile(e *v1.Endpoints) bool {
	return r.validateBackendForReconcile(store.GetObjectKey(e), serviceUDPRouteIndex,
		serviceUDPRouteIndexV1A2, serviceTCPRouteIndex, serviceTCPRouteIndexV1)
}

// validateBackendForReconcile checks whether the Service or StaticService belongs to a valid
// route. Uses the indexers in the argument.
func (r *routeReconciler) validateBackendForReconcile(key, udpIndex, udpIndexV1A2, tcpIndex, tcpIndexV1 string) bool {
	routeNum := 0

	// find the UDPRoutes referring to this service
	udpRouteList := &stnrgwv1.UDPRouteList{}
	if err := r.List(context.Background(), udpRouteList, &client.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(udpIndex, key),
	}); err != nil {
		r.log.Error(err, "Unable to find associated UDPRoute", "service", key)
	} else {
		routeNum += len(udpRouteList.Items)
	}

	if !r.skipGwapiv1a2 {
		// find V1A2 UDPRoutes referring to this service
		routeListV1A2 := &gwapiv1a2.UDPRouteList{}
		if err := r.List(context.Background(), routeListV1A2, &client.ListOptions{
			FieldSelector: fields.OneTermEqualSelector(udpIndexV1A2, key),
		}); err != nil {
			r.log.Error(err, "Unable to find associated UDPRouteV1A2", "service", key)
		} else {
			routeNum += len(routeListV1A2.Items)
		}
	}

	// find the TCPRoutes referring to this service
	tcpRouteList := &stnrgwv1.TCPRouteList{}
	if err := r.List(context.Background(), tcpRouteList, &client.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(tcpIndex, key),
	}); err != nil {
		r.log.Error(err, "Unable to find associated TCPRoute", "service", key)
	} else {
		routeNum += len(tcpRouteList.Items)
	}

	if !r.skipGwapiV1 {
		// find V1 TCPRoutes referring to this service
		routeListV1 := &gwapiv1.TCPRouteList{}
		if err := r.List(context.Background(), routeListV1, &client.ListOptions{
			FieldSelector: fields.OneTermEqualSelector(tcpIndexV1, key),
		}); err != nil {
			r.log.Error(err, "Unable to find associated TCPRouteV1", "service", key)
		} else {
			routeNum += len(routeListV1.Items)
		}
	}

	resStr := "not found"
	if routeNum > 0 {
		resStr = fmt.Sprintf("found %d routes", routeNum)
	}

	r.log.Info("Validating backend", "key", key, "route", resStr)

	return routeNum != 0
}

// validateEndpointSliceForReconcile checks whether an EndpointSlice belongs to a Service that
// belongs to a valid route.
func (r *routeReconciler) validateEndpointSliceForReconcile(esl *discoveryv1.EndpointSlice) bool {
	// find the Service corresponding to this EndpointSlice
	// TODO: also check ownership
	svcName, ok := esl.GetLabels()[discoveryv1.LabelServiceName]
	if !ok {
		r.log.Info("Calidate EndpointSlice:", "label", "not ok")
		return false
	}

	svc := &v1.Service{}
	if err := r.Get(context.Background(), types.NamespacedName{
		Namespace: esl.GetNamespace(),
		Name:      svcName,
	}, svc); err != nil {
		// not fatal
		if !apierrors.IsNotFound(err) {
			r.log.Error(err, "Error getting Service for EndpointSlice",
				"namespace", esl.GetNamespace(),
				"name", svcName)
		}
		return false
	}

	return r.validateBackendServiceForReconcile(svc)
}

// getServiceForBackend finds the Service associated with a backendRef
func (r *routeReconciler) getServiceForBackend(ctx context.Context, ro client.Object, ref *stnrgwv1.BackendRef) *v1.Service {
	// if no explicit Service namespace is provided, use the route namespace to lookup the
	// Service
	namespace := ro.GetNamespace()
	if ref.Namespace != nil {
		namespace = string(*ref.Namespace)
	}

	svc := v1.Service{}
	if err := r.Get(ctx,
		types.NamespacedName{Namespace: namespace, Name: string(ref.Name)},
		&svc,
	); err != nil {
		// not fatal
		if !apierrors.IsNotFound(err) {
			r.log.Error(err, "Error getting Service", "namespace", namespace,
				"name", string(ref.Name))
			return nil
		}

		r.log.Info("No Service found for route backend", "route",
			store.GetObjectKey(ro), "namespace", namespace,
			"name", string(ref.Name))
		return nil
	}

	return &svc
}

// getEndpointSlicesForBackend finds all EndpointSlices associated with a backendRef
func (r *routeReconciler) getEndpointSlicesForBackend(ctx context.Context, ro client.Object, ref *stnrgwv1.BackendRef) []client.Object {
	// if no explicit Endpoints namespace is provided, use the route namespace to lookup the
	// Endpoints
	namespace := ro.GetNamespace()
	if ref.Namespace != nil {
		namespace = string(*ref.Namespace)
	}

	// find the EndpointSlicce corresponding to the backend service
	esls := discoveryv1.EndpointSliceList{}
	labelSelector := labels.SelectorFromSet(labels.Set{discoveryv1.LabelServiceName: string(ref.Name)})
	listOptions := &client.ListOptions{
		Namespace:     namespace,
		LabelSelector: labelSelector,
	}

	if err := r.List(ctx, &esls, listOptions); err != nil {
		r.log.Error(err, "Error getting EndpointSlices for backend service",
			"namespace", namespace, "backend-name", string(ref.Name))
		return []client.Object{}
	}

	es := make([]client.Object, len(esls.Items))
	for i := range esls.Items {
		es[i] = &esls.Items[i]
	}

	if len(es) == 0 {
		r.log.Info("No EndpointSlice found for backend", "route",
			store.GetObjectKey(ro), "backend-ref",
			store.DumpBackendRef(ref))
	}

	return es
}

// getEndpointsForBackend finds the Endpoints associated with a backendRef
func (r *routeReconciler) getEndpointsForBackend(ctx context.Context, ro client.Object, ref *stnrgwv1.BackendRef) client.Object {
	// if no explicit Endpoints namespace is provided, use the route namespace to lookup the
	// Endpoints
	namespace := ro.GetNamespace()
	if ref.Namespace != nil {
		namespace = string(*ref.Namespace)
	}

	ep := v1.Endpoints{} //nolint:staticcheck
	if err := r.Get(ctx, types.NamespacedName{Namespace: namespace, Name: string(ref.Name)}, &ep); err != nil {
		// not fatal
		if !apierrors.IsNotFound(err) {
			r.log.Error(err, "Error getting Endpoints", "namespace", namespace, "name",
				string(ref.Name))
		}

		r.log.Info("No Endpoints found for route backend", "route",
			store.GetObjectKey(ro), "namespace", namespace, "name",
			string(ref.Name))

		return nil
	}

	return &ep
}

// getStaticServiceForBackend finds the StaticService associated with a backendRef
func (r *routeReconciler) getStaticServiceForBackend(ctx context.Context, ro client.Object, ref *stnrgwv1.BackendRef) *stnrgwv1.StaticService {
	svc := stnrgwv1.StaticService{}

	// if no explicit StaticService namespace is provided, use the route namespace to lookup
	// the StaticService
	namespace := ro.GetNamespace()
	if ref.Namespace != nil {
		namespace = string(*ref.Namespace)
	}

	if err := r.Get(ctx,
		types.NamespacedName{Namespace: namespace, Name: string(ref.Name)},
		&svc,
	); err != nil {
		// not fatal
		if !apierrors.IsNotFound(err) {
			r.log.Error(err, "Error getting StaticService", "namespace", namespace,
				"name", string(ref.Name))
			return nil
		}

		r.log.Info("No StaticService found for route backend", "route",
			store.GetObjectKey(ro), "namespace", namespace,
			"name", string(ref.Name))
		return nil
	}

	return &svc
}

// isRouteResourceServed checks whether the API server serves the given route resource at the
// group/version of the object.
func (r *routeReconciler) isRouteResourceServed(mgr manager.Manager, obj client.Object, resourceName string) (bool, error) {
	// Build a discovery client
	d, err := discovery.NewDiscoveryClientForConfig(mgr.GetConfig())
	if err != nil {
		return false, fmt.Errorf("failed to obtain a discovery client: %w", err)
	}

	// Get the Groupversion
	gvk, err := apiutil.GVKForObject(obj, mgr.GetScheme())
	if err != nil {
		return false, fmt.Errorf("failed to get GVK for %T: %w", obj, err)
	}
	gvStr := gvk.GroupVersion().String()

	resList, err := d.ServerResourcesForGroupVersion(gvStr)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to get server resources for %s: %w", gvStr, err)
	}

	for _, r := range resList.APIResources {
		if r.Name == resourceName {
			return true, nil
		}
	}

	return false, nil
}

// canonicalRoute converts any supported route object into the canonical STUNner-native
// representation for indexing. Returns nil for unsupported objects.
func canonicalRoute(o client.Object) (client.Object, []stnrgwv1.RouteRule) {
	switch ro := o.(type) {
	case *stnrgwv1.UDPRoute:
		return ro, ro.Spec.Rules
	case *gwapiv1a2.UDPRoute:
		c := stnrgwv1.ConvertV1A2UDPRouteToV1(ro)
		return c, c.Spec.Rules
	case *stnrgwv1.TCPRoute:
		return ro, ro.Spec.Rules
	case *gwapiv1.TCPRoute:
		c := stnrgwv1.ConvertV1TCPRouteToStnrV1(ro)
		return c, c.Spec.Rules
	default:
		return nil, nil
	}
}

func serviceRouteIndexFunc(o client.Object) []string {
	ro, rules := canonicalRoute(o)
	if ro == nil {
		return []string{}
	}

	var services []string
	for _, rule := range rules {
		for _, backend := range rule.BackendRefs {
			if !store.IsReferenceService(&backend) {
				continue
			}

			if backend.Kind == nil || string(*backend.Kind) == "Service" {
				// if no explicit Service namespace is provided, use the route
				// namespace to lookup the provided Service
				namespace := ro.GetNamespace()
				if backend.Namespace != nil {
					namespace = string(*backend.Namespace)
				}

				services = append(services,
					types.NamespacedName{
						Namespace: namespace,
						Name:      string(backend.Name),
					}.String(),
				)
			}
		}
	}

	return services
}

func staticServiceRouteIndexFunc(o client.Object) []string {
	ro, rules := canonicalRoute(o)
	if ro == nil {
		return []string{}
	}

	var staticServices []string
	for _, rule := range rules {
		for _, backend := range rule.BackendRefs {
			backend := backend

			if !store.IsReferenceStaticService(&backend) {
				continue
			}

			// if no explicit StaticService namespace is provided, use the route
			// namespace to lookup the provided static service
			namespace := ro.GetNamespace()
			if backend.Namespace != nil {
				namespace = string(*backend.Namespace)
			}

			staticServices = append(staticServices,
				types.NamespacedName{
					Namespace: namespace,
					Name:      string(backend.Name),
				}.String(),
			)
		}
	}

	return staticServices
}

func (r *routeReconciler) Terminate() {
	r.terminating = true
	r.eventCh.Put()
}

// TypedLabelSelectorPredicate is the generic version of LabelSelectorPredicate that somehow seems
// to be missing in controller-runtime to construct a TypedPredicate from a LabelSelector.  Only
// objects matching the LabelSelector will be admitted.
func ServiceLabelSelectorPredicate(s metav1.LabelSelector) (predicate.TypedPredicate[*v1.Service], error) {
	selector, err := metav1.LabelSelectorAsSelector(&s)
	if err != nil {
		return predicate.TypedFuncs[*v1.Service]{}, err
	}
	return predicate.NewTypedPredicateFuncs[*v1.Service](func(o *v1.Service) bool {
		return selector.Matches(labels.Set(o.GetLabels()))
	}), nil
}
