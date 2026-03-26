package astore

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"fmt"
	"testing"
	"unsafe"

	"cloud.google.com/go/datastore"
	"github.com/golang-jwt/jwt/v5"
	"github.com/golang/protobuf/ptypes/wrappers"
	"github.com/stretchr/testify/require"
	dpb "google.golang.org/genproto/googleapis/datastore/v1"

	"github.com/ccontavalli/enkit/lib/logger"
)

// testDatastore is a mock Datastore object that captures queries made.
type testDatastore struct {
	// Dummy return values for Get() operations so that code under test doesn't
	// panic
	getAllKey      *datastore.Key
	getAllArtifact *Artifact

	// Queries made against this object
	queries []*datastore.Query
}

// queryMirror mirrors the datastore.Query layout closely enough for tests to
// reconstruct the generated query without patching the upstream module.
type queryMirror struct {
	kind       string
	ancestor   *datastore.Key
	filter     []datastore.EntityFilter
	order      []queryOrderMirror
	projection []string

	distinct   bool
	distinctOn []string
	keysOnly   bool
	eventual   bool
	limit      int32
	offset     int32
	start      []byte
	end        []byte

	namespace string

	trans *datastore.Transaction

	err error
}

type queryOrderMirror struct {
	FieldName string
	Direction bool
}

func (d *testDatastore) Delete(context.Context, *datastore.Key) error {
	return fmt.Errorf("Delete() unimplemented")
}

func (d *testDatastore) Get(context.Context, *datastore.Key, interface{}) error {
	return fmt.Errorf("Get() unimplemented")
}

func (d *testDatastore) GetAll(ctx context.Context, q *datastore.Query, dst interface{}) ([]*datastore.Key, error) {
	d.queries = append(d.queries, q)

	artifacts := dst.(*[]*Artifact)
	*artifacts = append(*artifacts, d.getAllArtifact)
	return []*datastore.Key{d.getAllKey}, nil
}

func (d *testDatastore) Mutate(context.Context, ...*datastore.Mutation) ([]*datastore.Key, error) {
	return nil, fmt.Errorf("Mutate() unimplemented")
}

func (d *testDatastore) NewTransaction(context.Context, ...datastore.TransactionOption) (*datastore.Transaction, error) {
	return nil, fmt.Errorf("NewTransaction unimplemented")
}

func (d *testDatastore) Run(context.Context, *datastore.Query) *datastore.Iterator {
	return nil
}

// RecordedQueries returns a list of all the queries ran against the mock.
func (d *testDatastore) RecordedQueries(t *testing.T) []*dpb.RunQueryRequest {
	t.Helper()

	var ret []*dpb.RunQueryRequest
	for _, q := range d.queries {
		req, err := queryToRunQueryRequest(q)
		require.NoError(t, err)
		ret = append(ret, req)
	}
	return ret
}

func queryToRunQueryRequest(q *datastore.Query) (*dpb.RunQueryRequest, error) {
	query, err := queryToProto(q)
	if err != nil {
		return nil, err
	}

	return &dpb.RunQueryRequest{
		QueryType: &dpb.RunQueryRequest_Query{
			Query: query,
		},
	}, nil
}

