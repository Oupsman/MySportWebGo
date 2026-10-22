package models

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestPermissionsJSON(t *testing.T) {
	var p Permission
	if err := json.Unmarshal([]byte(`["activities:read","healthdatas:delete","equipments:create"]`), &p); err != nil {
		t.Fatal(err)
	}
	if p != ActivitiesRead|HealthDatasDelete|EquipmentsCreate {
		t.Fatalf("parsed %b", p)
	}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	// in bit order, whatever the order of the request
	if string(data) != `["activities:read","equipments:create","healthdatas:delete"]` {
		t.Fatalf("marshalled %s", data)
	}
	if err := json.Unmarshal([]byte(`["activities:read","activities:admin"]`), &p); err == nil {
		t.Fatal("unknown permission accepted")
	}
	if names := Permission(0).Names(); !reflect.DeepEqual(names, []string{}) {
		t.Fatalf("no permission gives %v, want an empty list", names)
	}
	if len(permissionNames) != 12 || HealthDatasDelete != 1<<11 {
		t.Fatal("permissionNames and the constants are out of sync")
	}
}

func TestPermissionHas(t *testing.T) {
	p := ActivitiesRead | ActivitiesCreate
	if !p.Has(ActivitiesRead) || !p.Has(ActivitiesRead|ActivitiesCreate) {
		t.Fatal("held permissions refused")
	}
	if p.Has(ActivitiesRead | ActivitiesDelete) {
		t.Fatal("a missing permission accepted")
	}
}
