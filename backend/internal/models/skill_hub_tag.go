package models

import "time"

type SkillHubTag struct {
	ID          int       `db:"id,primarykey,autoincrement" json:"id"`
	TagKey      string    `db:"tag_key" json:"tag_key"`
	Name        string    `db:"name" json:"name"`
	Description *string   `db:"description" json:"description,omitempty"`
	SortOrder   int       `db:"sort_order" json:"sort_order"`
	AdminOnly   bool      `db:"admin_only" json:"admin_only"`
	CreatedAt   time.Time `db:"created_at" json:"created_at"`
	UpdatedAt   time.Time `db:"updated_at" json:"updated_at"`
}

func (t SkillHubTag) TableName() string { return "skill_hub_tags" }

type SkillHubTagAssignment struct {
	ID        int       `db:"id,primarykey,autoincrement" json:"id"`
	SkillID   int       `db:"skill_id" json:"skill_id"`
	TagID     int       `db:"tag_id" json:"tag_id"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
}

func (a SkillHubTagAssignment) TableName() string { return "skill_hub_tag_assignments" }