func queryToProto(q *datastore.Query) (*dpb.Query, error) {
	mirror := (*queryMirror)(unsafe.Pointer(q))

	if len(mirror.projection) != 0 && mirror.keysOnly {
		return nil, fmt.Errorf("datastore: query cannot both project and be keys-only")
	}
	if len(mirror.distinctOn) != 0 && mirror.distinct {
		return nil, fmt.Errorf("datastore: query cannot be both distinct and distinct-on")
	}

	dst := &dpb.Query{}
	if mirror.kind != "" {
		dst.Kind = []*dpb.KindExpression{{Name: mirror.kind}}
	}

	for _, propertyName := range mirror.projection {
		dst.Projection = append(dst.Projection, &dpb.Projection{
			Property: &dpb.PropertyReference{Name: propertyName},
		})
	}
	for _, propertyName := range mirror.distinctOn {
		dst.DistinctOn = append(dst.DistinctOn, &dpb.PropertyReference{Name: propertyName})
	}
	if mirror.distinct {
		for _, propertyName := range mirror.projection {
			dst.DistinctOn = append(dst.DistinctOn, &dpb.PropertyReference{Name: propertyName})
		}
	}
	if mirror.keysOnly {
		dst.Projection = []*dpb.Projection{{
			Property: &dpb.PropertyReference{Name: "__key__"},
		}}
	}

	var filters []*dpb.Filter
	for _, filter := range mirror.filter {
		pbFilter, err := entityFilterToProto(filter)
		if err != nil {
			return nil, err
		}
		filters = append(filters, pbFilter)
	}
	if mirror.ancestor != nil {
		filters = append(filters, &dpb.Filter{
			FilterType: &dpb.Filter_PropertyFilter{
				PropertyFilter: &dpb.PropertyFilter{
					Property: &dpb.PropertyReference{Name: "__key__"},
					Op:       dpb.PropertyFilter_HAS_ANCESTOR,
					Value: &dpb.Value{
						ValueType: &dpb.Value_KeyValue{
							KeyValue: datastoreKeyToProto(mirror.ancestor),
						},
					},
				},
			},
		})
	}

	if len(filters) == 1 {
		dst.Filter = filters[0]
	} else if len(filters) > 1 {
		dst.Filter = &dpb.Filter{
			FilterType: &dpb.Filter_CompositeFilter{
				CompositeFilter: &dpb.CompositeFilter{
					Op:      dpb.CompositeFilter_AND,
					Filters: filters,
				},
			},
		}
	}

	for _, order := range mirror.order {
		direction := dpb.PropertyOrder_ASCENDING
		if order.Direction {
			direction = dpb.PropertyOrder_DESCENDING
		}
		dst.Order = append(dst.Order, &dpb.PropertyOrder{
			Property:  &dpb.PropertyReference{Name: order.FieldName},
			Direction: direction,
		})
	}

	if mirror.limit >= 0 {
		dst.Limit = int32Val(mirror.limit)
	}
	dst.Offset = mirror.offset
	dst.StartCursor = mirror.start
	dst.EndCursor = mirror.end
	return dst, nil
}

func entityFilterToProto(filter datastore.EntityFilter) (*dpb.Filter, error) {
	switch current := filter.(type) {
	case datastore.PropertyFilter:
		return propertyFilterToProto(current)
	case datastore.AndFilter:
		var filters []*dpb.Filter
		for _, nested := range current.Filters {
			pbFilter, err := entityFilterToProto(nested)
			if err != nil {
				return nil, err
			}
			filters = append(filters, pbFilter)
		}
		return &dpb.Filter{
			FilterType: &dpb.Filter_CompositeFilter{
				CompositeFilter: &dpb.CompositeFilter{
					Op:      dpb.CompositeFilter_AND,
					Filters: filters,
				},
			},
		}, nil
	case datastore.OrFilter:
		var filters []*dpb.Filter
		for _, nested := range current.Filters {
			pbFilter, err := entityFilterToProto(nested)
			if err != nil {
				return nil, err
			}
			filters = append(filters, pbFilter)
		}
		return &dpb.Filter{
			FilterType: &dpb.Filter_CompositeFilter{
				CompositeFilter: &dpb.CompositeFilter{
					Op:      dpb.CompositeFilter_OR,
					Filters: filters,
				},
			},
		}, nil
	default:
		return nil, fmt.Errorf("unsupported datastore filter type %T", filter)
	}
}

func propertyFilterToProto(filter datastore.PropertyFilter) (*dpb.Filter, error) {
	op, ok := map[string]dpb.PropertyFilter_Operator{
		"=":      dpb.PropertyFilter_EQUAL,
		"!=":     dpb.PropertyFilter_NOT_EQUAL,
		">":      dpb.PropertyFilter_GREATER_THAN,
		">=":     dpb.PropertyFilter_GREATER_THAN_OR_EQUAL,
		"<":      dpb.PropertyFilter_LESS_THAN,
		"<=":     dpb.PropertyFilter_LESS_THAN_OR_EQUAL,
		"in":     dpb.PropertyFilter_IN,
		"not-in": dpb.PropertyFilter_NOT_IN,
	}[filter.Operator]
	if !ok {
		return nil, fmt.Errorf("unsupported datastore filter operator %q", filter.Operator)
	}

	value, err := propertyValueToProto(filter.Value)
	if err != nil {
		return nil, err
	}

	return &dpb.Filter{
		FilterType: &dpb.Filter_PropertyFilter{
			PropertyFilter: &dpb.PropertyFilter{
				Property: &dpb.PropertyReference{Name: filter.FieldName},
				Op:       op,
				Value:    value,
			},
		},
	}, nil
}

func propertyValueToProto(value interface{}) (*dpb.Value, error) {
	switch current := value.(type) {
	case string:
		return &dpb.Value{
			ValueType: &dpb.Value_StringValue{
				StringValue: current,
			},
		}, nil
	case *datastore.Key:
		return &dpb.Value{
			ValueType: &dpb.Value_KeyValue{
				KeyValue: datastoreKeyToProto(current),
			},
		}, nil
	default:
		return nil, fmt.Errorf("unsupported datastore filter value type %T", value)
	}
}

