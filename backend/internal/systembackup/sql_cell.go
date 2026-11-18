package systembackup

import (
	"clawreef/internal/systembackup/team"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// A nil []byte argument is SQL NULL in the MySQL driver. JSON legitimately
// omits an empty byte slice; the explicit Cell.IsNull flag is authoritative.
func nonNullSQLBytes(b []byte) []byte {
	if b == nil {
		return []byte{}
	}
	return b
}

// Scan into any instead of []byte: parseTime connections otherwise silently
// serialize DATETIME values as RFC3339, which is not the captured SQL value.
func sqlCell(v any, column SQLColumn) (team.Cell, error) {
	if v == nil {
		return team.Null(), nil
	}
	switch x := v.(type) {
	case []byte:
		return team.Cell{Data: append([]byte{}, x...)}, nil
	case string:
		return team.Value(x), nil
	case int64:
		return team.Value(strconv.FormatInt(x, 10)), nil
	case uint64:
		return team.Value(strconv.FormatUint(x, 10)), nil
	case float64:
		return team.Value(strconv.FormatFloat(x, 'g', -1, 64)), nil
	case bool:
		if x {
			return team.Value("1"), nil
		}
		return team.Value("0"), nil
	case time.Time:
		kind := strings.ToLower(column.Type)
		layout := "2006-01-02 15:04:05"
		if kind == "date" {
			layout = "2006-01-02"
		} else if strings.Contains(kind, "(") {
			p := strings.Index(kind, "(")
			digits, e := strconv.Atoi(strings.TrimSuffix(kind[p+1:], ")"))
			if e != nil || digits < 0 || digits > 6 {
				return team.Cell{}, fmt.Errorf("unsupported timestamp precision")
			}
			if digits > 0 {
				layout += "." + strings.Repeat("0", digits)
			}
		}
		if x.IsZero() {
			s := "0000-00-00"
			if kind != "date" {
				s += " 00:00:00"
				if p := strings.Index(layout, "."); p >= 0 {
					s += layout[p:]
				}
			}
			return team.Value(s), nil
		}
		return team.Value(x.Format(layout)), nil
	default:
		return team.Cell{}, fmt.Errorf("unsupported SQL cell type")
	}
}
