package repository

import (
	"reflect"
	"testing"

	"clawreef/internal/models"
)

func TestInstanceListAutoSleepFilterPreservesScopeCountsAndPagination(t *testing.T) {
	r := runtimeSecurityMySQL(t)
	for _, query := range []string{
		`UPDATE instances SET status='stopped' WHERE id=7`,
		`INSERT INTO instances(id,user_id,name,status,runtime_generation) VALUES(8,3,'manual','stopped',1),(9,3,'legacy','stopped',1),(10,3,'running','running',1),(11,4,'other-owner','stopped',1)`,
		`INSERT INTO instance_model_activity(instance_id,generation,monitoring_since,stopped_reason) VALUES(7,2,NOW(),'idle_timeout'),(8,1,NOW(),'manual'),(10,1,NOW(),'idle_timeout'),(11,1,NOW(),'idle_timeout')`,
	} {
		if _, err := r.sess.SQL().Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	yes, no := true, false
	for _, tc := range []struct {
		name string
		auto *bool
		want []int
	}{
		{"all-stopped-compatibility", nil, []int{7, 8, 9}},
		{"auto-sleep", &yes, []int{7}},
		{"ordinary-stopped-includes-legacy", &no, []int{8, 9}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			filter := models.InstanceListFilter{Status: "stopped", AutoSleep: tc.auto}
			var items []models.Instance
			if err := r.filteredByUserID(3, filter).OrderBy("id").All(&items); err != nil {
				t.Fatal(err)
			}
			ids := make([]int, 0, len(items))
			for _, item := range items {
				ids = append(ids, item.ID)
			}
			if !reflect.DeepEqual(ids, tc.want) {
				t.Fatalf("IDs=%v, want %v", ids, tc.want)
			}
			count, err := r.CountFilteredByUserID(3, filter)
			if err != nil || count != len(tc.want) {
				t.Fatalf("count=%d err=%v", count, err)
			}
			if len(tc.want) > 1 {
				if err := r.filteredByUserID(3, filter).OrderBy("id").Offset(1).Limit(1).All(&items); err != nil || len(items) != 1 || items[0].ID != tc.want[1] {
					t.Fatalf("second page=%v err=%v", items, err)
				}
			}
		})
	}
}