func datastoreKeyToProto(key *datastore.Key) *dpb.Key {
	if key == nil {
		return nil
	}

	var path []*dpb.Key_PathElement
	for current := key; current != nil; current = current.Parent {
		element := &dpb.Key_PathElement{Kind: current.Kind}
		if current.Name != "" {
			element.IdType = &dpb.Key_PathElement_Name{Name: current.Name}
		} else if current.ID != 0 {
			element.IdType = &dpb.Key_PathElement_Id{Id: current.ID}
		}
		path = append([]*dpb.Key_PathElement{element}, path...)
	}

	return &dpb.Key{Path: path}
}

// serverForTest creates a test RPC handler and returns the handles to the
// underlying mock objects used.
func serverForTest() (*Server, *testDatastore) {
	ds := &testDatastore{
		getAllArtifact: &Artifact{
			Sid: "test_sid",
		},
		getAllKey: &datastore.Key{
			Kind: KindPathElement,
			Name: "baz",
			Parent: &datastore.Key{
				Kind: KindPathElement,
				Name: "bar",
				Parent: &datastore.Key{
					Kind: KindPathElement,
					Name: "foo",
				},
			},
		},
	}
	return &Server{
		ctx:     context.Background(),
		rng:     nil,
		gcs:     nil,
		bkt:     nil,
		ds:      ds,
		options: Options{logger: logger.Nil},
	}, ds
}

// Proto construction helper methods
// Datastore proto types have lots of oneofs, so literals are very verbose.
// These helper functions bind some parameters to shorten the characters needed
// in tests to make tests more readable.

func propertyEqualsString(field string, value string) *dpb.Filter {
	return &dpb.Filter{
		FilterType: &dpb.Filter_PropertyFilter{
			PropertyFilter: &dpb.PropertyFilter{
				Property: &dpb.PropertyReference{
					Name: field,
				},
				Op: dpb.PropertyFilter_EQUAL,
				Value: &dpb.Value{
					ValueType: &dpb.Value_StringValue{
						StringValue: value,
					},
				},
			},
		},
	}
}

func propertyHasAncestorPel(field string, arch string, pel ...string) *dpb.Filter {
	var elems []*dpb.Key_PathElement
	for _, p := range pel {
		path := &dpb.Key_PathElement{
			Kind: "Pel",
			IdType: &dpb.Key_PathElement_Name{
				Name: p,
			},
		}
		elems = append(elems, path)
	}
	if arch != "" {
		elems = append(elems, &dpb.Key_PathElement{
			Kind: "Arch",
			IdType: &dpb.Key_PathElement_Name{
				Name: arch,
			},
		})
	}
	return &dpb.Filter{
		FilterType: &dpb.Filter_PropertyFilter{
			PropertyFilter: &dpb.PropertyFilter{
				Property: &dpb.PropertyReference{
					Name: field,
				},
				Op: dpb.PropertyFilter_HAS_ANCESTOR,
				Value: &dpb.Value{
					ValueType: &dpb.Value_KeyValue{
						KeyValue: &dpb.Key{
							Path: elems,
						},
					},
				},
			},
		},
	}
}

func compositeAnd(fs ...*dpb.Filter) *dpb.Filter {
	return &dpb.Filter{
		FilterType: &dpb.Filter_CompositeFilter{
			CompositeFilter: &dpb.CompositeFilter{
				Op:      dpb.CompositeFilter_AND,
				Filters: fs,
			},
		},
	}
}

func runQueryRequest(q *dpb.Query) *dpb.RunQueryRequest {
	return &dpb.RunQueryRequest{
		QueryType: &dpb.RunQueryRequest_Query{
			Query: q,
		},
	}
}

func descendingBy(p string) *dpb.PropertyOrder {
	return &dpb.PropertyOrder{
		Property: &dpb.PropertyReference{
			Name: p,
		},
		Direction: dpb.PropertyOrder_DESCENDING,
	}
}

func int32Val(i int32) *wrappers.Int32Value {
	return &wrappers.Int32Value{Value: i}
}

func kindArtifact() []*dpb.KindExpression {
	return []*dpb.KindExpression{
		{
			Name: "Artifact",
		},
	}
}

func generateTokenKeypair(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	return k
}

func createToken(t *testing.T, key *rsa.PrivateKey, claims jwt.Claims) string {
	t.Helper()
	signed, err := jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(key)
	require.NoError(t, err)
	return signed
}

const (
	NoArch = ""
)
