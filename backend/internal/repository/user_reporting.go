package repository

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// UsernamesByIDs only hydrates identifiers already selected by a reporting
// page. Small batches bound the query without loading full user records.
func (r *userRepository) UsernamesByIDs(ids []int) (map[int]string, error) {
	names := make(map[int]string, len(ids))
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for start := 0; start < len(ids); start += 500 {
		end := min(start+500, len(ids))
		args := make([]interface{}, end-start)
		for i, id := range ids[start:end] {
			args[i] = id
		}
		query := "SELECT id,username FROM users WHERE id IN (" + strings.TrimSuffix(strings.Repeat("?,", len(args)), ",") + ")"
		rows, err := r.sess.SQL().QueryContext(ctx, query, args...)
		if err != nil {
			return nil, fmt.Errorf("load reporting usernames: %w", err)
		}
		for rows.Next() {
			var id int
			var name string
			if err := rows.Scan(&id, &name); err != nil {
				rows.Close()
				return nil, err
			}
			names[id] = name
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return names, nil
}
