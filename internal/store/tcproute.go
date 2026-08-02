package store

import (
	stnrgwv1 "github.com/l7mp/stunner-gateway-operator/api/v1"
)

// TCPRoutes stores the STUNner-native TCPRoute objects.
var TCPRoutes = NewTypedStore[*stnrgwv1.TCPRoute]()

// TCPRoutesV1 stores the Gateway API v1 TCPRoute objects, converted to the STUNner-native
// TCPRoute representation.
var TCPRoutesV1 = NewTypedStore[*stnrgwv1.TCPRoute]()
