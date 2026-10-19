package northbound

import (
	"encoding/json"
	"strings"
	"testing"

	"clawreef/internal/models"
)

func TestCreateAliasIsBackwardCompatibleAndNormalized(t *testing.T) {
	var omitted CreateLiteInstanceRequest
	if err := json.Unmarshal([]byte(`{"name":"instance-one","owner":"owner@example.com","type":"openclaw"}`), &omitted); err != nil {
		t.Fatal(err)
	}
	if omitted.Alias != nil {
		t.Fatalf("omitted alias = %v, want nil", omitted.Alias)
	}

	blank := "  \t "
	normalized, err := normalizeInstanceAlias(&blank)
	if err != nil || normalized != nil {
		t.Fatalf("blank alias = %v, %v; want nil, nil", normalized, err)
	}

	chinese := "  财务分析助手  "
	normalized, err = normalizeInstanceAlias(&chinese)
	if err != nil || normalized == nil || *normalized != "财务分析助手" {
		t.Fatalf("normalized alias = %v, %v", normalized, err)
	}
}

func TestCreateAliasRejectsOversizeAndControlCharacters(t *testing.T) {
	tooLong := strings.Repeat("助", 51)
	if _, err := normalizeInstanceAlias(&tooLong); err == nil {
		t.Fatal("expected alias longer than 50 characters to fail")
	}
	control := "财务\n助手"
	if _, err := normalizeInstanceAlias(&control); err == nil {
		t.Fatal("expected alias containing a newline to fail")
	}
}

func TestAliasPropagatesToCreateAndResponse(t *testing.T) {
	alias := "财务分析助手"
	request := liteCreateRequest(
		&models.NorthboundOperation{OperationID: "op_alias"},
		CreateLiteInstanceRequest{Name: "instance-one", Alias: &alias, Owner: "owner@example.com", Type: "openclaw"},
	)
	if request.Alias == nil || *request.Alias != alias {
		t.Fatalf("create alias = %v", request.Alias)
	}
	response := liteInstanceResponse(&models.Instance{Name: "instance-one", Alias: &alias})
	if response.Alias == nil || *response.Alias != alias {
		t.Fatalf("response alias = %v", response.Alias)
	}
}
