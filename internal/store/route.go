package store

import (
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Route is a kind-agnostic handle on a STUNner route object (UDPRoute, TCPRoute, etc.): it
// provides access to the pieces of a route that the route-processing machinery (parent
// resolution, namespace attachment policy, status handling) needs without knowing the concrete
// route kind. Kind-specific processing (most importantly cluster rendering) type-switches on the
// concrete type behind the interface.
type Route interface {
	client.Object
	// GetParentRefs returns the parent references of the route.
	GetParentRefs() []gwapiv1.ParentReference
	// GetRouteStatus returns the route status of the route.
	GetRouteStatus() *gwapiv1.RouteStatus
}
