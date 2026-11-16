package ownerrecords

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/upper/db/v4"
)

type CalendarSource struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	URL         string `json:"url"`
	PublishedAt string `json:"published_at"`
}
type CalendarDay struct {
	Date     string `json:"date"`
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	SourceID string `json:"source_id"`
}
type CalendarDocument struct {
	SchemaVersion int              `json:"schema_version"`
	Region        string           `json:"region"`
	Timezone      string           `json:"timezone"`
	Year          int              `json:"year"`
	Coverage      string           `json:"coverage"`
	Sources       []CalendarSource `json:"sources"`
	Days          []CalendarDay    `json:"days"`
}
type CalendarVersion struct {
	Document CalendarDocument `json:"document"`
	Version  uint64           `json:"version"`
	Actor    int              `json:"actor"`
	At       time.Time        `json:"at"`
	Builtin  bool             `json:"builtin,omitempty"`
}

var ErrCalendarConflict = errors.New("日历已被其他管理员更新，请重新预览")

// Uploaded JSON is data, not instructions. No source URL is fetched by the server.
func ParseCalendar(raw []byte) (CalendarDocument, error) {
	var d CalendarDocument
	if len(raw) > 256<<10 {
		return d, fmt.Errorf("日历文件不能超过 256 KiB")
	}
	if e := uniqueJSONKeys(json.NewDecoder(bytes.NewReader(raw)), 0); e != nil {
		return d, e
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if e := dec.Decode(&d); e != nil {
		return d, fmt.Errorf("日历 JSON 格式不正确: %w", e)
	}
	if e := dec.Decode(new(any)); e != io.EOF {
		return d, fmt.Errorf("只能上传一个 JSON 对象")
	}
	if d.SchemaVersion != 1 || d.Region != "CN" || d.Timezone != "Asia/Shanghai" || d.Year < 2000 || d.Year > 2100 {
		return d, fmt.Errorf("仅支持 schema_version=1、CN、Asia/Shanghai 和 2000—2100 年")
	}
	if d.Coverage != "official_full_year" && d.Coverage != "partial" {
		return d, fmt.Errorf("coverage 必须为 official_full_year 或 partial")
	}
	if len(d.Sources) == 0 || len(d.Sources) > 32 || len(d.Days) == 0 || len(d.Days) > 366 {
		return d, fmt.Errorf("来源或日期条目数量不正确")
	}
	sources := map[string]bool{}
	for _, s := range d.Sources {
		u, e := url.Parse(s.URL)
		_, de := time.Parse("2006-01-02", s.PublishedAt)
		if e != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || !(u.Hostname() == "gov.cn" || strings.HasSuffix(u.Hostname(), ".gov.cn")) || de != nil || s.ID == "" || len(s.ID) > 80 || sources[s.ID] || strings.TrimSpace(s.Title) == "" || len(s.Title) > 500 {
			return d, fmt.Errorf("来源必须有唯一 ID、正式标题、发布日期及 HTTPS 政府网站地址")
		}
		sources[s.ID] = true
	}
	dates := map[string]bool{}
	for _, day := range d.Days {
		t, e := time.Parse("2006-01-02", day.Date)
		if e != nil || t.Year() != d.Year || dates[day.Date] || (day.Kind != "holiday" && day.Kind != "workday") || !sources[day.SourceID] || strings.TrimSpace(day.Name) == "" || len(day.Name) > 160 {
			return d, fmt.Errorf("日期必须属于目标年、不得重复，kind/名称/source_id 必须有效")
		}
		dates[day.Date] = true
	}
	sort.Slice(d.Days, func(i, j int) bool { return d.Days[i].Date < d.Days[j].Date })
	return d, nil
}

func uniqueJSONKeys(dec *json.Decoder, depth int) error {
	if depth > 16 {
		return fmt.Errorf("日历 JSON 嵌套过深")
	}
	token, e := dec.Token()
	if e != nil {
		return e
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	if delim == '{' {
		seen := map[string]bool{}
		for dec.More() {
			key, e := dec.Token()
			if e != nil {
				return e
			}
			name, ok := key.(string)
			if !ok || seen[name] {
				return fmt.Errorf("JSON 字段重复或无效")
			}
			seen[name] = true
			if e = uniqueJSONKeys(dec, depth+1); e != nil {
				return e
			}
		}
	} else if delim == '[' {
		for dec.More() {
			if e = uniqueJSONKeys(dec, depth+1); e != nil {
				return e
			}
		}
	} else {
		return fmt.Errorf("JSON 结构无效")
	}
	_, e = dec.Token()
	return e
}

func calendarKey(year int) string { return Key("calendar-CN", strconv.Itoa(year)) }
func (s *Store) CalendarVersions(ctx context.Context, year int) ([]CalendarVersion, error) {
	rows, e := s.Session.SQL().QueryContext(ctx, `SELECT payload FROM northbound_owner_records WHERE record_type='calendar_version' AND period_type=? ORDER BY version DESC LIMIT 100`, strconv.Itoa(year))
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []CalendarVersion{}
	for rows.Next() {
		var raw []byte
		if e = rows.Scan(&raw); e != nil {
			return nil, e
		}
		var v CalendarVersion
		if e = json.Unmarshal(raw, &v); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) calendarHeads(ctx context.Context) (map[int]CalendarVersion, error) {
	rows, e := s.Session.SQL().QueryContext(ctx, `SELECT payload FROM northbound_owner_records WHERE record_type='calendar_head' AND status='published' LIMIT 102`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := builtinCalendars()
	for rows.Next() {
		var raw []byte
		if e = rows.Scan(&raw); e != nil {
			return nil, e
		}
		var v CalendarVersion
		if e = json.Unmarshal(raw, &v); e != nil {
			return nil, e
		}
		out[v.Document.Year] = v
	}
	return out, rows.Err()
}
func mergeCalendar(old CalendarDocument, next CalendarDocument) (CalendarDocument, error) {
	if next.Coverage != "partial" || old.Year == 0 {
		return next, nil
	}
	sources := map[string]CalendarSource{}
	for _, s := range old.Sources {
		sources[s.ID] = s
	}
	for _, s := range next.Sources {
		if prev, ok := sources[s.ID]; ok && prev != s {
			return next, fmt.Errorf("补充通知必须使用新的来源 ID")
		}
		sources[s.ID] = s
	}
	days := map[string]CalendarDay{}
	for _, d := range old.Days {
		days[d.Date] = d
	}
	for _, d := range next.Days {
		days[d.Date] = d
	}
	next.Sources = nil
	next.Days = nil
	for _, s := range sources {
		next.Sources = append(next.Sources, s)
	}
	for _, d := range days {
		next.Days = append(next.Days, d)
	}
	next.Coverage = old.Coverage
	sort.Slice(next.Sources, func(i, j int) bool { return next.Sources[i].ID < next.Sources[j].ID })
	sort.Slice(next.Days, func(i, j int) bool { return next.Days[i].Date < next.Days[j].Date })
	raw, _ := json.Marshal(next)
	return ParseCalendar(raw)
}

// Immutable revisions and a compare-and-swap head are committed together. Rollback
// republishes a previous revision; facts, XP and published statistics are untouched.
func (s *Store) PublishCalendar(ctx context.Context, d CalendarDocument, expected uint64, actor int, now time.Time, replace ...bool) (CalendarVersion, error) {
	var result CalendarVersion
	err := s.Session.TxContext(ctx, func(tx db.Session) error {
		_, e := tx.SQL().ExecContext(ctx, `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,status,version) VALUES('calendar_head',?,0,'empty',0) ON DUPLICATE KEY UPDATE record_key=record_key`, calendarKey(d.Year))
		if e != nil {
			return e
		}
		row, e := tx.SQL().QueryRowContext(ctx, `SELECT version,payload FROM northbound_owner_records WHERE record_type='calendar_head' AND record_key=? FOR UPDATE`, calendarKey(d.Year))
		if e != nil {
			return e
		}
		var version uint64
		var raw []byte
		if e = row.Scan(&version, &raw); e != nil {
			return e
		}
		if version != expected {
			return ErrCalendarConflict
		}
		old := builtinCalendar(d.Year)
		if len(raw) > 0 {
			if e = json.Unmarshal(raw, &old); e != nil {
				return e
			}
		}
		merged := d
		if len(replace) == 0 || !replace[0] {
			merged, e = mergeCalendar(old.Document, d)
			if e != nil {
				return e
			}
		}
		result = CalendarVersion{Document: merged, Version: version + 1, Actor: actor, At: now.UTC()}
		raw, e = json.Marshal(result)
		if e != nil {
			return e
		}
		_, e = tx.SQL().ExecContext(ctx, `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,status,period_type,version,occurred_at,payload) VALUES('calendar_version',?,0,'published',?,?,?,?)`, Key(calendarKey(d.Year), strconv.FormatUint(result.Version, 10)), strconv.Itoa(d.Year), result.Version, now.UTC(), string(raw))
		if e != nil {
			return e
		}
		_, e = tx.SQL().ExecContext(ctx, `UPDATE northbound_owner_records SET status='published',version=?,payload=?,occurred_at=? WHERE record_type='calendar_head' AND record_key=?`, result.Version, string(raw), now.UTC(), calendarKey(d.Year))
		return e
	}, nil)
	return result, err
}

type VisitDay struct {
	Date    string `json:"date"`
	Visited bool   `json:"visited"`
	Rest    bool   `json:"rest"`
	Name    string `json:"name"`
	Known   bool   `json:"known"`
}
type VisitCalendar struct {
	Today       string         `json:"today"`
	Streak      int            `json:"streak"`
	Longest     int            `json:"longest"`
	Total       int            `json:"total"`
	Provisional bool           `json:"provisional"`
	Versions    map[int]uint64 `json:"versions"`
	Days        []VisitDay     `json:"days"`
}

func calendarDay(date time.Time, heads map[int]CalendarVersion) VisitDay {
	key := date.Format("2006-01-02")
	v, ok := heads[date.Year()]
	d := VisitDay{Date: key, Rest: date.Weekday() == time.Saturday || date.Weekday() == time.Sunday, Known: ok && v.Document.Coverage == "official_full_year"}
	if d.Rest {
		d.Name = "周末"
	} else {
		d.Name = "工作日"
	}
	for _, x := range v.Document.Days {
		if x.Date == key {
			d.Rest = x.Kind == "holiday"
			d.Name = x.Name
			break
		}
	}
	return d
}
func calculateVisits(dates []string, heads map[int]CalendarVersion, now time.Time) VisitCalendar {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	today := now.In(loc).Format("2006-01-02")
	end, _ := time.ParseInLocation("2006-01-02", today, loc)
	out := VisitCalendar{Today: today, Versions: map[int]uint64{}, Days: []VisitDay{}}
	visits := map[string]bool{}
	first := today
	for _, d := range dates {
		if d <= today {
			visits[d] = true
			if d < first {
				first = d
			}
		}
	}
	out.Total = len(visits)
	start, _ := time.ParseInLocation("2006-01-02", first, loc)
	if start.IsZero() {
		start = end
	}
	run := 0
	for day := start; !day.After(end); day = day.AddDate(0, 0, 1) {
		d := calendarDay(day, heads)
		if !d.Known {
			out.Provisional = true
		}
		out.Versions[day.Year()] = heads[day.Year()].Version
		if visits[d.Date] {
			run++
		} else if !d.Rest && d.Date != today {
			run = 0
		}
		if run > out.Longest {
			out.Longest = run
		}
	}
	out.Streak = run
	for day := end.AddDate(0, 0, -89); !day.After(end); day = day.AddDate(0, 0, 1) {
		d := calendarDay(day, heads)
		d.Visited = visits[d.Date]
		out.Days = append(out.Days, d)
	}
	return out
}
func (s *Store) loginDates(ctx context.Context, owner string) ([]string, error) {
	rows, e := s.Session.SQL().QueryContext(ctx, `SELECT DISTINCT DATE_FORMAT(business_date,'%Y-%m-%d') AS visit_date FROM northbound_owner_records WHERE record_type='login_day' AND owner_normalized=? AND business_date IS NOT NULL ORDER BY visit_date LIMIT 10001`, owner)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	dates := []string{}
	for rows.Next() {
		var d string
		if e = rows.Scan(&d); e != nil {
			return nil, e
		}
		dates = append(dates, d)
	}
	if len(dates) > 10000 {
		return nil, fmt.Errorf("访问记录超出安全读取范围")
	}
	return dates, rows.Err()
}
func (s *Store) OwnerVisitCalendar(ctx context.Context, owner string, now time.Time) (*VisitCalendar, error) {
	var err error
	owner, err = normalizedOwner(owner)
	if err != nil {
		return nil, err
	}
	heads, e := s.calendarHeads(ctx)
	if e != nil {
		return nil, e
	}
	dates, e := s.loginDates(ctx, owner)
	if e != nil {
		return nil, e
	}
	v := calculateVisits(dates, heads, now)
	return &v, nil
}
func (s *Store) CalendarHead(ctx context.Context, year int) (CalendarVersion, error) {
	heads, e := s.calendarHeads(ctx)
	return heads[year], e
}
func (s *Store) CalendarRevision(ctx context.Context, year int, version uint64) (CalendarVersion, error) {
	var v CalendarVersion
	row, e := s.Session.SQL().QueryRowContext(ctx, `SELECT payload FROM northbound_owner_records WHERE record_type='calendar_version' AND record_key=?`, Key(calendarKey(year), strconv.FormatUint(version, 10)))
	if e != nil {
		return v, e
	}
	var raw []byte
	if e = row.Scan(&raw); e != nil {
		return v, e
	}
	e = json.Unmarshal(raw, &v)
	return v, e
}
