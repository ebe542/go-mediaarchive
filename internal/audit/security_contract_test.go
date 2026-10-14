package audit_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/audit"
)

func TestEventSchemaContainsOnlyAllowlistedFields(t *testing.T) {
	expected := []struct {
		name   string
		typeOf reflect.Type
	}{
		{"ID", reflect.TypeFor[string]()},
		{"OccurredAt", reflect.TypeFor[time.Time]()},
		{"Type", reflect.TypeFor[audit.Type]()},
		{"Outcome", reflect.TypeFor[audit.Outcome]()},
		{"ActorID", reflect.TypeFor[string]()},
		{"ActorUsername", reflect.TypeFor[string]()},
		{"ActorRole", reflect.TypeFor[string]()},
		{"TargetType", reflect.TypeFor[audit.TargetType]()},
		{"TargetID", reflect.TypeFor[string]()},
		{"TargetName", reflect.TypeFor[string]()},
		{"Reason", reflect.TypeFor[audit.Reason]()},
		{"Range", reflect.TypeFor[*audit.ByteRange]()},
	}

	eventType := reflect.TypeFor[audit.Event]()
	if eventType.NumField() != len(expected) {
		t.Fatalf(
			"expected %d allowlisted audit fields, got %d",
			len(expected),
			eventType.NumField(),
		)
	}
	for index, expectedField := range expected {
		actual := eventType.Field(index)
		if actual.Name != expectedField.name || actual.Type != expectedField.typeOf {
			t.Fatalf(
				"unexpected audit field %d: %s %s",
				index,
				actual.Name,
				actual.Type,
			)
		}
	}
}

func TestByteRangeSchemaContainsOnlyNumericBounds(t *testing.T) {
	expected := []string{"Start", "End", "Total"}
	rangeType := reflect.TypeFor[audit.ByteRange]()
	if rangeType.NumField() != len(expected) {
		t.Fatalf(
			"expected %d byte-range fields, got %d",
			len(expected),
			rangeType.NumField(),
		)
	}
	for index, expectedName := range expected {
		actual := rangeType.Field(index)
		if actual.Name != expectedName || actual.Type.Kind() != reflect.Int64 {
			t.Fatalf(
				"unexpected byte-range field %d: %s %s",
				index,
				actual.Name,
				actual.Type,
			)
		}
	}
}
