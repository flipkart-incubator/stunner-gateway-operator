/*
Copyright 2022 The l7mp/stunner team.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func init() {
	SchemeBuilder.Register(func(scheme *runtime.Scheme) error {
		scheme.AddKnownTypes(GroupVersion, &TCPRoute{}, &TCPRouteList{})
		return nil
	})
}

// TCPRoute provides a way to route TCP traffic. When combined with a Gateway listener, it can be
// used to forward traffic on the port specified by the listener to a set of backends specified by
// the TCPRoute.
//
// Differences from Gateway API TCPRoutes
//   - port-ranges are correctly handled ([port, endPort])
//   - port is not mandatory
//   - backend weight is not supported
//
// Rendering a TCPRoute into a dataplane cluster is supported only in the premium tier. Without a
// license the route is still accepted and its status maintained, but no cluster is rendered for
// it and the ResolvedRefs condition reports the reason.
//
// +kubebuilder:object:root=true
// +kubebuilder:resource:categories=stunner
// +kubebuilder:subresource:status
// +kubebuilder:storageversion
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type TCPRoute struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec defines the desired state of TCPRoute.
	Spec RouteSpec `json:"spec"`

	// Status defines the current state of TCPRoute.
	Status gwapiv1.TCPRouteStatus `json:"status,omitempty"`
}

// GetParentRefs returns the parent references of the route.
func (r *TCPRoute) GetParentRefs() []gwapiv1.ParentReference {
	return r.Spec.ParentRefs
}

// GetRules returns the rules of the route.
func (r *TCPRoute) GetRules() []RouteRule {
	return r.Spec.Rules
}

// GetRouteStatus returns the route status of the route.
func (r *TCPRoute) GetRouteStatus() *gwapiv1.RouteStatus {
	return &r.Status.RouteStatus
}

// +kubebuilder:object:root=true

// TCPRouteList contains a list of TCPRoute
type TCPRouteList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TCPRoute `json:"items"`
}

// conversions
func ConvertV1TCPRouteToStnrV1(src *gwapiv1.TCPRoute) *TCPRoute {
	if src == nil {
		return nil
	}
	dst := new(TCPRoute)
	ConvertV1TCPRouteToStnrV1Into(src, dst)
	return dst
}

func ConvertV1TCPRouteToStnrV1Into(src *gwapiv1.TCPRoute, dst *TCPRoute) {
	src.ObjectMeta.DeepCopyInto(&dst.ObjectMeta)
	src.Spec.CommonRouteSpec.DeepCopyInto(&dst.Spec.CommonRouteSpec)
	src.Status.RouteStatus.DeepCopyInto(&dst.Status.RouteStatus)

	dst.Spec.Rules = make([]RouteRule, len(src.Spec.Rules))
	for i := range src.Spec.Rules {
		dst.Spec.Rules[i].BackendRefs = make([]BackendRef, len(src.Spec.Rules[i].BackendRefs))
		for j := range src.Spec.Rules[i].BackendRefs {
			b := src.Spec.Rules[i].BackendRefs[j].BackendObjectReference
			dst.Spec.Rules[i].BackendRefs[j].BackendObjectReference = BackendObjectReference{
				Group:     b.Group,
				Kind:      b.Kind,
				Name:      b.Name,
				Namespace: b.Namespace,
				// ignore port and weight!
			}
		}
	}
}

func ConvertStnrV1TCPRouteToV1(src *TCPRoute) *gwapiv1.TCPRoute {
	if src == nil {
		return nil
	}
	dst := new(gwapiv1.TCPRoute)
	ConvertStnrV1TCPRouteToV1Into(src, dst)
	return dst
}

func ConvertStnrV1TCPRouteToV1Into(src *TCPRoute, dst *gwapiv1.TCPRoute) {
	src.ObjectMeta.DeepCopyInto(&dst.ObjectMeta)
	src.Spec.CommonRouteSpec.DeepCopyInto(&dst.Spec.CommonRouteSpec)
	src.Status.RouteStatus.DeepCopyInto(&dst.Status.RouteStatus)

	dst.Spec.Rules = make([]gwapiv1.TCPRouteRule, len(src.Spec.Rules))
	for i := range src.Spec.Rules {
		dst.Spec.Rules[i].BackendRefs = make([]gwapiv1.BackendRef, len(src.Spec.Rules[i].BackendRefs))
		for j := range src.Spec.Rules[i].BackendRefs {
			b := src.Spec.Rules[i].BackendRefs[j].BackendObjectReference
			dst.Spec.Rules[i].BackendRefs[j].BackendObjectReference = gwapiv1.BackendObjectReference{
				Group:     b.Group,
				Kind:      b.Kind,
				Name:      b.Name,
				Namespace: b.Namespace,
				// ignore port!
			}
		}
	}
}

func ConvertV1TCPRouteToStnrV1List(src *gwapiv1.TCPRouteList) *TCPRouteList {
	if src == nil {
		return nil
	}
	dst := new(TCPRouteList)
	ConvertV1TCPRouteToStnrV1ListInto(src, dst)
	return dst
}

func ConvertV1TCPRouteToStnrV1ListInto(src *gwapiv1.TCPRouteList, dst *TCPRouteList) {
	dst.TypeMeta = src.TypeMeta
	src.ListMeta.DeepCopyInto(&dst.ListMeta)
	dst.Items = make([]TCPRoute, len(src.Items))
	for i := range src.Items {
		ConvertV1TCPRouteToStnrV1Into(&src.Items[i], &dst.Items[i])
	}
}
