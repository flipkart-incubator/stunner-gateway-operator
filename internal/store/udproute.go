package store

import (
	stnrgwv1 "github.com/l7mp/stunner-gateway-operator/api/v1"
)

// UDPRoutes stores the STUNner-native UDPRoute objects.
var UDPRoutes = NewTypedStore[*stnrgwv1.UDPRoute]()

// UDPRoutesV1A2 stores the Gateway API v1alpha2 UDPRoute objects, converted to the STUNner-native
// UDPRoute representation.
var UDPRoutesV1A2 = NewTypedStore[*stnrgwv1.UDPRoute]()
