package observability

import (
	"encoding/json"
	"errors"
)

type BodyField struct{ Table, UID, Field string }

func CanonicalBodyField(column, uid string) BodyField {
	switch column {
	case "request_body_ref":
		return BodyField{"model_invocations", uid, "RequestPayload"}
	case "response_body_ref":
		return BodyField{"model_invocations", uid, "ResponsePayload"}
	case "details_body_ref":
		return BodyField{"audit_events", uid, "Details"}
	case "content_body_ref":
		return BodyField{"chat_messages", uid, "Content"}
	}
	return BodyField{}
}

type BodyDependency struct {
	Ref BodyRef
	// Only a top-level canonical SQL field can carry an event release identity.
	Field BodyField
}

// ArchiveDependencies also handles JSON columns exported by MySQL as strings.
// Ordinary strings, including user text containing JSON, remain opaque.
func ArchiveDependencies(raw []byte) ([]BodyDependency, error) {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return nil, nil
	}
	var found []BodyDependency
	var visit func(any, int, BodyField) error
	visit = func(v any, depth int, field BodyField) error {
		if depth > 64 {
			return errors.New("history JSON dependency depth exceeded")
		}
		switch x := v.(type) {
		case map[string]any:
			if _, hasKey := x["object_key"]; hasKey && x["encryption_version"] != nil {
				b, _ := json.Marshal(x)
				var ref BodyRef
				if json.Unmarshal(b, &ref) != nil || ref.Owner == "" || ref.ObjectKey == "" || ref.ByteLength < 0 || ref.EncryptionVersion != 1 {
					return errors.New("invalid encrypted history dependency")
				}
				found = append(found, BodyDependency{ref, field})
				return nil
			}
			uid, _ := x["event_uid"].(string)
			for name, child := range x {
				f := CanonicalBodyField(name, uid)
				if f.Table != "" && child != nil {
					if encoded, ok := child.(string); ok {
						if json.Unmarshal([]byte(encoded), &child) != nil {
							if uid != "" {
								return errors.New("invalid archived body field")
							}
							continue
						}
					}
				}
				if depth != 0 {
					f = BodyField{}
				}
				if err := visit(child, depth+1, f); err != nil {
					return err
				}
			}
		case []any:
			for _, child := range x {
				if err := visit(child, depth+1, BodyField{}); err != nil {
					return err
				}
			}
		}
		return nil
	}
	err := visit(value, 0, BodyField{})
	return found, err
}
