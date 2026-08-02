package store

import (
	"fmt"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TypedStore is a Store that stores objects of a single client.Object type and provides
// type-safe accessors.
type TypedStore[T client.Object] struct {
	Store
}

// NewTypedStore creates a new typed object storage.
func NewTypedStore[T client.Object]() *TypedStore[T] {
	return &TypedStore[T]{
		Store: NewStore(),
	}
}

// GetAll returns all objects from the storage.
func (s *TypedStore[T]) GetAll() []T {
	ret := make([]T, 0)

	objects := s.Objects()
	for i := range objects {
		r, ok := objects[i].(T)
		if !ok {
			// this is critical: throw up hands and die
			panic(fmt.Sprintf("access to an invalid object of type %T in a typed store",
				objects[i]))
		}

		ret = append(ret, r)
	}

	return ret
}

// GetObject returns a named object from the storage or the zero value of the type if the object
// was not found.
func (s *TypedStore[T]) GetObject(nsName types.NamespacedName) T {
	var zero T

	o := s.Get(nsName)
	if o == nil {
		return zero
	}

	r, ok := o.(T)
	if !ok {
		// this is critical: throw up hands and die
		panic(fmt.Sprintf("access to an invalid object of type %T in a typed store", o))
	}

	return r
}

// DeepCopy copies the storage.
func (s *TypedStore[T]) DeepCopy() *TypedStore[T] {
	ret := NewTypedStore[T]()
	for _, o := range s.GetAll() {
		ret.Upsert(o.DeepCopyObject().(client.Object))
	}
	return ret
}
