package api

import "testing"

func TestFirstJSONTextFindsNestedDoctorFields(t *testing.T) {
	data := map[string]any{
		"userInfo": map[string]any{
			"real_name": "张医生",
			"loginName": "doctor01",
		},
		"lastOrgRole": []any{
			map[string]any{
				"orgName":  "某社区卫生服务中心",
				"deptName": "全科",
				"roleName": "医生",
			},
		},
	}

	tests := map[string]string{
		"name":       firstJSONText(data, "realName"),
		"account":    firstJSONText(data, "loginName"),
		"hospital":   firstJSONText(data, "orgName"),
		"department": firstJSONText(data, "departmentName", "deptName"),
		"role":       firstJSONText(data, "roleName"),
	}
	want := map[string]string{
		"name":       "张医生",
		"account":    "doctor01",
		"hospital":   "某社区卫生服务中心",
		"department": "全科",
		"role":       "医生",
	}

	for field, got := range tests {
		if got != want[field] {
			t.Fatalf("%s = %q, want %q", field, got, want[field])
		}
	}
}
