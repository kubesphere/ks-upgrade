package helper

import (
	"context"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ObjectHelper interface {
	Object(string) MultiVersion
	Register(resource string, old *Reference, new *Reference)
}

type MultiVersion interface {
	V3() Reference
	V4() Reference
}

type Reference struct {
	version string
	obj     client.Object
	list    client.ObjectList
}

func NewReference(version string, obj client.Object, list client.ObjectList) *Reference {
	return &Reference{
		version: version,
		obj:     obj,
		list:    list,
	}
}

func (o Reference) Version() string {
	return o.version
}

func (o Reference) Type() client.Object {
	if o.obj == nil {
		return nil
	}
	return o.obj.DeepCopyObject().(client.Object)
}

func (o Reference) ListType() client.ObjectList {
	if o.list == nil {
		return nil
	}
	return o.list.DeepCopyObject().(client.ObjectList)
}

type objectHelper struct {
	objMap map[string]multiVersionObject
}

type multiVersionObject struct {
	v3 Reference
	v4 Reference
}

func (o multiVersionObject) V3() Reference {
	return o.v3
}

func (o multiVersionObject) V4() Reference {
	return o.v4
}

func (o objectHelper) Register(resource string, v3 *Reference, v4 *Reference) {
	versionObject := multiVersionObject{}
	if v3 != nil {
		versionObject.v3 = *v3
	}
	if v4 != nil {
		versionObject.v4 = *v4
	}
	o.objMap[resource] = versionObject
}

func (o objectHelper) Object(resource string) MultiVersion {
	return o.objMap[resource]
}

func NewObjectHelper(ctx context.Context, v3Client, v4Client client.Client, resources []string) (ObjectHelper, error) {
	oh := &objectHelper{
		objMap: map[string]multiVersionObject{},
	}
	for _, resource := range resources {
		crd := &apiextensionsv1.CustomResourceDefinition{}
		err := v3Client.Get(ctx, types.NamespacedName{Name: resource}, crd)
		if err != nil {
			if errors.IsNotFound(err) {
				continue
			}
			return nil, err
		}
		oldRef := Reference{}
		newRef := Reference{}
		if len(crd.Spec.Versions) == 1 {
			oldRef.version = crd.Spec.Versions[0].Name
		} else {
			for _, version := range crd.Spec.Versions {
				if version.Deprecated {
					oldRef.version = version.Name
				} else {
					newRef.version = version.Name
				}
			}
		}

		gk := schema.GroupKind{Group: crd.Spec.Group, Kind: crd.Spec.Names.Kind}
		listGk := schema.GroupKind{Group: crd.Spec.Group, Kind: crd.Spec.Names.ListKind}

		if oldRef.version != "" {
			object, err := v3Client.Scheme().New(gk.WithVersion(oldRef.Version()))
			if err != nil {
				return nil, err
			}
			oldRef.obj = object.(client.Object)

			list, err := v3Client.Scheme().New(listGk.WithVersion(oldRef.Version()))
			if err != nil {
				return nil, err
			}
			oldRef.list = list.(client.ObjectList)
		}

		if newRef.version != "" {
			newObject, err := v4Client.Scheme().New(gk.WithVersion(newRef.Version()))
			if err != nil {
				return nil, err
			}
			newRef.obj = newObject.(client.Object)

			newList, err := v4Client.Scheme().New(listGk.WithVersion(newRef.Version()))
			if err != nil {
				return nil, err
			}
			newRef.list = newList.(client.ObjectList)
		}

		oh.Register(resource, &oldRef, &newRef)
	}
	return oh, nil
}
